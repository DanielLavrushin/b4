package uicheck

import (
	"regexp"
	"testing"
)

func TestSelectIsNamedByItsLabelAndDescribedByItsHelper(t *testing.T) {
	src := readUI(t, "components/common/B4Select.tsx")
	wired := []struct {
		what string
		re   *regexp.Regexp
	}{
		{"the label has no id", regexp.MustCompile(`<InputLabel\s+id=\{labelId\}`)},
		{"the select is not named by its label", regexp.MustCompile(`<Select\b[^>]*?\blabelId=\{labelId\}`)},
		{"the select has no id, so its value is not read with the label", regexp.MustCompile(`<Select\b[^>]*?\bid=\{selectId\}`)},
		{"the select does not point at its helper line or message", regexp.MustCompile(`<Select\b[^>]*?\baria-describedby=\{described \? helperId : undefined\}`)},
		{"the helper line has no id", regexp.MustCompile(`<FormHelperText\s+id=\{helperId\}`)},
		{"the message has no id", regexp.MustCompile(`<Box id=\{helperId\}>\{shownMessage\}</Box>`)},
	}
	for _, w := range wired {
		if !w.re.MatchString(src) {
			t.Errorf("B4Select: %s", w.what)
		}
	}
}
