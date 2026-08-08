package convert

import "testing"

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
