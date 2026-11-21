package config

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

var updateSetWireBaseline = flag.Bool("update-set-wire-baseline", false, "record NewSetConfig() in testdata/set_wire_baseline.json")

const setWireBaselineFile = "testdata/set_wire_baseline.json"

type setWireBaseline struct {
	ConfigVersion int                    `json:"config_version"`
	Set           map[string]interface{} `json:"set"`
}

func wireLeaves(prefix string, v interface{}, out map[string]interface{}) {
	m, ok := v.(map[string]interface{})
	if !ok || len(m) == 0 {
		out[prefix] = v
		return
	}
	for key, value := range m {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		wireLeaves(path, value, out)
	}
}

func currentSetWireMap(t *testing.T) map[string]interface{} {
	t.Helper()
	set := NewSetConfig()
	m, err := SetToMap(&set)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestSetWireBaselineIsFrozen(t *testing.T) {
	current := currentSetWireMap(t)
	recorded := setWireBaseline{}
	data, err := os.ReadFile(setWireBaselineFile)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &recorded); err != nil {
			t.Fatalf("parse %s: %v", setWireBaselineFile, err)
		}
	case !*updateSetWireBaseline:
		t.Fatalf("read %s: %v", setWireBaselineFile, err)
	}

	now, was := map[string]interface{}{}, map[string]interface{}{}
	wireLeaves("", current, now)
	wireLeaves("", recorded.Set, was)

	var changed, removed, added []string
	for path, old := range was {
		value, ok := now[path]
		switch {
		case !ok:
			removed = append(removed, path)
		case !reflect.DeepEqual(old, value):
			changed = append(changed, fmt.Sprintf("%s: %s -> %s", path, compactJSON(old), compactJSON(value)))
		}
	}
	for path := range now {
		if _, ok := was[path]; !ok {
			added = append(added, path)
		}
	}
	sort.Strings(changed)
	sort.Strings(removed)
	sort.Strings(added)
	reinterpreted := len(changed)+len(removed) > 0

	if *updateSetWireBaseline {
		if reinterpreted && recorded.Set != nil && CurrentConfigVersion <= recorded.ConfigVersion {
			t.Fatalf("refusing to record changed set defaults without a config version bump: changed %v, removed %v", changed, removed)
		}
		out, err := json.MarshalIndent(setWireBaseline{ConfigVersion: CurrentConfigVersion, Set: current}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(setWireBaselineFile), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(setWireBaselineFile, append(out, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	if reinterpreted {
		t.Errorf("set defaults changed: %s%s\n"+
			"Config files, set exports and hub envelopes leave out every value equal to its default, "+
			"so a changed default rewrites what all of them mean, on this router and on every router that reads them. "+
			"To change what a new set starts with, use the new set template (ui.new_set_template). "+
			"If the wire default itself must change: bump CurrentConfigVersion, add a migration that writes the old literal "+
			"into every set whose raw JSON lacks the key, then record the baseline again with "+
			"go test ./config -run TestSetWireBaselineIsFrozen -update-set-wire-baseline",
			formatPaths("changed", changed), formatPaths("removed", removed))
	}
	if len(added) > 0 {
		t.Errorf("new set fields are not in %s: %s\n"+
			"Record them, so their defaults are frozen from their first release: "+
			"go test ./config -run TestSetWireBaselineIsFrozen -update-set-wire-baseline",
			setWireBaselineFile, strings.Join(added, ", "))
	}
}

func compactJSON(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(data)
}

func formatPaths(label string, paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	return "\n  " + label + ": " + strings.Join(paths, "\n  "+label+": ")
}
