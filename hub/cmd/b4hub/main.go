package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/api"
	"github.com/daniellavrushin/b4hub/internal/asn"
	"github.com/daniellavrushin/b4hub/internal/catalogue"
	"github.com/daniellavrushin/b4hub/internal/geo"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/ingest"
	"github.com/daniellavrushin/b4hub/internal/mirror"
	"github.com/daniellavrushin/b4hub/internal/ratelimit"
	"github.com/daniellavrushin/b4hub/internal/store"
	"github.com/daniellavrushin/b4hub/internal/web"
)

var Version = "dev"

const (
	defaultData   = "data"
	defaultListen = "127.0.0.1:7100"

	envData          = "B4HUB_DATA"
	envListen        = "B4HUB_LISTEN"
	envPublicURL     = "B4HUB_PUBLIC_URL"
	envGeoSiteURL    = "B4HUB_GEOSITE_URL"
	envGeoIPURL      = "B4HUB_GEOIP_URL"
	envAdminPassword = "B4HUB_ADMIN_PASSWORD"
	envUpstream      = "B4HUB_UPSTREAM"
	envUpstreamKey   = "B4HUB_UPSTREAM_KEY"

	shutdownGrace = 10 * time.Second
)

func usage() {
	fmt.Fprintf(os.Stderr, `b4hub %s

usage:
  b4hub keygen   [-data DIR]
  b4hub serve    [-data DIR] [-listen ADDR] [-public-url URL] [-geosite-url URL] [-geoip-url URL]
  b4hub build    [-data DIR] [-public-url URL] [-new-epoch] [-revoke KEYID]
  b4hub moderate [-data DIR] list | approve <id> | reject <id> <reason> | hide <id> <reason> | ban <key_hmac> [reason]
  b4hub moderate [-data DIR] mirrors | approve-mirror <id> | reject-mirror <id> <reason>
  b4hub mirror   -upstream URL [-upstream-key KEYID] [-data DIR] [-listen ADDR] [-public-url URL] [-announce] [-refresh 5m]

environment: %s %s %s %s %s %s %s %s
`, Version, envData, envListen, envPublicURL, envGeoSiteURL, envGeoIPURL, envAdminPassword, envUpstream, envUpstreamKey)
	os.Exit(2)
}

func envOr(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}

func dataFlag(fs *flag.FlagSet) *string {
	return fs.String("data", envOr(envData, defaultData), "data directory")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "b4hub:", err)
	os.Exit(1)
}

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	if len(os.Args) < 2 {
		usage()
	}
	args := os.Args[2:]
	switch os.Args[1] {
	case "keygen":
		runKeygen(args)
	case "serve":
		runServe(args)
	case "build":
		runBuild(args)
	case "moderate":
		runModerate(args)
	case "mirror":
		runMirror(args)
	case "version":
		fmt.Println(Version)
	default:
		usage()
	}
}

func runKeygen(args []string) {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	data := dataFlag(fs)
	_ = fs.Parse(args)
	layout := hubdata.Layout{Root: *data}
	if err := layout.EnsureDirs(); err != nil {
		fatal(err)
	}
	id, err := hubwire.NewIdentity()
	if err != nil {
		fatal(err)
	}
	if err := layout.WriteIdentity(id, false); err != nil {
		if errors.Is(err, hubdata.ErrIdentityPresent) {
			fatal(fmt.Errorf("%s already exists, refusing to overwrite it", layout.KeyPath()))
		}
		fatal(err)
	}
	fmt.Println(id.KeyID())
}

type services struct {
	layout   hubdata.Layout
	identity *hubwire.Identity
	secret   []byte
	store    *store.Store
}

func openServices(data string) *services {
	layout := hubdata.Layout{Root: data}
	if err := layout.EnsureDirs(); err != nil {
		fatal(err)
	}
	id, err := layout.LoadIdentity()
	if err != nil {
		fatal(err)
	}
	secret, err := layout.LoadOrCreateSecret()
	if err != nil {
		fatal(err)
	}
	st, err := store.Open(layout.DBPath())
	if err != nil {
		fatal(err)
	}
	return &services{layout: layout, identity: id, secret: secret, store: st}
}

func (s *services) builder(publicURL string, sources []hubwire.GeoSource) *catalogue.Builder {
	return &catalogue.Builder{
		Store:      s.store,
		Identity:   s.identity,
		PublicDir:  s.layout.Public(),
		PublicURL:  publicURL,
		GeoSources: sources,
		Mirrors:    &catalogue.MirrorHealth{Store: s.store, KeyID: s.identity.KeyID()},
	}
}

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	data := dataFlag(fs)
	listen := fs.String("listen", envOr(envListen, defaultListen), "listen address")
	publicURL := fs.String("public-url", envOr(envPublicURL, ""), "public base URL advertised as a mirror")
	geoSiteURL := fs.String("geosite-url", envOr(envGeoSiteURL, geo.DefaultGeoSiteURL), "geosite.dat source")
	geoIPURL := fs.String("geoip-url", envOr(envGeoIPURL, geo.DefaultGeoIPURL), "geoip.dat source")
	_ = fs.Parse(args)

	svc := openServices(*data)
	defer svc.store.Close()
	log.Printf("b4hub %s, key id %s, data %s", Version, svc.identity.KeyID(), svc.layout.Root)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	geoService := geo.New(geo.Options{Dir: svc.layout.Geo(), GeoSiteURL: *geoSiteURL, GeoIPURL: *geoIPURL})
	index := geo.NewIndex(geoService.GeoSitePath())
	builder := svc.builder(*publicURL, geoService.Sources())
	builder.OnBuild = func(result *catalogue.Result) {
		categories := make([]string, 0)
		for i := range result.Catalogue.Sets {
			categories = append(categories, store.TargetList(result.Catalogue.Sets[i].Set, "geosite_categories")...)
		}
		go func() {
			if err := index.Warm(categories); err != nil {
				log.Printf("geo: category index: %v", err)
			}
		}()
	}
	if err := builder.LoadPublished(); err != nil && !errors.Is(err, catalogue.ErrNotPublished) {
		log.Printf("catalogue: published files ignored: %v", err)
	}

	resolver := asn.New(nil, svc.store, nil)
	server := &api.Server{
		Store:     svc.store,
		Blobs:     svc.layout.Blobs(),
		PublicDir: svc.layout.Public(),
		Ingest: &ingest.Service{
			Store:   svc.store,
			Blobs:   svc.layout.Blobs(),
			Secret:  svc.secret,
			Limiter: ratelimit.New(nil),
			ASN:     resolver,
		},
		Catalogue:     builder,
		Geo:           index,
		AdminPassword: os.Getenv(envAdminPassword),
	}

	site := &web.Server{
		Store:         svc.store,
		Blobs:         svc.layout.Blobs(),
		Catalogue:     builder,
		ASN:           resolver,
		AdminPassword: server.AdminPassword,
		Rebuild: func() error {
			result, built, err := builder.BuildIfNeeded(context.Background())
			if built {
				log.Printf("catalogue: published %s with %d sets after moderation", result.Manifest.Catalogue.File, len(result.Catalogue.Sets))
			}
			return err
		},
	}
	mux := server.Router()
	site.Mount(mux)

	httpServer := newHTTPServer(*listen, mux)

	go geoService.RunDaily(ctx)
	go builder.Run(ctx, catalogue.DefaultInterval)
	serveUntilSignal(httpServer, cancel)
}

func newHTTPServer(listen string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

func serveUntilSignal(httpServer *http.Server, cancel context.CancelFunc) {
	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on %s", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	select {
	case sig := <-signals:
		log.Printf("received %s, shutting down", sig)
	case err := <-errCh:
		log.Printf("listener failed: %v", err)
	}
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

func runMirror(args []string) {
	fs := flag.NewFlagSet("mirror", flag.ExitOnError)
	data := dataFlag(fs)
	listen := fs.String("listen", envOr(envListen, defaultListen), "listen address")
	upstream := fs.String("upstream", envOr(envUpstream, ""), "central hub base URL")
	upstreamKey := fs.String("upstream-key", envOr(envUpstreamKey, ""), "central hub key id, the built-in key when empty")
	publicURL := fs.String("public-url", envOr(envPublicURL, ""), "public base URL of this mirror, announced to the central hub")
	announce := fs.Bool("announce", false, "announce this mirror to the central hub on start and daily")
	refresh := fs.Duration("refresh", mirror.DefaultRefresh, "how often to check the upstream manifest")
	_ = fs.Parse(args)
	if strings.TrimSpace(*upstream) == "" {
		fatal(errors.New("-upstream is required"))
	}
	layout := hubdata.Layout{Root: *data}
	if err := layout.EnsureDirs(); err != nil {
		fatal(err)
	}
	identity, err := layout.LoadIdentity()
	if err != nil && !errors.Is(err, hubdata.ErrNoIdentity) {
		fatal(err)
	}
	if *announce {
		if identity == nil {
			fatal(fmt.Errorf("-announce needs a mirror identity, run b4hub keygen -data %s first", *data))
		}
		if strings.TrimSpace(*publicURL) == "" {
			fatal(errors.New("-announce needs -public-url"))
		}
	}
	var trusted []string
	if *upstreamKey != "" {
		if _, err := hubwire.DecodeKey(*upstreamKey); err != nil {
			fatal(fmt.Errorf("-upstream-key: %w", err))
		}
		trusted = []string{strings.TrimSpace(*upstreamKey)}
	}
	svc, err := mirror.New(mirror.Options{
		Upstream:    *upstream,
		TrustedKeys: trusted,
		Layout:      layout,
		PublicURL:   strings.TrimRight(strings.TrimSpace(*publicURL), "/"),
		Identity:    identity,
		Announce:    *announce,
		Version:     Version,
		Refresh:     *refresh,
	})
	if err != nil {
		fatal(err)
	}
	keyID := "no identity"
	if identity != nil {
		keyID = identity.KeyID()
	}
	log.Printf("b4hub %s mirror of %s, key id %s, data %s", Version, svc.Status().Upstream, keyID, layout.Root)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go svc.Run(ctx)
	serveUntilSignal(newHTTPServer(*listen, svc.Router()), cancel)
}

func runBuild(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	data := dataFlag(fs)
	publicURL := fs.String("public-url", envOr(envPublicURL, ""), "public base URL advertised as a mirror")
	geoSiteURL := fs.String("geosite-url", envOr(envGeoSiteURL, geo.DefaultGeoSiteURL), "geosite.dat source")
	geoIPURL := fs.String("geoip-url", envOr(envGeoIPURL, geo.DefaultGeoIPURL), "geoip.dat source")
	newEpoch := fs.Bool("new-epoch", false, "start a new epoch before building")
	revoke := fs.String("revoke", "", "add a key id to the revoked list carried by every manifest")
	_ = fs.Parse(args)

	svc := openServices(*data)
	defer svc.store.Close()
	ctx := context.Background()
	if keyID := strings.TrimSpace(*revoke); keyID != "" {
		if _, err := hubwire.DecodeKey(keyID); err != nil {
			fatal(fmt.Errorf("-revoke: %w", err))
		}
		if err := svc.store.RevokeKey(ctx, keyID); err != nil {
			fatal(err)
		}
		fmt.Printf("revoked %s\n", keyID)
	}
	if *newEpoch {
		epoch, err := svc.store.NewEpoch(ctx, time.Now())
		if err != nil {
			fatal(err)
		}
		fmt.Printf("epoch %d\n", epoch)
	}
	sources := []hubwire.GeoSource{{SiteURL: *geoSiteURL, IPURL: *geoIPURL}}
	result, err := svc.builder(*publicURL, sources).Build(ctx)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("%s: %d sets, %d blobs, expires %s\n", result.Manifest.Catalogue.File, len(result.Catalogue.Sets), len(result.Catalogue.Blobs), result.Manifest.ExpiresAt)
}

func parseVersionRef(ref string) (string, int, error) {
	id, rest, hasVersion := strings.Cut(ref, "/")
	if !hubdata.ValidSetID(id) {
		return "", 0, fmt.Errorf("%q is not a set id", id)
	}
	if !hasVersion {
		return id, 0, nil
	}
	version, err := strconv.Atoi(rest)
	if err != nil || version <= 0 {
		return "", 0, fmt.Errorf("%q is not a version number", rest)
	}
	return id, version, nil
}

func resolveVersion(ctx context.Context, st *store.Store, ref, wantStatus string) (*store.Version, error) {
	id, version, err := parseVersionRef(ref)
	if err != nil {
		return nil, err
	}
	if version > 0 {
		return st.GetVersion(ctx, id, version)
	}
	_, versions, err := st.GetSet(ctx, id)
	if err != nil {
		return nil, err
	}
	for i := len(versions) - 1; i >= 0; i-- {
		if wantStatus == "" || versions[i].Status == wantStatus {
			return &versions[i], nil
		}
	}
	return nil, fmt.Errorf("set %s has no %s version, name one as %s/<version>", id, wantStatus, id)
}

func runModerate(args []string) {
	fs := flag.NewFlagSet("moderate", flag.ExitOnError)
	data := dataFlag(fs)
	_ = fs.Parse(args)
	rest := fs.Args()
	if len(rest) == 0 {
		usage()
	}
	svc := openServices(*data)
	defer svc.store.Close()
	ctx := context.Background()
	now := time.Now()

	need := func(n int) {
		if len(rest) < n {
			usage()
		}
	}
	switch rest[0] {
	case "list":
		pending, err := svc.store.PendingVersions(ctx)
		if err != nil {
			fatal(err)
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tVERSION\tTITLE\tFAMILY\tFLAGS\tAUTHOR\tASN\tRECEIVED")
		for _, v := range pending {
			fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%s\t%s\t%s\n", v.SetID, v.Version, v.Title, v.Family, strings.Join(v.Flags, ","), hubdata.AuthorLabel(v.UploaderHMAC), v.ASNObserved, v.CreatedAt.Format(time.RFC3339))
		}
		tw.Flush()
	case "approve":
		need(2)
		v, err := resolveVersion(ctx, svc.store, rest[1], hubwire.SetStatusPending)
		if err != nil {
			fatal(err)
		}
		if err := svc.store.Approve(ctx, v.SetID, v.Version, now); err != nil {
			fatal(err)
		}
		fmt.Printf("approved %s/%d\n", v.SetID, v.Version)
	case "reject":
		need(3)
		v, err := resolveVersion(ctx, svc.store, rest[1], hubwire.SetStatusPending)
		if err != nil {
			fatal(err)
		}
		if err := svc.store.Reject(ctx, v.SetID, v.Version, strings.Join(rest[2:], " "), now); err != nil {
			fatal(err)
		}
		fmt.Printf("rejected %s/%d\n", v.SetID, v.Version)
	case "hide":
		need(3)
		v, err := resolveVersion(ctx, svc.store, rest[1], hubwire.SetStatusActive)
		if err != nil {
			fatal(err)
		}
		if err := svc.store.Hide(ctx, v.SetID, v.Version, strings.Join(rest[2:], " "), now); err != nil {
			fatal(err)
		}
		fmt.Printf("hidden %s/%d\n", v.SetID, v.Version)
	case "ban":
		need(2)
		reason := "banned by moderator"
		if len(rest) > 2 {
			reason = strings.Join(rest[2:], " ")
		}
		if err := svc.store.BanKey(ctx, rest[1], reason, now); err != nil {
			fatal(err)
		}
		fmt.Printf("banned %s\n", rest[1])
	case "mirrors":
		mirrors, err := svc.store.Mirrors(ctx)
		if err != nil {
			fatal(err)
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tURL\tKEY\tSTATUS\tFIRST SEEN\tLAST SEEN\tLAST CHECK\tLAST OK\tREASON")
		for _, m := range mirrors {
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", m.ID, m.URL, hubdata.AuthorLabel(m.KeyHMAC), m.Status, stamp(m.FirstSeen), stamp(m.LastSeen), stamp(m.LastCheck), stamp(m.LastOK), m.Reason)
		}
		tw.Flush()
	case "approve-mirror":
		need(2)
		id := parseMirrorID(rest[1])
		if err := svc.store.SetMirrorStatus(ctx, id, store.MirrorApproved, "", now); err != nil {
			fatal(err)
		}
		fmt.Printf("approved mirror %d\n", id)
	case "reject-mirror":
		need(3)
		id := parseMirrorID(rest[1])
		if err := svc.store.SetMirrorStatus(ctx, id, store.MirrorRejected, strings.Join(rest[2:], " "), now); err != nil {
			fatal(err)
		}
		fmt.Printf("rejected mirror %d\n", id)
	default:
		usage()
	}
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format(time.RFC3339)
}

func parseMirrorID(raw string) int64 {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		fatal(fmt.Errorf("%q is not a mirror id", raw))
	}
	return id
}
