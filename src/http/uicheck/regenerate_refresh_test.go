package uicheck

import (
	"regexp"
	"strings"
	"testing"
)

func TestRegenerateDialogNamesTheSetsFromAFreshList(t *testing.T) {
	page := readUI(t, "components/settings/Capture.tsx")
	prop := regexp.MustCompile(`(?s)onRegenerate=\{(.*?)\n\s*\}\n`).FindStringSubmatch(page)
	if prop == nil {
		t.Fatal("no Regenerate action passed to the payload card")
	}
	if strings.Contains(prop[1], "setRegenerateDialog(") {
		t.Fatalf("Regenerate opens its dialog with used_by from the list as last loaded, while Delete refreshes it first: %s", strings.TrimSpace(prop[1]))
	}
	call := regexp.MustCompile(`\b(\w+)\(\s*capture\s*\)`).FindStringSubmatch(prop[1])
	if call == nil {
		t.Fatalf("the Regenerate action calls no handler with the payload: %s", strings.TrimSpace(prop[1]))
	}
	body := regexp.MustCompile(`(?s)const ` + call[1] + ` = async \([^)]*\) => \{(.*?)\n  \};\n`).FindStringSubmatch(page)
	if body == nil {
		t.Fatalf("no async %s handler on the Payloads page", call[1])
	}
	refresh := strings.Index(body[1], "refreshCaptures()")
	open := strings.Index(body[1], "setRegenerateDialog(")
	if refresh < 0 || open < 0 || refresh > open {
		t.Errorf("%s does not refresh the list before it opens the Regenerate dialog", call[1])
	}
	if !regexp.MustCompile(`t\(\s*"settings\.Capture\.regenerateFailed"\s*,\s*\{[^}]*error:`).MatchString(body[1]) {
		t.Errorf("%s does not show a failed list refresh with the server's text", call[1])
	}
}
