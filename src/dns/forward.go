package dns

import (
	"fmt"
	"net"
	"strconv"
	"syscall"
	"time"

	"github.com/daniellavrushin/b4/sock"
	"golang.org/x/sys/unix"
)

type ForwardOptions struct {
	Sender       *sock.Sender
	Fragment     bool
	Seg2Delay    int
	ReverseOrder bool
	Timeout      time.Duration
	Port         int
	Mark         int
}

func ResolveUpstream(query []byte, target net.IP, opts ForwardOptions) ([]byte, error) {
	port := opts.Port
	if port == 0 {
		port = 53
	}

	d := net.Dialer{}
	if opts.Mark != 0 {
		mark := opts.Mark
		d.Control = func(_, _ string, c syscall.RawConn) error {
			var serr error
			if cerr := c.Control(func(fd uintptr) {
				serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, mark)
			}); cerr != nil {
				return cerr
			}
			return serr
		}
	}

	c, err := d.Dial("udp", net.JoinHostPort(target.String(), strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	defer c.Close()
	conn, ok := c.(*net.UDPConn)
	if !ok {
		return nil, fmt.Errorf("unexpected conn type %T", c)
	}

	sent := false
	if opts.Fragment && opts.Sender != nil {
		sent = sendFragmentedQuery(conn, query, target, port, opts)
	}
	if !sent {
		if _, err := conn.Write(query); err != nil {
			return nil, err
		}
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}

	buf := make([]byte, 65535)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func sendFragmentedQuery(conn *net.UDPConn, query []byte, target net.IP, port int, opts ForwardOptions) bool {
	la, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return false
	}

	splitPos := findDNSSplitPoint(query)
	if splitPos <= 0 {
		splitPos = len(query) / 2
	}

	if t4 := target.To4(); t4 != nil {
		pkt := sock.BuildUDPPacketV4(la.IP, target, uint16(la.Port), uint16(port), query)
		if pkt == nil {
			return false
		}
		frags, ok := sock.IPv4FragmentUDP(pkt, splitPos)
		if !ok {
			return false
		}
		return sendTwoFragments(opts.Sender, true, frags, target, opts.Seg2Delay, opts.ReverseOrder) == nil
	}

	if !opts.Sender.IPv6Ready() {
		return false
	}
	pkt := sock.BuildUDPPacketV6(la.IP, target, uint16(la.Port), uint16(port), query)
	if pkt == nil {
		return false
	}
	frags, ok := sock.IPv6FragmentUDP(pkt, splitPos)
	if !ok {
		return false
	}
	return sendTwoFragments(opts.Sender, false, frags, target, opts.Seg2Delay, opts.ReverseOrder) == nil
}

func sendTwoFragments(s *sock.Sender, v4 bool, frags [][]byte, dst net.IP, delay int, reverse bool) error {
	send := func(p []byte) error {
		if v4 {
			return s.SendIPv4(p, dst)
		}
		return s.SendIPv6(p, dst)
	}
	a, b := frags[0], frags[1]
	if reverse {
		a, b = b, a
	}
	if err := send(a); err != nil {
		return err
	}
	if delay > 0 {
		time.Sleep(time.Duration(delay) * time.Millisecond)
	}
	return send(b)
}

func findDNSSplitPoint(dnsPayload []byte) int {
	if len(dnsPayload) < 13 {
		return -1
	}

	pos := 12
	qnameStart := pos
	qnameEnd := pos

	for pos < len(dnsPayload) {
		labelLen := int(dnsPayload[pos])
		if labelLen == 0 {
			qnameEnd = pos + 1
			break
		}
		if labelLen > 63 || pos+1+labelLen > len(dnsPayload) {
			return len(dnsPayload) / 2
		}
		pos += 1 + labelLen
	}

	if qnameEnd <= qnameStart {
		return len(dnsPayload) / 2
	}

	qnameLen := qnameEnd - qnameStart
	if qnameLen > 4 {
		return qnameStart + qnameLen/2
	}

	return len(dnsPayload) / 2
}
