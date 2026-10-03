package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/asn"
	"github.com/daniellavrushin/b4hub/internal/catalogue"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/ingest"
	"github.com/daniellavrushin/b4hub/internal/moderation"
	"github.com/daniellavrushin/b4hub/internal/ratelimit"
	"github.com/daniellavrushin/b4hub/internal/store"
)

type options struct {
	data      string
	publicURL string
	reset     bool
	days      int
	seed      uint64
}

func main() {
	var o options
	flag.StringVar(&o.data, "data", "data", "hub data directory to fill")
	flag.StringVar(&o.publicURL, "public-url", "http://127.0.0.1:7100", "public URL the hub will be served at")
	flag.BoolVar(&o.reset, "reset", false, "replace an existing database and published catalogue; the hub key and secret are kept")
	flag.IntVar(&o.days, "days", 45, "days of history to generate, at least 30")
	flag.Uint64Var(&o.seed, "seed", 1, "random seed")
	flag.Parse()
	if err := run(o); err != nil {
		fmt.Fprintln(os.Stderr, "b4hub-seed:", err)
		os.Exit(1)
	}
}

func run(o options) error {
	if o.days < 30 {
		return errors.New("--days must be at least 30")
	}
	base, err := url.Parse(o.publicURL)
	if err != nil || base.Host == "" {
		return fmt.Errorf("--public-url %q is not a URL", o.publicURL)
	}
	layout := hubdata.Layout{Root: o.data}
	id, err := preflight(layout, o)
	if err != nil {
		return err
	}
	if err := layout.EnsureDirs(); err != nil {
		return err
	}
	if id == nil {
		if id, err = createIdentity(layout); err != nil {
			return err
		}
	}
	if o.reset {
		if err := reset(layout); err != nil {
			return err
		}
	}
	secret, err := layout.LoadOrCreateSecret()
	if err != nil {
		return err
	}
	st, err := store.Open(layout.DBPath())
	if err != nil {
		return err
	}
	defer st.Close()

	end := time.Now().UTC()
	start := end.Add(-time.Duration(o.days) * 24 * time.Hour).Truncate(time.Hour)
	c := &clock{t: start}
	w := newWorld(rand.New(rand.NewPCG(o.seed, o.seed^0x9e3779b97f4a7c15)))
	s := &seeder{
		ctx:     context.Background(),
		st:      st,
		secret:  secret,
		clock:   c,
		rng:     w.rng,
		world:   w,
		start:   start,
		end:     end,
		days:    o.days,
		port:    base.Port(),
		console: moderation.Actor{Kind: store.ActorConsole, Ref: "5f3a9c1e", IP: "127.0.0.1"},
		cli:     moderation.Actor{Kind: store.ActorCLI, Ref: "hubadmin"},
		voted:   make(map[string]map[*router]bool),
		banned:  make(map[*router]bool),
		stats:   make(map[string]int),
	}
	s.ingest = &ingest.Service{
		Store:      st,
		Blobs:      layout.Blobs(),
		Secret:     secret,
		Limiter:    ratelimit.New(c.now),
		ASN:        asn.New(w.lookup, st, c.now),
		Now:        c.now,
		OnAutoHide: func() { s.Request(catalogue.TriggerAutoHide) },
	}
	s.mod = &moderation.Service{Store: st, Builds: s, HubKeyID: id.KeyID(), Now: c.now}
	s.builder = &catalogue.Builder{
		Store:     st,
		Identity:  id,
		PublicDir: layout.Public(),
		PublicURL: o.publicURL,
		Mirrors:   &catalogue.MirrorHealth{Store: st, KeyID: id.KeyID(), Now: c.now},
		Now:       c.now,
	}
	s.story()
	if err := s.run(); err != nil {
		return err
	}
	return s.summary(layout, id)
}

func preflight(layout hubdata.Layout, o options) (*hubwire.Identity, error) {
	id, err := layout.LoadIdentity()
	if err != nil && !errors.Is(err, hubdata.ErrNoIdentity) {
		return nil, err
	}
	if id != nil && slices.Contains(hubwire.BuiltinHubKeys, id.KeyID()) {
		return nil, fmt.Errorf("%s holds a key built into b4; refusing to seed a production hub", layout.KeyPath())
	}
	if _, err := os.Stat(layout.DBPath()); err == nil {
		if !o.reset {
			return nil, fmt.Errorf("%s already exists; pass --reset to replace it", layout.DBPath())
		}
		if running(o.publicURL) {
			return nil, fmt.Errorf("a hub answers at %s; stop it before replacing its database", o.publicURL)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return id, nil
}

func reset(layout hubdata.Layout) error {
	for _, name := range []string{layout.DBPath(), layout.DBPath() + "-wal", layout.DBPath() + "-shm"} {
		if err := os.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	for _, dir := range []string{layout.Public(), layout.Blobs().Dir} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func running(publicURL string) bool {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(publicURL + hubwire.PathHealth)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

func createIdentity(layout hubdata.Layout) (*hubwire.Identity, error) {
	id, err := hubwire.NewIdentity()
	if err != nil {
		return nil, err
	}
	if err := layout.WriteIdentity(id, false); err != nil {
		return nil, err
	}
	return id, nil
}

type clock struct {
	t time.Time
}

func (c *clock) now() time.Time {
	return c.t
}

func (c *clock) set(t time.Time) {
	if t.After(c.t) {
		c.t = t
	}
}
