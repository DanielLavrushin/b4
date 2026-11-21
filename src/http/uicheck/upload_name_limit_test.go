package uicheck

import (
	"os"
	"regexp"
	"testing"
)

func TestUploadNameFieldHoldsTheServersNameLimit(t *testing.T) {
	goSrc, err := os.ReadFile("../../capture/manager.go")
	if err != nil {
		t.Fatal(err)
	}
	goLimit := regexp.MustCompile(`const MaxNameLength = (\d+)`).FindSubmatch(goSrc)
	if goLimit == nil {
		t.Fatal("capture/manager.go has no MaxNameLength")
	}

	model := readUI(t, "models/capture.ts")
	uiLimit := regexp.MustCompile(`export const PAYLOAD_NAME_MAX_LENGTH\s*=\s*(\d+);`).FindStringSubmatch(model)
	if uiLimit == nil {
		t.Fatal("models/capture.ts holds no PAYLOAD_NAME_MAX_LENGTH, the upload name limit of capture.MaxNameLength")
	}
	if uiLimit[1] != string(goLimit[1]) {
		t.Errorf("PAYLOAD_NAME_MAX_LENGTH is %s, capture.MaxNameLength is %s", uiLimit[1], goLimit[1])
	}

	page := readUI(t, "components/settings/Capture.tsx")
	field := jsxElement(t, page, "B4TextField", `t("settings.Capture.nameDomain")`)
	if !regexp.MustCompile(`htmlInput:\s*\{[^}]*\bmaxLength:\s*PAYLOAD_NAME_MAX_LENGTH\b`).MatchString(field) {
		t.Errorf("the upload name field takes names longer than the server accepts: %s", field)
	}
	if !regexp.MustCompile(`\.slice\(\s*0\s*,\s*PAYLOAD_NAME_MAX_LENGTH\s*\)`).MatchString(page) {
		t.Error("a name pre-filled from the file name or its SNI can be longer than the server accepts")
	}
}
