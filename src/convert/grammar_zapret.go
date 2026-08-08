package convert

import (
	"errors"
	"strconv"
	"strings"
)

var zapretDesyncModes = map[string]string{
	"none":          "none",
	"synack":        "synack",
	"syndata":       "syndata",
	"fake":          "fake",
	"fakeknown":     "fakeknown",
	"rst":           "rst",
	"rstack":        "rstack",
	"hopbyhop":      "hopbyhop",
	"destopt":       "destopt",
	"ipfrag1":       "ipfrag1",
	"multisplit":    "multisplit",
	"split2":        "multisplit",
	"multidisorder": "multidisorder",
	"disorder2":     "multidisorder",
	"fakedsplit":    "fakedsplit",
	"split":         "fakedsplit",
	"fakeddisorder": "fakeddisorder",
	"disorder":      "fakeddisorder",
	"hostfakesplit": "hostfakesplit",
	"ipfrag2":       "ipfrag2",
	"udplen":        "udplen",
	"tamper":        "tamper",
}

var zapretFooling = map[string]bool{
	"none": true, "md5sig": true, "ts": true, "badseq": true,
	"badsum": true, "datanoack": true, "hopbyhop": true, "hopbyhop2": true,
}

var zapretPosMarkers = map[string]struct {
	anchor Anchor
	rel    Rel
}{
	"host":    {AnchorSNI, RelStart},
	"endhost": {AnchorSNI, RelEnd},
	"sld":     {AnchorSNI, RelStart},
	"endsld":  {AnchorSNI, RelEnd},
	"midsld":  {AnchorSNI, RelMid},
	"method":  {AnchorHost, RelStart},
	"sniext":  {AnchorSNIExt, RelStart},
}

func init() {
	grammars["zapret.desync"] = gZapretDesync
	grammars["zapret.fooling"] = gZapretFooling
	grammars["zapret.splitpos"] = gZapretSplitPos
	grammars["zapret.pos"] = gZapretSinglePos
	grammars["zapret.blob"] = gZapretBlob
	grammars["zapret.ports"] = gZapretPorts
	grammars["zapret.modlist"] = gZapretModList
	grammars["zapret.wsize"] = gZapretWSize
	grammars["zapret.autottl"] = gStr
	grammars["zapret.startcutoff"] = gStr
	grammars["bool01"] = gBool01
	grammars["csv"] = gCSV
}

func gBool01(raw string, _ grammarCtx) (Value, error) {
	switch strings.TrimSpace(raw) {
	case "", "1", "true", "yes":
		return Value{Bool: true, Str: raw}, nil
	case "0", "false", "no":
		return Value{Bool: false, Str: raw}, nil
	}
	return Value{}, errors.New("expected 0 or 1")
}

func gCSV(raw string, _ grammarCtx) (Value, error) {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return Value{}, errors.New("expected at least one value")
	}
	return Value{List: out, Str: raw}, nil
}

func gZapretDesync(raw string, _ grammarCtx) (Value, error) {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		mode, ok := zapretDesyncModes[part]
		if !ok {
			return Value{}, errors.New("unknown desync mode " + part)
		}
		out = append(out, mode)
	}
	if len(out) == 0 {
		return Value{}, errors.New("expected at least one desync mode")
	}
	if len(out) > 3 {
		return Value{}, errors.New("at most three desync modes are accepted")
	}
	return Value{List: out, Str: raw}, nil
}

func gZapretFooling(raw string, _ grammarCtx) (Value, error) {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !zapretFooling[part] {
			return Value{}, errors.New("unknown fooling mode " + part)
		}
		if part != "none" {
			out = append(out, part)
		}
	}
	return Value{List: out, Str: raw}, nil
}

func parseZapretPos(raw string) (Pos, error) {
	raw = strings.TrimSpace(raw)
	p := Pos{Raw: raw, Anchor: AnchorAbs, Rel: RelStart}
	if raw == "" {
		return p, errors.New("empty split position")
	}
	if n, err := strconv.Atoi(raw); err == nil {
		if n == 0 {
			return p, errors.New("absolute position 0 is not accepted")
		}
		p.Offset = n
		return p, nil
	}
	idx := strings.IndexAny(raw, "+-")
	name := raw
	offset := 0
	if idx > 0 {
		name = raw[:idx]
		n, err := strconv.Atoi(raw[idx:])
		if err != nil {
			return p, errors.New("expected marker+N or marker-N")
		}
		offset = n
	}
	marker, ok := zapretPosMarkers[name]
	if !ok {
		return p, errors.New("unknown split marker " + name)
	}
	p.Anchor, p.Rel, p.Offset = marker.anchor, marker.rel, offset
	return p, nil
}

func gZapretSplitPos(raw string, _ grammarCtx) (Value, error) {
	var out []Pos
	for _, part := range strings.Split(raw, ",") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		p, err := parseZapretPos(part)
		if err != nil {
			return Value{}, err
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return Value{}, errors.New("expected at least one split position")
	}
	return Value{Positions: out, Str: raw}, nil
}

func gZapretSinglePos(raw string, _ grammarCtx) (Value, error) {
	p, err := parseZapretPos(raw)
	if err != nil {
		return Value{}, err
	}
	return Value{Pos: p, Int: p.Offset, Str: raw}, nil
}

func gZapretBlob(raw string, _ grammarCtx) (Value, error) {
	raw = strings.TrimSpace(raw)
	switch {
	case raw == "" || strings.HasPrefix(raw, "!"):
		return Value{Str: "builtin"}, nil
	case strings.HasPrefix(raw, "0x") || strings.HasPrefix(raw, "0X"):
		return Value{Str: raw}, nil
	default:
		at := strings.LastIndex(raw, "@")
		if at >= 0 {
			return Value{Ref: raw[at+1:]}, nil
		}
		return Value{Ref: raw}, nil
	}
}

func gZapretPorts(raw string, _ grammarCtx) (Value, error) {
	var out []string
	negated := false
	all := false
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, "~") {
			negated = true
			part = part[1:]
		}
		if part == "*" {
			all = true
			continue
		}
		lo, hi, isRange := strings.Cut(part, "-")
		a, err := strconv.Atoi(lo)
		if err != nil || a < 1 || a > 65535 {
			return Value{}, errors.New("expected a port or port range")
		}
		if !isRange {
			out = append(out, strconv.Itoa(a))
			continue
		}
		b, err := strconv.Atoi(hi)
		if err != nil || b < 1 || b > 65535 {
			return Value{}, errors.New("expected a port or port range")
		}
		out = append(out, strconv.Itoa(a)+"-"+strconv.Itoa(b))
	}
	v := Value{List: out, Str: raw, Bool: negated}
	if all {
		v.List = nil
	}
	return v, nil
}

func gZapretModList(raw string, _ grammarCtx) (Value, error) {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" || part == "none" {
			continue
		}
		out = append(out, part)
	}
	return Value{List: out, Str: raw}, nil
}

func gZapretWSize(raw string, _ grammarCtx) (Value, error) {
	size, _, _ := strings.Cut(raw, ":")
	n, err := strconv.Atoi(strings.TrimSpace(size))
	if err != nil {
		return Value{}, errors.New("expected <window>[:<scale>]")
	}
	return Value{Int: n, Str: raw}, nil
}
