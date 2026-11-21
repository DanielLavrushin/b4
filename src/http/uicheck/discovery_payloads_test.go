package uicheck

import (
	"regexp"
	"testing"
)

func TestDiscoveryOffersOnlyPresentTLSPayloads(t *testing.T) {
	src := readUI(t, "components/discovery/Options.tsx")
	tls := filteredListName(src, "captures", "tls")
	if tls == "" {
		t.Fatal(`Discovery's payload choice is not filtered on protocol === "tls"`)
	}
	stmt := regexp.MustCompile(`(?s)const\s+` + tls + `\s*=\s*captures\s*\.filter\((.*?)\);`).FindStringSubmatch(src)
	if stmt == nil || !regexp.MustCompile(`!\s*\w+\.missing\b`).MatchString(stmt[1]) {
		t.Error("Discovery offers payloads whose file is missing, which it cannot load")
	}
	if !regexp.MustCompile(`\b` + tls + `\s*\.map\(`).MatchString(src) {
		t.Errorf("Discovery's payload options are not built from %s", tls)
	}
	if regexp.MustCompile(`\bhex_data\b`).MatchString(src + readUI(t, "components/discovery/Discovery.tsx")) {
		t.Error("Discovery still reads hex_data, which the payload list no longer carries")
	}
}
