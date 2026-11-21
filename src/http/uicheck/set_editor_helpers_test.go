package uicheck

import (
	"regexp"
	"strings"
	"testing"
)

const (
	tcpFakingFile = "components/sets/tcp/TcpFaking.tsx"
	udpFile       = "components/sets/Udp.tsx"
)

func jsxElement(t *testing.T, src, tag, marker string) string {
	t.Helper()
	at := strings.Index(src, marker)
	if at < 0 {
		t.Fatalf("no %s element with %s", tag, marker)
	}
	start := strings.LastIndex(src[:at], "<"+tag)
	if start < 0 {
		t.Fatalf("no <%s before %s", tag, marker)
	}
	end := regexp.MustCompile(`\n\s*/>`).FindStringIndex(src[at:])
	if end == nil {
		t.Fatalf("the <%s with %s never closes", tag, marker)
	}
	return src[start : at+end[1]]
}

func filteredListName(src, list, protocol string) string {
	re := regexp.MustCompile(`const\s+(\w+)\s*=\s*` + list + `\s*\.filter\(\s*\(?\s*(\w+)\s*\)?\s*=>\s*(\w+)\.protocol\s*===\s*"` + protocol + `"`)
	m := re.FindStringSubmatch(src)
	if m == nil || m[2] != m[3] {
		return ""
	}
	return m[1]
}
