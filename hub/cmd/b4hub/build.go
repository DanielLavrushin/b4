package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/spf13/cobra"
)

var buildFlags struct {
	geoFlags
	newEpoch bool
	revoke   string
}

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build and sign the catalogue once",
	Args:  cobra.NoArgs,
	RunE:  runBuild,
}

func init() {
	bindGeoFlags(buildCmd, &buildFlags.geoFlags)
	buildCmd.Flags().BoolVar(&buildFlags.newEpoch, "new-epoch", false, "start a new epoch before building")
	buildCmd.Flags().StringVar(&buildFlags.revoke, "revoke", "", "add a key id to the revoked list carried by every manifest")
}

func runBuild(cmd *cobra.Command, args []string) error {
	svc, err := openServices(dataDir)
	if err != nil {
		return err
	}
	defer svc.store.Close()
	ctx := context.Background()
	if keyID := strings.TrimSpace(buildFlags.revoke); keyID != "" {
		if _, err := hubwire.DecodeKey(keyID); err != nil {
			return fmt.Errorf("--revoke: %w", err)
		}
		if err := svc.store.RevokeKey(ctx, keyID); err != nil {
			return err
		}
		fmt.Printf("revoked %s\n", keyID)
	}
	if buildFlags.newEpoch {
		epoch, err := svc.store.NewEpoch(ctx, time.Now())
		if err != nil {
			return err
		}
		fmt.Printf("epoch %d\n", epoch)
	}
	sources := []hubwire.GeoSource{{SiteURL: buildFlags.geoSiteURL, IPURL: buildFlags.geoIPURL}}
	result, err := svc.builder(buildFlags.publicURL, sources).Build(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("%s: %d sets, %d blobs, expires %s\n", result.Manifest.Catalogue.File, len(result.Catalogue.Sets), len(result.Catalogue.Blobs), result.Manifest.ExpiresAt)
	return nil
}
