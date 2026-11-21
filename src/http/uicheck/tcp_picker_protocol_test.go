package uicheck

import (
	"regexp"
	"testing"
)

func TestTCPPayloadPickerListsOnlyTLSPayloads(t *testing.T) {
	src := readUI(t, tcpFakingFile)
	jsxElement(t, src, "B4Select", `path="faking.payload_file"`)

	if regexp.MustCompile(`\bcaptures\s*\.map\(`).MatchString(src) {
		t.Error("the TCP payload picker maps every payload into its options, QUIC files included")
	}
	tls := filteredListName(src, "captures", "tls")
	if tls == "" {
		t.Fatal(`the TCP payload picker never filters the payload list on protocol === "tls", as Discovery does`)
	}
	if !regexp.MustCompile(`\b` + tls + `\s*\.map\(`).MatchString(src) {
		t.Errorf("the TCP payload picker's options are not built from %s, the TLS payloads", tls)
	}
}
