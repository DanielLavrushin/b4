package discovery

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxNoteRunes = 200

func NormalizeNote(raw string) (string, bool) {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, raw)
	note := strings.Join(strings.Fields(cleaned), " ")
	if utf8.RuneCountInString(note) > MaxNoteRunes {
		return "", false
	}
	return note, true
}

func keptNote(suite *CheckSuite, note, previous string) string {
	if note != "" || suite.Source == "" || suite.Source == SourceWeb {
		return note
	}
	return previous
}
