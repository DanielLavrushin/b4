package hubwire

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIPPriorityIsNeverShared(t *testing.T) {
	set := sampleSet()
	set.Targets.IPs = []string{"37.18.153.136/29"}
	set.Targets.IPPriority = true

	projection, report, err := Scrub(&set)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := lookupPath(projection, "targets.ip_priority"); found {
		t.Error("the projection must not carry targets.ip_priority")
	}
	raw, _ := json.Marshal(projection)
	if strings.Contains(string(raw), "ip_priority") {
		t.Errorf("projection still carries ip_priority: %s", raw)
	}
	stripped := false
	for _, s := range report.Stripped {
		if s.Path == "targets.ip_priority" {
			stripped = true
		}
	}
	if !stripped {
		t.Errorf("an ip_priority that was on must be reported as stripped, got %+v", report.Stripped)
	}

	set.Targets.IPPriority = false
	_, report, err = Scrub(&set)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range report.Stripped {
		if s.Path == "targets.ip_priority" {
			t.Errorf("an ip_priority that was off must not be reported: %+v", s)
		}
	}
}

func TestIPPriorityIsDroppedOnImport(t *testing.T) {
	env := &Envelope{
		Format: Format,
		Title:  "crafted",
		Set: map[string]interface{}{
			"targets": map[string]interface{}{
				"ip":          []interface{}{"37.18.153.136/29"},
				"ip_priority": true,
			},
		},
	}
	imp, err := Open(env, OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if imp.Set.Targets.IPPriority {
		t.Error("an imported set must start with ip_priority off: the importer decides")
	}
	if len(imp.Set.Targets.IPs) != 1 {
		t.Errorf("the address targets must still cross: %v", imp.Set.Targets.IPs)
	}
}
