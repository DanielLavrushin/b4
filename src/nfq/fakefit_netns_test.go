package nfq

import (
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sock"
)

type fakeFitLink struct {
	src4, dst4, src6, dst6 net.IP
}

func fakeFitNetnsLink(t *testing.T, name string, mtu int, prefix4, prefix6 string) fakeFitLink {
	t.Helper()
	netnsHoldRequire(t)
	_ = exec.Command("ip", "link", "del", name).Run()
	netnsHoldRun(t, "ip", "link", "add", name, "type", "dummy")
	t.Cleanup(func() { _ = exec.Command("ip", "link", "del", name).Run() })
	netnsHoldRun(t, "ip", "link", "set", name, "mtu", strconv.Itoa(mtu), "up")
	netnsHoldRun(t, "ip", "addr", "add", prefix4+"1/24", "dev", name)
	netnsHoldRun(t, "ip", "-6", "addr", "add", prefix6+"1/64", "dev", name, "nodad")
	return fakeFitLink{
		src4: net.ParseIP(prefix4 + "1"), dst4: net.ParseIP(prefix4 + "2"),
		src6: net.ParseIP(prefix6 + "1"), dst6: net.ParseIP(prefix6 + "2"),
	}
}

func fakeFitRawSender(t *testing.T) *sock.Sender {
	t.Helper()
	s, err := sock.NewSenderWithMark(0)
	if err != nil {
		t.Fatalf("raw sender: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func fakeFitSend(s *sock.Sender, pkt []byte) error {
	if pkt[0]>>4 == IPv6 {
		return s.SendIPv6(pkt, net.IP(pkt[24:40]))
	}
	return s.SendIPv4(pkt, net.IP(pkt[16:20]))
}

func (l fakeFitLink) udpPacket(ver byte) ([]byte, net.IP) {
	payload := []byte("not a quic initial, any udp payload")
	if ver == IPv6 {
		raw := makeV6UDPPacketWithExt(payload, 51000, 50005, 0, nil)
		copy(raw[8:24], l.src6.To16())
		copy(raw[24:40], l.dst6.To16())
		return raw, l.dst6
	}
	return makeV4UDPPacket(payload, l.src4, l.dst4, 51000, 50005), l.dst4
}

func TestNetnsAnMD5FakeSizedToAFullSegmentLeavesThroughA1500ByteMTU(t *testing.T) {
	link := fakeFitNetnsLink(t, "b4ff0", 1500, "10.9.0.", "fd09::")
	sender := fakeFitRawSender(t)
	for _, ver := range []byte{IPv4, IPv6} {
		src, dst := link.src4, link.dst4
		if ver == IPv6 {
			src, dst = link.src6, link.dst6
		}
		for _, h := range md5FitHellos() {
			original := md5FitFullSegmentOriginal(ver, src, dst, h.hello)
			if err := fakeFitSend(sender, original); err != nil {
				t.Fatalf("control: the %d-byte real segment itself is refused: %v", len(original), err)
			}
			for _, set := range md5FitSets() {
				t.Run(fmt.Sprintf("v%d/%s/%s", ver, h.name, set.Name), func(t *testing.T) {
					w := newDualStackInjectingWorker(t, &set)
					sent := captureInjected(t, w)
					sendFakeSNIFor(w, &set, original)
					fakes := sent()
					if len(fakes) == 0 {
						t.Fatal("no fake built")
					}
					for _, fake := range fakes {
						if err := fakeFitSend(sender, fake); err != nil {
							t.Errorf("a %d-byte fake for a %d-byte segment is refused by the kernel: %v", len(fake), len(original), err)
						}
					}
				})
			}
		}
	}
}

func TestNetnsAUDPFakeOfAnyOfferedLengthLeavesThroughA1500ByteMTU(t *testing.T) {
	link := fakeFitNetnsLink(t, "b4ff1", 1500, "10.9.1.", "fd09:1::")
	sender := fakeFitRawSender(t)
	for _, ver := range []byte{IPv4, IPv6} {
		for _, fakeLen := range []int{1452, 1460, 1472, 1500} {
			t.Run(fmt.Sprintf("v%d/fake_len=%d", ver, fakeLen), func(t *testing.T) {
				set := udpFakeModeSet("udp-fake-fits", fakeLen)
				w := newDualStackInjectingWorker(t, &set)
				sent := captureInjected(t, w)
				raw, dst := link.udpPacket(ver)
				injectUDPFake(w, &set, ver, raw, dst)
				out := sent()
				if len(out) < 2 {
					t.Fatalf("want the fake and the real packet, sent %d packets", len(out))
				}
				for _, pkt := range out {
					if err := fakeFitSend(sender, pkt); err != nil {
						t.Errorf("a %d-byte packet for udp.fake_len=%d is refused by the kernel: %v", len(pkt), fakeLen, err)
					}
				}
			})
		}
	}
}

func TestNetnsAUDPFakeTooLongForTheLinkIsLoggedWithItsSetAndAHint(t *testing.T) {
	link := fakeFitNetnsLink(t, "b4ff2", 1280, "10.9.2.", "fd09:2::")
	for _, ver := range []byte{IPv4, IPv6} {
		t.Run(fmt.Sprintf("v%d", ver), func(t *testing.T) {
			buf := captureLog(t)
			set := udpFakeModeSet("udp-fake-too-long", 1452)
			set.UDP.FakeSeqLength = 3
			w := newDualStackInjectingWorker(t, &set)
			w.sock = fakeFitRawSender(t)
			raw, dst := link.udpPacket(ver)

			injectUDPFake(w, &set, ver, raw, dst)

			log.Flush()
			lines := udpFakeWarnings(buf, set.Name)
			if len(lines) != 1 {
				t.Fatalf("three UDP fakes refused as too long for a 1280-byte link: want one warning naming the set, got %d:\n%s", len(lines), buf.String())
			}
			if !strings.Contains(lines[0], "udp.fake_len") {
				t.Fatalf("the warning does not point at udp.fake_len: %s", lines[0])
			}
		})
	}
}
