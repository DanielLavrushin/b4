package convert

import "strings"

type Token struct {
	Index    int    `json:"index"`
	Raw      string `json:"raw"`
	Key      string `json:"key"`
	Value    string `json:"value"`
	HasValue bool   `json:"has_value"`
	Profile  int    `json:"profile"`
	Spec     OptionSpec
	Err      string `json:"err"`
}

func getoptLong(argv []string, t *optionTable, longOnly bool) []Token {
	var out []Token
	add := func(tk Token) {
		tk.Index = len(out)
		out = append(out, tk)
	}
	i := 0
	for i < len(argv) {
		a := argv[i]
		switch {
		case a == "--":
			for _, rest := range argv[i+1:] {
				add(Token{Raw: rest, Err: "operand"})
			}
			return out
		case strings.HasPrefix(a, "--"):
			name, value, hasEq := strings.Cut(a[2:], "=")
			spec, ok, ambiguous := t.longMatch(name)
			if ambiguous {
				add(Token{Raw: a, Err: "ambiguous"})
				i++
				continue
			}
			if !ok {
				add(Token{Raw: a, Err: "unknown"})
				i++
				continue
			}
			switch spec.Arg {
			case ArgRequired:
				if hasEq {
					add(Token{Raw: a, Key: spec.Key, Value: value, HasValue: true, Spec: spec})
				} else if i+1 < len(argv) {
					add(Token{Raw: a + " " + argv[i+1], Key: spec.Key, Value: argv[i+1], HasValue: true, Spec: spec})
					i++
				} else {
					add(Token{Raw: a, Key: spec.Key, Spec: spec, Err: "missing_value"})
				}
			case ArgOptional:
				add(Token{Raw: a, Key: spec.Key, Value: value, HasValue: hasEq, Spec: spec})
			default:
				if hasEq {
					add(Token{Raw: a, Key: spec.Key, Spec: spec, Err: "unexpected_value"})
				} else {
					add(Token{Raw: a, Key: spec.Key, Spec: spec})
				}
			}
			i++
		case len(a) > 1 && a[0] == '-' && !longOnly:
			j := 1
			for j < len(a) {
				c := string(a[j])
				spec, ok := t.short[c]
				if !ok {
					add(Token{Raw: "-" + c, Err: "unknown"})
					j++
					continue
				}
				switch spec.Arg {
				case ArgRequired:
					rest := a[j+1:]
					if rest != "" {
						add(Token{Raw: "-" + c + rest, Key: spec.Key, Value: rest, HasValue: true, Spec: spec})
					} else if i+1 < len(argv) {
						add(Token{Raw: "-" + c + " " + argv[i+1], Key: spec.Key, Value: argv[i+1], HasValue: true, Spec: spec})
						i++
					} else {
						add(Token{Raw: "-" + c, Key: spec.Key, Spec: spec, Err: "missing_value"})
					}
					j = len(a)
				case ArgOptional:
					rest := a[j+1:]
					add(Token{Raw: a[:j+1] + rest, Key: spec.Key, Value: rest, HasValue: rest != "", Spec: spec})
					j = len(a)
				default:
					add(Token{Raw: "-" + c, Key: spec.Key, Spec: spec})
					j++
				}
			}
			i++
		default:
			add(Token{Raw: a, Err: "operand"})
			i++
		}
	}
	return out
}
