package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/mirror"
	"github.com/spf13/cobra"
)

var mirrorFlags struct {
	listen      string
	upstream    string
	upstreamKey string
	publicURL   string
	announce    bool
	refresh     time.Duration
}

var mirrorCmd = &cobra.Command{
	Use:   "mirror",
	Short: "Run a child hub: mirror the upstream catalogue and relay signed records",
	Args:  cobra.NoArgs,
	RunE:  runMirror,
}

func init() {
	f := mirrorCmd.Flags()
	f.StringVar(&mirrorFlags.listen, "listen", envOr(envListen, defaultListen), "listen address")
	f.StringVar(&mirrorFlags.upstream, "upstream", envOr(envUpstream, ""), "central hub base URL")
	f.StringVar(&mirrorFlags.upstreamKey, "upstream-key", envOr(envUpstreamKey, ""), "central hub key id, the built-in key when empty")
	f.StringVar(&mirrorFlags.publicURL, "public-url", envOr(envPublicURL, ""), "public base URL of this mirror, announced to the central hub")
	f.BoolVar(&mirrorFlags.announce, "announce", false, "announce this mirror to the central hub on start and daily")
	f.DurationVar(&mirrorFlags.refresh, "refresh", mirror.DefaultRefresh, "how often to check the upstream manifest")
}

func runMirror(cmd *cobra.Command, args []string) error {
	if strings.TrimSpace(mirrorFlags.upstream) == "" {
		return errors.New("--upstream is required")
	}
	layout := hubdata.Layout{Root: dataDir}
	if err := layout.EnsureDirs(); err != nil {
		return err
	}
	identity, err := layout.LoadIdentity()
	if err != nil && !errors.Is(err, hubdata.ErrNoIdentity) {
		return err
	}
	if mirrorFlags.announce {
		if identity == nil {
			return fmt.Errorf("--announce needs a mirror identity, run b4hub keygen --data %s first", dataDir)
		}
		if strings.TrimSpace(mirrorFlags.publicURL) == "" {
			return errors.New("--announce needs --public-url")
		}
	}
	var trusted []string
	if mirrorFlags.upstreamKey != "" {
		if _, err := hubwire.DecodeKey(mirrorFlags.upstreamKey); err != nil {
			return fmt.Errorf("--upstream-key: %w", err)
		}
		trusted = []string{strings.TrimSpace(mirrorFlags.upstreamKey)}
	}
	svc, err := mirror.New(mirror.Options{
		Upstream:    mirrorFlags.upstream,
		TrustedKeys: trusted,
		Layout:      layout,
		PublicURL:   strings.TrimRight(strings.TrimSpace(mirrorFlags.publicURL), "/"),
		Identity:    identity,
		Announce:    mirrorFlags.announce,
		Version:     Version,
		Refresh:     mirrorFlags.refresh,
	})
	if err != nil {
		return err
	}
	keyID := "no identity"
	if identity != nil {
		keyID = identity.KeyID()
	}
	log.Printf("b4hub %s mirror of %s, key id %s, data %s", Version, svc.Status().Upstream, keyID, layout.Root)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go svc.Run(ctx)
	serveUntilSignal(newHTTPServer(mirrorFlags.listen, svc.Router()), cancel)
	return nil
}
