package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/catalogue"
	"github.com/daniellavrushin/b4hub/internal/geo"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/store"
	"github.com/spf13/cobra"
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

var dataDir string

var rootCmd = &cobra.Command{
	Use:   "b4hub",
	Short: "b4 community hub",
	Long: `b4hub publishes the signed catalogue of community sets, accepts shares, votes
and reports from b4 routers, and serves the moderation console at /admin.

Every flag has an environment variable counterpart used as its default:
  --data          ` + envData + `
  --listen        ` + envListen + `
  --public-url    ` + envPublicURL + `
  --geosite-url   ` + envGeoSiteURL + `
  --geoip-url     ` + envGeoIPURL + `
  --upstream      ` + envUpstream + `
  --upstream-key  ` + envUpstreamKey + `
The moderation password is read only from ` + envAdminPassword + `.`,
	Version:       Version,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	rootCmd.PersistentFlags().StringVar(&dataDir, "data", envOr(envData, defaultData), "data directory")
	rootCmd.AddCommand(keygenCmd, serveCmd, buildCmd, moderateCmd, mirrorCmd, versionCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "b4hub:", err)
		os.Exit(1)
	}
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version and exit",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(Version)
	},
}

var keygenCmd = &cobra.Command{
	Use:   "keygen",
	Short: "Create the hub signing identity",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		layout := hubdata.Layout{Root: dataDir}
		if err := layout.EnsureDirs(); err != nil {
			return err
		}
		id, err := hubwire.NewIdentity()
		if err != nil {
			return err
		}
		if err := layout.WriteIdentity(id, false); err != nil {
			if errors.Is(err, hubdata.ErrIdentityPresent) {
				return fmt.Errorf("%s already exists, refusing to overwrite it", layout.KeyPath())
			}
			return err
		}
		fmt.Println(id.KeyID())
		return nil
	},
}

func envOr(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}

type services struct {
	layout   hubdata.Layout
	identity *hubwire.Identity
	secret   []byte
	store    *store.Store
}

func openServices(data string) (*services, error) {
	layout := hubdata.Layout{Root: data}
	if err := layout.EnsureDirs(); err != nil {
		return nil, err
	}
	id, err := layout.LoadIdentity()
	if err != nil {
		return nil, err
	}
	secret, err := layout.LoadOrCreateSecret()
	if err != nil {
		return nil, err
	}
	st, err := store.Open(layout.DBPath())
	if err != nil {
		return nil, err
	}
	return &services{layout: layout, identity: id, secret: secret, store: st}, nil
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

type geoFlags struct {
	publicURL  string
	geoSiteURL string
	geoIPURL   string
}

func bindGeoFlags(cmd *cobra.Command, f *geoFlags) {
	cmd.Flags().StringVar(&f.publicURL, "public-url", envOr(envPublicURL, ""), "public base URL advertised as a mirror")
	cmd.Flags().StringVar(&f.geoSiteURL, "geosite-url", envOr(envGeoSiteURL, geo.DefaultGeoSiteURL), "geosite.dat source")
	cmd.Flags().StringVar(&f.geoIPURL, "geoip-url", envOr(envGeoIPURL, geo.DefaultGeoIPURL), "geoip.dat source")
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format(time.RFC3339)
}
