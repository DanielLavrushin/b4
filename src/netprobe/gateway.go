package netprobe

import (
	"context"
	"errors"
	"net"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const GatewayDetail = "TCP is terminated on the LAN gateway (a transparent proxy on the router); packets from this host never reach the ISP"

var reservedNets = func() []*net.IPNet {
	var out []*net.IPNet
	for _, cidr := range []string{
		"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15",
		"198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4",
		"100::/64", "2001:db8::/32",
	} {
		if _, n, err := net.ParseCIDR(cidr); err == nil {
			out = append(out, n)
		}
	}
	return out
}()

func PublicAddress(ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() || ip.IsMulticast() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	for _, n := range reservedNets {
		if n.Contains(ip) {
			return false
		}
	}
	return true
}

func firstHopControl(mark int) func(string, string, syscall.RawConn) error {
	return func(network, _ string, c syscall.RawConn) error {
		var serr error
		if cerr := c.Control(func(fd uintptr) {
			if network == "tcp6" {
				serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_UNICAST_HOPS, 1)
			} else {
				serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_TTL, 1)
			}
			if serr == nil && mark != 0 {
				serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, mark)
			}
		}); cerr != nil {
			return cerr
		}
		return serr
	}
}

var gatewayDial = func(ctx context.Context, network, addr string, mark int, timeout time.Duration) (net.Conn, error) {
	d := &net.Dialer{Timeout: timeout, Control: firstHopControl(mark)}
	return d.DialContext(ctx, network, addr)
}

func GatewayTerminates(ctx context.Context, ip string, port int, mark int, timeout time.Duration) bool {
	parsed := net.ParseIP(ip)
	if !PublicAddress(parsed) {
		return false
	}
	network := "tcp4"
	if parsed.To4() == nil {
		network = "tcp6"
	}
	conn, err := gatewayDial(ctx, network, net.JoinHostPort(ip, strconv.Itoa(port)), mark, timeout)
	if err == nil {
		conn.Close()
		return true
	}
	return errors.Is(err, syscall.ECONNREFUSED)
}

var GatewayProbe = GatewayTerminates
