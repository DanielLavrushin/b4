package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/urlesistiana/v2dat/v2data"
	"google.golang.org/protobuf/proto"
)

func TestHeadlessASNReloadKeepsWorkingOnADamagedGeosite(t *testing.T) {
	site, err := proto.Marshal(&v2data.GeoSiteList{Entry: []*v2data.GeoSite{
		{CountryCode: "TELEGRAM", Domain: []*v2data.Domain{{Type: v2data.Domain_Domain, Value: "telegram.org"}}},
		{CountryCode: "DISCORD", Domain: []*v2data.Domain{{Type: v2data.Domain_Domain, Value: "discord.com"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	sitePath := filepath.Join(t.TempDir(), "geosite.dat")
	if err := os.WriteFile(sitePath, site[:len(site)-4], 0o644); err != nil {
		t.Fatal(err)
	}

	s := config.InitAsnStore(filepath.Join(t.TempDir(), "b4.json"))
	t.Cleanup(func() { config.InitAsnStore("") })

	current := headlessASNConfig()
	current.System.Geo.GeoSitePath = sitePath
	tg := current.GetSetById("tg")
	tg.Targets.GeoSiteCategories = []string{"telegram", "discord"}
	current.LoadTargets()

	prefixes := []string{"91.108.4.0/22"}
	if err := s.Put(&config.AsnInfo{ID: "62041", Name: "Telegram", Prefixes: prefixes, UpdatedAt: time.Now().Unix(), Source: config.AsnSourceRIPEstat}); err != nil {
		t.Fatal(err)
	}
	commits := 0
	reloadASNTargetsHeadless(context.Background(), func() *config.Config { return current }, []string{"62041"}, func(_, next *config.Config) error {
		commits++
		current = next
		return nil
	})

	if commits != 1 {
		t.Fatalf("a damaged geosite must not hold back new ASN prefixes, commits = %d", commits)
	}
	set := current.GetSetById("tg")
	if !slices.Equal(set.Targets.IpsToMatch, []string{"91.108.4.0/22", "198.51.100.7"}) {
		t.Fatalf("IpsToMatch = %v", set.Targets.IpsToMatch)
	}
	if !slices.Equal(set.Targets.DomainsToMatch, []string{"telegram.org"}) {
		t.Fatalf("the readable category stays, DomainsToMatch = %v", set.Targets.DomainsToMatch)
	}
}
