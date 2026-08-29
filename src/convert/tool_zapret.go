package convert

import (
	"errors"
	"strconv"
	"strings"

	"github.com/daniellavrushin/b4/config"
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
	registerNormalizer("zapret", normalizeZapret)
	registerToolEmitter("zapret", emitZapret)
	grammars["zapret.desync"] = gZapretDesync
	grammars["zapret.fooling"] = gZapretFooling
	grammars["zapret.splitpos"] = gZapretSplitPos
	grammars["zapret.legacysplit"] = gZapretLegacySplit
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

var zapretLegacySplit = map[string]Pos{
	"method": {Anchor: AnchorHost, Rel: RelStart, Offset: 2},
	"host":   {Anchor: AnchorSNI, Rel: RelStart, Offset: 1},
	"sni":    {Anchor: AnchorSNI, Rel: RelStart, Offset: 1},
	"sniext": {Anchor: AnchorSNIExt, Rel: RelStart, Offset: 1},
	"snisld": {Anchor: AnchorSNI, Rel: RelMid, Offset: 0},
}

func gZapretLegacySplit(raw string, _ grammarCtx) (Value, error) {
	name := strings.TrimSpace(raw)
	p, ok := zapretLegacySplit[name]
	if !ok {
		return Value{}, errors.New("unknown split marker " + name)
	}
	p.Raw = name
	return Value{Positions: []Pos{p}, Str: raw}, nil
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

var zapretDroppedModes = map[string]bool{
	"udplen": true, "tamper": true, "hopbyhop": true, "destopt": true,
}

func emitZapretExtras(set *config.SetConfig, prof *Profile, ti tokenIndex, notes *noteSet) {
	if prof.Desync.Mode != "" {
		set.TCP.Desync.Mode = prof.Desync.Mode
	}
	if prof.SynFake.Enabled {
		set.TCP.SynFake = true
		set.TCP.SynFakeLen = prof.SynFake.Len
	}
	if prof.Duplicate > 0 {
		set.TCP.Duplicate.Enabled = true
		set.TCP.Duplicate.Count = clamp(prof.Duplicate, 1, 10)
		if tok, ok := ti.first(prof.Index, "dup"); ok {
			notes.set(tok, StatusMapped, "duplicateMapped",
				"tcp.duplicate.enabled=true", "tcp.duplicate.count="+strconv.Itoa(set.TCP.Duplicate.Count))
		}
	}
	if prof.SeqOvl.Length > 0 {
		set.Fragmentation.SeqOverlapLength = prof.SeqOvl.Length
		set.Fragmentation.SeqOverlapPattern = seqOvlPattern(prof.SeqOvl.Pattern)
		if tok, ok := ti.first(prof.Index, "seqovl"); ok {
			notes.set(tok, StatusMapped, "seqOvlMapped",
				"fragmentation.seq_overlap_length="+strconv.Itoa(prof.SeqOvl.Length))
		}
		if tok, ok := ti.first(prof.Index, "seqovl_pat"); ok {
			notes.set(tok, StatusApproximated, "seqOvlPatternMapped", "fragmentation.seq_overlap_pattern")
		}
	}
	if prof.WinSize > 0 {
		set.TCP.Win.Mode = "zero"
		if tok, ok := ti.first(prof.Index, "wssize"); ok {
			notes.set(tok, StatusApproximated, "wsSizeApproximated", "tcp.win.mode=zero")
		}
	}
	if len(prof.Filters.Excluded) > 0 {
		if tok, ok := ti.first(prof.Index, "hostlist_excl_dom", "hostlist_exclude"); ok {
			notes.set(tok, StatusUnsupported, "excludeListUnsupported")
		}
	}
	if prof.Skip {
		set.Enabled = false
		if tok, ok := ti.first(prof.Index, "skip"); ok {
			notes.set(tok, StatusMapped, "skipMapped", "enabled=false")
		}
	}
}

func noteDesyncModes(set *config.SetConfig, prof *Profile, ti tokenIndex, notes *noteSet) {
	tok, ok := ti.first(prof.Index, "desync")
	if !ok || len(prof.DesyncModes) == 0 {
		return
	}
	var fields, dropped []string
	if set.Fragmentation.Strategy != config.ConfigNone {
		fields = append(fields, "fragmentation.strategy="+set.Fragmentation.Strategy)
	}
	if set.Faking.SNI {
		fields = append(fields, "faking.sni=true")
	}
	if set.TCP.Desync.Mode != config.ConfigOff {
		fields = append(fields, "tcp.desync.mode="+set.TCP.Desync.Mode)
	}
	if set.TCP.SynFake {
		fields = append(fields, "tcp.syn_fake=true")
	}
	if prof.UDP.Present {
		fields = append(fields, "udp.mode="+set.UDP.Mode)
	}
	for _, m := range prof.DesyncModes {
		if zapretDroppedModes[m] {
			dropped = append(dropped, m)
		}
	}
	if len(dropped) > 0 {
		n := notes.set(tok, StatusUnsupported, "desyncModesDropped", fields...)
		n.Params = map[string]any{"dropped": strings.Join(dropped, ", ")}
		return
	}
	if len(fields) == 0 {
		notes.set(tok, StatusDegenerate, "desyncModesEmpty")
		return
	}
	notes.set(tok, StatusApproximated, "desyncModesMapped", fields...)
}

func seqOvlPattern(raw string) []string {
	hex := strings.TrimPrefix(strings.TrimPrefix(raw, "0x"), "0X")
	if hex == "" || len(hex)%2 != 0 {
		return []string{"0x16", "0x03", "0x03", "0x00", "0x00"}
	}
	out := make([]string, 0, len(hex)/2)
	for i := 0; i+1 < len(hex); i += 2 {
		if !isHex(hex[i]) || !isHex(hex[i+1]) {
			return []string{"0x16", "0x03", "0x03", "0x00", "0x00"}
		}
		out = append(out, "0x"+hex[i:i+2])
	}
	return out
}

func onlyExtSplit(plain, disorder []SplitOp) bool {
	ops := append(append([]SplitOp{}, plain...), disorder...)
	if len(ops) != 1 {
		return false
	}
	return ops[0].Pos.Anchor == AnchorSNIExt && ops[0].Pos.Offset == 0
}

func plainOrDisorder(plain, disorder []SplitOp) SplitOp {
	if len(plain) > 0 {
		return plain[0]
	}
	return disorder[0]
}

func emitZapret(set *config.SetConfig, prof *Profile, ti tokenIndex, notes *noteSet) {
	emitZapretExtras(set, prof, ti, notes)
	noteDesyncModes(set, prof, ti, notes)
	noteDroppedSplitPositions(prof, ti, notes)
}

func noteDroppedSplitPositions(prof *Profile, ti tokenIndex, notes *noteSet) {
	used := map[int]bool{}
	for _, op := range prof.Splits {
		used[op.Token] = true
	}
	for _, key := range []string{"split_pos", "split_http_req", "split_tls"} {
		ti.each(prof.Index, key, func(t Token) {
			if used[t.Index] {
				return
			}
			if _, done := notes.byToken[t.Index]; done {
				return
			}
			notes.set(t, StatusApproximated, "extraSplitPositionDropped",
				"fragmentation.strategy="+prof.Desync.Mode)
		})
	}
}

func normalizeZapret(prog *Program, _ []Token, notes *noteSet) {
	for _, prof := range prog.Profiles {
		normalizeZapretProfile(prof, notes)
		promoteUDPFake(prof)
	}
}

func promoteUDPFake(prof *Profile) {
	if !prof.UDPOnly() {
		return
	}
	prof.UDP.Present = prof.Fake.Present
	prof.UDP.Repeats = prof.Fake.Repeats
	prof.UDP.QUICRef = prof.Fake.QUICRef
	prof.UDP.TTL = prof.Fake.TTL
	prof.UDP.TTLSet = prof.Fake.TTLSet
	prof.UDP.Ports = append(prof.UDP.Ports, prof.Filters.UDPPorts...)
}

func normalizeZapretProfile(prof *Profile, _ *noteSet) {
	positions := prof.SplitPositions
	token := prof.SplitPosToken
	if len(positions) == 0 {
		positions = []Pos{{Raw: "1", Offset: 1, Anchor: AnchorAbs, Rel: RelStart}}
		token = prof.DesyncToken
	}

	for _, mode := range prof.DesyncModes {
		switch mode {
		case "fake", "fakeknown":
			prof.Fake.Present = true
		case "rst", "rstack":
			prof.Desync.Mode = "rst"
		case "synack":
			prof.SynFake.Enabled = true
		case "syndata":
			prof.SynFake.Enabled = true
			prof.SynFake.Len = 1
		case "multisplit":
			appendSplits(prof, SplitPlain, positions, token)
		case "multidisorder":
			appendSplits(prof, SplitDisorder, positions, token)
		case "fakedsplit", "hostfakesplit":
			prof.Fake.Present = true
			appendSplits(prof, SplitPlain, positions[:1], token)
		case "fakeddisorder":
			prof.Fake.Present = true
			appendSplits(prof, SplitDisorder, positions[:1], token)
		case "ipfrag1", "ipfrag2":
			appendSplits(prof, SplitIPFrag, positions[:1], token)
		}
	}
}

func appendSplits(prof *Profile, kind SplitKind, positions []Pos, token int) {
	for _, p := range positions {
		at := token
		if p.Token != 0 {
			at = p.Token
		}
		prof.Splits = append(prof.Splits, SplitOp{Kind: kind, Pos: p, Token: at})
	}
}
