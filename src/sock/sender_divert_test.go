package sock

import (
	"net"
	"syscall"
	"testing"
)

type pairedSender struct {
	*Sender
	peers []int
}

func newPairedSender(t *testing.T, ipv6 bool) *pairedSender {
	t.Helper()
	pair := func() [2]int {
		fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_SEQPACKET|syscall.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatalf("socketpair: %v", err)
		}
		t.Cleanup(func() {
			_ = syscall.Close(fds[0])
			_ = syscall.Close(fds[1])
		})
		return [2]int{fds[0], fds[1]}
	}
	v4 := pair()
	p := &pairedSender{Sender: NewSenderWithFDs(v4[0], -1), peers: []int{v4[1]}}
	if ipv6 {
		v6 := pair()
		p.fd6 = v6[0]
		p.peers = append(p.peers, v6[1])
	}
	return p
}

func (p *pairedSender) received() int {
	n := 0
	buf := make([]byte, 2048)
	for _, fd := range p.peers {
		for {
			if _, _, err := syscall.Recvfrom(fd, buf, syscall.MSG_DONTWAIT); err != nil {
				break
			}
			n++
		}
	}
	return n
}

func TestADivertedSenderHandsOnlyThePickedPacketsOn(t *testing.T) {
	own, other := newPairedSender(t, true), newPairedSender(t, true)
	picked := []byte{1}
	own.Divert(other.Sender, func(packet []byte) bool { return packet[0] == 1 })

	dst4, dst6 := net.ParseIP("192.0.2.1"), net.ParseIP("2001:db8::1")
	for _, pkt := range [][]byte{picked, {2}} {
		if err := own.SendIPv4(pkt, dst4); err != nil {
			t.Fatalf("SendIPv4: %v", err)
		}
		if err := own.SendIPv6(pkt, dst6); err != nil {
			t.Fatalf("SendIPv6: %v", err)
		}
	}
	if got := other.received(); got != 2 {
		t.Errorf("the other sender sent %d packets, want the picked one of each family", got)
	}
	if got := own.received(); got != 2 {
		t.Errorf("the sender itself sent %d packets, want the other one of each family", got)
	}
}

func TestADivertWithoutIPv6KeepsIPv6PacketsOnTheSender(t *testing.T) {
	own, other := newPairedSender(t, true), newPairedSender(t, false)
	own.Divert(other.Sender, func([]byte) bool { return true })
	if err := own.SendIPv6([]byte{1}, net.ParseIP("2001:db8::1")); err != nil {
		t.Fatalf("SendIPv6: %v", err)
	}
	if got := own.received(); got != 1 {
		t.Errorf("an IPv6 packet the other sender cannot send was sent %d times by the sender itself, want once rather than lost", got)
	}
}

func TestADivertNeverLoops(t *testing.T) {
	a, b := newPairedSender(t, false), newPairedSender(t, false)
	always := func([]byte) bool { return true }
	a.Divert(a.Sender, always)
	if a.divert != nil {
		t.Fatal("a sender diverts to itself, so its first picked packet never leaves")
	}
	b.Divert(a.Sender, always)
	a.Divert(b.Sender, always)
	if a.divert != nil {
		t.Fatal("a sender diverts to one that diverts back")
	}
	if err := b.SendIPv4([]byte{1}, net.ParseIP("192.0.2.1")); err != nil {
		t.Fatalf("SendIPv4: %v", err)
	}
	if got := a.received(); got != 1 {
		t.Errorf("the packet left %d times by the sender diverted to, want once", got)
	}
}
