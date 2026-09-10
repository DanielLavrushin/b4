package log

import (
	"bytes"
	"testing"
)

func TestCapLevelKeepsErrorsAndDropsChatter(t *testing.T) {
	var sink bytes.Buffer
	w := CapLevel(&sink, LevelError)
	lines := []string{
		"2026/09/10 10:00:00.000001 [INFO] matched connection\n",
		"2026/09/10 10:00:00.000002 [ERROR] nft failed\n",
		"  continuation of the error\n",
		"2026/09/10 10:00:00.000003 [WARN] something odd\n",
		"2026/09/10 10:00:00.000004 [TRACE] packet\n",
		"  continuation of the trace\n",
		"2026/09/10 10:00:00.000005 [DEBUG] more\n",
	}
	for _, l := range lines {
		if _, err := w.Write([]byte(l)); err != nil {
			t.Fatal(err)
		}
	}
	got := sink.String()
	want := lines[1] + lines[2] + lines[3]
	if got != want {
		t.Fatalf("console cap at error level passed through:\n%q\nwant:\n%q", got, want)
	}
}

func TestCapLevelReassemblesSplitWrites(t *testing.T) {
	var sink bytes.Buffer
	w := CapLevel(&sink, LevelInfo)
	chunks := []string{
		"2026/09/10 10:00:00.000001 [TR",
		"ACE] half a line\n2026/09/10 10:00:00.000002 [INFO] whole",
		" line\n",
	}
	for _, c := range chunks {
		if _, err := w.Write([]byte(c)); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := sink.String(), "2026/09/10 10:00:00.000002 [INFO] whole line\n"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestParseLevelAcceptsNamesAndNumbers(t *testing.T) {
	cases := map[string]Level{"error": LevelError, "0": LevelError, "Info": LevelInfo, "2": LevelTrace, "debug": LevelDebug}
	for in, want := range cases {
		got, ok := ParseLevel(in)
		if !ok || got != want {
			t.Fatalf("ParseLevel(%q) = %v,%v want %v", in, got, ok, want)
		}
	}
	if _, ok := ParseLevel(""); ok {
		t.Fatal("an empty value must not parse as a level")
	}
	if _, ok := ParseLevel("warn"); ok {
		t.Fatal("b4 has no warn level, so it must not parse")
	}
}
