package notify

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	telegramLimit = 4096
	textBudget    = 3900
	fieldRunes    = 80
	buildTime     = "2006-01-02 15:04 UTC"
)

type plural struct {
	one, few, many string
}

type catalog struct {
	counts   map[string]plural
	sections map[string]string
	reports  plural
	more     string
	console  string
	test     string
	rule     func(n int) int
}

func englishRule(n int) int {
	if n == 1 {
		return 0
	}
	return 2
}

func russianRule(n int) int {
	mod10, mod100 := n%10, n%100
	switch {
	case mod10 == 1 && mod100 != 11:
		return 0
	case mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14):
		return 1
	}
	return 2
}

var catalogs = map[string]catalog{
	LanguageEN: {
		counts: map[string]plural{
			EventShare:       {"new set", "new sets", "new sets"},
			EventReport:      {"report", "reports", "reports"},
			EventAutoHide:    {"set hidden by reports", "sets hidden by reports", "sets hidden by reports"},
			EventBuildFailed: {"failed build", "failed builds", "failed builds"},
			EventMirror:      {"new mirror", "new mirrors", "new mirrors"},
		},
		sections: map[string]string{
			EventShare:       "New sets in the queue",
			EventReport:      "Reports",
			EventAutoHide:    "Hidden by reports",
			EventBuildFailed: "Failed builds",
			EventMirror:      "Mirrors waiting for approval",
		},
		reports: plural{"report", "reports", "reports"},
		more:    "and %d more",
		console: "Console: %s",
		test:    "b4hub: test notification. This channel is set up and delivers messages.",
		rule:    englishRule,
	},
	LanguageRU: {
		counts: map[string]plural{
			EventShare:       {"новый сет", "новых сета", "новых сетов"},
			EventReport:      {"жалоба", "жалобы", "жалоб"},
			EventAutoHide:    {"сет скрыт по жалобам", "сета скрыты по жалобам", "сетов скрыто по жалобам"},
			EventBuildFailed: {"неудачная сборка", "неудачные сборки", "неудачных сборок"},
			EventMirror:      {"новое зеркало", "новых зеркала", "новых зеркал"},
		},
		sections: map[string]string{
			EventShare:       "Новые сеты в очереди",
			EventReport:      "Жалобы",
			EventAutoHide:    "Скрыты по жалобам",
			EventBuildFailed: "Неудачные сборки",
			EventMirror:      "Зеркала ждут одобрения",
		},
		reports: plural{"жалоба", "жалобы", "жалоб"},
		more:    "и ещё %d",
		console: "Консоль: %s",
		test:    "b4hub: тестовое уведомление. Канал настроен и доставляет сообщения.",
		rule:    russianRule,
	},
}

func catalogFor(lang string) catalog {
	if c, ok := catalogs[lang]; ok {
		return c
	}
	return catalogs[LanguageEN]
}

func (c catalog) count(p plural, n int) string {
	form := p.many
	switch c.rule(n) {
	case 0:
		form = p.one
	case 1:
		form = p.few
	}
	return strconv.Itoa(n) + " " + form
}

func (c catalog) header(d digest) string {
	parts := make([]string, 0, len(d.batches))
	for _, b := range d.batches {
		parts = append(parts, c.count(c.counts[b.Kind], b.Count))
	}
	return "b4hub: " + strings.Join(parts, ", ")
}

func setLabel(e store.NotifyEvent) string {
	ref := e.SetID + " v" + strconv.Itoa(e.Version)
	if title := cleanText(e.Title, fieldRunes); title != "" {
		return title + " (" + ref + ")"
	}
	return ref
}

func (c catalog) line(e store.NotifyEvent) string {
	var b strings.Builder
	b.WriteString("- ")
	switch e.Kind {
	case EventShare:
		b.WriteString(setLabel(e))
		if asn := cleanText(e.ASN, 20); asn != "" {
			b.WriteString(", AS" + asn)
		}
		if country := cleanText(e.Country, 4); country != "" {
			b.WriteString(" " + country)
		}
	case EventReport:
		b.WriteString(setLabel(e))
		if reason := cleanText(e.Reason, fieldRunes); reason != "" {
			b.WriteString(": " + reason)
		}
	case EventAutoHide:
		b.WriteString(setLabel(e))
		if e.Reports > 0 {
			b.WriteString(", " + c.count(c.reports, e.Reports))
		}
	case EventBuildFailed:
		b.WriteString("#" + strconv.FormatInt(e.ID, 10))
		if !e.At.IsZero() {
			b.WriteString(", " + e.At.UTC().Format(buildTime))
		}
		if reason := cleanText(e.Reason, fieldRunes); reason != "" {
			b.WriteString(": " + reason)
		}
	case EventMirror:
		b.WriteString(cleanText(e.URL, 200))
		if version := cleanText(e.Detail, 40); version != "" {
			b.WriteString(" (" + version + ")")
		}
	}
	return b.String()
}

func telegramText(lang string, d digest, console string) string {
	c := catalogFor(lang)
	head := c.header(d)
	foot := ""
	if console != "" {
		foot = "\n\n" + fmt.Sprintf(c.console, console)
	}
	used := textLen(head) + textLen(foot)
	var b strings.Builder
	b.WriteString(head)
	for _, batch := range d.batches {
		section := "\n\n" + c.sections[batch.Kind] + " (" + strconv.Itoa(batch.Count) + "):"
		moreReserve := textLen("\n" + fmt.Sprintf(c.more, batch.Count))
		if used+textLen(section)+moreReserve > textBudget {
			break
		}
		b.WriteString(section)
		used += textLen(section)
		shown := 0
		for _, e := range batch.Events {
			line := "\n" + c.line(e)
			if used+textLen(line)+moreReserve > textBudget {
				break
			}
			b.WriteString(line)
			used += textLen(line)
			shown++
		}
		if rest := batch.Count - shown; rest > 0 {
			more := "\n" + fmt.Sprintf(c.more, rest)
			b.WriteString(more)
			used += textLen(more)
		}
	}
	b.WriteString(foot)
	return clipUnits(b.String(), telegramLimit)
}

func testText(lang string) string {
	return catalogFor(lang).test
}

func textLen(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

func clipUnits(s string, limit int) string {
	if textLen(s) <= limit {
		return s
	}
	n := 0
	for i, r := range s {
		w := utf16.RuneLen(r)
		if n+w > limit {
			return s[:i]
		}
		n += w
	}
	return s
}

func invisible(r rune) bool {
	switch {
	case r >= 0x200b && r <= 0x200f:
		return true
	case r >= 0x202a && r <= 0x202e:
		return true
	case r >= 0x2066 && r <= 0x2069:
		return true
	case r == 0xfeff:
		return true
	}
	return false
}

func cleanText(raw string, limit int) string {
	var b strings.Builder
	space := false
	for _, r := range raw {
		switch {
		case unicode.IsSpace(r):
			space = b.Len() > 0
			continue
		case unicode.IsControl(r) || invisible(r):
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
	}
	out := b.String()
	runes := []rune(out)
	switch {
	case limit <= 0 || len(runes) <= limit:
		return out
	case limit <= 3:
		return string(runes[:limit])
	}
	return strings.TrimSpace(string(runes[:limit-3])) + "..."
}
