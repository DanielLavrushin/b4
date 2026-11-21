package uicheck

import (
	"regexp"
	"testing"
)

func TestPayloadButtonsHaveTheirOwnBusyFlags(t *testing.T) {
	hook := readUI(t, "hooks/useCapture.ts")
	for _, flag := range []string{"uploading", "generating"} {
		decl := regexp.MustCompile(`\[` + flag + `,\s*set` + string(flag[0]-32) + flag[1:] + `\]\s*=\s*useState`)
		if !decl.MatchString(hook) {
			t.Errorf("useCaptures keeps no %s state of its own", flag)
		}
	}

	page := payloadsPageSource(t)
	for label, want := range map[string]string{
		`t\(\s*"core\.uploading"\s*\)`:               "uploading",
		`t\(\s*"settings\.Capture\.generating"\s*\)`: "generating",
	} {
		re := regexp.MustCompile(`(\w+)\s*\?\s*` + label)
		matches := re.FindAllStringSubmatch(page, -1)
		if len(matches) == 0 {
			t.Errorf("the Payloads page never shows %s", label)
		}
		for _, m := range matches {
			if m[1] != want {
				t.Errorf("the label %s follows %q instead of %q, so a list refresh changes it", label, m[1], want)
			}
		}
	}
}
