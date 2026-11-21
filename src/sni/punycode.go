package sni

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	punyBase        = 36
	punyTMin        = 1
	punyTMax        = 26
	punySkew        = 38
	punyDamp        = 700
	punyInitialBias = 72
	punyInitialN    = 128
	punyACEPrefix   = "xn--"
)

const maxLabelLength = 63

var (
	errPunycodeOverflow = errors.New("punycode: overflow")
	errLabelTooLong     = errors.New("a host name label is longer than 63 characters")
)

func HostToASCII(host string) (string, error) {
	if !utf8.ValidString(host) {
		return "", errors.New("host name is not valid UTF-8")
	}
	labels := strings.Split(host, ".")
	for i, label := range labels {
		if isASCII(label) {
			continue
		}
		if utf8.RuneCountInString(label) > maxLabelLength {
			return "", errLabelTooLong
		}
		for _, r := range label {
			if r >= utf8.RuneSelf && !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r) {
				return "", fmt.Errorf("%q is not a letter, digit or mark a host name may carry", r)
			}
		}
		encoded, err := punycodeEncode(label)
		if err != nil {
			return "", err
		}
		labels[i] = punyACEPrefix + encoded
	}
	return strings.Join(labels, "."), nil
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

func punycodeEncode(label string) (string, error) {
	input := []rune(label)
	var out strings.Builder
	for _, r := range input {
		if r < utf8.RuneSelf {
			out.WriteRune(r)
		}
	}
	basic := out.Len()
	handled := basic
	if basic > 0 {
		out.WriteByte('-')
	}

	n, delta, bias := punyInitialN, 0, punyInitialBias
	for handled < len(input) {
		m := math.MaxInt32
		for _, r := range input {
			if int(r) >= n && int(r) < m {
				m = int(r)
			}
		}
		if m-n > (math.MaxInt32-delta)/(handled+1) {
			return "", errPunycodeOverflow
		}
		delta += (m - n) * (handled + 1)
		n = m
		for _, r := range input {
			if int(r) < n {
				if delta == math.MaxInt32 {
					return "", errPunycodeOverflow
				}
				delta++
			}
			if int(r) != n {
				continue
			}
			q := delta
			for k := punyBase; ; k += punyBase {
				t := min(max(k-bias, punyTMin), punyTMax)
				if q < t {
					break
				}
				out.WriteByte(punyDigit(t + (q-t)%(punyBase-t)))
				q = (q - t) / (punyBase - t)
			}
			out.WriteByte(punyDigit(q))
			bias = punyAdapt(delta, handled+1, handled == basic)
			delta = 0
			handled++
		}
		delta++
		n++
	}
	return out.String(), nil
}

func punyDigit(d int) byte {
	if d < 26 {
		return byte('a' + d)
	}
	return byte('0' + d - 26)
}

func punyAdapt(delta, numPoints int, first bool) int {
	if first {
		delta /= punyDamp
	} else {
		delta /= 2
	}
	delta += delta / numPoints
	k := 0
	for delta > ((punyBase-punyTMin)*punyTMax)/2 {
		delta /= punyBase - punyTMin
		k += punyBase
	}
	return k + (punyBase-punyTMin+1)*delta/(delta+punySkew)
}
