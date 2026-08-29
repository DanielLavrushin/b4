package convert

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type Value struct {
	Str       string
	Int       int
	Bool      bool
	Byte      byte
	Pos       Pos
	Positions []Pos
	List      []string
	Ref       string
}

type grammarCtx struct {
	Version string
	Opt     OptionSpec
}

type grammarFn func(raw string, ctx grammarCtx) (Value, error)

var grammars = map[string]grammarFn{
	"flag":         gFlag,
	"int":          gInt,
	"str":          gStr,
	"float_sec":    gFloatSec,
	"portrange":    gPortRange,
	"range":        gRange,
	"csvfirstchar": gCSVFirstChar,
	"csvkv":        gCSVKeyValue,
}

func runGrammar(name, raw string, ctx grammarCtx) (Value, error) {
	if name == "" {
		name = "str"
	}
	fn, ok := grammars[name]
	if !ok {
		return Value{}, fmt.Errorf("unknown grammar %q", name)
	}
	return fn(raw, ctx)
}

func gFlag(string, grammarCtx) (Value, error) { return Value{Bool: true}, nil }

func gStr(raw string, _ grammarCtx) (Value, error) { return Value{Str: raw}, nil }

func gInt(raw string, _ grammarCtx) (Value, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 0, 32)
	if err != nil {
		return Value{}, errors.New("expected an integer")
	}
	return Value{Int: int(n), Str: raw}, nil
}

func gFloatSec(raw string, _ grammarCtx) (Value, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return Value{}, errors.New("expected a number of seconds")
	}
	return Value{Int: int(f * 1000), Str: raw}, nil
}

func gRange(raw string, _ grammarCtx) (Value, error) {
	lo, hi, ok := strings.Cut(raw, "-")
	a, err := strconv.Atoi(strings.TrimSpace(lo))
	if err != nil {
		return Value{}, errors.New("expected a number or number range")
	}
	b := a
	if ok {
		b, err = strconv.Atoi(strings.TrimSpace(hi))
		if err != nil {
			return Value{}, errors.New("expected a number or number range")
		}
	}
	return Value{Int: a, List: []string{strconv.Itoa(a), strconv.Itoa(b)}, Str: raw}, nil
}

func gPortRange(raw string, _ grammarCtx) (Value, error) {
	lo, hi, ok := strings.Cut(raw, "-")
	a, err := strconv.Atoi(strings.TrimSpace(lo))
	if err != nil || a < 1 || a > 65535 {
		return Value{}, errors.New("expected a port or port range")
	}
	b := a
	if ok {
		b, err = strconv.Atoi(strings.TrimSpace(hi))
		if err != nil || b < 1 || b > 65535 {
			return Value{}, errors.New("expected a port or port range")
		}
	}
	if b < a {
		a, b = b, a
	}
	return Value{Int: a, List: []string{strconv.Itoa(a), strconv.Itoa(b)}, Str: raw}, nil
}

func gCSVFirstChar(raw string, _ grammarCtx) (Value, error) {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part[:1])
	}
	if len(out) == 0 {
		return Value{}, errors.New("expected at least one value")
	}
	return Value{List: out, Str: raw}, nil
}

func gCSVKeyValue(raw string, _ grammarCtx) (Value, error) {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		head := part[:1]
		if _, v, ok := strings.Cut(part, "="); ok {
			head += "=" + v
		}
		out = append(out, head)
	}
	if len(out) == 0 {
		return Value{}, errors.New("expected at least one value")
	}
	return Value{List: out, Str: raw}, nil
}

func parseCForm(s string) ([]byte, error) {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			out = append(out, s[i])
			continue
		}
		i++
		if i >= len(s) {
			return nil, errors.New("trailing backslash")
		}
		switch s[i] {
		case 'r':
			out = append(out, '\r')
		case 'n':
			out = append(out, '\n')
		case 't':
			out = append(out, '\t')
		case 'f':
			out = append(out, '\f')
		case 'b':
			out = append(out, '\b')
		case 'v':
			out = append(out, '\v')
		case 'a':
			out = append(out, '\a')
		case '\\':
			out = append(out, '\\')
		case 'x':
			j := i + 1
			for j < len(s) && j <= i+2 && isHex(s[j]) {
				j++
			}
			if j == i+1 {
				return nil, errors.New("bad \\x escape")
			}
			n, err := strconv.ParseUint(s[i+1:j], 16, 8)
			if err != nil {
				return nil, errors.New("bad \\x escape")
			}
			out = append(out, byte(n))
			i = j - 1
		default:
			j := i
			for j < len(s) && j < i+3 && s[j] >= '0' && s[j] <= '7' {
				j++
			}
			if j == i {
				return nil, fmt.Errorf("unknown escape \\%c", s[i])
			}
			n, err := strconv.ParseUint(s[i:j], 8, 16)
			if err != nil {
				return nil, errors.New("bad octal escape")
			}
			out = append(out, byte(n))
			i = j - 1
		}
	}
	return out, nil
}

func isHexLiteral(s string) bool {
	return len(s) > 2 && (s[:2] == "0x" || s[:2] == "0X")
}

func isZeroHexLiteral(s string) bool {
	if !isHexLiteral(s) {
		return false
	}
	for i := 2; i < len(s); i++ {
		if s[i] != '0' {
			return false
		}
	}
	return true
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func splitLeadingInt(s string) (int, string, error) {
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	start := i
	for i < len(s) && ((s[i] >= '0' && s[i] <= '9') ||
		(i == start+1 && (s[i] == 'x' || s[i] == 'X') && s[start] == '0') ||
		(i > start+1 && s[start] == '0' && (s[start+1] == 'x' || s[start+1] == 'X') && isHex(s[i]))) {
		i++
	}
	if i == start {
		return 0, s, errors.New("expected a number")
	}
	n, err := strconv.ParseInt(s[:i], 0, 32)
	if err != nil {
		return 0, s, errors.New("expected a number")
	}
	return int(n), s[i:], nil
}
