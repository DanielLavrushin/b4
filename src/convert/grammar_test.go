package convert

import "testing"

func TestParsePosV013_Valid(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		offset int
		anchor Anchor
		rel    Rel
	}{
		{"plain", "1", 1, AnchorAbs, RelStart},
		{"negative", "-1", -1, AnchorAbs, RelStart},
		{"hex", "0x10", 16, AnchorAbs, RelStart},
		{"sni", "2+s", 2, AnchorSNI, RelStart},
		{"host", "3+h", 3, AnchorHost, RelStart},
		{"end", "4+e", 4, AnchorPacket, RelEnd},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := parsePosV013(tt.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.Offset != tt.offset || p.Anchor != tt.anchor || p.Rel != tt.rel {
				t.Fatalf("got offset=%d anchor=%s rel=%s, want %d/%s/%s", p.Offset, p.Anchor, p.Rel, tt.offset, tt.anchor, tt.rel)
			}
		})
	}
}

func TestParsePosV013_Rejects(t *testing.T) {
	for _, in := range []string{"1:11+sm", "1+sm", "1+x", "abc", "1+", "1junk"} {
		t.Run(in, func(t *testing.T) {
			if _, err := parsePosV013(in); err == nil {
				t.Fatalf("expected %q to be rejected", in)
			}
		})
	}
}

func TestParsePosV017_Valid(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		offset  int
		repeats int
		skip    int
		anchor  Anchor
		rel     Rel
	}{
		{"plain", "1", 1, 0, 0, AnchorAbs, RelStart},
		{"negative", "-1", -1, 0, 0, AnchorAbs, RelStart},
		{"sniMid", "1:11+sm", 1, 11, 0, AnchorSNI, RelMid},
		{"repeatsSkip", "1:3:5", 1, 3, 5, AnchorAbs, RelStart},
		{"sniStart", "0+s", 0, 0, 0, AnchorSNI, RelStart},
		{"sniEnd", "0+se", 0, 0, 0, AnchorSNI, RelEnd},
		{"sniRand", "0+sr", 0, 0, 0, AnchorSNI, RelRand},
		{"hostMid", "2+hm", 2, 0, 0, AnchorHost, RelMid},
		{"packetMid", "0+nm", 0, 0, 0, AnchorPacket, RelMid},
		{"nullBase", "5+n", 5, 0, 0, AnchorAbs, RelStart},
		{"unknownSecondCharIgnored", "5+sX", 5, 0, 0, AnchorSNI, RelStart},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := parsePosV017(tt.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.Offset != tt.offset || p.Repeats != tt.repeats || p.Skip != tt.skip || p.Anchor != tt.anchor || p.Rel != tt.rel {
				t.Fatalf("got %+v, want offset=%d repeats=%d skip=%d anchor=%s rel=%s",
					p, tt.offset, tt.repeats, tt.skip, tt.anchor, tt.rel)
			}
		})
	}
}

func TestParsePosV017_Rejects(t *testing.T) {
	for _, in := range []string{"1:0", "abc", "1+x", "1+"} {
		t.Run(in, func(t *testing.T) {
			if _, err := parsePosV017(in); err == nil {
				t.Fatalf("expected %q to be rejected", in)
			}
		})
	}
}

func TestParseCForm_Escapes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "abc", "abc"},
		{"newline", `a\nb`, "a\nb"},
		{"hex", `\x41\x42`, "AB"},
		{"octal", `\101`, "A"},
		{"backslash", `a\\b`, `a\b`},
		{"crlf", `\r\n`, "\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCForm(tt.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(got) != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGCSVFirstChar_MatchesFirstCharacterOnly(t *testing.T) {
	v, err := gCSVFirstChar("torst,redirect,ssl_err", grammarCtx{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"t", "r", "s"}
	if len(v.List) != len(want) {
		t.Fatalf("got %v, want %v", v.List, want)
	}
	for i := range want {
		if v.List[i] != want[i] {
			t.Fatalf("got %v, want %v", v.List, want)
		}
	}
}

func TestGCSVKeyValue_CapturesMSize(t *testing.T) {
	v, err := gCSVKeyValue("rand,msize=100", grammarCtx{})
	if err != nil {
		t.Fatal(err)
	}
	if len(v.List) != 2 || v.List[0] != "r" || v.List[1] != "m=100" {
		t.Fatalf("got %v", v.List)
	}
}

func TestGHostList_InlineVsFile(t *testing.T) {
	inline, err := gHostList(":a.com b.com,c.com", grammarCtx{})
	if err != nil {
		t.Fatal(err)
	}
	if len(inline.List) != 3 {
		t.Fatalf("got %v", inline.List)
	}
	file, err := gHostList("/etc/byedpi/hosts.txt", grammarCtx{})
	if err != nil {
		t.Fatal(err)
	}
	if file.Ref != "/etc/byedpi/hosts.txt" {
		t.Fatalf("got ref %q", file.Ref)
	}
}

func TestSanitizeHost(t *testing.T) {
	tests := []struct{ in, want string }{
		{"https://www.gosuslugi.ru/", "www.gosuslugi.ru"},
		{"www.gosuslugi.ru", "www.gosuslugi.ru"},
		{"example.com:443", "example.com"},
		{"http://example.com/path?a=b", "example.com"},
		{"", ""},
		{"not a host", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := sanitizeHost(tt.in); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
