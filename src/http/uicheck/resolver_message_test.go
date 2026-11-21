package uicheck

import (
	"regexp"
	"strings"
	"testing"
)

func TestResolverListWarningIsAnnouncedWithTheEntryField(t *testing.T) {
	src := readUI(t, "components/common/B4ResolverInput.tsx")
	at := strings.Index(src, "<B4TextField")
	if at < 0 {
		t.Fatal("B4ResolverInput.tsx has no entry field")
	}
	end := strings.Index(src[at:], "/>")
	if end < 0 {
		t.Fatal("the entry field of B4ResolverInput.tsx is never closed")
	}
	field := src[at : at+end]
	if regexp.MustCompile(`\bmessage=`).MatchString(field) {
		t.Error("the list warning went into the entry field's message slot, where the limit note of a full list would hide it")
	}
	if !strings.Contains(field, "id={fieldId}") {
		t.Error("the entry field has no id of its own, so its helper line cannot be named next to the list warning")
	}
	if !regexp.MustCompile(`htmlInput: \{ "aria-describedby": describedBy \}`).MatchString(field) {
		t.Error("the entry field does not name the list warning in aria-describedby")
	}
	if !regexp.MustCompile(`<Box id=\{messageId\}>\{shownMessage\}</Box>`).MatchString(src) {
		t.Error("the list warning has no id for the entry field to refer to")
	}
	if !regexp.MustCompile("`\\$\\{fieldId\\}-helper-text`").MatchString(src) {
		t.Error("aria-describedby drops the entry field's own helper line")
	}
}
