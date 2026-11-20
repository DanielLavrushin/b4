package selfdial

import (
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	flowTTL  = 2 * time.Minute
	flowsMax = 4096
)

type flow struct {
	port   uint16
	remote netip.AddrPort
}

var (
	mu    sync.Mutex
	flows = make(map[flow]time.Time)
	live  atomic.Int64
)

func keyOf(port uint16, remote netip.AddrPort) flow {
	return flow{port: port, remote: netip.AddrPortFrom(remote.Addr().Unmap(), remote.Port())}
}

func Note(port uint16, remote netip.AddrPort) {
	noteAt(port, remote, time.Now())
}

func noteAt(port uint16, remote netip.AddrPort, now time.Time) {
	if port == 0 || !remote.IsValid() {
		return
	}
	k := keyOf(port, remote)
	mu.Lock()
	defer mu.Unlock()
	if _, had := flows[k]; !had && len(flows) >= flowsMax {
		sweepLocked(now)
		for old := range flows {
			if len(flows) < flowsMax {
				break
			}
			delete(flows, old)
		}
	}
	flows[k] = now.Add(flowTTL)
	live.Store(int64(len(flows)))
}

func Holds(port uint16, remote netip.AddrPort) bool {
	if live.Load() == 0 {
		return false
	}
	return holdsAt(port, remote, time.Now())
}

func holdsAt(port uint16, remote netip.AddrPort, now time.Time) bool {
	k := keyOf(port, remote)
	mu.Lock()
	defer mu.Unlock()
	until, had := flows[k]
	if !had {
		return false
	}
	if !now.Before(until) {
		delete(flows, k)
		live.Store(int64(len(flows)))
		return false
	}
	flows[k] = now.Add(flowTTL)
	return true
}

func sweepLocked(now time.Time) {
	for k, until := range flows {
		if !now.Before(until) {
			delete(flows, k)
		}
	}
}

func Control(mark int) func(string, string, syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		remote, err := netip.ParseAddrPort(address)
		if err != nil {
			return fmt.Errorf("own connection to %s: %w", address, err)
		}
		var port uint16
		var serr error
		if cerr := c.Control(func(fd uintptr) {
			if mark != 0 {
				if serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, mark); serr != nil {
					serr = fmt.Errorf("failed to set SO_MARK=%d: %w", mark, serr)
					return
				}
			}
			var local unix.Sockaddr = &unix.SockaddrInet4{}
			if strings.HasSuffix(network, "6") {
				local = &unix.SockaddrInet6{}
			}
			if serr = unix.Bind(int(fd), local); serr != nil {
				serr = fmt.Errorf("bind before connecting to %s: %w", address, serr)
				return
			}
			bound, gerr := unix.Getsockname(int(fd))
			if gerr != nil {
				serr = fmt.Errorf("local port before connecting to %s: %w", address, gerr)
				return
			}
			switch sa := bound.(type) {
			case *unix.SockaddrInet4:
				port = uint16(sa.Port)
			case *unix.SockaddrInet6:
				port = uint16(sa.Port)
			}
		}); cerr != nil {
			return cerr
		}
		if serr != nil {
			return serr
		}
		Note(port, remote)
		return nil
	}
}
