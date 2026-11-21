package uicheck

import (
	"strings"
	"testing"
)

var setTemplateMarkers = []string{"new_set_template", "setTemplate", "createSetFromTemplate", "SetTemplate"}

func TestOnlyCreateSetReadsTheNewSetTemplate(t *testing.T) {
	baselineReaders := map[string]string{
		"utils/setExport.ts":                "the export strips against b4's set defaults",
		"components/sets/ImportExport.tsx":  "a pasted set merges onto b4's set defaults",
		"models/hub.ts":                     "community set cards merge onto b4's set defaults",
		"models/defaults.ts":                "createDefaultSet is the baseline every reader above shares",
		"components/sets/ShareEnvelope.tsx": "a shared envelope is built from b4's set defaults",
	}
	for file, why := range baselineReaders {
		src := readUI(t, file)
		for _, marker := range setTemplateMarkers {
			if strings.Contains(src, marker) {
				t.Errorf("%s mentions %q, but %s: a missing key must mean the same on every router, whatever the user's template", file, marker, why)
			}
		}
	}

	page := readUI(t, "components/sets/Page.tsx")
	if !strings.Contains(page, "createSetFromTemplate(") || !strings.Contains(page, "new_set_template") {
		t.Error("the Sets page no longer builds a new set from the user's template (createSetFromTemplate with config.ui.new_set_template)")
	}
	if strings.Contains(page, "createDefaultSet(") {
		t.Error("the Sets page creates a set from createDefaultSet, which skips the user's template")
	}
}

func TestInvalidTemplateJSONIsAnUnsavedChange(t *testing.T) {
	block := readUI(t, "components/settings/NewSetTemplate.tsx")
	for want, why := range map[string]string{
		"onDraftChange?.(FIELD, value)":           "the block no longer hands its invalid text to the settings page, so Save would store the last valid draft while the box shows something else",
		"onDraftChange?.(FIELD, null)":            "the block no longer clears its invalid text on the settings page once the JSON is valid again",
		"keptText ?? formatSetTemplate(template)": "the block no longer restores its invalid text after a switch to another sub-tab and back",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("NewSetTemplate.tsx lost %q: %s", want, why)
		}
	}

	core := readUI(t, "components/settings/CoreSettings.tsx")
	for _, want := range []string{"onDraftChange={onFieldDraft}", "invalidDrafts={invalidDrafts}", "key={resetToken}"} {
		if !strings.Contains(core, want) {
			t.Errorf("CoreSettings no longer passes %s to the New set defaults block", want)
		}
	}

	page := readUI(t, "components/settings/Page.tsx")
	for want, why := range map[string]string{
		"const saveBlocked = invalidDrafts.size > 0;":     "invalid text no longer blocks Save",
		"const anyChanges = hasChanges || saveBlocked;":   "invalid text no longer counts as an unsaved change",
		"if (!config || saveBlocked) return;":             "Save runs with invalid text",
		"disabled={!hasChanges || saving || saveBlocked}": "the Save button ignores invalid text",
		"disabled={!anyChanges || saving}":                "Discard stays disabled for an edit that is only invalid",
		"{anyChanges && (":                                "the Modified chip ignores invalid text",
		"...invalidDrafts.keys()":                         "the sub-tab dot ignores invalid text",
		"total={changeItems.length + invalidDrafts.size}": "the unsaved-changes dialogs do not count invalid text",
		"onFieldDraft={handleFieldDraft}":                 "the settings page no longer receives invalid text",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the settings page lost %q: %s", want, why)
		}
	}
	if n := strings.Count(page, "setInvalidDrafts(new Map());"); n < 2 {
		t.Errorf("invalid text is cleared in %d places; loading the config and Discard must both clear it", n)
	}
}

func TestNewSetTemplateStringsExistInBothLocales(t *testing.T) {
	keys := []string{
		"core.saveBlockedInvalid",
		"settings.coreTabs.other",
		"settings.NewSetTemplate.title",
		"settings.NewSetTemplate.info",
		"settings.NewSetTemplate.label",
		"settings.NewSetTemplate.placeholder",
		"settings.NewSetTemplate.help",
		"settings.NewSetTemplate.invalid",
		"settings.NewSetTemplate.clear",
		"sets.card.useForNewSets",
		"sets.templateDialog.title",
		"sets.templateDialog.intro",
		"sets.templateDialog.empty",
		"sets.templateDialog.note",
		"sets.templateDialog.confirm",
		"sets.templateDialog.saved",
		"sets.templateDialog.failed",
		"errors.invalid_set_template",
	}
	for _, locale := range locales {
		doc := loadLocale(t, locale)
		for _, key := range keys {
			requireKey(t, doc, locale, key)
		}
	}
}
