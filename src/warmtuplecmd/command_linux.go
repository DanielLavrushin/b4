package warmtuplecmd

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/nfq"
	"github.com/daniellavrushin/b4/socks5"
	"github.com/daniellavrushin/b4/warmtuple"
	"github.com/spf13/cobra"
)

type options struct {
	host, target, source, listen string
	first, last, queue           uint16
	duration                     time.Duration
}

func NewCommand() *cobra.Command {
	o := &options{}
	c := &cobra.Command{
		Use: "warm-tuple", Short: "Experimental same-tuple warm/pass SOCKS5 relay in an isolated Linux namespace",
		Args: cobra.NoArgs, SilenceUsage: true,
		RunE: func(c *cobra.Command, _ []string) error { return o.run(c) },
	}
	f := c.Flags()
	f.StringVar(&o.host, "host", "", "Exact TLS/SOCKS5 hostname")
	f.StringVar(&o.target, "target", "", "Frozen IPv4:port, no DNS lookup")
	f.StringVar(&o.source, "source", "", "Explicit local source IPv4")
	f.Uint16Var(&o.first, "first-port", 0, "First leased source port")
	f.Uint16Var(&o.last, "last-port", 0, "Last leased source port, at most 32 ports")
	f.Uint16Var(&o.queue, "queue", 0, "Dedicated NFQUEUE number, firewall externally managed")
	f.StringVar(&o.listen, "listen", "127.0.0.1:1089", "Loopback SOCKS5 IPv4:port")
	f.DurationVar(&o.duration, "duration", time.Minute, "Experiment lifetime, at most 5m")
	for _, name := range []string{"host", "target", "source", "first-port", "last-port", "queue"} {
		_ = c.MarkFlagRequired(name)
	}
	return c
}

func (o options) parse() (warmtuple.Config, config.Socks5Config, error) {
	var cfg warmtuple.Config
	var proxy config.Socks5Config
	if o.queue == 0 || o.duration <= 0 || o.duration > 5*time.Minute {
		return cfg, proxy, errors.New("dedicated nonzero queue and duration up to 5m are required")
	}
	target, err := netip.ParseAddrPort(o.target)
	if err != nil {
		return cfg, proxy, err
	}
	source, err := netip.ParseAddr(o.source)
	if err != nil {
		return cfg, proxy, err
	}
	listen, err := netip.ParseAddrPort(o.listen)
	if err != nil || listen.Addr() != netip.MustParseAddr("127.0.0.1") || listen.Port() == 0 {
		return cfg, proxy, errors.New("SOCKS5 must bind to an explicit 127.0.0.1 port")
	}
	cfg = warmtuple.Config{
		Host: o.host, Target: target, Source: source, FirstPort: o.first, LastPort: o.last,
		WarmTimeout: 3 * time.Second, ReuseTimeout: 5 * time.Second,
		RetryInterval: 50 * time.Millisecond, PassLifetime: time.Minute,
	}
	proxy = config.Socks5Config{Enabled: true, BindAddress: listen.Addr().String(), Port: int(listen.Port())}
	return cfg, proxy, nil
}

func requireNamespace() error {
	self, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		return err
	}
	init, err := os.Readlink("/proc/1/ns/net")
	if err != nil {
		return err
	}
	if self == init {
		return errors.New("experimental warm-tuple requires an isolated Linux network namespace")
	}
	return nil
}

func (o options) run(cmd *cobra.Command) error {
	profile, proxy, err := o.parse()
	if err != nil {
		return err
	}
	m, err := warmtuple.New(profile)
	if err != nil {
		return err
	}
	defer m.Close()
	if err := requireNamespace(); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, o.duration)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg := warmtuple.QueueConfig(profile.Target, o.queue)
	cfg.System.Socks5 = proxy
	p := nfq.NewPool(&cfg)
	defer func() {
		m.Close()
		p.Stop()
	}()
	if err := p.Start(); err != nil {
		return err
	}
	var logMu sync.Mutex
	encoder := json.NewEncoder(cmd.ErrOrStderr())
	emit := func(event map[string]any) {
		logMu.Lock()
		defer logMu.Unlock()
		_ = encoder.Encode(event)
	}
	s := socks5.NewServerWithConnector(&cfg, func(ctx context.Context, dest string) (net.Conn, error) {
		c, err := m.DialContext(ctx, dest)
		event := map[string]any{"event": "connect", "destination": dest}
		if err != nil {
			event["error"] = err.Error()
		} else {
			pass := c.(*warmtuple.Conn)
			event["local"], event["remote"] = pass.LocalAddr().String(), pass.RemoteAddr().String()
			event["warm_ms"], event["reuse_ms"] = pass.WarmDuration.Milliseconds(), pass.ReuseDuration.Milliseconds()
		}
		emit(event)
		return c, err
	})
	if err := s.Start(); err != nil {
		return err
	}
	defer s.Stop()
	emit(map[string]any{"event": "ready", "listen": o.listen, "host": o.host, "target": profile.Target.String(), "firewall": "external"})
	<-ctx.Done()
	return nil
}
