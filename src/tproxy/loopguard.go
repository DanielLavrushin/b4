package tproxy

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	localAddrsTTL      = 10 * time.Second
	localAddrsRetry    = time.Second
	upstreamResolveTTL = time.Minute
	upstreamResolveMax = 2 * time.Second
	ownerRetry         = 5 * time.Second
	procTCPListen      = 0x0A
	procInodeField     = 9
	procStateField     = 3
	procMinFields      = 10
	defaultProcRoot    = "/proc"
	socketLinkPrefix   = "socket:["
)

type localAddrs struct {
	mu      sync.Mutex
	addrs   map[string]struct{}
	fetched time.Time
	list    func() ([]net.Addr, error)
}

var routerAddrs = &localAddrs{list: net.InterfaceAddrs}

func (a *localAddrs) has(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	key := ip.String()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.addrs == nil || time.Since(a.fetched) > localAddrsTTL {
		a.refreshLocked()
	}
	if _, ok := a.addrs[key]; ok {
		return true
	}
	if time.Since(a.fetched) <= localAddrsRetry {
		return false
	}
	a.refreshLocked()
	_, ok := a.addrs[key]
	return ok
}

func (a *localAddrs) refreshLocked() {
	a.fetched = time.Now()
	addrs, err := a.list()
	if err != nil {
		return
	}
	fresh := make(map[string]struct{}, len(addrs))
	for _, addr := range addrs {
		if ipn, ok := addr.(*net.IPNet); ok {
			fresh[ipn.IP.String()] = struct{}{}
		}
	}
	a.addrs = fresh
}

type upstreamOwner struct {
	pids  []int
	name  string
	inode uint64
}

type loopGuard struct {
	upstreamHost string
	upstreamPort int
	procRoot     string
	locals       *localAddrs
	lookup       func(ctx context.Context, host string) ([]net.IP, error)

	addrMu      sync.Mutex
	upstreamIPs []net.IP
	resolvedAt  time.Time
	resolving   atomic.Bool

	mu        sync.Mutex
	owner     upstreamOwner
	checkedAt time.Time
}

func newLoopGuard(host string, port int) *loopGuard {
	g := &loopGuard{
		upstreamPort: port,
		procRoot:     defaultProcRoot,
		locals:       routerAddrs,
		lookup:       lookupHostIPs,
	}
	h := strings.TrimSpace(host)
	switch ip := net.ParseIP(h); {
	case ip != nil:
		g.upstreamIPs = []net.IP{loopbackFor(ip)}
	case strings.EqualFold(h, "localhost"):
		g.upstreamIPs = []net.IP{net.IPv4(127, 0, 0, 1)}
	default:
		g.upstreamHost = h
	}
	return g
}

func lookupHostIPs(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

func loopbackFor(ip net.IP) net.IP {
	if !ip.IsUnspecified() {
		return ip
	}
	if ip.To4() != nil {
		return net.IPv4(127, 0, 0, 1)
	}
	return net.IPv6loopback
}

func (g *loopGuard) addresses() []net.IP {
	g.addrMu.Lock()
	ips, host := g.upstreamIPs, g.upstreamHost
	stale := host != "" && time.Since(g.resolvedAt) > upstreamResolveTTL
	g.addrMu.Unlock()
	if stale && g.resolving.CompareAndSwap(false, true) {
		go g.resolve(host)
	}
	return ips
}

func (g *loopGuard) resolve(host string) {
	defer g.resolving.Store(false)
	ctx, cancel := context.WithTimeout(context.Background(), upstreamResolveMax)
	defer cancel()
	ips, err := g.lookup(ctx, host)
	resolved := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		resolved = append(resolved, loopbackFor(ip))
	}
	g.addrMu.Lock()
	defer g.addrMu.Unlock()
	g.resolvedAt = time.Now()
	if err == nil && len(resolved) > 0 {
		g.upstreamIPs = resolved
	}
}

func (g *loopGuard) upstreamOnRouter() (net.IP, bool) {
	if g == nil {
		return nil, false
	}
	for _, ip := range g.addresses() {
		if g.locals.has(ip) {
			return ip, true
		}
	}
	return nil, false
}

func (g *loopGuard) fromUpstreamHost(client net.IP) bool {
	if g == nil || client == nil {
		return false
	}
	if _, onRouter := g.upstreamOnRouter(); onRouter {
		return false
	}
	for _, ip := range g.addresses() {
		if client.Equal(ip) {
			return true
		}
	}
	return false
}

func (g *loopGuard) fromUpstreamProcess(client, dst *net.TCPAddr) (string, int, bool) {
	if client == nil || dst == nil {
		return "", 0, false
	}
	upIP, onRouter := g.upstreamOnRouter()
	if !onRouter || !g.locals.has(client.IP) {
		return "", 0, false
	}
	owner := g.knownOwner(upIP)
	if len(owner.pids) == 0 {
		return "", 0, false
	}
	inode := findSocketInode(g.procRoot, client, dst)
	if inode == 0 {
		return "", 0, false
	}
	pid, stale := ownerHolding(g.procRoot, owner, inode)
	if pid > 0 {
		return owner.name, pid, true
	}
	owner, refreshed := g.refreshOwner(upIP, stale)
	if !refreshed || len(owner.pids) == 0 {
		return "", 0, false
	}
	if pid, _ = ownerHolding(g.procRoot, owner, inode); pid > 0 {
		return owner.name, pid, true
	}
	return "", 0, false
}

func ownerHolding(procRoot string, owner upstreamOwner, inode uint64) (int, bool) {
	stale := false
	for _, pid := range owner.pids {
		held := processSockets(procRoot, pid)
		if _, ok := held[inode]; ok {
			return pid, false
		}
		if _, ok := held[owner.inode]; !ok {
			stale = true
		}
	}
	return 0, stale
}

func (g *loopGuard) knownOwner(upIP net.IP) upstreamOwner {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.owner.pids) == 0 && (g.checkedAt.IsZero() || time.Since(g.checkedAt) > ownerRetry) {
		g.checkedAt = time.Now()
		g.owner = findUpstreamOwner(g.procRoot, upIP, g.upstreamPort)
	}
	return g.owner
}

func (g *loopGuard) refreshOwner(upIP net.IP, stale bool) (upstreamOwner, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !stale && time.Since(g.checkedAt) <= ownerRetry {
		return g.owner, false
	}
	g.checkedAt = time.Now()
	g.owner = findUpstreamOwner(g.procRoot, upIP, g.upstreamPort)
	return g.owner, true
}

type procSocket struct {
	localIP    net.IP
	localPort  int
	remoteIP   net.IP
	remotePort int
	state      uint64
	inode      uint64
}

func parseProcNetLine(line string) (procSocket, bool) {
	f := strings.Fields(line)
	if len(f) < procMinFields {
		return procSocket{}, false
	}
	lip, lport, ok := decodeProcAddr(f[1])
	if !ok {
		return procSocket{}, false
	}
	rip, rport, ok := decodeProcAddr(f[2])
	if !ok {
		return procSocket{}, false
	}
	state, err := strconv.ParseUint(f[procStateField], 16, 8)
	if err != nil {
		return procSocket{}, false
	}
	inode, err := strconv.ParseUint(f[procInodeField], 10, 64)
	if err != nil {
		return procSocket{}, false
	}
	return procSocket{localIP: lip, localPort: lport, remoteIP: rip, remotePort: rport, state: state, inode: inode}, true
}

func decodeProcAddr(s string) (net.IP, int, bool) {
	colon := strings.IndexByte(s, ':')
	if colon < 0 {
		return nil, 0, false
	}
	port, err := strconv.ParseUint(s[colon+1:], 16, 16)
	if err != nil {
		return nil, 0, false
	}
	raw, err := hex.DecodeString(s[:colon])
	if err != nil || (len(raw) != net.IPv4len && len(raw) != net.IPv6len) {
		return nil, 0, false
	}
	ip := make(net.IP, len(raw))
	for i := 0; i < len(raw); i += 4 {
		binary.NativeEndian.PutUint32(ip[i:i+4], binary.BigEndian.Uint32(raw[i:i+4]))
	}
	return ip, int(port), true
}

func encodeProcAddr(raw []byte, port int) string {
	var b strings.Builder
	b.Grow(len(raw)*2 + 5)
	for i := 0; i+4 <= len(raw); i += 4 {
		fmt.Fprintf(&b, "%08X", binary.NativeEndian.Uint32(raw[i:i+4]))
	}
	fmt.Fprintf(&b, ":%04X", port)
	return b.String()
}

func procAddrForms(addr *net.TCPAddr) (v4, v6 string) {
	if ip4 := addr.IP.To4(); ip4 != nil {
		v4 = encodeProcAddr(ip4, addr.Port)
	}
	if ip16 := addr.IP.To16(); ip16 != nil {
		v6 = encodeProcAddr(ip16, addr.Port)
	}
	return v4, v6
}

func findSocketInode(procRoot string, local, remote *net.TCPAddr) uint64 {
	l4, l6 := procAddrForms(local)
	r4, r6 := procAddrForms(remote)
	for _, t := range []struct{ name, local, remote string }{{"tcp", l4, r4}, {"tcp6", l6, r6}} {
		if t.local == "" || t.remote == "" {
			continue
		}
		if inode := scanForSocket(filepath.Join(procRoot, "net", t.name), t.local, t.remote); inode != 0 {
			return inode
		}
	}
	return 0
}

func scanForSocket(path, local, remote string) uint64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.Contains(line, local) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < procMinFields || fields[1] != local || fields[2] != remote {
			continue
		}
		if inode, err := strconv.ParseUint(fields[procInodeField], 10, 64); err == nil && inode != 0 {
			return inode
		}
	}
	return 0
}

func findListenerInode(procRoot string, ip net.IP, port int) uint64 {
	for _, name := range []string{"tcp", "tcp6"} {
		f, err := os.Open(filepath.Join(procRoot, "net", name))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		var inode uint64
		for sc.Scan() {
			s, ok := parseProcNetLine(sc.Text())
			if !ok || s.state != procTCPListen || s.inode == 0 || s.localPort != port {
				continue
			}
			if s.localIP.IsUnspecified() || s.localIP.Equal(ip) {
				inode = s.inode
				break
			}
		}
		f.Close()
		if inode != 0 {
			return inode
		}
	}
	return 0
}

func processSockets(procRoot string, pid int) map[uint64]struct{} {
	if pid <= 0 {
		return nil
	}
	dir := filepath.Join(procRoot, strconv.Itoa(pid), "fd")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	held := make(map[uint64]struct{}, len(entries))
	for _, e := range entries {
		target, err := os.Readlink(filepath.Join(dir, e.Name()))
		if err != nil || !strings.HasPrefix(target, socketLinkPrefix) || !strings.HasSuffix(target, "]") {
			continue
		}
		if inode, err := strconv.ParseUint(target[len(socketLinkPrefix):len(target)-1], 10, 64); err == nil {
			held[inode] = struct{}{}
		}
	}
	return held
}

func findUpstreamOwner(procRoot string, ip net.IP, port int) upstreamOwner {
	inode := findListenerInode(procRoot, ip, port)
	if inode == 0 {
		return upstreamOwner{}
	}
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return upstreamOwner{}
	}
	owner := upstreamOwner{inode: inode}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 1 {
			continue
		}
		if _, ok := processSockets(procRoot, pid)[inode]; ok {
			owner.pids = append(owner.pids, pid)
			if owner.name == "" {
				owner.name = processName(procRoot, pid)
			}
		}
	}
	return owner
}

func processName(procRoot string, pid int) string {
	data, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "comm"))
	if err != nil {
		return "pid " + strconv.Itoa(pid)
	}
	return strings.TrimSpace(string(data))
}
