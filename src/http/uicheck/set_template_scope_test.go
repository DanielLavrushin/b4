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

func TestInvalidTemplateJSONBlocksTheSettingsSave(t *testing.T) {
	block := readUI(t, "components/settings/NewSetTemplate.tsx")
	if !strings.Contains(block, "onInvalidChange?.(FIELD, invalid)") {
		t.Error("the New set defaults block no longer tells the settings page that its JSON is invalid, so Save would store the last valid draft while the box shows something else")
	}

	core := readUI(t, "components/settings/CoreSettings.tsx")
	for _, want := range []string{"onInvalidChange={onFieldInvalid}", "key={resetToken}"} {
		if !strings.Contains(core, want) {
			t.Errorf("CoreSettings no longer passes %s to the New set defaults block", want)
		}
	}

	page := readUI(t, "components/settings/Page.tsx")
	for _, want := range []string{
		"const saveBlocked = invalidFields.size > 0;",
		"if (!config || saveBlocked) return;",
		"disabled={!hasChanges || saving || saveBlocked}",
		"onFieldInvalid={handleFieldInvalid}",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the settings page lost %q: invalid JSON in a block would no longer block Save", want)
		}
	}
	if n := strings.Count(page, "setInvalidFields(new Set());"); n < 2 {
		t.Errorf("the invalid-field flags are cleared in %d places; loading the config and Discard must both clear them", n)
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
