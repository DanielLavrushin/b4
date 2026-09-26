package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/urlesistiana/v2dat/v2data"
	"google.golang.org/protobuf/proto"
)

func truncatedGeoFiles(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()

	site, err := proto.Marshal(&v2data.GeoSiteList{Entry: []*v2data.GeoSite{
		{CountryCode: "YOUTUBE", Domain: []*v2data.Domain{{Type: v2data.Domain_Domain, Value: "youtube.com"}}},
		{CountryCode: "DISCORD", Domain: []*v2data.Domain{{Type: v2data.Domain_Domain, Value: "discord.com"}, {Type: v2data.Domain_Domain, Value: "discord.gg"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	ip, err := proto.Marshal(&v2data.GeoIPList{Entry: []*v2data.GeoIP{
		{CountryCode: "TELEGRAM", Cidr: []*v2data.CIDR{{Ip: []byte{91, 108, 4, 0}, Prefix: 22}}},
		{CountryCode: "CLOUDFLARE", Cidr: []*v2data.CIDR{{Ip: []byte{104, 16, 0, 0}, Prefix: 13}}},
	}})
	if err != nil {
		t.Fatal(err)
	}

	sitePath := filepath.Join(dir, "geosite.dat")
	ipPath := filepath.Join(dir, "geoip.dat")
	if err := os.WriteFile(sitePath, site[:len(site)-4], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ipPath, ip[:len(ip)-4], 0o644); err != nil {
		t.Fatal(err)
	}
	return sitePath, ipPath
}

func TestLoadTargetsKeepsRunningOnADamagedGeoFile(t *testing.T) {
	sitePath, ipPath := truncatedGeoFiles(t)
	cfg := NewConfig()
	cfg.System.Geo.GeoSitePath = sitePath
	cfg.System.Geo.GeoIpPath = ipPath

	readable := NewSetConfig()
	readable.Id, readable.Name, readable.Enabled = "readable", "Readable", true
	readable.Targets.GeoSiteCategories = []string{"youtube"}
	readable.Targets.GeoIpCategories = []string{"telegram"}

	mixed := NewSetConfig()
	mixed.Id, mixed.Name, mixed.Enabled = "mixed", "Mixed", true
	mixed.Targets.GeoSiteCategories = []string{"discord"}
	mixed.Targets.SNIDomains = []string{"manual.example"}
	mixed.Targets.GeoIpCategories = []string{"cloudflare"}
	mixed.Targets.IPs = []string{"203.0.113.7"}

	geoOnly := NewSetConfig()
	geoOnly.Id, geoOnly.Name, geoOnly.Enabled = "geo-only", "Geo only", true
	geoOnly.Targets.GeoSiteCategories = []string{"discord"}

	cfg.Sets = []*SetConfig{&readable, &mixed, &geoOnly}

	sets, domains, ips, warnings := cfg.LoadTargets()

	if len(sets) != 3 {
		t.Fatalf("every enabled set is loaded, got %d", len(sets))
	}
	if len(warnings) != 2 {
		t.Fatalf("want one warning per damaged file, got %v", warnings)
	}
	site, ip := warnings[0].Error(), warnings[1].Error()
	if !strings.Contains(site, "discord") || !strings.Contains(site, "'Mixed'") || !strings.Contains(site, "'Geo only'") || strings.Contains(site, "'Readable'") {
		t.Fatalf("the GeoSite warning names the unread category and the affected sets: %s", site)
	}
	if !strings.Contains(ip, "cloudflare") || !strings.Contains(ip, "'Mixed'") {
		t.Fatalf("the GeoIP warning names the unread category and the affected set: %s", ip)
	}

	if !slices.Equal(readable.Targets.DomainsToMatch, []string{"youtube.com"}) || !slices.Equal(readable.Targets.IpsToMatch, []string{"91.108.4.0/22"}) {
		t.Fatalf("categories before the cut still load: %v %v", readable.Targets.DomainsToMatch, readable.Targets.IpsToMatch)
	}
	if !slices.Equal(mixed.Targets.DomainsToMatch, []string{"manual.example"}) || !slices.Equal(mixed.Targets.IpsToMatch, []string{"203.0.113.7"}) {
		t.Fatalf("a set keeps its manual targets when its categories cannot be read: %v %v", mixed.Targets.DomainsToMatch, mixed.Targets.IpsToMatch)
	}
	if len(geoOnly.Targets.DomainsToMatch) != 0 || !geoOnly.DeclaresDestinationTargets() {
		t.Fatalf("a geo-only set matches nothing but still declares targets: %v", geoOnly.Targets.DomainsToMatch)
	}
	if domains != 2 || ips != 2 {
		t.Fatalf("totals = %d domains, %d ips", domains, ips)
	}
}

func TestLoadTargetsReportsNothingForIntactFiles(t *testing.T) {
	dir := t.TempDir()
	site, err := proto.Marshal(&v2data.GeoSiteList{Entry: []*v2data.GeoSite{
		{CountryCode: "YOUTUBE", Domain: []*v2data.Domain{{Type: v2data.Domain_Domain, Value: "youtube.com"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	sitePath := filepath.Join(dir, "geosite.dat")
	if err := os.WriteFile(sitePath, site, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := NewConfig()
	cfg.System.Geo.GeoSitePath = sitePath
	set := NewSetConfig()
	set.Enabled = true
	set.Targets.GeoSiteCategories = []string{"YouTube", "absent"}
	set.Targets.SNIDomains = []string{"manual.example"}
	cfg.Sets = []*SetConfig{&set}

	_, domains, _, warnings := cfg.LoadTargets()
	if warnings != nil {
		t.Fatalf("no warnings for an intact file, got %v", warnings)
	}
	if !slices.Equal(set.Targets.DomainsToMatch, []string{"youtube.com", "manual.example"}) || domains != 2 {
		t.Fatalf("domains = %v (%d)", set.Targets.DomainsToMatch, domains)
	}
}
