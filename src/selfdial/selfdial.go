package selfdial

import (
	"context"
	"fmt"
	"net"
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

type entry struct {
	until   time.Time
	gen     uint64
	isn     uint32
	claimed bool
}

var (
	mu      sync.Mutex
	flows   = make(map[flow]entry)
	live    atomic.Int64
	lastGen uint64
)

func keyOf(port uint16, remote netip.AddrPort) flow {
	return flow{port: port, remote: netip.AddrPortFrom(remote.Addr().Unmap(), remote.Port())}
}

func Note(port uint16, remote netip.AddrPort) {
	noteAt(port, remote, time.Now())
}

func noteAt(port uint16, remote netip.AddrPort, now time.Time) uint64 {
	if port == 0 || !remote.IsValid() {
		return 0
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
	lastGen++
	flows[k] = entry{until: now.Add(flowTTL), gen: lastGen}
	live.Store(int64(len(flows)))
	return lastGen
}

func release(k flow, gen uint64) {
	mu.Lock()
	defer mu.Unlock()
	if e, had := flows[k]; had && e.gen == gen {
		delete(flows, k)
		live.Store(int64(len(flows)))
	}
}

func Holds(port uint16, remote netip.AddrPort) bool {
	if live.Load() == 0 {
		return false
	}
	return holdsAt(port, remote, time.Now())
}

func holdsAt(port uint16, remote netip.AddrPort, now time.Time) bool {
	return lookupAt(keyOf(port, remote), now, false, 0)
}

func HoldsSegment(port uint16, remote netip.AddrPort, syn bool, seq uint32) bool {
	if live.Load() == 0 {
		return false
	}
	return segmentAt(port, remote, syn, seq, time.Now())
}

func segmentAt(port uint16, remote netip.AddrPort, syn bool, seq uint32, now time.Time) bool {
	return lookupAt(keyOf(port, remote), now, syn, seq)
}

func lookupAt(k flow, now time.Time, syn bool, seq uint32) bool {
	mu.Lock()
	defer mu.Unlock()
	e, had := flows[k]
	if !had {
		return false
	}
	if !now.Before(e.until) || (syn && e.claimed && e.isn != seq) {
		delete(flows, k)
		live.Store(int64(len(flows)))
		return false
	}
	if syn && !e.claimed {
		e.claimed, e.isn = true, seq
	}
	e.until = now.Add(flowTTL)
	flows[k] = e
	return true
}

func sweepLocked(now time.Time) {
	for k, e := range flows {
		if !now.Before(e.until) {
			delete(flows, k)
		}
	}
}

type registration struct {
	key flow
	gen uint64
}

type tracker struct {
	mark  int
	mu    sync.Mutex
	done  bool
	keep  flow
	noted []registration
}

func Dial(ctx context.Context, network, address string, mark int, timeout time.Duration) (net.Conn, error) {
	t := &tracker{mark: mark}
	d := net.Dialer{Timeout: timeout, Control: t.control}
	conn, err := d.DialContext(ctx, network, address)
	t.finish(conn)
	return conn, err
}

func (t *tracker) control(network, address string, c syscall.RawConn) error {
	remote, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("own connection to %s: %w", address, err)
	}
	var port uint16
	var serr error
	if cerr := c.Control(func(fd uintptr) {
		if t.mark != 0 {
			if serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, t.mark); serr != nil {
				serr = fmt.Errorf("failed to set SO_MARK=%d: %w", t.mark, serr)
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
	t.record(keyOf(port, remote), noteAt(port, remote, time.Now()))
	return nil
}

func (t *tracker) record(k flow, gen uint64) {
	if gen == 0 {
		return
	}
	t.mu.Lock()
	if t.done && k != t.keep {
		t.mu.Unlock()
		release(k, gen)
		return
	}
	t.noted = append(t.noted, registration{key: k, gen: gen})
	t.mu.Unlock()
}

func (t *tracker) finish(conn net.Conn) {
	var keep flow
	if conn != nil {
		local, lok := conn.LocalAddr().(*net.TCPAddr)
		remote, rok := conn.RemoteAddr().(*net.TCPAddr)
		if lok && rok {
			keep = keyOf(uint16(local.Port), remote.AddrPort())
		}
	}
	t.mu.Lock()
	t.done, t.keep = true, keep
	noted := t.noted
	t.noted = nil
	t.mu.Unlock()
	for _, r := range noted {
		if r.key != keep {
			release(r.key, r.gen)
		}
	}
}
