package sock

import (
	"crypto/rand"
	"strings"

	"github.com/daniellavrushin/b4/sni"
)

const (
	hostLetters = "abcdefghijklmnopqrstuvwxyz"
	hostAlnum   = "abcdefghijklmnopqrstuvwxyz0123456789"
)

func MirrorClientHello(real, fakePayload []byte) []byte {
	s, e, ok := sni.LocateServerName(real)
	if !ok || e <= s {
		return nil
	}
	template, _, found := sni.ParseTLSClientHelloSNI(fakePayload)
	if !found {
		return nil
	}
	out := cloneBytes(real)
	copy(out[s:e], FitHostName(template, e-s))
	return out
}

func FitHostName(template string, n int) string {
	if n <= 0 {
		return ""
	}
	switch {
	case len(template) == n:
		return template
	case n < 4:
		return randomLabel(n)
	case len(template) == n-1:
		return randomLabel(1) + template
	case len(template) < n:
		return randomLabels(n-len(template)-1) + "." + template
	}
	tail := template[len(template)-n:]
	if strings.Contains(tail[1:], ".") {
		if tail[0] == '.' || tail[0] == '-' {
			return randomLabel(1) + tail[1:]
		}
		return tail
	}
	tld := template[strings.LastIndexByte(template, '.')+1:]
	keep := len(tld)
	if keep > n-2 {
		keep = n - 2
	}
	return randomLabel(n-1-keep) + "." + tld[len(tld)-keep:]
}

func randomLabels(n int) string {
	if n <= 63 {
		return randomLabel(n)
	}
	first := 63
	if n-first-1 < 1 {
		first = n - 2
	}
	return randomLabel(first) + "." + randomLabels(n-first-1)
}

func randomLabel(n int) string {
	if n <= 0 {
		return ""
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		for i := range buf {
			buf[i] = byte(i * 7)
		}
	}
	out := make([]byte, n)
	out[0] = hostLetters[int(buf[0])%len(hostLetters)]
	for i := 1; i < n; i++ {
		out[i] = hostAlnum[int(buf[i])%len(hostAlnum)]
	}
	return string(out)
}
