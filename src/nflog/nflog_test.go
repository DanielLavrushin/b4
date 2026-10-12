package nflog

import (
	"bytes"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/mdlayher/netlink"
	"github.com/mdlayher/netlink/nlenc"
	"github.com/mdlayher/netlink/nltest"
	"golang.org/x/sys/unix"
)

func nativeConfigHeader(length, seq uint32) []byte {
	b := make([]byte, 16)
	nlenc.PutUint32(b[0:4], length)
	nlenc.PutUint16(b[4:6], 0x0401)
	nlenc.PutUint16(b[6:8], uint16(netlink.Request|netlink.Acknowledge))
	nlenc.PutUint32(b[8:12], seq)
	return b
}

func nativeAttr(typ uint16, value ...byte) []byte {
	b := make([]byte, 4, 4+len(value)+3)
	nlenc.PutUint16(b[0:2], uint16(4+len(value)))
	nlenc.PutUint16(b[2:4], typ)
	b = append(b, value...)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

func joined(parts ...[]byte) []byte {
	return bytes.Join(parts, nil)
}

func TestConfigMessages(t *testing.T) {
	bind, err := bindMessage(537)
	if err != nil {
		t.Fatal(err)
	}
	mode, err := modeMessage(537)
	if err != nil {
		t.Fatal(err)
	}
	group := []byte{0x00, 0x00, 0x02, 0x19}
	for _, tc := range []struct {
		name string
		msg  netlink.Message
		seq  uint32
		want []byte
	}{
		{"bind", bind, 7, joined(nativeConfigHeader(28, 7), group, nativeAttr(1, 0x01))},
		{"copy mode, queue threshold and sequence numbers", mode, 8, joined(nativeConfigHeader(48, 8), group,
			nativeAttr(2, 0x00, 0x00, 0xff, 0xff, 0x02, 0x00),
			nativeAttr(5, 0x00, 0x00, 0x00, 0x01),
			nativeAttr(6, 0x00, 0x01))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.msg
			m.Header.Length = uint32(16 + len(m.Data))
			m.Header.Sequence = tc.seq
			got, err := m.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, tc.want) {
				t.Fatalf("the message for group 537 is\n% x\nwant\n% x", got, tc.want)
			}
		})
	}
}

func leftoverCopy() netlink.Message {
	return netlink.Message{
		Header: netlink.Header{Type: netlink.HeaderType(nfulnlMsgPacket)},
		Data:   joined([]byte{unix.AF_INET, unix.NFNETLINK_V0, 0x02, 0x19}, nativeAttr(nfulaPrefix, 'b', '4', 'd', 'n', 's', 0)),
	}
}

func fastBindRetries(t *testing.T) {
	t.Helper()
	prev := bindRetryDelay
	bindRetryDelay = time.Millisecond
	t.Cleanup(func() { bindRetryDelay = prev })
}

type handshakeLog struct {
	bind, mode []byte
	sent       []string
}

func newHandshakeLog(t *testing.T) *handshakeLog {
	t.Helper()
	bind, err := bindMessage(537)
	if err != nil {
		t.Fatal(err)
	}
	mode, err := modeMessage(537)
	if err != nil {
		t.Fatal(err)
	}
	return &handshakeLog{bind: bind.Data, mode: mode.Data}
}

func (h *handshakeLog) note(t *testing.T, req netlink.Message) string {
	t.Helper()
	if req.Header.Type != netlink.HeaderType(nfulnlMsgConfig) || req.Header.Flags != netlink.Request|netlink.Acknowledge {
		t.Fatalf("sent type %#x flags %s, want a config request that asks for an ack", uint16(req.Header.Type), req.Header.Flags)
	}
	kind := "other"
	switch {
	case bytes.Equal(req.Data, h.bind):
		kind = "bind"
	case bytes.Equal(req.Data, h.mode):
		kind = "mode"
	}
	h.sent = append(h.sent, kind)
	return kind
}

func TestHandshakeSkipsPacketMessages(t *testing.T) {
	t.Run("copies that arrive before the acks are skipped", func(t *testing.T) {
		h := newHandshakeLog(t)
		var waiting []netlink.Message
		conn := nltest.Dial(func(reqs []netlink.Message) ([]netlink.Message, error) {
			if len(reqs) == 0 {
				if waiting == nil {
					return nil, errors.New("Receive was called with no request waiting for its ack")
				}
				ack, err := nltest.Error(0, waiting)
				waiting = nil
				return ack, err
			}
			if h.note(t, reqs[0]) == "bind" {
				waiting = reqs
				return []netlink.Message{leftoverCopy()}, nil
			}
			ack, err := nltest.Error(0, reqs)
			return append([]netlink.Message{leftoverCopy(), leftoverCopy()}, ack...), err
		})
		defer conn.Close()

		if err := configure(conn, 537); err != nil {
			t.Fatalf("configure: %v", err)
		}
		if want := []string{"bind", "mode"}; !slices.Equal(h.sent, want) {
			t.Fatalf("sent %v, want %v", h.sent, want)
		}
	})

	t.Run("an error that follows a copy is still the answer", func(t *testing.T) {
		fastBindRetries(t)
		h := newHandshakeLog(t)
		var waiting []netlink.Message
		conn := nltest.Dial(func(reqs []netlink.Message) ([]netlink.Message, error) {
			if len(reqs) == 0 {
				if waiting == nil {
					return nil, errors.New("Receive was called with no request waiting for its ack")
				}
				nak, err := nltest.Error(int(unix.EPERM), waiting)
				waiting = nil
				return nak, err
			}
			h.note(t, reqs[0])
			waiting = reqs
			return []netlink.Message{leftoverCopy()}, nil
		})
		defer conn.Close()

		if err := configure(conn, 537); !errors.Is(err, ErrGroupTaken) {
			t.Fatalf("configure returned %v, want ErrGroupTaken", err)
		}
		if want := []string{"bind", "bind", "bind"}; !slices.Equal(h.sent, want) {
			t.Fatalf("sent %v, want %v", h.sent, want)
		}
	})

	t.Run("a group another program keeps is reported after the retries", func(t *testing.T) {
		fastBindRetries(t)
		h := newHandshakeLog(t)
		conn := nltest.Dial(func(reqs []netlink.Message) ([]netlink.Message, error) {
			if len(reqs) == 0 {
				return nil, errors.New("Receive was called with no request waiting for its ack")
			}
			h.note(t, reqs[0])
			return nltest.Error(int(unix.EPERM), reqs)
		})
		defer conn.Close()

		err := configure(conn, 537)
		if !errors.Is(err, ErrGroupTaken) {
			t.Fatalf("configure returned %v, want ErrGroupTaken", err)
		}
		if want := []string{"bind", "bind", "bind"}; !slices.Equal(h.sent, want) {
			t.Fatalf("sent %v, want %v", h.sent, want)
		}
	})

	t.Run("a group freed during the retries is bound", func(t *testing.T) {
		fastBindRetries(t)
		h := newHandshakeLog(t)
		conn := nltest.Dial(func(reqs []netlink.Message) ([]netlink.Message, error) {
			if len(reqs) == 0 {
				return nil, errors.New("Receive was called with no request waiting for its ack")
			}
			if h.note(t, reqs[0]) == "bind" && len(h.sent) < bindAttempts {
				return nltest.Error(int(unix.EPERM), reqs)
			}
			return nltest.Error(0, reqs)
		})
		defer conn.Close()

		if err := configure(conn, 537); err != nil {
			t.Fatalf("configure: %v", err)
		}
		if want := []string{"bind", "bind", "bind", "mode"}; !slices.Equal(h.sent, want) {
			t.Fatalf("sent %v, want %v", h.sent, want)
		}
	})

	t.Run("other errors are returned as they are", func(t *testing.T) {
		fastBindRetries(t)
		h := newHandshakeLog(t)
		conn := nltest.Dial(func(reqs []netlink.Message) ([]netlink.Message, error) {
			if len(reqs) == 0 {
				return nil, errors.New("Receive was called with no request waiting for its ack")
			}
			if h.note(t, reqs[0]) == "mode" {
				return nltest.Error(int(unix.EOPNOTSUPP), reqs)
			}
			return nltest.Error(0, reqs)
		})
		defer conn.Close()

		err := configure(conn, 537)
		if err == nil || errors.Is(err, ErrGroupTaken) || !errors.Is(err, unix.EOPNOTSUPP) {
			t.Fatalf("configure returned %v, want the EOPNOTSUPP from the kernel", err)
		}
		if want := []string{"bind", "mode"}; !slices.Equal(h.sent, want) {
			t.Fatalf("sent %v, want %v", h.sent, want)
		}
	})
}
