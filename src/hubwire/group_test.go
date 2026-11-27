package hubwire

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGroupNeverCrossesAndLeavesTheFingerprintAlone(t *testing.T) {
	plain := sampleSet()
	grouped := sampleSet()
	grouped.Group = "local-label"

	projection, report, err := Scrub(&grouped)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(projection)
	if strings.Contains(string(raw), "local-label") || strings.Contains(string(raw), `"group"`) {
		t.Errorf("the projection carries the group: %s", raw)
	}
	for _, s := range report.Stripped {
		if s.Path == "group" {
			t.Errorf("a group is local and must be stripped without a report: %+v", s)
		}
	}

	want, err := LiveFingerprint(&plain, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := LiveFingerprint(&grouped, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Error("grouping a set changed its fingerprint, so a hub copy would read as edited")
	}
}
