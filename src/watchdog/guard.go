package watchdog

import (
	"net"
	"net/netip"
	"sync"
	"syscall"
	"time"

	"github.com/daniellavrushin/b4/utils"
	"golang.org/x/sys/unix"
)

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
