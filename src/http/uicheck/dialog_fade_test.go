package uicheck

import (
	"regexp"
	"strings"
	"testing"
)

type pageDialog struct {
	open     string
	title    string
	children string
}

func payloadsPageDialogs(t *testing.T, page string) []pageDialog {
	t.Helper()
	var out []pageDialog
	rest := page
	for {
		at := strings.Index(rest, "<B4Dialog")
		if at < 0 {
			return out
		}
		indent := rest[strings.LastIndex(rest[:at], "\n")+1 : at]
		tagEnd := strings.Index(rest[at:], "\n"+indent+">\n")
		closeAt := strings.Index(rest[at:], "</B4Dialog>")
		if tagEnd < 0 || closeAt < tagEnd {
			t.Fatalf("a B4Dialog on the Payloads page has no opening tag of its own: %.80s", rest[at:])
		}
		tag := rest[at : at+tagEnd]
		open := regexp.MustCompile(`\bopen=\{\s*(\w+)\s*!==\s*null\s*\}`).FindStringSubmatch(tag)
		title := regexp.MustCompile(`(?s)\btitle=\{(.*?)\}\n\s+(?:subtitle|icon)=`).FindStringSubmatch(tag)
		if open == nil || title == nil {
			t.Fatalf("a B4Dialog on the Payloads page has no open={x !== null} or no title: %.200s", tag)
		}
		out = append(out, pageDialog{open: open[1], title: title[1], children: rest[at+tagEnd : at+closeAt]})
		rest = rest[at+closeAt:]
	}
}

func TestPayloadDialogsKeepTheirTextWhileTheyFadeOut(t *testing.T) {
	page := readUI(t, "components/settings/Capture.tsx")
	dialogs := payloadsPageDialogs(t, page)
	if len(dialogs) < 3 {
		t.Fatalf("the Payloads page has %d dialogs, fewer than Delete, Replace and Regenerate", len(dialogs))
	}
	for _, d := range dialogs {
		live := regexp.MustCompile(`\b` + d.open + `\b`)
		if live.MatchString(d.title) {
			t.Errorf("the title of the %s dialog reads %s, which is null while the dialog fades out: %s", d.open, d.open, strings.TrimSpace(d.title))
		}
		if live.MatchString(d.children) {
			t.Errorf("the body of the %s dialog reads %s, which is null while the dialog fades out", d.open, d.open)
		}
		kept := regexp.MustCompile(`const (\w+)\s*=\s*useLastTarget\(\s*` + d.open + `\s*\)`).FindStringSubmatch(page)
		if kept == nil {
			t.Errorf("the %s dialog does not keep its last target with useLastTarget", d.open)
			continue
		}
		if !regexp.MustCompile(`\b` + kept[1] + `\b`).MatchString(d.title + d.children) {
			t.Errorf("the %s dialog never shows %s, its last target", d.open, kept[1])
		}
	}

	hook := readUI(t, "hooks/useLastTarget.ts")
	if !regexp.MustCompile(`export function useLastTarget\b`).MatchString(hook) {
		t.Error("hooks/useLastTarget.ts exports no useLastTarget")
	}
}
