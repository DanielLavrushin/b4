package uicheck

import (
	"regexp"
	"testing"
)

var (
	fileInputRe  = regexp.MustCompile(`(?s)<input\b[^<]*?type="file".*?/>`)
	inputResetRe = regexp.MustCompile(`(?:target|currentTarget)\.value\s*=\s*""|\bkey=\{`)
)

func TestPayloadFileInputIsResetAfterReading(t *testing.T) {
	inputs := fileInputRe.FindAllString(payloadsPageSource(t), -1)
	if len(inputs) == 0 {
		t.Fatal("the Payloads page has no file input")
	}
	for _, in := range inputs {
		if !inputResetRe.MatchString(in) {
			t.Errorf("the file input is neither cleared after a file is read nor keyed for a remount, so choosing the same file again fires nothing:\n%s", in)
		}
	}
}
