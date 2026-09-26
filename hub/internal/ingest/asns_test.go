package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/daniellavrushin/b4hub/internal/store"
	"github.com/daniellavrushin/b4hub/internal/testkit"
)

func targetsProjection(targets map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{"targets": targets}
}

func TestTargetsKeyKeepsItsFormForSetsWithoutASNs(t *testing.T) {
	legacy := map[string][]string{
		"sni_domains": {"a.example"},
		"ip":          {},
		"geosite":     {"youtube"},
		"geoip":       {},
	}
	data, _ := json.Marshal(legacy)
	sum := sha256.Sum256(data)
	want := hex.EncodeToString(sum[:])

	plain := targetsProjection(map[string]interface{}{
		"sni_domains":        []interface{}{"a.example"},
		"geosite_categories": []interface{}{"youtube"},
	})
	if got := TargetsKey(plain); got != want {
		t.Errorf("a set without ASNs must keep the key stored hub databases already hold: got %s, want %s", got, want)
	}
	withEmpty := targetsProjection(map[string]interface{}{
		"sni_domains":        []interface{}{"a.example"},
		"geosite_categories": []interface{}{"youtube"},
		"asns":               []interface{}{},
	})
	if got := TargetsKey(withEmpty); got != want {
		t.Errorf("an empty ASN list must not change the key: got %s, want %s", got, want)
	}
}

func TestTargetsKeyCanonicalisesASNs(t *testing.T) {
	a := targetsProjection(map[string]interface{}{"asns": []interface{}{"AS62041", "62041", "44907"}})
	b := targetsProjection(map[string]interface{}{"asns": []interface{}{"44907", " as62041 "}})
	if TargetsKey(a) != TargetsKey(b) {
		t.Errorf("an AS prefix, case, order and duplicates must not change the key")
	}
	c := targetsProjection(map[string]interface{}{"asns": []interface{}{"62041"}})
	if TargetsKey(a) == TargetsKey(c) {
		t.Errorf("different ASNs must give different keys")
	}
	if TargetsKey(c) == TargetsKey(targetsProjection(map[string]interface{}{})) {
		t.Errorf("an ASN target must count in the key")
	}
}

func TestTargetSetIncludesASNs(t *testing.T) {
	got := TargetSet(targetsProjection(map[string]interface{}{
		"sni_domains": []interface{}{"a.example"},
		"asns":        []interface{}{"AS62041"},
	}))
	want := map[string]struct{}{"sni:a.example": {}, "asn:62041": {}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unexpected target set %v", got)
	}
}

func TestShareAcceptsASetThatTargetsOnlyASNs(t *testing.T) {
	f := newFixture(t)
	author := testkit.Identity(t)
	set := testkit.SampleSet("Telegram")
	set.Targets.ASNs = []string{"62041", "44907"}

	resp := f.share(t, author, set, peerA)
	expect(t, resp, http.StatusAccepted, "")
	setID := resp.Body["set_id"].(string)
	v, err := f.store.GetVersion(context.Background(), setID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := store.TargetList(v.Projection, "asns"); !reflect.DeepEqual(got, []string{"62041", "44907"}) {
		t.Errorf("the stored projection must carry the ASNs, got %v", got)
	}
	if v.B4Min != "1.84.0" {
		t.Errorf("a set with ASNs needs the release that reads them, got %q", v.B4Min)
	}

	other := testkit.SampleSet("Telegram")
	other.Targets.ASNs = []string{"15169"}
	resp = f.share(t, testkit.Identity(t), other, peerB)
	expect(t, resp, http.StatusAccepted, "")
	if resp.Body["set_id"] == setID {
		t.Errorf("the same strategy for other ASNs is another set, got %v", resp.Body)
	}

	widened := testkit.SampleSet("Telegram")
	widened.Targets.ASNs = []string{"62041", "211157"}
	widened.Faking.TTL = 9
	resp = f.share(t, author, widened, peerA)
	expect(t, resp, http.StatusAccepted, "")
	if resp.Body["set_id"] != setID || resp.Body["version"] != 2 {
		t.Errorf("the author's set with the same title and a shared ASN must become version 2, got %v", resp.Body)
	}
}
