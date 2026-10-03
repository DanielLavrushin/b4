package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"
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

func TestTargetsKeyIsCanonical(t *testing.T) {
	a := targetsProjection(map[string]interface{}{
		"sni_domains":        []interface{}{"B.example", "a.example."},
		"geosite_categories": []interface{}{"YouTube"},
	})
	b := targetsProjection(map[string]interface{}{
		"sni_domains":        []interface{}{"a.example", "b.example", "b.example"},
		"geosite_categories": []interface{}{"youtube"},
	})
	if TargetsKey(a) != TargetsKey(b) {
		t.Errorf("order, case, trailing dots and duplicates must not change the key")
	}
	c := targetsProjection(map[string]interface{}{"sni_domains": []interface{}{"a.example"}})
	if TargetsKey(a) == TargetsKey(c) {
		t.Errorf("different targets must differ")
	}
}

func TestTargetEntriesIncludeASNs(t *testing.T) {
	got := TargetEntries(targetsProjection(map[string]interface{}{
		"sni_domains": []interface{}{"a.example"},
		"asns":        []interface{}{"AS62041"},
	}))
	want := map[string]struct{}{"sni:a.example": {}, "asn:62041": {}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unexpected target entries %v", got)
	}
}
