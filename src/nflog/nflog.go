package nflog

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

const (
	nfulnlMsgPacket = unix.NFNL_SUBSYS_ULOG << 8
	nfulnlMsgConfig = unix.NFNL_SUBSYS_ULOG<<8 | 1

	nfulaCfgCmd     = 1
	nfulaCfgMode    = 2
	nfulaCfgQThresh = 5
	nfulaCfgFlags   = 6

	nfulnlCfgCmdBind = 1
	nfulnlCopyPacket = 2
	nfulnlCfgFSeq    = 0x0001

	copyRange        = 0xffff
	recvBufferBytes  = 128 << 10
	handshakeTimeout = 2 * time.Second
	bindAttempts     = 3

	drainPatience = 50 * time.Millisecond
)

var (
	bindRetryDelay = 200 * time.Millisecond
	readBatch      = 64
	clockBase      = time.Now()
)

var ErrGroupTaken = errors.New("the NFLOG group is bound by another program")

type Packet struct {
	Payload []byte
	Prefix  string
	Mark    uint32
	InDev   uint32
	OutDev  uint32
	Hook    uint8
	Seq     uint32
	HasSeq  bool
}

type Conn struct {
	sock io.Closer
	raw  syscall.RawConn

	mu       sync.Mutex
	buf      []byte
	seqNext  uint32
	seqKnown bool

	gaps         atomic.Uint64
	overruns     atomic.Uint64
	closing      atomic.Bool
	backlogSince atomic.Int64
}

func Open(group uint16, readBuffer int) (*Conn, error) {
	nl, err := netlink.Dial(unix.NETLINK_NETFILTER, nil)
	if err != nil {
		return nil, fmt.Errorf("open a netfilter netlink socket: %w", err)
	}
	_ = nl.SetOption(netlink.NoENOBUFS, true)
	if readBuffer > 0 {
		_ = nl.SetReadBuffer(readBuffer)
	}
	if err := configure(nl, group); err != nil {
		_ = nl.Close()
		return nil, err
	}
	if err := nl.SetReadDeadline(time.Time{}); err != nil {
		_ = nl.Close()
		return nil, fmt.Errorf("NFLOG group %d: %w", group, err)
	}
	raw, err := nl.SyscallConn()
	if err != nil {
		_ = nl.Close()
		return nil, fmt.Errorf("NFLOG group %d: %w", group, err)
	}
	return newConn(nl, raw), nil
}

func newConn(sock io.Closer, raw syscall.RawConn) *Conn {
	return &Conn{sock: sock, raw: raw, buf: make([]byte, recvBufferBytes)}
}

func configMessage(group uint16, attrs func(ae *netlink.AttributeEncoder)) (netlink.Message, error) {
	ae := netlink.NewAttributeEncoder()
	ae.ByteOrder = binary.BigEndian
	attrs(ae)
	b, err := ae.Encode()
	if err != nil {
		return netlink.Message{}, err
	}
	data := []byte{unix.AF_UNSPEC, unix.NFNETLINK_V0, 0, 0}
	binary.BigEndian.PutUint16(data[2:], group)
	return netlink.Message{
		Header: netlink.Header{Type: netlink.HeaderType(nfulnlMsgConfig), Flags: netlink.Request | netlink.Acknowledge},
		Data:   append(data, b...),
	}, nil
}

func bindMessage(group uint16) (netlink.Message, error) {
	return configMessage(group, func(ae *netlink.AttributeEncoder) {
		ae.Uint8(nfulaCfgCmd, nfulnlCfgCmdBind)
	})
}

func modeMessage(group uint16) (netlink.Message, error) {
	return configMessage(group, func(ae *netlink.AttributeEncoder) {
		mode := make([]byte, 6)
		binary.BigEndian.PutUint32(mode[0:4], copyRange)
		mode[4] = nfulnlCopyPacket
		ae.Bytes(nfulaCfgMode, mode)
		ae.Uint32(nfulaCfgQThresh, 1)
		ae.Uint16(nfulaCfgFlags, nfulnlCfgFSeq)
	})
}

func configure(nl *netlink.Conn, group uint16) error {
	bind, err := bindMessage(group)
	if err != nil {
		return err
	}
	mode, err := modeMessage(group)
	if err != nil {
		return err
	}
	for attempt := 1; ; attempt++ {
		err = exchange(nl, bind)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EPERM) {
			return fmt.Errorf("bind NFLOG group %d: %w", group, err)
		}
		if attempt == bindAttempts {
			return ErrGroupTaken
		}
		time.Sleep(bindRetryDelay)
	}
	if err := exchange(nl, mode); err != nil {
		if errors.Is(err, unix.EPERM) {
			return ErrGroupTaken
		}
		return fmt.Errorf("configure NFLOG group %d: %w", group, err)
	}
	return nil
}

func exchange(nl *netlink.Conn, m netlink.Message) error {
	_ = nl.SetReadDeadline(time.Now().Add(handshakeTimeout))
	req, err := nl.Send(m)
	if err != nil {
		return err
	}
	for {
		replies, err := nl.Receive()
		if err != nil {
			return err
		}
		for _, r := range replies {
			if r.Header.Type == netlink.Error && r.Header.Sequence == req.Header.Sequence {
				return nil
			}
		}
	}
}

func (c *Conn) Run(fn func(Packet)) error {
	var failed error
	err := c.raw.Read(func(fd uintptr) bool {
		for {
			more, err := c.drainBatch(fd, fn)
			if err != nil {
				failed = err
				return true
			}
			if !more {
				return false
			}
		}
	})
	if c.closing.Load() {
		return nil
	}
	if failed != nil {
		return failed
	}
	return err
}

func (c *Conn) Drain(fn func(Packet)) error {
	var failed error
	err := c.raw.Control(func(fd uintptr) {
		for !c.backlogged() {
			more, err := c.drainBatch(fd, fn)
			if err != nil || !more {
				failed = err
				return
			}
		}
	})
	if c.closing.Load() {
		return nil
	}
	if err != nil {
		return err
	}
	return failed
}

func (c *Conn) Lost() uint64 {
	return c.gaps.Load() + c.overruns.Load()
}

func (c *Conn) Close() error {
	if c.closing.Swap(true) {
		return nil
	}
	return c.sock.Close()
}

func monotonicNow() int64 {
	return int64(time.Since(clockBase)) + 1
}

func (c *Conn) backlogged() bool {
	since := c.backlogSince.Load()
	return since != 0 && time.Duration(monotonicNow()-since) > drainPatience
}

func (c *Conn) drainBatch(fd uintptr, fn func(Packet)) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for range readBatch {
		if c.closing.Load() {
			return false, nil
		}
		n, _, err := unix.Recvfrom(int(fd), c.buf, unix.MSG_DONTWAIT)
		switch err {
		case nil:
			c.process(c.buf[:n], fn)
		case unix.EAGAIN:
			c.backlogSince.Store(0)
			return false, nil
		case unix.EINTR:
		case unix.ENOBUFS:
			c.overruns.Add(1)
		default:
			return false, err
		}
	}
	c.backlogSince.CompareAndSwap(0, monotonicNow())
	return true, nil
}
