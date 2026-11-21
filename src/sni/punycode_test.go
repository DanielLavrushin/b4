package sni

import (
	"strings"
	"testing"
)

func TestPunycodeEncodesKnownLabels(t *testing.T) {
	for _, c := range []struct{ label, want string }{
		{"пример", "e1afmkfd"},
		{"рф", "p1ai"},
		{"яндекс", "d1acpjx3f"},
		{"bücher", "bcher-kva"},
		{"münchen", "mnchen-3ya"},
		{"例え", "r8jz45g"},
		{"テスト", "zckzah"},
		{"他们为什么不说中文", "ihqwcrb4cv8a8dqg056pqjye"},
		{"почемужеонинеговорятпорусски", "b1abfaaepdrnnbgefbadotcwatmq2g4l"},
		{"3年B組金八先生", "3B-ww4c5e180e575a65lsy2b"},
		{"ü", "tda"},
		{"aü", "a-eha"},
		{"ドメイン名例", "eckwd4c7cu47r2wf"},
		{"παράδειγμα", "hxajbheg2az3al"},
		{"δοκιμή", "jxalpdlp"},
	} {
		got, err := punycodeEncode(c.label)
		if err != nil || got != c.want {
			t.Errorf("punycodeEncode(%q) = %q, %v; want %q", c.label, got, err, c.want)
		}
	}
}

func TestHostToASCIIEncodesOnlyTheLabelsThatNeedIt(t *testing.T) {
	for _, c := range []struct{ host, want string }{
		{"пример.рф", "xn--e1afmkfd.xn--p1ai"},
		{"яндекс.рф", "xn--d1acpjx3f.xn--p1ai"},
		{"bücher.example", "xn--bcher-kva.example"},
		{"münchen.de", "xn--mnchen-3ya.de"},
		{"例え.テスト", "xn--r8jz45g.xn--zckzah"},
		{"www.пример.рф", "www.xn--e1afmkfd.xn--p1ai"},
		{"παράδειγμα.δοκιμή", "xn--hxajbheg2az3al.xn--jxalpdlp"},
		{"xn--e1afmkfd.xn--p1ai", "xn--e1afmkfd.xn--p1ai"},
		{"www.google.com", "www.google.com"},
	} {
		got, err := HostToASCII(c.host)
		if err != nil || got != c.want {
			t.Errorf("HostToASCII(%q) = %q, %v; want %q", c.host, got, err, c.want)
		}
	}
	for _, host := range []string{"bad\xffbyte.example", "bad�byte.example", "snow☃man.example", "пример。рф"} {
		if got, err := HostToASCII(host); err == nil {
			t.Errorf("HostToASCII(%q) = %q, want an error", host, got)
		}
	}
}

func TestHostToASCIIRefusesALabelTooLongBeforeEncodingIt(t *testing.T) {
	label := func(n int) string {
		runes := make([]rune, n)
		for i := range runes {
			runes[i] = rune(0x4E00 + i)
		}
		return string(runes)
	}
	for _, n := range []int{64, 10000} {
		if got, err := HostToASCII(label(n) + ".example"); err == nil {
			t.Errorf("a label of %d characters was encoded (%d bytes) instead of being refused, although no label over 63 characters can fit a host name", n, len(got))
		}
	}
}

func TestHostToASCIIRefusesALabelThatEncodesPastTheLimit(t *testing.T) {
	long := strings.Repeat("a", 57) + "ü.example"
	if got, err := HostToASCII(long); err == nil {
		t.Errorf("HostToASCII(%q) = %q, whose first label is %d bytes, want an error", long, got, len(strings.Split(got, ".")[0]))
	}
	if got, err := HostToASCII("пример.рф"); err != nil || got != "xn--e1afmkfd.xn--p1ai" {
		t.Errorf("HostToASCII(\"пример.рф\") = %q, %v; want xn--e1afmkfd.xn--p1ai", got, err)
	}
}

func TestValidHostNameKeepsTheDNSLimits(t *testing.T) {
	label63 := strings.Repeat("a", 63)
	for _, host := range []string{"example.com", label63 + ".example", "xn--e1afmkfd.xn--p1ai"} {
		if !ValidHostName(host) {
			t.Errorf("ValidHostName(%q) = false, want true", host)
		}
	}
	name254 := strings.Repeat(strings.Repeat("c", 49)+".", 5) + "cccc"
	for _, host := range []string{"", label63 + "a.example", "a..example", ".example", name254, "1.2.3.4", "bad host.example"} {
		if ValidHostName(host) {
			t.Errorf("ValidHostName(%q) = true, want false", host)
		}
	}
}
