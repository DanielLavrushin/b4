package watchdog

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"syscall"
	"time"

	"github.com/daniellavrushin/b4/utils"
	"golang.org/x/sys/unix"
)

type ErrUnhandledFamily struct {
	Host   string
	Family string
	From   string
}

func (e *ErrUnhandledFamily) Error() string {
	why := "IPv6 support is off, or the TUN engine is running"
	if e.Family == "IPv4" {
		why = "IPv4 support is off"
	}
	msg := fmt.Sprintf("%s has only %s addresses, and b4 applies no strategy to %s now (%s)", e.Host, e.Family, e.Family, why)
	if e.From != "" {
		msg += ", reached by a redirect from " + e.From
	}
	return msg
}

type dialGuard struct {
	mu         sync.Mutex
	refused    *ErrPrivateDestination
	usable     bool
	isReserved func(netip.Addr) bool
}

func newDialGuard(isReserved func(netip.Addr) bool) *dialGuard {
	if isReserved == nil {
		isReserved = utils.IsReservedAddr
	}
	return &dialGuard{isReserved: isReserved}
}

func (g *dialGuard) dialer(mark uint, timeout time.Duration) *net.Dialer {
	return &net.Dialer{
		Timeout:   timeout / 2,
		KeepAlive: timeout,
		Control: func(_, address string, c syscall.RawConn) error {
			hostPart, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			addr, err := netip.ParseAddr(hostPart)
			if err != nil {
				return err
			}
			if g.isReserved(addr) {
				refusal := &ErrPrivateDestination{Addr: addr.String()}
				g.mu.Lock()
				if g.refused == nil {
					g.refused = refusal
				}
				g.mu.Unlock()
				return refusal
			}
			g.mu.Lock()
			g.usable = true
			g.mu.Unlock()
			if mark == 0 {
				return nil
			}
			var ctrlErr error
			if err := c.Control(func(fd uintptr) {
				ctrlErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, int(mark))
			}); err != nil {
				return err
			}
			return ctrlErr
		},
	}
}

func (g *dialGuard) dialContext(mark uint, timeout time.Duration, network string) func(context.Context, string, string) (net.Conn, error) {
	dialer := g.dialer(mark, timeout)
	if network == "" {
		return dialer.DialContext
	}
	return func(ctx context.Context, _, address string) (net.Conn, error) {
		conn, err := dialer.DialContext(ctx, network, address)
		if err == nil {
			return conn, nil
		}
		if unhandled := g.otherFamilyOnly(ctx, network, address, err, timeout/2); unhandled != nil {
			return nil, unhandled
		}
		return nil, err
	}
}

func (g *dialGuard) otherFamilyOnly(ctx context.Context, network, address string, err error, budget time.Duration) *ErrUnhandledFamily {
	var dnsErr *net.DNSError
	var addrErr *net.AddrError
	noAddress := errors.As(err, &dnsErr) && dnsErr.IsNotFound || errors.As(err, &addrErr)
	host, _, splitErr := net.SplitHostPort(address)
	if !noAddress || splitErr != nil {
		return nil
	}
	other, family := "ip6", "IPv6"
	if network == "tcp6" {
		other, family = "ip4", "IPv4"
	}
	lookupCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	addrs, lookupErr := net.DefaultResolver.LookupNetIP(lookupCtx, other, host)
	if lookupErr != nil || len(addrs) == 0 {
		return nil
	}
	for _, addr := range addrs {
		if !g.isReserved(addr.Unmap()) {
			return &ErrUnhandledFamily{Host: host, Family: family}
		}
	}
	g.refuse(&ErrPrivateDestination{Addr: addrs[0].Unmap().String()})
	return nil
}

func (g *dialGuard) reservedLiteral(host string) *ErrPrivateDestination {
	addr, err := netip.ParseAddr(host)
	if err != nil || !g.isReserved(addr) {
		return nil
	}
	return &ErrPrivateDestination{Addr: addr.String()}
}

func (g *dialGuard) nextHop() {
	g.mu.Lock()
	g.refused = nil
	g.usable = false
	g.mu.Unlock()
}

func (g *dialGuard) refuse(refusal *ErrPrivateDestination) {
	g.mu.Lock()
	g.refused = refusal
	g.usable = false
	g.mu.Unlock()
}

func (g *dialGuard) refusal() *ErrPrivateDestination {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.refused != nil && !g.usable {
		return g.refused
	}
	return nil
}
