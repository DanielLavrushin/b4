package utils

import (
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"
)

func TestSplitKeepingURLs(t *testing.T) {
	lenient := func(r rune) bool { return r == ',' || r == ';' || r == '|' || unicode.IsSpace(r) }
	commaOrSpace := func(r rune) bool { return r == ',' || unicode.IsSpace(r) }
	commaOnly := func(r rune) bool { return r == ',' }

	for _, tc := range []struct {
		name  string
		raw   string
		isSep func(rune) bool
		want  []string
	}{
		{"plain list", " a.com, b.com | c.com;\td.com. ", lenient, []string{"a.com", "b.com", "c.com", "d.com."}},
		{"comma in a query", "https://example.com/search?q=one,two", lenient, []string{"https://example.com/search?q=one,two"}},
		{"semicolons in a path", "https://example.com/a;b=c;d=e", lenient, []string{"https://example.com/a;b=c;d=e"}},
		{"pipe in a query", "https://example.com/?x=a|b", lenient, []string{"https://example.com/?x=a|b"}},
		{"separators in credentials", "https://us,er:p;w@example.com/x,y", lenient, []string{"https://us,er:p;w@example.com/x,y"}},
		{"empty pieces inside a URL", "https://example.com/q?a=1,,2;3", lenient, []string{"https://example.com/q?a=1,,2;3"}},
		{"two URLs", "https://a.com/x,https://b.com/y", lenient, []string{"https://a.com/x", "https://b.com/y"}},
		{"a domain before a URL", "youtube.com,https://example.com/q?a=1;2", lenient, []string{"youtube.com", "https://example.com/q?a=1;2"}},
		{"whitespace ends a URL", "https://example.com/q?a=1,2 ntc.party", lenient, []string{"https://example.com/q?a=1,2", "ntc.party"}},
		{"a separator followed by a space ends a URL", "https://a.com/x, b.com", commaOnly, []string{"https://a.com/x", "b.com"}},
		{"a space before a separator ends a URL", "https://a.com/x ,b.com", commaOnly, []string{"https://a.com/x", "b.com"}},
		{"a trailing separator", "https://example.com,", lenient, []string{"https://example.com"}},
		{"a quoted URL", "\"https://example.com/a,b\"", commaOrSpace, []string{"\"https://example.com/a,b\""}},
		{"case is kept", "https://i.ytimg.com/vi/AbC,dEf/hq.jpg", commaOrSpace, []string{"https://i.ytimg.com/vi/AbC,dEf/hq.jpg"}},
		{"a space is not a separator here", "a b.com", commaOnly, []string{"a b.com"}},
		{"comma-only list", "a.com,b.com , c.com", commaOnly, []string{"a.com", "b.com", "c.com"}},
		{"nothing", "  ,;| ", lenient, nil},
		{"a bare host right after a URL's host", "https://a.com,b.com", lenient, []string{"https://a.com", "b.com"}},
		{"a bare host after a URL's host, comma only", "https://a.com,b.com", commaOnly, []string{"https://a.com", "b.com"}},
		{"a bare host after a host and port", "https://a.com:8443;b.com", lenient, []string{"https://a.com:8443", "b.com"}},
		{"a bare host after an IPv6 host", "https://[2001:db8::1],b.com", lenient, []string{"https://[2001:db8::1]", "b.com"}},
		{"a separator in the credentials before an at sign", "https://a.com,b.com@c.com/x", lenient, []string{"https://a.com,b.com@c.com/x"}},
		{"a closing quote ends a URL", "\"https://a.com/x\",\"b.com\"", commaOrSpace, []string{"\"https://a.com/x\"", "\"b.com\""}},
	} {
		if got := SplitKeepingURLs(tc.raw, tc.isSep); !slices.Equal(got, tc.want) {
			t.Errorf("%s: SplitKeepingURLs(%q) = %q, want %q", tc.name, tc.raw, got, tc.want)
		}
	}
}

func TestSplitKeepingURLsStaysLinearOnSeparatorHeavyInput(t *testing.T) {
	commaOnly := func(r rune) bool { return r == ',' }
	for name, raw := range map[string]string{
		"credentials": "https://" + strings.Repeat("u,", 1<<18) + "user@example.com/",
		"query":       "https://example.com/?" + strings.Repeat("a,", 1<<18),
		"host part":   "https://" + strings.Repeat("a,", 1<<18),
	} {
		done := make(chan int, 1)
		go func() { done <- len(SplitKeepingURLs(raw, commaOnly)) }()
		select {
		case n := <-done:
			if name != "host part" && n != 1 {
				t.Errorf("%s: %d items, want the one URL", name, n)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: splitting %d KiB took over 5 s, so each separator rescans the input", name, len(raw)>>10)
		}
	}
}
