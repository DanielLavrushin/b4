package config

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
)

var updateSetWireBaseline = flag.Bool("update-set-wire-baseline", false, "record NewSetConfig() in testdata/set_wire_baseline.json")

const setWireBaselineFile = "testdata/set_wire_baseline.json"

type setWireBaseline struct {
	ConfigVersion int                    `json:"config_version"`
	Fields        []string               `json:"fields"`
	Set           map[string]interface{} `json:"set"`
}

type setWireDiff struct {
	changed, removed, added []string
}

func (d setWireDiff) reinterprets() bool {
	return len(d.changed)+len(d.removed) > 0
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

func setWireFields(t reflect.Type, prefix string, out *[]string) {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "-" || name == "" {
			continue
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		ft := f.Type
		for ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct {
			before := len(*out)
			setWireFields(ft, path, out)
			if len(*out) > before {
				continue
			}
		}
		*out = append(*out, path)
	}
}

func currentSetWireFields() []string {
	var fields []string
	setWireFields(reflect.TypeOf(SetConfig{}), "", &fields)
	sort.Strings(fields)
	return fields
}

func knownSetWireField(path string, known map[string]bool) bool {
	for p := path; p != ""; {
		if known[p] {
			return true
		}
		i := strings.LastIndex(p, ".")
		if i < 0 {
			break
		}
		p = p[:i]
	}
	return false
}

func compareSetWireBaseline(recorded setWireBaseline, currentSet map[string]interface{}, currentFields []string) setWireDiff {
	now, was := map[string]interface{}{}, map[string]interface{}{}
	wireLeaves("", currentSet, now)
	wireLeaves("", recorded.Set, was)
	known := make(map[string]bool, len(recorded.Fields))
	for _, f := range recorded.Fields {
		known[f] = true
	}

	var d setWireDiff
	for path, old := range was {
		value, ok := now[path]
		switch {
		case !ok:
			d.removed = append(d.removed, path)
		case !reflect.DeepEqual(old, value):
			d.changed = append(d.changed, fmt.Sprintf("%s: %s -> %s", path, compactJSON(old), compactJSON(value)))
		}
	}
	for path, value := range now {
		if _, ok := was[path]; ok {
			continue
		}
		if knownSetWireField(path, known) {
			d.changed = append(d.changed, fmt.Sprintf("%s: <omitted> -> %s", path, compactJSON(value)))
		} else {
			d.added = append(d.added, path)
		}
	}
	for _, f := range recorded.Fields {
		if !slices.Contains(currentFields, f) && !slices.Contains(d.removed, f) {
			d.removed = append(d.removed, f)
		}
	}
	sort.Strings(d.changed)
	sort.Strings(d.removed)
	sort.Strings(d.added)
	return d
}

func refuseSetWireRecord(d setWireDiff, recorded setWireBaseline) bool {
	return d.reinterprets() && recorded.Set != nil && CurrentConfigVersion <= recorded.ConfigVersion
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
	fields := currentSetWireFields()
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

	d := compareSetWireBaseline(recorded, current, fields)

	if *updateSetWireBaseline {
		if refuseSetWireRecord(d, recorded) {
			t.Fatalf("refusing to record changed set defaults without a config version bump: changed %v, removed %v", d.changed, d.removed)
		}
		out, err := json.MarshalIndent(setWireBaseline{ConfigVersion: CurrentConfigVersion, Fields: fields, Set: current}, "", "  ")
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

	if d.reinterprets() {
		t.Errorf("set defaults changed: %s%s\n"+
			"Config files, set exports and hub envelopes leave out every value equal to its default, "+
			"so a changed default rewrites what all of them mean, on this router and on every router that reads them. "+
			"To change what a new set starts with, use the new set template (ui.new_set_template). "+
			"If the wire default itself must change: bump CurrentConfigVersion, add a migration that writes the old literal "+
			"into every set whose raw JSON lacks the key, then record the baseline again with "+
			"go test ./config -run TestSetWireBaselineIsFrozen -update-set-wire-baseline",
			formatPaths("changed", d.changed), formatPaths("removed", d.removed))
	}
	if len(d.added) > 0 {
		t.Errorf("new set fields are not in %s: %s\n"+
			"Record them, so their defaults are frozen from their first release: "+
			"go test ./config -run TestSetWireBaselineIsFrozen -update-set-wire-baseline",
			setWireBaselineFile, strings.Join(d.added, ", "))
	}
}

func TestSetWireBaselineComparison(t *testing.T) {
	recorded := setWireBaseline{
		ConfigVersion: CurrentConfigVersion,
		Fields:        []string{"dns.pins", "dns.upstreams", "name", "old_field", "tcp.ttl"},
		Set:           map[string]interface{}{"name": "", "tcp": map[string]interface{}{"ttl": float64(7)}},
	}
	current := map[string]interface{}{
		"name":      "",
		"tcp":       map[string]interface{}{"ttl": float64(7)},
		"dns":       map[string]interface{}{"upstreams": []interface{}{"1.1.1.1"}, "pins": map[string]interface{}{"a.example": []interface{}{"192.0.2.1"}}},
		"new_field": float64(1),
	}
	fields := []string{"dns.pins", "dns.upstreams", "name", "new_field", "tcp.ttl"}

	d := compareSetWireBaseline(recorded, current, fields)

	wantChanged := []string{`dns.pins.a.example: <omitted> -> ["192.0.2.1"]`, `dns.upstreams: <omitted> -> ["1.1.1.1"]`}
	if !slices.Equal(d.changed, wantChanged) {
		t.Errorf("changed = %v, want %v: a field whose empty default was omitted and now has a value is a changed default", d.changed, wantChanged)
	}
	if !slices.Equal(d.added, []string{"new_field"}) {
		t.Errorf("added = %v, want [new_field]", d.added)
	}
	if !slices.Equal(d.removed, []string{"old_field"}) {
		t.Errorf("removed = %v, want [old_field]: a field gone from SetConfig needs a migration even when its default was omitted", d.removed)
	}
	if !refuseSetWireRecord(d, recorded) {
		t.Error("recording these changes without a config version bump was allowed")
	}
	if refuseSetWireRecord(setWireDiff{added: []string{"new_field"}}, recorded) {
		t.Error("recording a genuinely new field was refused")
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
