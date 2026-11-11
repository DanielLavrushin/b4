package warmtuple

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/urlesistiana/v2dat/v2data"
	"google.golang.org/protobuf/proto"
)

func geoServiceFixture(t *testing.T) (*Service, *config.Config, *atomic.Int32) {
	t.Helper()
	s, cfg, warms := serviceFixture(t)
	b, err := proto.Marshal(&v2data.GeoIPList{Entry: []*v2data.GeoIP{{CountryCode: "BLOCKED_TLS_RU",
		Cidr: []*v2data.CIDR{{Ip: []byte{203, 0, 113, 2}, Prefix: 32}}}}})
	if err != nil {
		t.Fatal(err)
	}
	cfg.System.Geo.GeoIpPath = filepath.Join(t.TempDir(), "googlevideo.dat")
	if err := os.WriteFile(cfg.System.Geo.GeoIpPath, b, 0600); err != nil {
		t.Fatal(err)
	}
	cfg.Sets[0].Targets = config.TargetsConfig{GeoIpCategories: []string{"BLOCKED_TLS_RU"}, IPPriority: true}
	ordinary := config.NewSetConfig()
	ordinary.Id = "ordinary"
	ordinary.Targets.SNIDomains = []string{"googlevideo.com"}
	cfg.Sets = append(cfg.Sets, &ordinary)
	if _, _, _, warnings := cfg.LoadTargets(); len(warnings) != 0 {
		t.Fatal(warnings)
	}
	s.UpdateConfig(cfg)
	return s, cfg, warms
}

func TestServiceGeoIPBeforeSYNAndFrozenOrdinaryTarget(t *testing.T) {
	s, cfg, warms := geoServiceFixture(t)
	if len(cfg.Sets[0].Targets.IpsToMatch) != 1 || len(cfg.Sets[0].Targets.DomainsToMatch) != 0 {
		t.Fatal("GeoIP fixture did not load an address-only set")
	}
	var lookups atomic.Int32
	s.lookup = func(context.Context, string) ([]netip.Addr, error) {
		n := lookups.Add(1)
		if n == 1 {
			return []netip.Addr{netip.MustParseAddr("203.0.113.2")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("203.0.113.9")}, nil
	}
	c, id, handled, resolved, err := s.DialContext(context.Background(), "rr3.googlevideo.com:443", "")
	if err != nil || !handled || id != "warm" || resolved != "203.0.113.2:443" {
		t.Fatalf("GeoIP warm: %s %t %s %v", id, handled, resolved, err)
	}
	c.Close()
	if lookups.Load() != 1 || warms.Load() != 1 {
		t.Fatal("warm/pass repeated DNS or skipped the warm")
	}
	c, _, handled, resolved, err = s.DialContext(context.Background(), "other.googlevideo.com:443", "")
	if c != nil || handled || err != nil || resolved != "203.0.113.9:443" || lookups.Load() != 2 || warms.Load() != 1 {
		t.Fatalf("outside GeoIP: %t %s %v", handled, resolved, err)
	}
	if s.Status().Active != 0 || s.Status().Pending != 0 || s.flows.Active() != 0 {
		t.Fatal("unselected target allocated a warm")
	}
}

func TestServiceGeoIPHonorsNativePriorityAndSourceFilters(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*config.Config)
	}{
		{"earlier name", func(c *config.Config) { c.Sets[0], c.Sets[1] = c.Sets[1], c.Sets[0] }},
		{"IP priority disabled", func(c *config.Config) { c.Sets[0].Targets.IPPriority = false }},
		{"other source", func(c *config.Config) { c.Sets[0].Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:ff"} }},
		{"domain only", func(c *config.Config) { c.Sets[0].Targets.DomainOnly = true }},
		{"disabled", func(c *config.Config) { c.Sets[0].Enabled = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, cfg, warms := geoServiceFixture(t)
			tc.change(cfg)
			s.UpdateConfig(cfg)
			s.lookup = func(context.Context, string) ([]netip.Addr, error) {
				t.Fatal("ordinary name should not need a warm-only lookup")
				return nil, nil
			}
			if _, _, handled, _, err := s.DialContext(context.Background(), "rr.googlevideo.com:443", ""); handled || err != nil || warms.Load() != 0 {
				t.Fatal("ignored native priority/source constraints")
			}
		})
	}
}

func TestServiceGeoIPMissingCategoryDoesNotBroadenSelection(t *testing.T) {
	s, cfg, warms := geoServiceFixture(t)
	cfg.Sets[0].Targets.GeoIpCategories = []string{"ABSENT"}
	cfg.LoadTargets()
	s.UpdateConfig(cfg)
	if _, _, handled, _, err := s.DialContext(context.Background(), "rr.googlevideo.com:443", ""); handled || err != nil || warms.Load() != 0 {
		t.Fatal("missing GeoIP category selected arbitrary TCP443")
	}
	cfg.Sets[0].Targets.GeoIpCategories = []string{"BLOCKED_TLS_RU"}
	cfg.System.Geo.GeoIpPath += ".missing"
	cfg.LoadTargets()
	if len(cfg.Sets[0].Targets.IpsToMatch) != 0 {
		t.Fatal("missing database kept stale address targets")
	}
	s.UpdateConfig(cfg)
	if _, _, handled, _, err := s.DialContext(context.Background(), "rr.googlevideo.com:443", ""); handled || err != nil {
		t.Fatal("missing database broadened selection")
	}
}

func TestServiceGeoIPLookupSnapshotAndOtherPorts(t *testing.T) {
	s, cfg, warms := geoServiceFixture(t)
	s.lookup = func(context.Context, string) ([]netip.Addr, error) {
		s.UpdateConfig(cfg.Clone())
		return []netip.Addr{netip.MustParseAddr("203.0.113.2")}, nil
	}
	if _, _, handled, _, err := s.DialContext(context.Background(), "rr.googlevideo.com:443", ""); !handled || err == nil || warms.Load() != 0 {
		t.Fatal("stale lookup started a warm")
	}
	s.lookup = func(context.Context, string) ([]netip.Addr, error) {
		t.Fatal("other port triggered GeoIP lookup")
		return nil, errors.New("unexpected lookup")
	}
	if _, _, handled, _, err := s.DialContext(context.Background(), "rr.googlevideo.com:8443", ""); handled || err != nil {
		t.Fatal("claimed other port")
	}
}
