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

	Synthetic bool `json:"synthetic,omitempty"`
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
				if isTemplatePlaceholder(tok.Raw) {
					notes.set(tok, StatusNotApplicable, "templatePlaceholder")
				} else {
					notes.set(tok, StatusUnknown, "strayArgument")
				}
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
				tok.Err = "bad_value"
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
		tok.Accumulated = applyTarget(prog, prof, tok, v, notes)
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

func applyTarget(prog *Program, prof *Profile, tok Token, v Value, notes *noteSet) bool {
	switch tok.Spec.Target {
	case "filters.hosts", "filters.ips":
		applyTargetValue(prog, prof, tok, v, notes)
		return v.Ref == ""
	}
	applyTargetValue(prog, prof, tok, v, notes)
	return accumulatingTargets[tok.Spec.Target]
}

func applyTargetValue(prog *Program, prof *Profile, tok Token, v Value, notes *noteSet) {
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
	case "filters.hosts_ref":
		prof.Filters.HostsRef = v.Str
	case "filters.hosts_list":
		prof.Filters.Hosts = append(prof.Filters.Hosts, v.List...)
	case "filters.hosts_exclude":
		prof.Filters.Excluded = append(prof.Filters.Excluded, v.List...)
	case "filters.hosts_exclude_ref":
		prof.Filters.Excluded = append(prof.Filters.Excluded, v.Str)
	case "filters.ips_ref":
		prof.Filters.IPsRef = v.Str
	case "filters.ips_list":
		prof.Filters.IPs = append(prof.Filters.IPs, v.List...)
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

	case "desync.modes":
		prof.DesyncModes = append(prof.DesyncModes, v.List...)
		prof.DesyncToken = tok.Index
	case "splits.positions":
		for _, pos := range v.Positions {
			pos.Token = tok.Index
			prof.SplitPositions = append(prof.SplitPositions, pos)
		}
		prof.SplitPosToken = tok.Index
	case "fake.fooling":
		prof.Fake.Fooling = append(prof.Fake.Fooling, v.List...)
	case "fake.repeats":
		prof.Fake.Repeats = v.Int
	case "fake.seq_increment":
		prof.Fake.SeqIncrement = v.Int
	case "fake.ts_increment":
		prof.Fake.TSIncrement = v.Int
	case "fake.quic":
		prof.Fake.QUICRef = orDefault(v.Ref, v.Str)
	case "fake.blob":
		switch {
		case v.Ref != "":
			prof.Fake.DataRef = v.Ref
		case strings.HasPrefix(v.Str, "0x"), strings.HasPrefix(v.Str, "0X"):
		case v.Str != "" && v.Str != "builtin":
			prof.Fake.DataInline = v.Str
		}
	case "fake.tls_sni":
		for _, m := range v.List {
			if host, ok := strings.CutPrefix(m, "sni="); ok {
				prof.Fake.SNIs = append(prof.Fake.SNIs, host)
				continue
			}
			prof.Fake.TLSMod = append(prof.Fake.TLSMod, m)
		}
	case "profile.desync_mode":
		prof.Desync.Mode = v.Str
	case "profile.duplicate":
		prof.Duplicate = v.Int
	case "profile.win_size":
		prof.WinSize = v.Int
	case "profile.skip":
		prof.Skip = true
	case "profile.seqovl_len":
		if v.Int <= 0 {
			notes.set(tok, StatusUnsupported, "seqOvlLengthUnsupported")
			return
		}
		prof.SeqOvl.Length = v.Int
	case "profile.seqovl_pattern":
		prof.SeqOvl.Pattern = orDefault(v.Ref, v.Str)
	case "filters.l7":
		prof.Filters.Protos = append(prof.Filters.Protos, v.List...)
	case "filters.tcp_ports":
		if v.Bool {
			notes.set(tok, StatusUnsupported, "negatedPortFilter")
			return
		}
		if len(v.List) == 0 {
			notes.set(tok, StatusMapped, "everyPortMatched", "tcp.dport_filter=")
			return
		}
		prof.Filters.TCPPorts = append(prof.Filters.TCPPorts, v.List...)
	case "filters.udp_ports":
		if v.Bool {
			notes.set(tok, StatusUnsupported, "negatedPortFilter")
			return
		}
		if len(v.List) == 0 {
			notes.set(tok, StatusMapped, "everyPortMatched", "udp.dport_filter=")
			return
		}
		prof.Filters.UDPPorts = append(prof.Filters.UDPPorts, v.List...)
	case "filters.l3":
		for _, l3 := range v.List {
			switch l3 {
			case "ipv4":
				prof.Filters.IPVersion = "4"
			case "ipv6":
				prof.Filters.IPVersion = "6"
			}
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

func dropTrailing(prog *Program, tokens []Token, model string) []Token {
	if model != "alternative" || len(prog.Profiles) < 2 {
		return tokens
	}
	last := prog.Profiles[len(prog.Profiles)-1]
	if !last.carriesNothing() || !last.Filters.Empty() {
		return tokens
	}
	prog.Profiles = prog.Profiles[:len(prog.Profiles)-1]
	prev := prog.Profiles[len(prog.Profiles)-1].Index
	for i := range tokens {
		if tokens[i].Profile == last.Index {
			tokens[i].Profile = prev
		}
	}
	return tokens
}

func foldUDPProfiles(prog *Program, tokens []Token, notes *noteSet) []Token {
	var carriers, foldable []*Profile
	for _, p := range prog.Profiles {
		switch {
		case p.UDPOnly() && !p.hasOwnTargets():
			foldable = append(foldable, p)
		case p.IsEntry() && !p.UDPOnly():
			carriers = append(carriers, p)
		}
	}
	if len(carriers) != 1 || len(foldable) != 1 {
		return tokens
	}

	dst, src := carriers[0], foldable[0]
	dst.UDP = src.UDP
	dst.FoldedProtoTokens = append(dst.FoldedProtoTokens, src.ProtoTokens...)
	dst.FoldedTokens = append(dst.FoldedTokens, src.Tokens...)
	folded := map[int]bool{src.Index: true}

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
	for _, note := range notes.byToken {
		if to, ok := remap[note.Profile]; ok {
			note.Profile = to
		}
	}
	for _, p := range prog.Profiles {
		moved := map[int]bool{}
		for _, idx := range p.FoldedTokens {
			moved[idx] = true
		}
		for _, idx := range p.FoldedProtoTokens {
			moved[idx] = true
		}
		for i := range tokens {
			if !moved[tokens[i].Index] {
				continue
			}
			tokens[i].Profile = p.Index
			if tokens[i].Key != "" {
				tokens[i].Key = udpFoldPrefix + tokens[i].Key
			}
		}
	}
	return tokens
}

func isTemplatePlaceholder(raw string) bool {
	raw = strings.TrimSpace(raw)
	if len(raw) < 3 {
		return false
	}
	pairs := [][2]string{{"<", ">"}, {"˂", "˃"}, {"${", "}"}, {"{{", "}}"}, {"%", "%"}}
	for _, p := range pairs {
		if strings.HasPrefix(raw, p[0]) && strings.HasSuffix(raw, p[1]) {
			return true
		}
	}
	return false
}

var accumulatingTargets = map[string]bool{
	"filters.hosts_list": true, "filters.hosts_exclude": true,
	"filters.ips_list": true, "filters.tcp_ports": true,
	"filters.udp_ports": true, "filters.l7": true, "filters.proto": true,
	"filters.hosts_exclude_ref": true,
	"splits.positions":          true, "splits[]": true, "desync.modes": true, "fake.fooling": true,
	"fake.sni[]": true, "fake.tls_mod": true, "fake.tls_sni": true, "profile.http_mod": true,
}

type tokenGroup struct {
	profile int
	key     string
}

func reconcileRepeats(tokens []Token, notes *noteSet) {
	groups := map[tokenGroup][]Token{}
	var order []tokenGroup
	for _, t := range tokens {
		if t.Key == "" || t.Err != "" {
			continue
		}
		g := tokenGroup{t.Profile, t.Key}
		if _, seen := groups[g]; !seen {
			order = append(order, g)
		}
		groups[g] = append(groups[g], t)
	}

	for _, g := range order {
		list := groups[g]
		if len(list) < 2 {
			continue
		}
		noted := -1
		for i, t := range list {
			if _, ok := notes.byToken[t.Index]; ok {
				noted = i
			}
		}
		if noted < 0 {
			continue
		}
		src := *notes.byToken[list[noted].Index]

		if list[noted].Accumulated {
			for _, t := range list {
				if _, ok := notes.byToken[t.Index]; ok {
					continue
				}
				n := notes.set(t, src.Status, "repeatedOptionCombined", src.Fields...)
				n.Params = map[string]any{"count": len(list)}
			}
			continue
		}

		last := list[len(list)-1]
		if last.Index != list[noted].Index {
			winner := notes.set(last, src.Status, src.Reason, src.Fields...)
			winner.Params = src.Params
			delete(notes.byToken, list[noted].Index)
			notes.order = dropIndex(notes.order, list[noted].Index)
		}
		if list[noted].Spec.Arg == ArgNone {
			continue
		}
		for _, t := range list {
			if t.Index == last.Index {
				continue
			}
			if n, ok := notes.byToken[t.Index]; ok && keepsOwnNote(*n, src) {
				continue
			}
			n := notes.set(t, StatusDegenerate, "supersededByLater")
			n.Params = map[string]any{"winner": last.Raw}
		}
	}
}

func keepsOwnNote(n, src Note) bool {
	switch n.Status {
	case StatusUnsupported, StatusInvalid, StatusNotApplicable:
		return true
	}
	return n.Reason != src.Reason
}

func dropIndex(order []int, idx int) []int {
	out := order[:0]
	for _, v := range order {
		if v != idx {
			out = append(out, v)
		}
	}
	return out
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
