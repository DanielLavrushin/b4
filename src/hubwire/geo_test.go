package hubwire

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/geodat"
)

func TestOnlyABuiltInGeodataSourceIsShared(t *testing.T) {
	sources, err := geodat.Sources()
	if err != nil || len(sources) == 0 {
		t.Fatalf("no built-in sources: %v", err)
	}
	site, ip := sources[0].GeositeURL, sources[0].GeoipURL
	private := "https://canaryuser:canarypass@files.example/canarytoken/geosite.dat?sig=canarysig"
	withLogin := strings.Replace(site, "https://", "https://canaryuser:canarypass@", 1)
	for _, tc := range []struct {
		name string
		in   *GeoSource
		want *GeoSource
	}{
		{"none", nil, nil},
		{"empty", &GeoSource{}, nil},
		{"private", &GeoSource{SiteURL: private, IPURL: private}, nil},
		{"built in", &GeoSource{SiteURL: site, IPURL: ip}, &GeoSource{SiteURL: site, IPURL: ip}},
		{"private geosite", &GeoSource{SiteURL: private, IPURL: ip}, &GeoSource{IPURL: ip}},
		{"private geoip", &GeoSource{SiteURL: site, IPURL: private}, &GeoSource{SiteURL: site}},
		{"built in with a login", &GeoSource{SiteURL: withLogin}, nil},
		{"built in with a query", &GeoSource{SiteURL: site + "?token=canary", IPURL: ip + "#canary"}, nil},
		{"public but not built in", &GeoSource{SiteURL: "https://github.com/someone/lists/releases/latest/download/geosite.dat"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			same := func(where string, got *GeoSource) {
				t.Helper()
				if (got == nil) != (tc.want == nil) || got != nil && *got != *tc.want {
					t.Errorf("%s: got %+v want %+v", where, got, tc.want)
				}
			}
			var before GeoSource
			if tc.in != nil {
				before = *tc.in
			}
			same("the rule", BuiltInGeo(tc.in))
			if tc.in == nil {
				return
			}
			if *tc.in != before {
				t.Errorf("the caller's value changed: %+v", tc.in)
			}
			set := sampleSet()
			env, _, err := Build(&set, BuildOptions{GeoSiteURL: tc.in.SiteURL, GeoIPURL: tc.in.IPURL})
			if err != nil {
				t.Fatal(err)
			}
			same("the envelope", env.Geo)
		})
	}
}
