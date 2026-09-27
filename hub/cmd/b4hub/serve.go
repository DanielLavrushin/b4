package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/api"
	"github.com/daniellavrushin/b4hub/internal/asn"
	"github.com/daniellavrushin/b4hub/internal/catalogue"
	"github.com/daniellavrushin/b4hub/internal/geo"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/ingest"
	"github.com/daniellavrushin/b4hub/internal/notify"
	"github.com/daniellavrushin/b4hub/internal/ratelimit"
	"github.com/daniellavrushin/b4hub/internal/web"
	"github.com/spf13/cobra"
)

var serveFlags struct {
	geoFlags
	listen         string
	trustedProxies string
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the hub: catalogue, ingest API and the moderation console",
	Args:  cobra.NoArgs,
	RunE:  runServe,
}

func init() {
	serveCmd.Flags().StringVar(&serveFlags.listen, "listen", envOr(envListen, defaultListen), "listen address")
	bindTrustedProxies(serveCmd, &serveFlags.trustedProxies)
	bindGeoFlags(serveCmd, &serveFlags.geoFlags)
}

func runServe(cmd *cobra.Command, args []string) error {
	if err := asn.SetTrustedProxies(serveFlags.trustedProxies); err != nil {
		return err
	}
	svc, err := openServices(dataDir)
	if err != nil {
		return err
	}
	defer svc.store.Close()
	log.Printf("b4hub %s, key id %s, data %s", versionString(), svc.identity.KeyID(), svc.layout.Root)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	geoService := geo.New(geo.Options{Dir: svc.layout.Geo(), GeoSiteURL: serveFlags.geoSiteURL, GeoIPURL: serveFlags.geoIPURL})
	builder := svc.builder(serveFlags.publicURL, geoService.Sources())
	if err := builder.LoadPublished(); err != nil && !errors.Is(err, catalogue.ErrNotPublished) {
		log.Printf("catalogue: published files ignored: %v", err)
	}

	notifier := &notify.Service{Store: svc.store, Secret: svc.secret, PublicURL: serveFlags.publicURL}
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
			OnAccepted: func(kind string) {
				if kind == hubwire.RecordShare || kind == hubwire.RecordReport {
					notifier.Nudge()
				}
			},
		},
		Catalogue: builder,
	}

	site := &web.Server{
		Store:         svc.store,
		Blobs:         svc.layout.Blobs(),
		Catalogue:     builder,
		Geo:           geoService,
		ASN:           resolver,
		Secret:        svc.secret,
		AdminPassword: os.Getenv(envAdminPassword),
		Version:       Version,
		Source:        Source,
		KeyID:         svc.identity.KeyID(),
		PublicURL:     serveFlags.publicURL,
		Notify:        notifier,
	}
	mux := server.Router()
	site.Mount(mux)

	go geoService.RunDaily(ctx)
	go builder.Run(ctx, catalogue.DefaultInterval)
	go builder.Mirrors.Run(ctx, catalogue.DefaultMirrorCheckInterval, func() {
		drift, err := builder.MirrorsDrift(ctx)
		if err != nil || !drift {
			return
		}
		if err := svc.store.MarkDirty(ctx); err != nil {
			log.Printf("mirrors: %v", err)
			return
		}
		builder.Request(catalogue.TriggerMirrors)
	})
	go maintain(ctx, svc, hubdata.SweepMinAge)
	go notifier.Run(ctx)
	serveUntilSignal(newHTTPServer(serveFlags.listen, mux), cancel)
	return nil
}

const (
	unchangedBuildsKept = 14 * 24 * time.Hour
	buildsKept          = 365 * 24 * time.Hour
)

func maintain(ctx context.Context, svc *services, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		referenced, err := svc.store.ReferencedBlobs(ctx)
		if err != nil {
			log.Printf("blobs: sweep skipped: %v", err)
			continue
		}
		removed, err := svc.layout.Blobs().Sweep(referenced, hubdata.SweepMinAge, time.Now())
		if err != nil {
			log.Printf("blobs: sweep: %v", err)
		}
		if len(removed) > 0 {
			log.Printf("blobs: removed %d unreferenced payload(s)", len(removed))
		}
		if err := svc.store.PruneBuilds(ctx, time.Now(), unchangedBuildsKept, buildsKept); err != nil {
			log.Printf("builds: prune: %v", err)
		}
	}
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
