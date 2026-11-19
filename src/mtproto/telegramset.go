package mtproto

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/netprobe"
)

type TelegramRoute struct {
	Mode        string
	Mark        uint32
	IPv4        bool
	IPv6        bool
	RuleMissing bool
}

type TelegramSetDialer interface {
	CanDialViaSet(setID string) bool
	DialViaSetBy(setID, host string, port int, by time.Time) (net.Conn, bool, error)
}

type telegramRouting struct {
	routeOf func(setID string) (TelegramRoute, bool)
	via     TelegramSetDialer
}

type telegramSelection struct {
	id   string
	name string
	mode string
	key  string
}

type telegramPathKind int

const (
	telegramPathNone telegramPathKind = iota
	telegramPathStrategy
	telegramPathInterface
	telegramPathProxy
)

type telegramPath struct {
	kind  telegramPathKind
	mark  uint32
	ipv4  bool
	ipv6  bool
	setID string
}

const telegramRouteGrace = 20 * time.Second

var (
	telegramSel     atomic.Pointer[telegramSelection]
	telegramHooks   atomic.Pointer[telegramRouting]
	telegramSetMu   sync.Mutex
	telegramSetNote string
	telegramMissing struct {
		sync.Mutex
		key    string
		since  time.Time
		warned bool
	}
)

func SetTelegramRouting(routeOf func(setID string) (TelegramRoute, bool), via TelegramSetDialer) {
	telegramHooks.Store(&telegramRouting{routeOf: routeOf, via: via})
}

func setTelegramSet(cfg *config.Config) {
	if set := cfg.TelegramUpstreamSet(); set != nil {
		telegramSel.Store(&telegramSelection{id: set.Id, name: set.Name, mode: set.TelegramRouteMode(), key: telegramSetKey(cfg)})
	} else {
		telegramSel.Store(nil)
	}
	noteTelegramSet(telegramSetProblem(cfg))
}

func telegramSetProblem(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	id := cfg.System.MTProto.UpstreamSet
	if id == "" {
		return ""
	}
	set := cfg.GetSetById(id)
	switch {
	case set == nil:
		return "the set chosen for b4's own Telegram connections (id " + id + ") no longer exists"
	case !set.Enabled:
		return "the set " + set.Name + " chosen for b4's own Telegram connections is disabled"
	case !config.CarriesTelegram(set):
		return "the set " + set.Name + " chosen for b4's own Telegram connections blocks its traffic or routes it through Telegram over WebSocket"
	case set.RoutingIncomplete():
		return "the routing of the set " + set.Name + " chosen for b4's own Telegram connections is incomplete"
	case cfg.Queue.Mode == "tun":
		return "the set " + set.Name + " chosen for b4's own Telegram connections does not apply in TUN mode"
	case cfg.TelegramSetMarkConflict():
		return fmt.Sprintf("the set %s chosen for b4's own Telegram connections does not apply while the packet mark or a Discovery mark carries the bits of 0x%x, the mark of those connections", set.Name, config.TelegramSetDialMark)
	}
	return ""
}

func noteTelegramSet(problem string) {
	telegramSetMu.Lock()
	defer telegramSetMu.Unlock()
	if problem == telegramSetNote {
		return
	}
	telegramSetNote = problem
	if problem != "" {
		log.Warnf("%s %s, so those connections go as they would without one", tg(""), problem)
	}
}

func telegramSetKey(cfg *config.Config) string {
	set := cfg.TelegramUpstreamSet()
	if set == nil {
		return ""
	}
	r := set.Routing
	mode := set.TelegramRouteMode()
	v4, v6 := true, true
	if mode == config.RoutingModeInterface {
		v4, v6 = cfg.Queue.IPv4Enabled, cfg.Queue.IPv6Enabled
	}
	return fmt.Sprintf("%s|%s|%s|%s|%s|%t|%d|%d|%s|%d|%t|%t|%t|%t", set.Id, mode, r.EgressInterface, r.EgressIP, r.EgressGateway, r.KillSwitch,
		r.FWMark, r.Table, r.Upstream.Host, r.Upstream.Port, r.Upstream.UseDomain, r.Upstream.FailOpen, v4, v6)
}

type telegramAccount struct {
	username string
	password string
}

func telegramSetAccount(cfg *config.Config) telegramAccount {
	set := cfg.TelegramUpstreamSet()
	if set == nil || set.TelegramRouteMode() != config.RoutingModeProxy {
		return telegramAccount{}
	}
	return telegramAccount{username: set.Routing.Upstream.Username, password: set.Routing.Upstream.Password}
}

func telegramSetChanged(old, next *config.Config) bool {
	return telegramSetKey(old) != telegramSetKey(next) || telegramSetAccount(old) != telegramSetAccount(next)
}

func currentTelegramPath() telegramPath {
	sel := telegramSel.Load()
	if sel == nil {
		return telegramPath{}
	}
	if sel.mode == "" {
		return telegramPath{kind: telegramPathStrategy, setID: sel.id}
	}
	hooks := telegramHooks.Load()
	switch sel.mode {
	case config.RoutingModeInterface:
		if hooks != nil && hooks.routeOf != nil {
			if r, ok := hooks.routeOf(sel.id); ok && r.Mode == config.RoutingModeInterface && r.Mark != 0 && !r.RuleMissing {
				noteTelegramRoute(sel, true)
				return telegramPath{kind: telegramPathInterface, mark: r.Mark, ipv4: r.IPv4, ipv6: r.IPv6, setID: sel.id}
			}
		}
	case config.RoutingModeProxy:
		if hooks != nil && hooks.via != nil && hooks.via.CanDialViaSet(sel.id) {
			noteTelegramRoute(sel, true)
			return telegramPath{kind: telegramPathProxy, setID: sel.id}
		}
	}
	noteTelegramRoute(sel, false)
	return telegramPath{}
}

func noteTelegramRoute(sel *telegramSelection, found bool) {
	m := &telegramMissing
	m.Lock()
	defer m.Unlock()
	if found {
		if m.key == sel.key && m.warned {
			log.Infof("%s the route of the set %s is in place again, so b4's own Telegram connections follow it", tg(""), sel.name)
		}
		m.key, m.warned = "", false
		return
	}
	now := time.Now()
	if m.key != sel.key {
		m.key, m.since, m.warned = sel.key, now, false
		return
	}
	if m.warned || now.Sub(m.since) < telegramRouteGrace {
		return
	}
	m.warned = true
	log.Warnf("%s the set %s chosen for b4's own Telegram connections routes through %s, but that route is not in place, so those connections go as they would without a set", tg(""), sel.name, sel.mode)
}

func resolveTelegramMark(base uint) (uint, bool) {
	if base&uint(config.TelegramSetBit) == 0 {
		return base, false
	}
	switch p := currentTelegramPath(); p.kind {
	case telegramPathStrategy, telegramPathProxy:
		return base, true
	case telegramPathInterface:
		return uint(config.SelfDialRelayMark | p.mark), true
	}
	return base &^ uint(config.TelegramSetBit), false
}

func dialMarkFor(cfg *config.Config) uint {
	base := coverDialMark()
	sel := telegramSel.Load()
	if base == 0 || sel == nil || cfg == nil || cfg.System.MTProto.UpstreamSet != sel.id {
		return base
	}
	return base | uint(config.TelegramSetBit)
}

func appliedTelegramSet(mark uint) string {
	sel := telegramSel.Load()
	if sel == nil || mark&uint(config.TelegramSetBit) == 0 || currentTelegramPath().kind == telegramPathNone {
		return ""
	}
	return sel.name
}

func coverDialMark() uint {
	return selfDialMark() &^ uint(config.TelegramSetBit)
}

func dialTelegramVia(ctx context.Context, mark uint, addr string, timeout time.Duration) (net.Conn, bool, error) {
	if mark&uint(config.TelegramSetBit) == 0 {
		return nil, false, nil
	}
	p := currentTelegramPath()
	hooks := telegramHooks.Load()
	if p.kind != telegramPathProxy || hooks == nil || hooks.via == nil {
		return nil, false, nil
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, true, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, true, fmt.Errorf("port of %s: %w", addr, err)
	}
	return dialViaWithin(ctx, hooks.via, p.setID, host, port, timeout)
}

type telegramViaTimeout struct{}

func (telegramViaTimeout) Error() string   { return "timed out" }
func (telegramViaTimeout) Timeout() bool   { return true }
func (telegramViaTimeout) Temporary() bool { return true }

var errTelegramViaTimeout error = telegramViaTimeout{}

func dialViaWithin(ctx context.Context, via TelegramSetDialer, setID, host string, port int, timeout time.Duration) (net.Conn, bool, error) {
	type result struct {
		conn    net.Conn
		handled bool
		err     error
	}
	by := time.Now().Add(max(timeout, time.Millisecond))
	if d, ok := ctx.Deadline(); ok && d.Before(by) {
		by = d
	}
	done := make(chan result, 1)
	go func() {
		conn, handled, err := via.DialViaSetBy(setID, host, port, by)
		done <- result{conn, handled, err}
	}()
	timer := time.NewTimer(max(time.Until(by), time.Millisecond))
	defer timer.Stop()
	abandon := func(cause error) (net.Conn, bool, error) {
		go func() {
			if r := <-done; r.conn != nil {
				_ = r.conn.Close()
			}
		}()
		return nil, true, fmt.Errorf("dial %s through set %s: %w", net.JoinHostPort(host, strconv.Itoa(port)), setID, cause)
	}
	select {
	case r := <-done:
		return r.conn, r.handled, r.err
	case <-timer.C:
		return abandon(errTelegramViaTimeout)
	case <-ctx.Done():
		return abandon(ctx.Err())
	}
}

func dialTelegramTCP(ctx context.Context, network, addr string, mark uint, timeout time.Duration) (net.Conn, error) {
	if conn, handled, err := dialTelegramVia(ctx, mark, addr, timeout); handled {
		return conn, err
	}
	if mark&uint(config.TelegramSetBit) != 0 && currentTelegramPath().kind != telegramPathStrategy {
		mark &^= uint(config.TelegramSetBit)
	}
	dialer := net.Dialer{Timeout: timeout, Control: netprobe.MarkControl(int(mark))}
	return dialer.DialContext(ctx, network, addr)
}

func telegramViaProxy(mark uint) bool {
	return mark&uint(config.TelegramSetBit) != 0 && currentTelegramPath().kind == telegramPathProxy
}

func telegramHTTPClient(timeout time.Duration) *http.Client {
	return netprobe.HTTPClientDial(func(ctx context.Context, network, addr string) (net.Conn, error) {
		mark, _ := resolveTelegramMark(selfDialMark())
		if network == "tcp" {
			network = dialNetwork(mark)
		}
		return dialTelegramTCP(ctx, network, addr, mark, timeout)
	}, timeout)
}

func DialTelegramDirect(ctx context.Context, dest string) (net.Conn, bool, error) {
	base := selfDialMark()
	if base&uint(config.TelegramSetBit) == 0 {
		return nil, false, nil
	}
	mark, final := resolveTelegramMark(base)
	if !final {
		return nil, false, nil
	}
	timeout := tcpDialTimeout
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
	}
	conn, err := dialTelegramTCP(ctx, routeNetwork(mark), dest, mark, timeout)
	return conn, true, err
}
