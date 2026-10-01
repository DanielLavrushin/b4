package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/catalogue"
	"github.com/daniellavrushin/b4hub/internal/moderation"
	"github.com/spf13/cobra"
)

var buildFlags struct {
	geoFlags
	newEpoch     bool
	revoke       string
	allowBuiltin bool
	newDatabase  bool
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
	buildCmd.Flags().StringVar(&buildFlags.revoke, "revoke", "", "add a key id to the revoked list carried by every manifest; routers never forget a revocation")
	buildCmd.Flags().BoolVar(&buildFlags.allowBuiltin, "allow-builtin", false, "allow --revoke to name a key built into b4")
	bindNewDatabase(buildCmd, &buildFlags.newDatabase)
}

func runBuild(cmd *cobra.Command, args []string) error {
	svc, err := openServices(dataDir)
	if err != nil {
		return err
	}
	defer svc.store.Close()
	ctx := context.Background()
	mod := svc.moderation(nil)
	if keyID := strings.TrimSpace(buildFlags.revoke); keyID != "" {
		res, err := mod.Revoke(ctx, cliActor(), keyID, moderation.RevokeOptions{Confirm: keyID, AllowBuiltin: buildFlags.allowBuiltin})
		if err != nil {
			return fmt.Errorf("--revoke: %w", err)
		}
		fmt.Println(res.Notice)
	}
	if buildFlags.newEpoch {
		res, err := mod.NewEpoch(ctx, cliActor())
		if err != nil {
			return err
		}
		fmt.Println(res.Notice)
	}
	sources := []hubwire.GeoSource{{SiteURL: buildFlags.geoSiteURL, IPURL: buildFlags.geoIPURL}}
	builder := svc.builder(buildFlags.publicURL, sources)
	builder.NewDatabase = buildFlags.newDatabase
	if _, err := builder.Mirrors.CheckAll(ctx); err != nil {
		return err
	}
	result, err := builder.BuildFor(ctx, catalogue.TriggerCLI)
	if err != nil {
		return err
	}
	fmt.Printf("%s: %d sets, %d blobs, expires %s\n", result.Manifest.Catalogue.File, len(result.Catalogue.Sets), len(result.Catalogue.Blobs), result.Manifest.ExpiresAt)
	return nil
}
