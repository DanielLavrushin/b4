package convert

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type Note struct {
	Token   string         `json:"token"`
	Profile int            `json:"profile"`
	Status  Status         `json:"status"`
	Reason  string         `json:"reason"`
	Fields  []string       `json:"fields,omitempty"`
	Params  map[string]any `json:"params,omitempty"`
}

type noteSet struct {
	byToken map[int]*Note
	order   []int
	extra   []Note
}

func newNoteSet() *noteSet {
	return &noteSet{byToken: map[int]*Note{}}
}

func (n *noteSet) set(tok Token, st Status, reason string, fields ...string) *Note {
	note, ok := n.byToken[tok.Index]
	if !ok {
		note = &Note{Token: tok.Raw, Profile: tok.Profile}
		n.byToken[tok.Index] = note
		n.order = append(n.order, tok.Index)
	}
	note.Status = st
	note.Reason = reason
	note.Fields = fields
	return note
}

func (n *noteSet) param(tok Token, key string, val any) {
	if note, ok := n.byToken[tok.Index]; ok {
		if note.Params == nil {
			note.Params = map[string]any{}
		}
		note.Params[key] = val
	}
}

func (n *noteSet) list() []Note {
	sort.Ints(n.order)
	out := make([]Note, 0, len(n.order)+len(n.extra))
	for _, i := range n.order {
		out = append(out, *n.byToken[i])
	}
	out = append(out, n.extra...)
	return out
}

func buildProgram(spec *Spec, version string, tokens []Token, notes *noteSet) (*Program, []Token) {
	prog := &Program{Tool: spec.Tool, Version: version}
	prog.current()

	resolved := make([]Token, 0, len(tokens))
	for _, tok := range tokens {
		if tok.Err != "" {
			tok.Profile = len(prog.Profiles) - 1
			switch tok.Err {
			case "operand":
				notes.set(tok, StatusUnknown, "strayArgument")
			case "unknown":
				notes.set(tok, StatusUnknown, "unknownOption")
			case "ambiguous":
				notes.set(tok, StatusInvalid, "ambiguousOption")
			case "missing_value":
				notes.set(tok, StatusInvalid, "missingValue")
			case "unexpected_value":
				notes.set(tok, StatusInvalid, "unexpectedValue")
			}
			resolved = append(resolved, tok)
			continue
		}

		v := Value{}
		if tok.Spec.Arg != ArgNone && tok.HasValue {
			parsed, err := runGrammar(tok.Spec.Grammar, tok.Value, grammarCtx{Version: version, Opt: tok.Spec})
			if err != nil {
				tok.Profile = len(prog.Profiles) - 1
				n := notes.set(tok, StatusInvalid, "badValue")
				n.Params = map[string]any{"detail": err.Error()}
				resolved = append(resolved, tok)
				continue
			}
			v = parsed
		} else {
			v = Value{Bool: true}
		}

		if spec.isBreak(tok.Spec.Key) {
			prog.newProfile(triggerFrom(v.List))
		}
		prof := prog.current()
		tok.Profile = prof.Index
		applyTarget(prog, prof, tok, v, notes)
		if tok.Spec.Scope == ScopeGlobal {
			prog.Globals.Tokens = append(prog.Globals.Tokens, tok.Index)
		} else {
			prof.Tokens = append(prof.Tokens, tok.Index)
		}
		resolved = append(resolved, tok)
	}
	return prog, resolved
}

func triggerFrom(chars []string) Trigger {
	t := Trigger{Kind: TriggerNone}
	for _, c := range chars {
		switch c {
		case "t":
			t.OnRST = true
		case "r":
			t.OnRedirect = true
		case "s", "a":
			t.OnTLSErr = true
		}
	}
	if t.OnRST || t.OnRedirect || t.OnTLSErr {
		t.Kind = TriggerDetect
	}
	return t
}

var protoNames = map[string]string{"t": "tls", "h": "http", "u": "udp", "i": "ipv4"}

func applyTarget(prog *Program, prof *Profile, tok Token, v Value, notes *noteSet) {
	target := tok.Spec.Target
	switch target {
	case "", "_.ignore":
		return
	case "_.na":
		notes.set(tok, StatusNotApplicable, orDefault(tok.Spec.Note, "proxyRuntime"))
		return

	case "trigger":
		return

	case "global.no_domain":
		prog.Globals.NoDomain = true
	case "global.no_ipv6":
		prog.Globals.NoIPv6 = true
		notes.set(tok, StatusMapped, "noIPv6Mapped", "targets.ip_version=4")
	case "global.no_udp":
		prog.Globals.NoUDP = true
		notes.set(tok, StatusApproximated, "noUDPUpstream")
	case "global.def_ttl":
		prog.Globals.DefTTL = v.Int
	case "global.timeout":
		prog.Globals.TimeoutMs = v.Int
	case "global.delay":
		prog.Globals.DelayMs = v.Int
		notes.set(tok, StatusApproximated, "delayMapped", "tcp.seg2delay")
	case "global.fake_sni":
		host := sanitizeHost(v.Str)
		prog.Globals.FakeSNI = host
		if host == "" {
			notes.set(tok, StatusInvalid, "fakeSNINotAHost")
		} else if host != v.Str {
			n := notes.set(tok, StatusApproximated, "fakeSNINormalised", "faking.payload_domain="+host)
			n.Params = map[string]any{"from": v.Str, "to": host}
		} else {
			notes.set(tok, StatusMapped, "fakeSNIMapped", "faking.sni_type=domain", "faking.payload_domain="+host)
		}
	case "global.auto_mode":
		prog.Globals.AutoMode = strings.Join(v.List, ",")

	case "filters.proto":
		prof.ProtoTokens = append(prof.ProtoTokens, tok.Index)
		for _, c := range v.List {
			if name, ok := protoNames[c]; ok {
				prof.Filters.Protos = append(prof.Filters.Protos, name)
			}
		}
	case "filters.hosts":
		if v.Ref != "" {
			prof.Filters.HostsRef = v.Ref
		} else {
			prof.Filters.Hosts = append(prof.Filters.Hosts, v.List...)
		}
	case "filters.ips":
		if v.Ref != "" {
			prof.Filters.IPsRef = v.Ref
		} else {
			prof.Filters.IPs = append(prof.Filters.IPs, v.List...)
		}
	case "filters.ports":
		prof.Filters.PortMin, _ = strconv.Atoi(v.List[0])
		prof.Filters.PortMax, _ = strconv.Atoi(v.List[1])

	case "splits[]":
		kind := SplitKind(tok.Spec.Const["kind"])
		prof.Splits = append(prof.Splits, SplitOp{Kind: kind, Pos: v.Pos, Token: tok.Index})
		if kind == SplitFake {
			prof.Fake.Present = true
			prof.Fake.Pos = v.Pos
		}

	case "fake.ttl":
		prof.Fake.TTL = v.Int
		prof.Fake.TTLSet = true
	case "fake.md5sig":
		prof.Fake.MD5Sig = true
	case "fake.ip_opt":
		prof.Fake.IPOpt = true
	case "fake.data":
		if v.Ref != "" {
			prof.Fake.DataRef = v.Ref
		} else {
			prof.Fake.DataInline = v.Str
		}
	case "fake.sni[]":
		prof.Fake.SNIs = append(prof.Fake.SNIs, v.Str)
	case "fake.offset":
		prof.Fake.Offset = v.Int
		prof.Fake.OffsetSet = true
	case "fake.offset_pos":
		prof.Fake.Offset = v.Pos.Offset
		prof.Fake.OffsetSet = true
	case "fake.tls_mod":
		for _, m := range v.List {
			if strings.HasPrefix(m, "m=") {
				n, err := strconv.Atoi(m[2:])
				if err == nil {
					prof.Fake.TLSSize = n
					prof.Fake.TLSSizeSet = true
				}
				continue
			}
			prof.Fake.TLSMod = append(prof.Fake.TLSMod, m)
		}

	case "profile.oob_byte":
		prof.OOBByte = v.Byte
		prof.OOBSet = true
	case "profile.drop_sack":
		prof.DropSACK = true
	case "profile.http_mod":
		prof.HTTPMod = append(prof.HTTPMod, v.List...)
	case "profile.udp_fake_count":
		prof.UDP.FakeCount = v.Int
	case "profile.round":
		prof.RoundMin, _ = strconv.Atoi(v.List[0])
		prof.RoundMax, _ = strconv.Atoi(v.List[1])
	case "profile.tls_minor":
		prof.TLSMinor = v.Int
	case "profile.unsupported":
		notes.set(tok, StatusUnsupported, orDefault(tok.Spec.Note, "noEquivalent"))
	default:
		notes.set(tok, StatusUnknown, "unmappedTarget")
	}
}

func foldUDPProfiles(prog *Program, tokens []Token) []Token {
	var carriers []*Profile
	for _, p := range prog.Profiles {
		if p.IsEntry() && !p.UDPOnly() {
			carriers = append(carriers, p)
		}
	}
	if len(carriers) == 0 {
		return tokens
	}

	folded := map[int]bool{}
	for _, src := range prog.Profiles {
		if !src.UDPOnly() || src.hasOwnTargets() {
			continue
		}
		for _, dst := range carriers {
			if dst.UDP.FakeCount == 0 {
				dst.UDP = src.UDP
			}
			dst.FoldedProtoTokens = append(dst.FoldedProtoTokens, src.ProtoTokens...)
		}
		folded[src.Index] = true
	}
	if len(folded) == 0 {
		return tokens
	}

	remap := make(map[int]int, len(prog.Profiles))
	kept := make([]*Profile, 0, len(prog.Profiles))
	for _, p := range prog.Profiles {
		if folded[p.Index] {
			continue
		}
		remap[p.Index] = len(kept)
		p.Index = len(kept)
		kept = append(kept, p)
	}
	for old := range folded {
		remap[old] = carriers[0].Index
	}
	prog.Profiles = kept

	for i := range tokens {
		if to, ok := remap[tokens[i].Profile]; ok {
			tokens[i].Profile = to
		}
	}
	for _, p := range prog.Profiles {
		for _, idx := range p.FoldedProtoTokens {
			for i := range tokens {
				if tokens[i].Index == idx {
					tokens[i].Profile = p.Index
				}
			}
		}
	}
	return tokens
}

func noteUnaccounted(tokens []Token, notes *noteSet) {
	for _, t := range tokens {
		if t.Key == "" || t.Err != "" || t.Spec.Target == "_.ignore" {
			continue
		}
		if _, ok := notes.byToken[t.Index]; ok {
			continue
		}
		notes.set(t, StatusUnknown, "unaccountedOption")
	}
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func describePos(p Pos) string {
	switch p.Anchor {
	case AnchorSNI:
		return fmt.Sprintf("SNI%s%+d", relSuffix(p.Rel), p.Offset)
	case AnchorHost:
		return fmt.Sprintf("Host%s%+d", relSuffix(p.Rel), p.Offset)
	case AnchorPacket:
		return fmt.Sprintf("packet%s%+d", relSuffix(p.Rel), p.Offset)
	default:
		return strconv.Itoa(p.Offset)
	}
}

func relSuffix(r Rel) string {
	switch r {
	case RelMid:
		return ".mid"
	case RelEnd:
		return ".end"
	case RelRand:
		return ".rand"
	default:
		return ""
	}
}
