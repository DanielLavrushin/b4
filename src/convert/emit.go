package convert

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/daniellavrushin/b4/config"
)

const (
	byedpiDefaultFakeTTL = 8
	byedpiDefaultOOBByte = 'a'
	maxSNIPosition       = 50
	maxOOBPosition       = 50
	maxTLSRecPosition    = 100
)

type emitOpts struct {
	NamePrefix     string
	Domains        []string
	ProfileDomains map[int][]string
}

func (o emitOpts) domainsFor(prof *Profile) []string {
	if !prof.IsEntry() {
		return nil
	}
	if custom, ok := o.ProfileDomains[prof.Index]; ok {
		return custom
	}
	return o.Domains
}

type tokenIndex map[int]map[string][]Token

func indexTokens(tokens []Token) tokenIndex {
	idx := tokenIndex{}
	for _, t := range tokens {
		if t.Key == "" {
			continue
		}
		if idx[t.Profile] == nil {
			idx[t.Profile] = map[string][]Token{}
		}
		idx[t.Profile][t.Key] = append(idx[t.Profile][t.Key], t)
	}
	return idx
}

func (ti tokenIndex) first(profile int, keys ...string) (Token, bool) {
	for _, k := range keys {
		if list := ti[profile][k]; len(list) > 0 {
			return list[0], true
		}
	}
	return Token{}, false
}

func (ti tokenIndex) each(profile int, key string, fn func(Token)) {
	for _, t := range ti[profile][key] {
		fn(t)
	}
}

func emit(prog *Program, tokens []Token, notes *noteSet, opts emitOpts) []config.SetConfig {
	ti := indexTokens(tokens)
	prefix := opts.NamePrefix
	if prefix == "" {
		prefix = prog.Tool
	}

	sets := make([]config.SetConfig, 0, len(prog.Profiles))
	for _, prof := range prog.Profiles {
		set := config.NewSetConfig()
		set.Id = fmt.Sprintf("p%d", prof.Index)
		set.Name = fmt.Sprintf("%s #%d", prefix, prof.Index+1)

		udpOnly := prof.UDPOnly()

		emitFilters(&set, prof, ti, notes, udpOnly)
		emitUDP(&set, prof, ti, notes, udpOnly)
		if !udpOnly {
			emitSplits(&set, prof, ti, notes)
			emitFake(&set, prog, prof, ti, notes)
		} else {
			set.Fragmentation.Strategy = config.ConfigNone
			set.Faking.SNI = false
			noteFakeOptionsUnused(prof, ti, notes)
		}
		emitMisc(&set, prog, prof, ti, notes)

		set.Targets.SNIDomains = append(set.Targets.SNIDomains, opts.domainsFor(prof)...)
		for _, idx := range prof.FoldedProtoTokens {
			notes.set(tokenByIndex(ti, prof.Index, idx), StatusApproximated, "udpFoldedIntoSet",
				"udp.mode="+set.UDP.Mode, "udp.filter_quic="+set.UDP.FilterQUIC)
		}
		sets = append(sets, set)
	}

	chainEscalation(sets, prog, ti, notes)
	for _, prof := range prog.Profiles {
		noteEmptyProfile(prof, notes)
	}
	finalize(sets, notes)
	return sets
}

func emitFilters(set *config.SetConfig, prof *Profile, ti tokenIndex, notes *noteSet, udpOnly bool) {
	f := prof.Filters

	if len(f.Hosts) > 0 {
		set.Targets.SNIDomains = append(set.Targets.SNIDomains, f.Hosts...)
		if tok, ok := ti.first(prof.Index, "hosts"); ok {
			notes.set(tok, StatusMapped, "hostsMapped", "targets.sni_domains")
			notes.param(tok, "count", len(f.Hosts))
		}
	}
	if f.HostsRef != "" {
		if tok, ok := ti.first(prof.Index, "hosts"); ok {
			n := notes.set(tok, StatusApproximated, "hostsFileUnresolved")
			n.Params = map[string]any{"path": f.HostsRef}
		}
	}
	if len(f.IPs) > 0 {
		set.Targets.IPs = append(set.Targets.IPs, f.IPs...)
		if tok, ok := ti.first(prof.Index, "ipset"); ok {
			notes.set(tok, StatusMapped, "ipsMapped", "targets.ip")
		}
	}
	if f.IPsRef != "" {
		if tok, ok := ti.first(prof.Index, "ipset"); ok {
			n := notes.set(tok, StatusApproximated, "ipsetFileUnresolved")
			n.Params = map[string]any{"path": f.IPsRef}
		}
	}

	portFilter := ""
	if f.PortMin > 0 {
		if f.PortMax > f.PortMin {
			portFilter = fmt.Sprintf("%d-%d", f.PortMin, f.PortMax)
		} else {
			portFilter = strconv.Itoa(f.PortMin)
		}
	} else if len(f.Protos) > 0 {
		var ports []string
		if f.HasProto("http") {
			ports = append(ports, "80")
		}
		if f.HasProto("tls") {
			ports = append(ports, "443")
		}
		portFilter = strings.Join(ports, ",")
	}

	if portFilter != "" {
		if udpOnly {
			set.UDP.DPortFilter = portFilter
		} else {
			set.TCP.DPortFilter = portFilter
			if f.HasProto("udp") {
				set.UDP.DPortFilter = portFilter
			}
		}
	}

	if tok, ok := ti.first(prof.Index, "pf"); ok {
		notes.set(tok, StatusMapped, "portFilterMapped", "tcp.dport_filter")
	}
	ti.each(prof.Index, "proto", func(tok Token) {
		switch {
		case udpOnly:
			notes.set(tok, StatusApproximated, "protoUDPOnly", "udp.filter_quic", "fragmentation.strategy")
		case portFilter != "":
			notes.set(tok, StatusApproximated, "protoAsPorts", "tcp.dport_filter="+portFilter)
		default:
			notes.set(tok, StatusApproximated, "protoAsPorts")
		}
	})

	if f.HasProto("ipv4") {
		set.Targets.IPVersion = "4"
	}
}

func emitUDP(set *config.SetConfig, prof *Profile, ti tokenIndex, notes *noteSet, udpOnly bool) {
	if prof.UDP.FakeCount <= 0 && !udpOnly {
		return
	}
	set.UDP.Mode = "fake"
	set.UDP.FilterQUIC = "all"
	if prof.UDP.FakeCount > 0 {
		set.UDP.FakeSeqLength = prof.UDP.FakeCount
		if tok, ok := ti.first(prof.Index, "udp_fake"); ok {
			notes.set(tok, StatusApproximated, "udpFakeCount", "udp.mode=fake", "udp.fake_seq_length")
		}
	}
}

func emitSplits(set *config.SetConfig, prof *Profile, ti tokenIndex, notes *noteSet) {
	var plain, disorder, oob, disoob, tlsrec []SplitOp
	for _, s := range prof.Splits {
		switch s.Kind {
		case SplitPlain:
			plain = append(plain, s)
		case SplitDisorder:
			disorder = append(disorder, s)
		case SplitOOB:
			oob = append(oob, s)
		case SplitDisOOB:
			disoob = append(disoob, s)
		case SplitTLSRec:
			tlsrec = append(tlsrec, s)
		}
	}

	segmenting := len(plain) + len(disorder) + len(oob) + len(disoob)

	switch {
	case segmenting == 0 && len(tlsrec) > 0:
		set.Fragmentation.Strategy = "tls"
		pos := clamp(absOffset(tlsrec[0].Pos, 1), 1, maxTLSRecPosition)
		set.Fragmentation.TLSRecordPosition = pos
		noteSplit(notes, tokenAt(tokens(ti, prof.Index, "tlsrec"), 0), StatusMapped, "tlsRecMapped",
			"fragmentation.strategy=tls", "fragmentation.tlsrec_pos="+strconv.Itoa(pos))
	case segmenting == 0:
		set.Fragmentation.Strategy = config.ConfigNone
	case len(disoob) > 0:
		set.Fragmentation.Strategy = "disorder"
		applySplitPositions(set, append(append(plain, disorder...), disoob...), notes, ti, prof)
		ti.each(prof.Index, "disoob", func(t Token) {
			notes.set(t, StatusApproximated, "disoobNoOOB", "fragmentation.strategy=disorder")
		})
	case len(oob) > 0 && len(plain) == 0 && len(disorder) == 0:
		set.Fragmentation.Strategy = "oob"
		pos := clamp(absOffset(oob[0].Pos, 1), 1, maxOOBPosition)
		set.Fragmentation.OOBPosition = pos
		set.Fragmentation.OOBChar = byedpiDefaultOOBByte
		ti.each(prof.Index, "oob", func(t Token) {
			notes.set(t, StatusMapped, "oobMapped",
				"fragmentation.strategy=oob", "fragmentation.oob_position="+strconv.Itoa(pos))
		})
	case len(plain) > 0 && len(disorder) > 0:
		set.Fragmentation.Strategy = "combo"
		applySplitPositions(set, append(plain, disorder...), notes, ti, prof)
	case len(disorder) > 0:
		set.Fragmentation.Strategy = "disorder"
		applySplitPositions(set, disorder, notes, ti, prof)
	default:
		set.Fragmentation.Strategy = "tcp"
		applySplitPositions(set, plain, notes, ti, prof)
	}

	if len(oob) > 0 && set.Fragmentation.Strategy != "oob" {
		ti.each(prof.Index, "oob", func(t Token) {
			notes.set(t, StatusApproximated, "oobDroppedForCombo", "fragmentation.strategy="+set.Fragmentation.Strategy)
		})
	}
	if len(tlsrec) > 0 && segmenting > 0 {
		ti.each(prof.Index, "tlsrec", func(t Token) {
			notes.set(t, StatusUnsupported, "singleStrategyOnly")
		})
	}
	if prof.OOBSet {
		set.Fragmentation.OOBChar = prof.OOBByte
		if tok, ok := ti.first(prof.Index, "oob_data"); ok {
			notes.set(tok, StatusMapped, "oobByteMapped", "fragmentation.oob_char")
		}
	}
}

func applySplitPositions(set *config.SetConfig, ops []SplitOp, notes *noteSet, ti tokenIndex, prof *Profile) {
	strategy := set.Fragmentation.Strategy
	honoursFixed := strategy == "tcp"

	middle := false
	firstByte := false
	fixed := 0

	for _, op := range ops {
		switch op.Pos.Anchor {
		case AnchorSNI, AnchorHost, AnchorPacket:
			middle = true
		default:
			if op.Pos.Offset == 1 {
				firstByte = true
			} else if op.Pos.Offset > 1 && fixed == 0 {
				fixed = op.Pos.Offset
			} else if op.Pos.Offset < 0 {
				middle = true
			}
		}
	}
	if !middle && !firstByte && fixed == 0 {
		middle = true
	}

	set.Fragmentation.MiddleSNI = middle
	if honoursFixed && fixed > 0 {
		set.Fragmentation.SNIPosition = clamp(fixed, 1, maxSNIPosition)
	} else if honoursFixed && firstByte {
		set.Fragmentation.SNIPosition = 1
	} else {
		set.Fragmentation.SNIPosition = 0
	}
	if strategy == "combo" {
		set.Fragmentation.Combo.FirstByteSplit = firstByte
		set.Fragmentation.Combo.ExtensionSplit = !firstByte && !middle
		set.Fragmentation.Combo.DecoyEnabled = prof.Fake.Present
		if !set.Fragmentation.Combo.FirstByteSplit && !set.Fragmentation.Combo.ExtensionSplit && !middle {
			set.Fragmentation.MiddleSNI = true
		}
	}

	if len(ops) > 3 {
		notes.extra = append(notes.extra, Note{
			Token:   fmt.Sprintf("%d x -s/-d/-o", len(ops)),
			Profile: prof.Index,
			Status:  StatusApproximated,
			Reason:  "splitPointsCollapsed",
			Fields:  []string{"fragmentation.strategy=" + strategy},
			Params:  map[string]any{"count": len(ops)},
		})
	}

	for _, op := range ops {
		tok := tokenByIndex(ti, prof.Index, op.Token)
		st, reason, fields := describeSplitMapping(op, strategy, honoursFixed, set)
		n := notes.set(tok, st, reason, fields...)
		n.Params = map[string]any{"position": describePos(op.Pos)}

		if op.Pos.Repeats > 1 {
			if op.Pos.Skip == 0 {
				n.Status = StatusDegenerate
				n.Reason = "repeatsWithoutSkip"
				n.Params["repeats"] = op.Pos.Repeats
			} else {
				n.Status = StatusApproximated
				n.Reason = "repeatsUnsupported"
				n.Params["repeats"] = op.Pos.Repeats
				n.Params["skip"] = op.Pos.Skip
			}
		}
	}
}

func describeSplitMapping(op SplitOp, strategy string, honoursFixed bool, set *config.SetConfig) (Status, string, []string) {
	fields := []string{"fragmentation.strategy=" + strategy}
	switch op.Pos.Anchor {
	case AnchorSNI:
		fields = append(fields, "fragmentation.middle_sni=true")
		if op.Pos.Rel == RelMid && op.Pos.Offset == 0 {
			return StatusMapped, "sniMiddleMapped", fields
		}
		return StatusApproximated, "sniAnchorApproximated", fields
	case AnchorHost:
		fields = append(fields, "fragmentation.middle_sni=true")
		return StatusApproximated, "httpHostAnchor", fields
	case AnchorPacket:
		fields = append(fields, "fragmentation.middle_sni=true")
		return StatusApproximated, "packetAnchorApproximated", fields
	}

	if op.Pos.Offset < 0 {
		fields = append(fields, "fragmentation.middle_sni=true")
		return StatusApproximated, "negativeOffset", fields
	}
	if op.Pos.Offset == 0 {
		return StatusDegenerate, "zeroOffset", fields
	}
	if op.Pos.Offset == 1 && strategy == "combo" && set.Fragmentation.Combo.FirstByteSplit {
		fields = append(fields, "fragmentation.combo.first_byte_split=true")
		return StatusMapped, "firstByteMapped", fields
	}
	if !honoursFixed {
		fields = append(fields, "fragmentation.middle_sni=true")
		return StatusApproximated, "fixedPositionIgnored", fields
	}
	if set.Fragmentation.SNIPosition != op.Pos.Offset {
		if op.Pos.Offset > maxSNIPosition && set.Fragmentation.SNIPosition == maxSNIPosition {
			fields = append(fields, "fragmentation.sni_position="+strconv.Itoa(maxSNIPosition))
			return StatusApproximated, "positionClamped", fields
		}
		fields = append(fields, "fragmentation.middle_sni=true")
		return StatusApproximated, "fixedPositionIgnored", fields
	}
	fields = append(fields, "fragmentation.sni_position="+strconv.Itoa(set.Fragmentation.SNIPosition))
	return StatusMapped, "fixedPositionMapped", fields
}

func emitFake(set *config.SetConfig, prog *Program, prof *Profile, ti tokenIndex, notes *noteSet) {
	if !prof.Fake.Present {
		set.Faking.SNI = false
		noteFakeOptionsUnused(prof, ti, notes)
		return
	}
	set.Faking.SNI = true
	set.Faking.Strategy = "ttl"
	set.Faking.ApplyTTL = true
	ttl := byedpiDefaultFakeTTL
	if prof.Fake.TTLSet {
		ttl = prof.Fake.TTL
	}
	set.Faking.TTL = uint8(clamp(ttl, 1, 255))

	for _, op := range prof.Splits {
		if op.Kind != SplitFake {
			continue
		}
		tok := tokenByIndex(ti, prof.Index, op.Token)
		n := notes.set(tok, StatusApproximated, "fakeMapped",
			"faking.sni=true", "faking.strategy=ttl", "faking.ttl="+strconv.Itoa(int(set.Faking.TTL)))
		n.Params = map[string]any{"position": describePos(op.Pos)}
	}
	if tok, ok := ti.first(prof.Index, "ttl"); ok {
		notes.set(tok, StatusMapped, "fakeTTLMapped", "faking.ttl", "faking.apply_ttl=true")
	}

	if prof.Fake.MD5Sig {
		set.Faking.MD5OnFake = true
		if tok, ok := ti.first(prof.Index, "md5sig"); ok {
			notes.set(tok, StatusMapped, "md5sigMapped", "faking.md5_on_fake=true")
		}
	}

	sni := ""
	if len(prof.Fake.SNIs) > 0 {
		sni = prof.Fake.SNIs[0]
	} else if prog.Globals.FakeSNI != "" {
		sni = prog.Globals.FakeSNI
	}
	switch {
	case prof.Fake.DataInline != "":
		set.Faking.SNIType = config.FakePayloadCustom
		set.Faking.CustomPayload = prof.Fake.DataInline
		if tok, ok := ti.first(prof.Index, "fake_data"); ok {
			notes.set(tok, StatusMapped, "fakeDataMapped", "faking.sni_type=custom", "faking.custom_payload")
		}
	case sni != "":
		host := sanitizeHost(sni)
		tok, hasTok := ti.first(prof.Index, "fake_sni")
		if host == "" {
			if hasTok {
				notes.set(tok, StatusInvalid, "fakeSNINotAHost")
			}
			break
		}
		set.Faking.SNIType = config.FakePayloadDomain
		set.Faking.PayloadDomain = host
		if hasTok {
			if host != sni {
				n := notes.set(tok, StatusApproximated, "fakeSNINormalised", "faking.payload_domain="+host)
				n.Params = map[string]any{"from": sni, "to": host}
			} else {
				notes.set(tok, StatusMapped, "fakeSNIMapped", "faking.sni_type=domain", "faking.payload_domain="+host)
			}
		}
	}

	if prof.Fake.DataRef != "" {
		if tok, ok := ti.first(prof.Index, "fake_data"); ok {
			n := notes.set(tok, StatusApproximated, "fakeDataFileUnresolved")
			n.Params = map[string]any{"path": prof.Fake.DataRef}
		}
	}

	for _, m := range prof.Fake.TLSMod {
		switch m {
		case "r":
			set.Faking.TLSMod = appendUnique(set.Faking.TLSMod, "rnd")
		}
	}
	if tok, ok := ti.first(prof.Index, "fake_tls_mod"); ok {
		if len(set.Faking.TLSMod) > 0 {
			notes.set(tok, StatusMapped, "fakeTLSModMapped", "faking.tls_mod=rnd")
		} else {
			notes.set(tok, StatusUnsupported, "fakeTLSModUnsupported")
		}
	}
	if prof.Fake.OffsetSet {
		if tok, ok := ti.first(prof.Index, "fake_offset", "fake_offset_pos"); ok {
			notes.set(tok, StatusUnsupported, "fakeOffsetUnsupported")
		}
	}
	if prof.Fake.IPOpt {
		if tok, ok := ti.first(prof.Index, "ip_opt"); ok {
			notes.set(tok, StatusUnsupported, "ipOptUnsupported")
		}
	}
}

func emitMisc(set *config.SetConfig, prog *Program, prof *Profile, ti tokenIndex, notes *noteSet) {
	if prof.DropSACK {
		set.TCP.DropSACK = true
		if tok, ok := ti.first(prof.Index, "drop_sack"); ok {
			notes.set(tok, StatusMapped, "dropSackMapped", "tcp.drop_sack=true")
		}
	}
	if len(prof.HTTPMod) > 0 {
		if tok, ok := ti.first(prof.Index, "mod_http"); ok {
			notes.set(tok, StatusUnsupported, "httpTamper")
		}
	}
	if prof.TLSMinor != 0 {
		if tok, ok := ti.first(prof.Index, "tls_minor"); ok {
			notes.set(tok, StatusUnsupported, "noEquivalent")
		}
	}
	if prof.RoundMin > 1 {
		if tok, ok := ti.first(prof.Index, "round"); ok {
			notes.set(tok, StatusUnsupported, "roundUnsupported")
		}
	}
	if prog.Globals.DelayMs > 0 {
		set.TCP.Seg2Delay = prog.Globals.DelayMs
	}
	if prog.Globals.NoIPv6 {
		set.Targets.IPVersion = "4"
	}
}

func chainEscalation(sets []config.SetConfig, prog *Program, ti tokenIndex, notes *noteSet) {
	for i, prof := range prog.Profiles {
		if prof.Trigger.Kind != TriggerDetect || i == 0 {
			continue
		}
		prev := i - 1
		sets[prev].Escalate.To = sets[i].Id
		tok, ok := ti.first(prof.Index, "auto")
		if !ok {
			continue
		}
		fields := []string{fmt.Sprintf("sets[%d].escalate.to=%s", prev, sets[i].Name)}
		if prof.Trigger.OnRedirect || prof.Trigger.OnTLSErr {
			n := notes.set(tok, StatusApproximated, "escalateTriggersPartial", fields...)
			n.Params = map[string]any{
				"redirect": prof.Trigger.OnRedirect,
				"tls_err":  prof.Trigger.OnTLSErr,
			}
		} else {
			notes.set(tok, StatusMapped, "escalateMapped", fields...)
		}
	}
	for i, prof := range prog.Profiles {
		if prof.Trigger.Kind != TriggerNone {
			continue
		}
		tok, ok := ti.first(prof.Index, "auto")
		if !ok {
			continue
		}
		if i == 0 {
			notes.set(tok, StatusMapped, "autoNoneEntrySet")
			continue
		}
		notes.set(tok, StatusApproximated, "autoNoneAsSeparateSet")
	}
}

func finalize(sets []config.SetConfig, notes *noteSet) {
	escalationTarget := map[string]bool{}
	for _, s := range sets {
		if s.Escalate.To != "" {
			escalationTarget[s.Escalate.To] = true
		}
	}
	for i := range sets {
		s := &sets[i]
		hasTargets := len(s.Targets.SNIDomains) > 0 || len(s.Targets.IPs) > 0
		if escalationTarget[s.Id] && !hasTargets {
			s.TCP.DPortFilter = ""
			s.UDP.DPortFilter = ""
			s.Enabled = true
			continue
		}
		s.Enabled = hasTargets
	}
}

var fakeOnlyKeys = []string{
	"ttl", "md5sig", "fake_data", "fake_sni", "fake_tls_mod",
	"fake_offset", "fake_offset_pos", "ip_opt",
}

func noteFakeOptionsUnused(prof *Profile, ti tokenIndex, notes *noteSet) {
	for _, key := range fakeOnlyKeys {
		ti.each(prof.Index, key, func(t Token) {
			notes.set(t, StatusDegenerate, "requiresFake")
		})
	}
}

func noteEmptyProfile(prof *Profile, notes *noteSet) {
	if len(prof.Splits) > 0 || prof.Fake.Present || prof.UDP.FakeCount > 0 ||
		prof.DropSACK || prof.OOBSet || len(prof.HTTPMod) > 0 || !prof.Filters.Empty() {
		return
	}
	notes.extra = append(notes.extra, Note{
		Token:   fmt.Sprintf("profile %d", prof.Index+1),
		Profile: prof.Index,
		Status:  StatusApproximated,
		Reason:  "profileWithoutDesync",
		Fields:  []string{"fragmentation.strategy=none", "faking.sni=false"},
	})
}

func sanitizeHost(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.Contains(s, "://") {
		if u, err := url.Parse(s); err == nil && u.Hostname() != "" {
			return u.Hostname()
		}
	}
	s = strings.TrimSuffix(s, "/")
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if h, _, ok := strings.Cut(s, ":"); ok {
		s = h
	}
	if s == "" || strings.ContainsAny(s, " \t") {
		return ""
	}
	return s
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func absOffset(p Pos, fallback int) int {
	if p.Anchor == AnchorAbs && p.Offset > 0 {
		return p.Offset
	}
	return fallback
}

func tokens(ti tokenIndex, profile int, key string) []Token {
	return ti[profile][key]
}

func tokenAt(list []Token, i int) Token {
	if i < len(list) {
		return list[i]
	}
	return Token{Index: -1}
}

func tokenByIndex(ti tokenIndex, profile, index int) Token {
	for _, list := range ti[profile] {
		for _, t := range list {
			if t.Index == index {
				return t
			}
		}
	}
	return Token{Index: index}
}

func noteSplit(notes *noteSet, tok Token, st Status, reason string, fields ...string) {
	if tok.Index < 0 {
		return
	}
	notes.set(tok, st, reason, fields...)
}
