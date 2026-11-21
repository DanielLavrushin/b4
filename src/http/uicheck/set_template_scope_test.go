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

func TestNewSetTemplateStringsExistInBothLocales(t *testing.T) {
	keys := []string{
		"settings.coreTabs.sets",
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
