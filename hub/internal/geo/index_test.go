package geo

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/daniellavrushin/b4/sni"
)

const (
	kindPlain  = 0
	kindRegex  = 1
	kindDomain = 2
	kindFull   = 3
)

type fixtureDomain struct {
	kind  uint64
	value string
}

func varint(n uint64) []byte {
	buf := make([]byte, binary.MaxVarintLen64)
	return buf[:binary.PutUvarint(buf, n)]
}

func bytesField(field int, data []byte) []byte {
	out := varint(uint64(field<<3 | 2))
	out = append(out, varint(uint64(len(data)))...)
	return append(out, data...)
}

func varintField(field int, v uint64) []byte {
	return append(varint(uint64(field<<3)), varint(v)...)
}

func writeGeoSite(t *testing.T, categories map[string][]fixtureDomain) string {
	t.Helper()
	var list []byte
	for tag, domains := range categories {
		site := bytesField(1, []byte(tag))
		for _, d := range domains {
			domain := append(varintField(1, d.kind), bytesField(2, []byte(d.value))...)
			site = append(site, bytesField(2, domain)...)
		}
		list = append(list, bytesField(1, site)...)
	}
	path := filepath.Join(t.TempDir(), "geosite.dat")
	if err := os.WriteFile(path, list, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestIndexMatchesDomainsAndRegexps(t *testing.T) {
	path := writeGeoSite(t, map[string][]fixtureDomain{
		"YOUTUBE": {
			{kindDomain, "youtube.com"},
			{kindFull, "accounts.google.com"},
			{kindRegex, `^i[0-9]*\.ytimg\.com$`},
		},
		"OTHER": {{kindDomain, "other.example"}},
	})
	index := NewIndex(path)
	if !index.Available() {
		t.Fatal("fixture must be readable")
	}
	cases := []struct {
		category, domain string
		relation         sni.DomainRelation
		entry            string
	}{
		{"youtube", "youtube.com", sni.RelationExact, "youtube.com"},
		{"YouTube", "www.youtube.com", sni.RelationCovered, "youtube.com"},
		{"youtube", "accounts.google.com", sni.RelationExact, "accounts.google.com"},
		{"youtube", "i9.ytimg.com", sni.RelationRegexp, `regexp:^i[0-9]*\.ytimg\.com$`},
		{"other", "sub.other.example", sni.RelationCovered, "other.example"},
	}
	for _, c := range cases {
		cov, ok := index.Match(c.category, c.domain)
		if !ok || cov.Relation != c.relation || cov.Entry != c.entry {
			t.Errorf("%s in %s: got %+v %v, want %s %s", c.domain, c.category, cov, ok, c.relation, c.entry)
		}
	}
	for _, c := range [][2]string{{"youtube", "example.com"}, {"youtube", "notyoutube.com"}, {"missing", "youtube.com"}, {"other", "youtube.com"}} {
		if _, ok := index.Match(c[0], c[1]); ok {
			t.Errorf("%s must not be covered by %s", c[1], c[0])
		}
	}
	if err := index.Warm([]string{"youtube", "other", "missing"}); err != nil {
		t.Fatal(err)
	}
	if len(index.Loaded()) != 3 || index.Size("youtube") != 3 || index.Size("missing") != 0 {
		t.Errorf("unexpected cache state %v", index.Loaded())
	}
}

func TestIndexWithoutFileOnlyDisablesCategorySearch(t *testing.T) {
	index := NewIndex(filepath.Join(t.TempDir(), "geosite.dat"))
	if index.Available() {
		t.Fatal("a missing file is not available")
	}
	if _, ok := index.Match("youtube", "youtube.com"); ok {
		t.Errorf("a missing file must not match anything")
	}
	if err := index.Warm([]string{"youtube"}); err != nil {
		t.Errorf("warming without a file must not fail: %v", err)
	}
}

func TestIndexReloadsWhenFileChanges(t *testing.T) {
	path := writeGeoSite(t, map[string][]fixtureDomain{"cat": {{kindDomain, "one.example"}}})
	index := NewIndex(path)
	if _, ok := index.Match("cat", "one.example"); !ok {
		t.Fatal("first file must match")
	}
	replacement := writeGeoSite(t, map[string][]fixtureDomain{"cat": {{kindDomain, "two.example"}, {kindDomain, "three.example"}}})
	raw, _ := os.ReadFile(replacement)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := index.Match("cat", "two.example"); !ok {
		t.Errorf("a replaced file must be picked up")
	}
	if _, ok := index.Match("cat", "one.example"); ok {
		t.Errorf("stale entries must be gone after the reload")
	}
}
