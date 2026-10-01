package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
)

func TestTextEditOfAListedVersion(t *testing.T) {
	f := newFixture(t, password)
	id, _ := f.share("Old title", authorAddress, "retitle.example")
	f.approve(id)
	f.build()
	before := f.published(id)
	if before == nil || before.Title != "Old title" {
		t.Fatalf("published before: %+v", before)
	}
	v := f.versionOf(id, 1)

	f.expectError(f.admin(http.MethodPost, setPath(id, 1, "text"), TextEditRequest{Title: "  "}), http.StatusBadRequest, "title_required")
	stale := v.UpdatedAt.Add(-time.Hour)
	f.expectError(f.admin(http.MethodPost, setPath(id, 1, "text"), TextEditRequest{Title: "New", Expect: &stale}), http.StatusConflict, "stale")

	rebuilds := f.rebuilds
	res := f.expectOK(f.admin(http.MethodPost, setPath(id, 1, "text"), TextEditRequest{Title: "New title", Description: "Better words", Note: "typo", Expect: &v.UpdatedAt}))
	if res.Code != "set.text_edited" || f.rebuilds != rebuilds+1 {
		t.Fatalf("a text edit of a listed version publishes: %+v rebuilds %d", res, f.rebuilds-rebuilds)
	}
	after := f.versionOf(id, 1)
	if after.Title != "New title" || after.Description != "Better words" || after.Projection["name"] != "New title" {
		t.Fatalf("the version carries the new text: %+v", after)
	}
	if after.FP != v.FP || after.TargetsKey != v.TargetsKey || !after.UpdatedAt.Equal(v.UpdatedAt) || after.Status != hubwire.SetStatusActive {
		t.Fatalf("a text edit changes nothing routers compare: %+v", after)
	}
	if after.OriginalTitle != "Old title" || after.EditNote != "typo" || after.EditedAt.IsZero() {
		t.Fatalf("the first text edit keeps the original: %+v", after)
	}
	f.build()
	if cs := f.published(id); cs == nil || cs.Title != "New title" || cs.Description != "Better words" || cs.Version != 1 || cs.FP != before.FP || cs.UpdatedAt != before.UpdatedAt {
		t.Fatalf("the catalogue carries the text and keeps version, fingerprint and date: %+v", cs)
	}
	f.expectError(f.admin(http.MethodPost, setPath(id, 1, "text"), TextEditRequest{Title: "New title", Description: "Better words"}), http.StatusBadRequest, "unchanged")
	edited := f.versionOf(id, 1)
	f.expectOK(f.admin(http.MethodPost, setPath(id, 1, "text"), TextEditRequest{Title: "Third", Expect: &edited.EditedAt}))
	if again := f.versionOf(id, 1); again.OriginalTitle != "Old title" {
		t.Fatalf("a second edit keeps the first original: %+v", again)
	}
	log := f.auditLog("?action=set.text")
	if len(log.Items) != 2 || log.Items[1].Before["title"] != "Old title" {
		t.Fatalf("text edits are audited: %+v", log.Items)
	}

	rejected, _ := f.share("Rejected", otherAddress, "rejected.example")
	f.expectOK(f.admin(http.MethodPost, setPath(rejected, 1, "reject"), map[string]string{"reason": "no"}))
	f.expectError(f.admin(http.MethodPost, setPath(rejected, 1, "text"), TextEditRequest{Title: "Nope"}), http.StatusConflict, "not_editable")
}

func TestSavedReasons(t *testing.T) {
	f := newFixture(t, password)
	f.expectError(f.admin(http.MethodPost, PathAPI+"/reasons", ReasonPresetRequest{Scope: "nope", Text: "x"}), http.StatusBadRequest, "bad_scope")
	f.expectError(f.admin(http.MethodPost, PathAPI+"/reasons", ReasonPresetRequest{Scope: "reject", Text: " "}), http.StatusBadRequest, "reason_required")
	f.expectOK(f.admin(http.MethodPost, PathAPI+"/reasons", ReasonPresetRequest{Scope: "reject", Label: "dup", Text: "Duplicate of a listed set"}))
	f.expectOK(f.admin(http.MethodPost, PathAPI+"/reasons", ReasonPresetRequest{Scope: "reject", Text: "Does not work"}))
	f.expectError(f.admin(http.MethodPost, PathAPI+"/reasons", ReasonPresetRequest{Scope: "reject", Text: "Does not work"}), http.StatusConflict, "preset_exists")

	var presets []ReasonPresetView
	f.admin(http.MethodGet, PathAPI+"/reasons", nil).decode(t, &presets)
	if len(presets) != 2 || presets[0].Position != 0 || presets[1].Position != 1 || presets[0].Label != "dup" {
		t.Fatalf("presets: %+v", presets)
	}

	id, _ := f.share("Twice", authorAddress, "twice.example")
	f.expectOK(f.admin(http.MethodPost, setPath(id, 1, "reject"), map[string]string{"reason": "Duplicate of a listed set"}))
	f.admin(http.MethodGet, PathAPI+"/reasons", nil).decode(t, &presets)
	if presets[0].Uses != 1 || presets[0].LastUsed == nil || presets[1].Uses != 0 {
		t.Fatalf("using a preset's text counts it: %+v", presets)
	}

	path := PathAPI + "/reasons/" + itoa(int(presets[1].ID))
	f.expectOK(f.admin(http.MethodPut, path, ReasonPresetRequest{Scope: "reject", Text: "Does not work anywhere", Position: 5}))
	f.expectOK(f.admin(http.MethodDelete, path, nil))
	f.expectError(f.admin(http.MethodDelete, path, nil), http.StatusNotFound, "not_found")
	f.admin(http.MethodGet, PathAPI+"/reasons", nil).decode(t, &presets)
	if len(presets) != 1 || !strings.HasPrefix(presets[0].Text, "Duplicate") {
		t.Fatalf("after delete: %+v", presets)
	}
}

func TestSavedReasonsMoveAtomicallyAndChangeScope(t *testing.T) {
	f := newFixture(t, password)
	for _, text := range []string{"First", "Second", "Third"} {
		f.expectOK(f.admin(http.MethodPost, PathAPI+"/reasons", ReasonPresetRequest{Scope: "reject", Text: text}))
	}
	order := func(scope string) []string {
		var presets []ReasonPresetView
		f.admin(http.MethodGet, PathAPI+"/reasons", nil).decode(t, &presets)
		out := []string{}
		for _, p := range presets {
			if p.Scope == scope {
				out = append(out, p.Text)
			}
		}
		return out
	}
	idOf := func(text string) string {
		var presets []ReasonPresetView
		f.admin(http.MethodGet, PathAPI+"/reasons", nil).decode(t, &presets)
		for _, p := range presets {
			if p.Text == text {
				return itoa(int(p.ID))
			}
		}
		t.Fatalf("no preset %q", text)
		return ""
	}
	move := func(text string, delta int) response {
		return f.admin(http.MethodPost, PathAPI+"/reasons/"+idOf(text)+"/move", ReasonMoveRequest{Delta: delta})
	}

	f.expectOK(move("Third", -1))
	if got := strings.Join(order("reject"), ","); got != "First,Third,Second" {
		t.Fatalf("one move swaps a preset with its neighbour: %s", got)
	}
	f.expectOK(move("First", -1))
	if got := strings.Join(order("reject"), ","); got != "First,Third,Second" {
		t.Fatalf("moving the first preset up changes nothing: %s", got)
	}
	f.expectError(move("First", 2), http.StatusBadRequest, "bad_delta")
	f.expectError(f.admin(http.MethodPost, PathAPI+"/reasons/9999/move", ReasonMoveRequest{Delta: 1}), http.StatusNotFound, "not_found")

	f.expectOK(f.admin(http.MethodPost, PathAPI+"/reasons", ReasonPresetRequest{Scope: "hide", Text: "Hidden one"}))
	f.expectOK(f.admin(http.MethodPut, PathAPI+"/reasons/"+idOf("Second"), ReasonPresetRequest{Scope: "hide", Text: "Second"}))
	if got := strings.Join(order("hide"), ","); got != "Hidden one,Second" {
		t.Fatalf("a preset moved to another scope goes to the end of it: %s", got)
	}
	if got := strings.Join(order("reject"), ","); got != "First,Third" {
		t.Fatalf("and leaves its old scope: %s", got)
	}
}
