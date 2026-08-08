package convert

import (
	"errors"
	"strings"
)

const byedpiV013 = "0.13"

func init() {
	grammars["byedpi.pos"] = gByedpiPos
	grammars["cchar"] = gCChar
	grammars["cdata"] = gCData
	grammars["hostlist"] = gHostList
	grammars["iplist"] = gHostList
}

func gCChar(raw string, _ grammarCtx) (Value, error) {
	dec, err := parseCForm(raw)
	if err != nil {
		return Value{}, err
	}
	if len(dec) != 1 {
		return Value{}, errors.New("expected exactly one byte")
	}
	return Value{Byte: dec[0], Str: raw}, nil
}

func gCData(raw string, _ grammarCtx) (Value, error) {
	if strings.HasPrefix(raw, ":") {
		dec, err := parseCForm(raw[1:])
		if err != nil {
			return Value{}, err
		}
		return Value{Str: string(dec)}, nil
	}
	return Value{Ref: raw}, nil
}

func gHostList(raw string, _ grammarCtx) (Value, error) {
	if !strings.HasPrefix(raw, ":") {
		return Value{Ref: raw}, nil
	}
	fields := strings.FieldsFunc(raw[1:], func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == ',' || r == ';'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != "" {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		return Value{}, errors.New("expected at least one entry")
	}
	return Value{List: out, Str: raw}, nil
}

func gByedpiPos(raw string, ctx grammarCtx) (Value, error) {
	if ctx.Version == byedpiV013 {
		p, err := parsePosV013(raw)
		return Value{Pos: p, Str: raw}, err
	}
	p, err := parsePosV017(raw)
	return Value{Pos: p, Str: raw}, err
}

func parsePosV013(raw string) (Pos, error) {
	p := Pos{Raw: raw, Anchor: AnchorAbs, Rel: RelStart}
	n, rest, err := splitLeadingInt(raw)
	if err != nil {
		return p, err
	}
	p.Offset = n
	if rest == "" {
		return p, nil
	}
	if rest[0] != '+' || len(rest) != 2 {
		return p, errors.New("expected <n>, <n>+s, <n>+h or <n>+e")
	}
	switch rest[1] {
	case 's':
		p.Anchor, p.Rel = AnchorSNI, RelStart
	case 'h':
		p.Anchor, p.Rel = AnchorHost, RelStart
	case 'e':
		p.Anchor, p.Rel = AnchorPacket, RelEnd
	default:
		return p, errors.New("expected +s, +h or +e")
	}
	return p, nil
}

func parsePosV017(raw string) (Pos, error) {
	p := Pos{Raw: raw, Anchor: AnchorAbs, Rel: RelStart}
	n, rest, err := splitLeadingInt(raw)
	if err != nil {
		return p, err
	}
	p.Offset = n
	for len(rest) > 0 && rest[0] == ':' {
		var v int
		v, rest, err = splitLeadingInt(rest[1:])
		if err != nil || v < 0 {
			return p, errors.New("expected <offset>[:repeats[:skip]]")
		}
		if p.Repeats == 0 {
			if v == 0 {
				return p, errors.New("repeats must be greater than zero")
			}
			p.Repeats = v
		} else {
			p.Skip = v
			break
		}
	}
	if rest == "" {
		return p, nil
	}
	if rest[0] != '+' || len(rest) < 2 {
		return p, errors.New("expected +s, +h or +n after the offset")
	}
	switch rest[1] {
	case 's':
		p.Anchor = AnchorSNI
	case 'h':
		p.Anchor = AnchorHost
	case 'n':
		p.Anchor = AnchorPacket
	default:
		return p, errors.New("expected +s, +h or +n after the offset")
	}
	if len(rest) > 2 {
		switch rest[2] {
		case 'e':
			p.Rel = RelEnd
		case 'm':
			p.Rel = RelMid
		case 'r':
			p.Rel = RelRand
		case 's':
			p.Rel = RelStart
		}
	}
	if p.Anchor == AnchorPacket && p.Rel == RelStart {
		p.Anchor = AnchorAbs
	}
	return p, nil
}
