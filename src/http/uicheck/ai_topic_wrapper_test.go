package uicheck

import (
	"regexp"
	"testing"
)

func TestFieldsWithAnAiTopicKeepTheirWrapperWhileTheAssistantLoads(t *testing.T) {
	bare := regexp.MustCompile(`if \(!helpVisible\) return \w+;`)
	kept := regexp.MustCompile(`if \(!helpVisible && !aiTopic\) return \w+;`)
	button := regexp.MustCompile(`\{helpVisible && \(\s*<Box sx=\{\{ mt: "6px" \}\}>\s*<B4Help\b`)
	for _, rel := range []string{
		"components/common/B4TextField.tsx",
		"components/common/B4Select.tsx",
	} {
		src := readUI(t, rel)
		if bare.MatchString(src) {
			t.Errorf("%s returns the bare control while help is hidden, so a field with only an AI topic is remounted when the assistant's status arrives", rel)
		}
		if !kept.MatchString(src) {
			t.Errorf("%s does not keep its wrapper for a field with an AI topic", rel)
		}
		if !button.MatchString(src) {
			t.Errorf("%s does not leave the help button out of the kept wrapper while help is hidden", rel)
		}
	}
}
