package uicheck

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var probeStatusRe = regexp.MustCompile(`DomainStatus\s*=\s*"([A-Z0-9_]+)"`)

func probeStatuses(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, rel := range []string{"../../netprobe/classify.go", "../../watchdog/probe.go"} {
		raw, err := os.ReadFile(rel)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		for _, m := range probeStatusRe.FindAllStringSubmatch(string(raw), -1) {
			out = append(out, m[1])
		}
	}
	if len(out) < 10 || !slices.Contains(out, "REDIRECT") || !slices.Contains(out, "TLS_RST") {
		t.Fatalf("the probe statuses were not found where expected: %v", out)
	}
	return out
}

var cardStatusRe = regexp.MustCompile("t\\(\\s*`detector\\.status\\.\\$\\{result\\.status\\}`")

func TestTheHubTestDialogSpeaksTheUIsLanguage(t *testing.T) {
	src := readUI(t, "components/hub/TestDialog.tsx")
	if strings.Contains(src, "describeApiError") || !strings.Contains(src, "localizeApiError(test.error)") {
		t.Error("the Test dialog shows the server's English error text instead of its translation")
	}
	if !cardStatusRe.MatchString(src) {
		t.Error("the probe cards show the raw status code instead of its label")
	}
	var keys []string
	for _, status := range probeStatuses(t) {
		keys = append(keys, "detector.status."+status)
	}
	requireKeysInBothLocales(t, keys)
}
