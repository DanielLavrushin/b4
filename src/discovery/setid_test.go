package discovery

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSuiteAndHistoryCarryTheSetTheRunIsFor(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	suite := &CheckSuite{
		Id: "run-set", Status: CheckStatusComplete, EndTime: time.Now(), SetId: "set-yt",
		Domains: []DomainInput{{Domain: "www.youtube.com", CheckURL: "https://www.youtube.com/"}},
		DomainDiscoveryResults: map[string]*DomainDiscoveryResult{
			"www.youtube.com": {Domain: "www.youtube.com", Url: "https://www.youtube.com/", BestPreset: "best", BestSuccess: true},
		},
	}

	data, err := json.Marshal(suite)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"set_id":"set-yt"`) {
		t.Errorf("the suite JSON must name the set, got %s", data)
	}

	SaveToHistory(suite, cfgPath)
	hist := LoadDiscoveryHistory(cfgPath)
	if len(hist.Entries) != 1 || hist.Entries[0].SetId != "set-yt" {
		t.Fatalf("the history entry must remember the set, got %+v", hist.Entries)
	}

	plain := &CheckSuite{Id: "run-plain", Status: CheckStatusComplete, EndTime: time.Now()}
	data, err = json.Marshal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "set_id") {
		t.Errorf("a run without a set must omit set_id, got %s", data)
	}
}

func TestKnownServiceHosts(t *testing.T) {
	cases := []struct {
		geosite []string
		want    []string
	}{
		{nil, []string{}},
		{[]string{"unknown-category"}, []string{}},
		{[]string{"YouTube"}, []string{"youtube.com"}},
		{[]string{"tiktok"}, []string{"tiktokv.com"}},
		{[]string{"youtube", "meta", "youtube"}, []string{"instagram.com", "youtube.com"}},
		{[]string{"google"}, []string{"googleapis.com"}},
	}
	for _, c := range cases {
		got := KnownServiceHosts(c.geosite)
		if got == nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("KnownServiceHosts(%v) = %#v, want %#v", c.geosite, got, c.want)
		}
	}
}
