package web

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4hub/internal/testkit"
)

func l7Set(name string, classes ...string) config.SetConfig {
	set := config.NewSetConfig()
	set.Name = name
	set.UDP.DPortFilter = "19294-19344,50000-50100"
	set.UDP.FakePayloadFile = config.FakePayloadAutoSTUN
	set.UDP.FilterL7 = classes
	return set
}

func termOf(terms []Term, code string) *Term {
	for i := range terms {
		if terms[i].Code == code {
			return &terms[i]
		}
	}
	return nil
}

func withSetting(t *testing.T, projection map[string]interface{}, section, key string, value interface{}) map[string]interface{} {
	t.Helper()
	out := projectionCopy(t, projection)
	block, _ := out[section].(map[string]interface{})
	if block == nil {
		block = map[string]interface{}{}
		out[section] = block
	}
	if value == nil {
		delete(block, key)
	} else {
		block[key] = value
	}
	return out
}

func TestEditKeepsASetThatSelectsUDPProtocols(t *testing.T) {
	f := newFixture(t, password)
	id, _ := f.shareAs(testkit.Identity(t), l7Set("Discord voice", config.UDPL7Discord), authorAddress)
	before := f.versionOf(id, 1)
	want := []string{config.UDPL7Discord}

	req := EditRequest{Title: "Discord voice calls", Projection: before.Projection}
	preview := f.preview(id, req)
	if !preview.Changed || preview.FPChanged || preview.B4Min != "1.88.0" || warningOf(preview.Warnings, noTargetsWarning) != nil {
		t.Fatalf("a title edit of a protocol selection passes the check: %+v", preview)
	}
	if !reflect.DeepEqual(preview.Targets.UDPL7, want) || len(preview.Targets.Domains) != 0 {
		t.Fatalf("the preview must name the protocols the set selects: %+v", preview.Targets)
	}
	if udp := termOf(preview.Strategy, TechUDP); udp == nil || udp.Params["payload"] != "stun" {
		t.Fatalf("a generated STUN decoy must be described as one: %+v", preview.Strategy)
	}
	f.expectOK(f.admin(http.MethodPost, setPath(id, 1, "edit"), req))
	after := f.versionOf(id, 1)
	if after.Title != "Discord voice calls" || after.FP != before.FP || after.B4Min != "1.88.0" {
		t.Fatalf("stored edit: %q %s %q", after.Title, after.FP, after.B4Min)
	}

	if e := entryOf(f.setsView().Pending, id, 1); e == nil || !reflect.DeepEqual(e.Targets.UDPL7, want) {
		t.Fatalf("the entry must name the protocols the set selects: %+v", e)
	}
	var rows SetRowsView
	f.admin(http.MethodGet, PathAPI+"/sets/rows?group=pending&q=discord", nil).decode(t, &rows)
	if len(rows.Rows) != 1 || rows.Rows[0].SetID != id || !reflect.DeepEqual(rows.Rows[0].Targets.UDPL7, want) {
		t.Fatalf("the row must carry the protocols and be found by them: %+v", rows.Rows)
	}

	plain, _ := f.share("Plain", otherAddress, "plain.example")
	if e := entryOf(f.setsView().Pending, plain, 1); e == nil || e.Targets.UDPL7 == nil || len(e.Targets.UDPL7) != 0 {
		t.Fatalf("a set without protocol selection must report an empty list, not null: %+v", e)
	}
}

func TestEditRefusesL7SetsARouterWouldNotSave(t *testing.T) {
	f := newFixture(t, password)
	id, _ := f.shareAs(testkit.Identity(t), l7Set("Discord voice", config.UDPL7Discord), authorAddress)
	v := f.versionOf(id, 1)

	for _, c := range []struct {
		name       string
		projection map[string]interface{}
		path       string
		code       string
	}{
		{"targets added", withTargets(t, v.Projection, map[string]interface{}{"sni_domains": []string{"discord.com"}}), "udp.filter_l7", "conflict"},
		{"ports removed", withSetting(t, v.Projection, "udp", "dport_filter", nil), "udp.dport_filter", "required"},
		{"TCP ports added", withSetting(t, v.Projection, "tcp", "dport_filter", "1-65535"), "tcp.dport_filter", "conflict"},
	} {
		req := EditRequest{Title: v.Title, Projection: c.projection}
		for _, action := range []string{"preview", "edit"} {
			body := f.expectError(f.admin(http.MethodPost, setPath(id, 1, action), req), http.StatusBadRequest, codeInvalidSet)
			if fields := invalidFields(t, body); len(fields) != 1 || fields[0].Path != c.path || fields[0].Code != c.code {
				t.Fatalf("%s, %s: expected %s on %s, got %+v", c.name, action, c.code, c.path, fields)
			}
		}
	}

	dropped := EditRequest{Title: v.Title, Projection: withSetting(t, v.Projection, "udp", "filter_l7", nil)}
	for _, action := range []string{"preview", "edit"} {
		body := f.expectError(f.admin(http.MethodPost, setPath(id, 1, action), dropped), http.StatusBadRequest, codeInvalidSet)
		if !strings.Contains(body.Error, "no targets") {
			t.Fatalf("%s: a set left with only its ports targets nothing: %+v", action, body)
		}
	}
	if after := f.versionOf(id, 1); !after.EditedAt.IsZero() {
		t.Fatalf("a refused edit stores nothing: %+v", after)
	}
}
