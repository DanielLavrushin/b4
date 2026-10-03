package metrics

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestParseStatTicksReadsAfterTheLastParen(t *testing.T) {
	line := []byte("4242 (b4 (odd) name) S 1 4242 4242 0 -1 4194560 2181 0 3 0 1234 567 0 0 20 0 9 0 1859 1277952 1031 18446744073709551615 1 1 0 0 0 0 0 0 0 0 0 0 17 2 0 0 0 0 0\n")
	got, ok := parseStatTicks(line)
	if !ok || got != 1234+567 {
		t.Fatalf("utime+stime = %d ok=%v, want %d", got, ok, 1234+567)
	}
	if _, ok := parseStatTicks([]byte("4242 (b4) S 1 2")); ok {
		t.Fatal("a truncated stat line must not parse")
	}
	if _, ok := parseStatTicks([]byte("no paren")); ok {
		t.Fatal("a line without the comm field must not parse")
	}
}

func TestProcfsSamplerReadsThisProcess(t *testing.T) {
	var s procfsSampler
	if _, ok := s.cpuTicks(); !ok {
		t.Skip("no /proc/self/stat here")
	}
	if s.rss() == 0 {
		t.Fatal("rss of a running process is not 0")
	}
	if s.threads() < 1 {
		t.Fatal("at least one thread")
	}
	if s.memTotal() == 0 {
		t.Fatal("MemTotal must parse on Linux")
	}
}

func TestParseAuxvClkTck(t *testing.T) {
	entry := func(word int, tag, val uint64) []byte {
		b := make([]byte, 2*word)
		if word == 8 {
			binary.NativeEndian.PutUint64(b, tag)
			binary.NativeEndian.PutUint64(b[8:], val)
		} else {
			binary.NativeEndian.PutUint32(b, uint32(tag))
			binary.NativeEndian.PutUint32(b[4:], uint32(val))
		}
		return b
	}
	for _, word := range []int{4, 8} {
		var data []byte
		data = append(data, entry(word, 6, 4096)...)
		data = append(data, entry(word, 17, 250)...)
		data = append(data, entry(word, 0, 0)...)
		if got := parseAuxvClkTck(data, word); got != 250 {
			t.Errorf("word %d: got %d, want 250", word, got)
		}
		if got := parseAuxvClkTck(entry(word, 0, 0), word); got != 100 {
			t.Errorf("word %d: missing AT_CLKTCK must fall back to 100, got %d", word, got)
		}
	}
	if got := clkTck(); got == 0 {
		t.Fatal("clkTck must never be 0")
	}
}

func TestConntrackReadsCountAndMax(t *testing.T) {
	dir := t.TempDir()
	count := filepath.Join(dir, "nf_conntrack_count")
	max := filepath.Join(dir, "nf_conntrack_max")
	if err := os.WriteFile(count, []byte("930\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(max, []byte("1000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	savedCount, savedMax := conntrackCountPath, conntrackMaxPaths
	t.Cleanup(func() { conntrackCountPath, conntrackMaxPaths = savedCount, savedMax })

	conntrackCountPath = count
	conntrackMaxPaths = []string{filepath.Join(dir, "missing"), max}
	ct, ok := procfsSampler{}.conntrack()
	if !ok || ct.Count != 930 || ct.Max != 1000 {
		t.Fatalf("conntrack %+v ok=%v", ct, ok)
	}
	conntrackCountPath = filepath.Join(dir, "missing")
	if _, ok := (procfsSampler{}).conntrack(); ok {
		t.Fatal("an unreadable count must report not ok")
	}
}

func TestParseUintField(t *testing.T) {
	cases := map[string]struct {
		v  uint64
		ok bool
	}{
		"0": {0, true}, "42": {42, true}, "": {0, false}, "4x": {0, false}, "18446744073709551615": {^uint64(0), true}, "18446744073709551616": {0, false},
	}
	for in, want := range cases {
		v, ok := parseUintField([]byte(in))
		if v != want.v || ok != want.ok {
			t.Errorf("parseUintField(%q) = %d %v, want %d %v", in, v, ok, want.v, want.ok)
		}
	}
}
