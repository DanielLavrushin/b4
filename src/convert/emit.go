package convert

import (
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"github.com/daniellavrushin/b4/config"
)

const (
	maxSNIPosition    = 50
	maxOOBPosition    = 50
	maxTLSRecPosition = 100
)

type emitOpts struct {
	NamePrefix     string
	Domains        []string
	ProfileDomains map[int][]string
	ProfileModel   string
	BreakKeys      []string
	Defaults       SpecDefaults
}

func noteBreakTokens(prof *Profile, ti tokenIndex, notes *noteSet, keys []string, model string) {
	if model != "alternative" {
		return
	}
	for _, key := range keys {
		ti.each(prof.Index, key, func(t Token) {
			notes.set(t, StatusMapped, "newProfileAsSet")
		})
	}
}

func (o emitOpts) domainsFor(prof *Profile, sharedGiven *bool) []string {
	if !prof.IsEntry() {
		return nil
	}
	if custom, ok := o.ProfileDomains[prof.Index]; ok {
		return custom
	}
	if prof.hasOwnTargets() || *sharedGiven || len(o.Domains) == 0 {
		return nil
	}
	*sharedGiven = true
	return o.Domains
}

type tokenIndex map[int]map[string][]Token

func indexTokens(tokens []Token) tokenIndex {
	idx := tokenIndex{}
	for _, t := range tokens {
		if t.Key == "" || t.Err != "" {
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

	sharedGiven := false
	sets := make([]config.SetConfig, 0, len(prog.Profiles))
	for _, prof := range prog.Profiles {
		set := config.NewSetConfig()
		set.Id = fmt.Sprintf("p%d", prof.Index)
		set.Name = fmt.Sprintf("%s #%d", prefix, prof.Index+1)

		udpOnly := prof.UDPOnly()

		emitFilters(&set, prof, ti, notes, udpOnly)
		emitUDP(&set, prof, ti, notes, udpOnly)
		if !udpOnly {
			emitSplits(&set, prof, ti, notes, opts)
			emitFake(&set, prog, prof, ti, notes, opts.Defaults)
		} else {
			set.Fragmentation.Strategy = config.ConfigNone
			set.Faking.SNI = false
			emitUDPFake(prof, ti, notes)
		}
		if udpOnly && len(prog.Profiles) > 1 {
			notes.extra = append(notes.extra, Note{
				Token:     set.Name,
				Profile:   prof.Index,
				Status:    StatusApproximated,
				Reason:    "udpOnlySetNotProtocolScoped",
				Synthetic: true,
			})
		}
		emitMisc(&set, prog, prof, ti, notes)
		runToolEmitter(prog.Tool, &set, prof, ti, notes)
		noteBreakTokens(prof, ti, notes, opts.BreakKeys, opts.ProfileModel)

		set.Targets.SNIDomains = append(set.Targets.SNIDomains, opts.domainsFor(prof, &sharedGiven)...)
		for _, idx := range prof.FoldedProtoTokens {
			notes.set(tokenByIndex(ti, prof.Index, idx), StatusApproximated, "udpFoldedIntoSet",
				"udp.mode="+set.UDP.Mode, "udp.filter_quic="+set.UDP.FilterQUIC)
		}
		sets = append(sets, set)
	}

	chainEscalation(sets, prog, ti, notes, opts.ProfileModel)
	dropIdenticalEscalations(sets, notes)
	for _, prof := range prog.Profiles {
		noteEmptyProfile(prof, notes)
	}
	finalize(sets, prog, notes)
	return sets
}

func emitFilters(set *config.SetConfig, prof *Profile, ti tokenIndex, notes *noteSet, udpOnly bool) {
	f := prof.Filters

	if len(f.Hosts) > 0 {
		set.Targets.SNIDomains = append(set.Targets.SNIDomains, f.Hosts...)
		if tok, ok := ti.first(prof.Index, "hosts", "hostlist_domains", "hostlist"); ok {
			notes.set(tok, StatusMapped, "hostsMapped", "targets.sni_domains")
			notes.param(tok, "count", len(f.Hosts))
		}
	}
	if f.HostsRef != "" {
		if tok, ok := ti.first(prof.Index, "hosts", "hostlist_domains", "hostlist"); ok {
			n := notes.set(tok, StatusApproximated, "hostsFileUnresolved")
			n.Params = map[string]any{"path": f.HostsRef}
		}
	}
	if len(f.IPs) > 0 {
		set.Targets.IPs = append(set.Targets.IPs, f.IPs...)
		if tok, ok := ti.first(prof.Index, "ipset", "ipset_ip"); ok {
			notes.set(tok, StatusMapped, "ipsMapped", "targets.ip")
		}
	}
	if f.IPsRef != "" {
		if tok, ok := ti.first(prof.Index, "ipset", "ipset_ip"); ok {
			n := notes.set(tok, StatusApproximated, "ipsetFileUnresolved")
			n.Params = map[string]any{"path": f.IPsRef}
		}
	}

	if len(f.TCPPorts) > 0 {
		set.TCP.DPortFilter = strings.Join(f.TCPPorts, ",")
		if tok, ok := ti.first(prof.Index, "filter_tcp"); ok {
			notes.set(tok, StatusMapped, "portFilterMapped", "tcp.dport_filter="+set.TCP.DPortFilter)
		}
	}
	if len(f.UDPPorts) > 0 {
		set.UDP.DPortFilter = strings.Join(f.UDPPorts, ",")
		if tok, ok := ti.first(prof.Index, "filter_udp"); ok {
			notes.set(tok, StatusMapped, "portFilterMapped", "udp.dport_filter="+set.UDP.DPortFilter)
		}
	}
	if f.IPVersion != "" {
		set.Targets.IPVersion = f.IPVersion
		if tok, ok := ti.first(prof.Index, "filter_l3"); ok {
			notes.set(tok, StatusMapped, "ipVersionMapped", "targets.ip_version="+f.IPVersion)
		}
	}
	if f.HasProto("quic") {
		set.UDP.FilterQUIC = "sni"
		set.UDP.Mode = "fake"
	}
	if tok, ok := ti.first(prof.Index, "filter_l7"); ok {
		if f.HasProto("quic") {
			notes.set(tok, StatusApproximated, "l7QUICMapped", "udp.filter_quic=sni")
		} else {
			notes.set(tok, StatusApproximated, "l7FilterApproximated")
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
		switch {
		case udpOnly:
			if len(f.UDPPorts) == 0 {
				set.UDP.DPortFilter = portFilter
			}
		default:
			if len(f.TCPPorts) == 0 {
				set.TCP.DPortFilter = portFilter
			}
			if f.HasProto("udp") && len(f.UDPPorts) == 0 {
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

const udpFoldPrefix = "udpfold:"

func emitUDP(set *config.SetConfig, prof *Profile, ti tokenIndex, notes *noteSet, udpOnly bool) {
	if prof.UDP.Empty() && !udpOnly {
		return
	}
	set.UDP.Mode = "fake"
	set.UDP.FilterQUIC = "all"

	if len(prof.UDP.Ports) > 0 && set.UDP.DPortFilter == "" {
		set.UDP.DPortFilter = strings.Join(prof.UDP.Ports, ",")
	}
	if prof.UDP.FakeCount > 0 {
		set.UDP.FakeSeqLength = prof.UDP.FakeCount
		if tok, ok := ti.first(prof.Index, "udp_fake", udpFoldPrefix+"udp_fake"); ok {
			notes.set(tok, StatusApproximated, "udpFakeCount", "udp.mode=fake", "udp.fake_seq_length")
		}
	}
	if prof.UDP.Repeats > 0 {
		set.UDP.FakeSeqLength = prof.UDP.Repeats
		if tok, ok := ti.first(prof.Index, "repeats", udpFoldPrefix+"repeats"); ok {
			notes.set(tok, StatusMapped, "udpFakeRepeatsMapped", "udp.fake_seq_length")
		}
	}
	if prof.UDP.QUICRef != "" {
		set.UDP.FakePayloadFile = config.FakePayloadAutoQUIC
		if tok, ok := ti.first(prof.Index, "fake_quic", udpFoldPrefix+"fake_quic"); ok {
			notes.set(tok, StatusApproximated, "fakeQUICApproximated",
				"udp.fake_payload_file="+config.FakePayloadAutoQUIC)
		}
		if tok, ok := ti.first(prof.Index, "fake_unk_udp", udpFoldPrefix+"fake_unk_udp"); ok {
			notes.set(tok, StatusApproximated, "fakeUnknownUDPApproximated",
				"udp.fake_payload_file="+config.FakePayloadAutoQUIC)
		}
	}
	if prof.UDP.ZeroRef {
		set.UDP.FakePayloadFile = ""
		for _, key := range []string{"fake_quic", "fake_unk_udp", udpFoldPrefix + "fake_quic", udpFoldPrefix + "fake_unk_udp"} {
			ti.each(prof.Index, key, func(tok Token) {
				notes.set(tok, StatusMapped, "fakeZeroPayload", "udp.fake_payload_file=")
			})
		}
	}
	if prof.AnyProtocol {
		set.UDP.FilterQUIC = "all"
		if tok, ok := ti.first(prof.Index, "any_protocol", udpFoldPrefix+"any_protocol"); ok {
			notes.set(tok, StatusApproximated, "anyProtocolUDP", "udp.filter_quic=all")
		}
	}
	if prof.UDP.TTLSet {
		if prof.UDP.TTL > 0 {
			set.UDP.FakingStrategy = "ttl"
		}
		if tok, ok := ti.first(prof.Index, "desync_ttl", "ttl", udpFoldPrefix+"desync_ttl"); ok {
			if prof.UDP.TTL > 0 {
				notes.set(tok, StatusMapped, "udpFakeTTLMapped", "udp.faking_strategy=ttl")
			} else {
				notes.set(tok, StatusMapped, "fakeTTLOriginal", "udp.faking_strategy=none")
			}
		}
	}
	noteFoldedTokens(prof, ti, notes, set)
}

func noteFoldedTokens(prof *Profile, ti tokenIndex, notes *noteSet, set *config.SetConfig) {
	for key, list := range ti[prof.Index] {
		if !strings.HasPrefix(key, udpFoldPrefix) {
			continue
		}
		for _, tok := range list {
			if _, done := notes.byToken[tok.Index]; done {
				continue
			}
			notes.set(tok, StatusApproximated, "udpFoldedIntoSet",
				"udp.mode="+set.UDP.Mode, "udp.dport_filter="+set.UDP.DPortFilter)
		}
	}
}

func emitUDPFake(prof *Profile, ti tokenIndex, notes *noteSet) {
	if !prof.UDP.Present && prof.UDP.QUICRef == "" {
		noteFakeOptionsUnused(prof, ti, notes)
		return
	}
	for _, key := range fakeOnlyKeys {
		if key == "repeats" || key == "desync_ttl" || key == "ttl" {
			continue
		}
		ti.each(prof.Index, key, func(t Token) {
			if _, done := notes.byToken[t.Index]; !done {
				notes.set(t, StatusUnsupported, "tcpOnlyFakeOption")
			}
		})
	}
}

func emitSplits(set *config.SetConfig, prof *Profile, ti tokenIndex, notes *noteSet, opts emitOpts) {
	var plain, disorder, oob, disoob, tlsrec, ipfrag []SplitOp
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
		case SplitIPFrag:
			ipfrag = append(ipfrag, s)
		}
	}

	segmenting := len(plain) + len(disorder) + len(oob) + len(disoob)

	if segmenting == 0 && len(ipfrag) > 0 {
		set.Fragmentation.Strategy = "ip"
		ti.each(prof.Index, "desync", func(t Token) {
			notes.set(t, StatusApproximated, "ipFragMapped", "fragmentation.strategy=ip")
		})
		return
	}
	if onlyExtSplit(plain, disorder) {
		set.Fragmentation.Strategy = "extsplit"
		noteSplit(notes, tokenByIndex(ti, prof.Index, plainOrDisorder(plain, disorder).Token),
			StatusMapped, "sniExtMapped", "fragmentation.strategy=extsplit")
		return
	}

	switch {
	case segmenting == 0 && len(tlsrec) > 0:
		set.Fragmentation.Strategy = "tls"
		tok := tokenAt(tokens(ti, prof.Index, "tlsrec"), 0)
		if tlsrec[0].Pos.Anchor != AnchorAbs {
			set.Fragmentation.MiddleSNI = true
			set.Fragmentation.TLSRecordPosition = 0
			noteSplit(notes, tok, StatusApproximated, "tlsRecAnchorApproximated",
				"fragmentation.strategy=tls", "fragmentation.middle_sni=true")
			break
		}
		pos := clamp(absOffset(tlsrec[0].Pos, 1), 1, maxTLSRecPosition)
		set.Fragmentation.MiddleSNI = false
		set.Fragmentation.TLSRecordPosition = pos
		noteSplit(notes, tok, StatusMapped, "tlsRecMapped",
			"fragmentation.strategy=tls", "fragmentation.tlsrec_pos="+strconv.Itoa(pos))
	case segmenting == 0:
		set.Fragmentation.Strategy = config.ConfigNone
		if len(prof.Splits) > 0 {
			notes.extra = append(notes.extra, Note{
				Token:     splitTokenList(prof.Splits, ti, prof.Index),
				Synthetic: true,
				Profile:   prof.Index,
				Status:    StatusApproximated,
				Reason:    "fakeSplitBoundaryLost",
				Fields:    []string{"fragmentation.strategy=none"},
			})
		}
	case len(disoob) > 0:
		set.Fragmentation.Strategy = "disorder"
		applySplitPositions(set, append(append(plain, disorder...), disoob...), notes, ti, prof)
		ti.each(prof.Index, "disoob", func(t Token) {
			notes.set(t, StatusApproximated, "disoobNoOOB", "fragmentation.strategy=disorder")
		})
	case len(oob) > 0 && len(plain) == 0 && len(disorder) == 0:
		set.Fragmentation.Strategy = "oob"
		if opts.Defaults.OOBByte > 0 {
			set.Fragmentation.OOBChar = byte(opts.Defaults.OOBByte)
		}
		anchored := oob[0].Pos.Anchor != AnchorAbs
		raw := absOffset(oob[0].Pos, 1)
		pos := clamp(raw, 1, maxOOBPosition)
		set.Fragmentation.MiddleSNI = anchored
		if anchored {
			set.Fragmentation.OOBPosition = 0
		} else {
			set.Fragmentation.OOBPosition = pos
		}
		ti.each(prof.Index, "oob", func(t Token) {
			switch {
			case anchored:
				notes.set(t, StatusApproximated, "oobAnchorApproximated",
					"fragmentation.strategy=oob", "fragmentation.middle_sni=true")
			case raw > maxOOBPosition:
				n := notes.set(t, StatusApproximated, "positionClamped",
					"fragmentation.strategy=oob", "fragmentation.oob_position="+strconv.Itoa(pos))
				n.Params = map[string]any{"position": describePos(oob[0].Pos)}
			default:
				notes.set(t, StatusMapped, "oobMapped",
					"fragmentation.strategy=oob", "fragmentation.oob_position="+strconv.Itoa(pos))
			}
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
		tok, hasTok := ti.first(prof.Index, "oob_data")
		if set.Fragmentation.Strategy != "oob" {
			if hasTok {
				notes.set(tok, StatusDegenerate, "requiresOOB")
			}
		} else {
			set.Fragmentation.OOBChar = prof.OOBByte
			if hasTok {
				notes.set(tok, StatusMapped, "oobByteMapped", "fragmentation.oob_char")
			}
		}
	}
}

func applySplitPositions(set *config.SetConfig, ops []SplitOp, notes *noteSet, ti tokenIndex, prof *Profile) {
	strategy := set.Fragmentation.Strategy
	honoursFixed := strategy == "tcp"

	middle := false
	firstByte := false
	fixed, fixedMax := 0, 0

	for _, op := range ops {
		switch op.Pos.Anchor {
		case AnchorSNI, AnchorHost, AnchorPacket, AnchorSNIExt:
			middle = true
		default:
			if op.Pos.Offset == 1 {
				firstByte = true
			} else if op.Pos.Offset > 1 {
				if fixed == 0 || op.Pos.Offset < fixed {
					fixed = op.Pos.Offset
				}
				if op.Pos.Offset > fixedMax {
					fixedMax = op.Pos.Offset
				}
			} else if op.Pos.Offset < 0 {
				middle = true
			}
		}
	}
	if !middle && !firstByte && fixed == 0 {
		middle = true
	}

	set.Fragmentation.MiddleSNI = middle
	set.Fragmentation.SNIPosition = 0
	set.Fragmentation.SNIPositionMax = 0
	if honoursFixed && fixed > 0 {
		set.Fragmentation.SNIPosition = clamp(fixed, 1, maxSNIPosition)
		if hi := clamp(fixedMax, 1, maxSNIPosition); hi > set.Fragmentation.SNIPosition {
			set.Fragmentation.SNIPositionMax = hi
		}
	} else if honoursFixed && firstByte {
		set.Fragmentation.SNIPosition = 1
	}
	if strategy == "combo" {
		set.Fragmentation.Combo.FirstByteSplit = firstByte
		set.Fragmentation.Combo.ExtensionSplit = !firstByte && !middle
		set.Fragmentation.Combo.DecoyEnabled = prof.Fake.Present
		if !set.Fragmentation.Combo.FirstByteSplit && !set.Fragmentation.Combo.ExtensionSplit && !middle {
			set.Fragmentation.MiddleSNI = true
		}
	}
	comboPoint := strategy == "combo" &&
		(set.Fragmentation.Combo.FirstByteSplit || set.Fragmentation.Combo.ExtensionSplit)
	if !set.Fragmentation.MiddleSNI && set.Fragmentation.SNIPosition == 0 && !comboPoint {
		set.Fragmentation.MiddleSNI = true
	}

	if len(ops) > 3 {
		notes.extra = append(notes.extra, Note{
			Token:     splitTokenList(ops, ti, prof.Index),
			Synthetic: true,
			Profile:   prof.Index,
			Status:    StatusApproximated,
			Reason:    "splitPointsCollapsed",
			Fields:    []string{"fragmentation.strategy=" + strategy},
			Params:    map[string]any{"count": len(ops)},
		})
	}

	collapsed := len(ops) > 3
	for i, op := range ops {
		tok := tokenByIndex(ti, prof.Index, op.Token)
		st, reason, fields := describeSplitMapping(op, strategy, honoursFixed, set)
		fields = resolveSplitFields(fields, set)
		n := notes.set(tok, st, reason, fields...)
		n.Synthetic = collapsed && i > 0
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

func resolveSplitFields(fields []string, set *config.SetConfig) []string {
	out := fields[:0:0]
	for _, f := range fields {
		if f != "fragmentation.middle_sni=true" {
			out = append(out, f)
			continue
		}
		if set.Fragmentation.MiddleSNI {
			out = append(out, f)
		}
	}
	if set.Fragmentation.SNIPositionMax > set.Fragmentation.SNIPosition {
		out = append(out, "fragmentation.sni_position_max="+strconv.Itoa(set.Fragmentation.SNIPositionMax))
	}
	return out
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
	case AnchorSNIExt:
		fields = append(fields, "fragmentation.middle_sni=true")
		return StatusApproximated, "sniExtApproximated", fields
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
		if set.Fragmentation.SNIPositionMax > set.Fragmentation.SNIPosition &&
			op.Pos.Offset >= set.Fragmentation.SNIPosition && op.Pos.Offset <= set.Fragmentation.SNIPositionMax {
			fields = append(fields, "fragmentation.sni_position="+strconv.Itoa(set.Fragmentation.SNIPosition))
			return StatusApproximated, "positionInRange", fields
		}
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

func emitFake(set *config.SetConfig, prog *Program, prof *Profile, ti tokenIndex, notes *noteSet, defaults SpecDefaults) {
	if !prof.Fake.Present {
		set.Faking.SNI = false
		noteFakeOptionsUnused(prof, ti, notes)
		return
	}
	set.Faking.SNI = true
	applyFooling(set, prof, ti, notes, defaults)
	if prof.Fake.Repeats > 0 {
		set.Faking.SNISeqLength = prof.Fake.Repeats
		if tok, ok := ti.first(prof.Index, "repeats"); ok {
			notes.set(tok, StatusMapped, "fakeRepeatsMapped", "faking.sni_seq_length")
		}
	}

	for _, op := range prof.Splits {
		if op.Kind != SplitFake {
			continue
		}
		tok := tokenByIndex(ti, prof.Index, op.Token)
		n := notes.set(tok, StatusApproximated, "fakeMapped",
			"faking.sni=true", "faking.strategy=ttl", "faking.ttl="+strconv.Itoa(int(set.Faking.TTL)))
		n.Params = map[string]any{"position": describePos(op.Pos)}
	}
	if tok, ok := ti.first(prof.Index, "ttl", "desync_ttl"); ok {
		if prof.Fake.TTLSet && prof.Fake.TTL == 0 {
			notes.set(tok, StatusMapped, "fakeTTLOriginal", "faking.apply_ttl=false")
		} else {
			notes.set(tok, StatusMapped, "fakeTTLMapped",
				"faking.ttl="+strconv.Itoa(int(set.Faking.TTL)),
				"faking.apply_ttl="+strconv.FormatBool(set.Faking.ApplyTTL))
		}
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
	case prof.Fake.ZeroPayload && sni == "":
		set.Faking.SNIType = config.FakePayloadZero
	case sni != "":
		host := sanitizeHost(sni)
		tok, hasTok := ti.first(prof.Index, "fake_sni", "tls_sni")
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
	ti.each(prof.Index, "fake_tls", func(tok Token) {
		v := strings.TrimSpace(tok.Value)
		switch {
		case isZeroHexLiteral(v):
			if set.Faking.SNIType == config.FakePayloadZero {
				notes.set(tok, StatusMapped, "fakeZeroPayload", "faking.sni_type=zero")
			} else {
				notes.set(tok, StatusApproximated, "fakeZeroPayloadOverridden",
					"faking.sni_type="+strconv.Itoa(set.Faking.SNIType))
			}
		case isHexLiteral(v):
			notes.set(tok, StatusUnsupported, "fakeHexPayload")
		case v == "" || strings.HasPrefix(v, "!"):
			notes.set(tok, StatusMapped, "fakeBuiltinPayload", "faking.sni_type=preset")
		default:
			n := notes.set(tok, StatusApproximated, "fakeDataFileUnresolved")
			n.Params = map[string]any{"path": v}
		}
	})

	var droppedMods []string
	for _, m := range prof.Fake.TLSMod {
		switch m {
		case "r", "rnd", "rndsni":
			set.Faking.TLSMod = appendUnique(set.Faking.TLSMod, "rnd")
		case "dupsid":
			set.Faking.TLSMod = appendUnique(set.Faking.TLSMod, "dupsid")
		default:
			droppedMods = append(droppedMods, m)
		}
	}
	if tok, ok := ti.first(prof.Index, "fake_tls_mod"); ok {
		var fields []string
		if len(set.Faking.TLSMod) > 0 {
			fields = append(fields, "faking.tls_mod="+strings.Join(set.Faking.TLSMod, "+"))
		}
		if set.Faking.SNIType == config.FakePayloadDomain {
			fields = append(fields, "faking.payload_domain="+set.Faking.PayloadDomain)
		}
		switch {
		case len(fields) == 0:
			notes.set(tok, StatusUnsupported, "fakeTLSModUnsupported")
		case len(droppedMods) > 0:
			n := notes.set(tok, StatusApproximated, "fakeTLSModPartial", fields...)
			n.Params = map[string]any{"dropped": strings.Join(droppedMods, ", ")}
		default:
			notes.set(tok, StatusMapped, "fakeTLSModMapped", fields...)
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

func chainEscalation(sets []config.SetConfig, prog *Program, ti tokenIndex, notes *noteSet, model string) {
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
		if model == "alternative" {
			notes.set(tok, StatusMapped, "newProfileAsSet")
			continue
		}
		if i == 0 {
			notes.set(tok, StatusMapped, "autoNoneEntrySet")
			continue
		}
		notes.set(tok, StatusApproximated, "autoNoneAsSeparateSet")
	}
}

func dropIdenticalEscalations(sets []config.SetConfig, notes *noteSet) {
	byID := map[string]int{}
	for i := range sets {
		byID[sets[i].Id] = i
	}
	for i := range sets {
		to := sets[i].Escalate.To
		if to == "" {
			continue
		}
		j, ok := byID[to]
		if !ok {
			continue
		}
		if !reflect.DeepEqual(sets[i].Fragmentation, sets[j].Fragmentation) ||
			!reflect.DeepEqual(sets[i].Faking, sets[j].Faking) {
			continue
		}
		sets[i].Escalate.To = ""
		notes.extra = append(notes.extra, Note{
			Token:     sets[i].Name,
			Synthetic: true,
			Profile:   i,
			Status:    StatusDegenerate,
			Reason:    "escalationTargetIdentical",
			Fields:    []string{"escalate.to="},
			Params:    map[string]any{"other": sets[j].Name},
		})
	}
}

func finalize(sets []config.SetConfig, prog *Program, notes *noteSet) {
	escalationTarget := map[string]bool{}
	for _, s := range sets {
		if s.Escalate.To != "" {
			escalationTarget[s.Escalate.To] = true
		}
	}
	for i := range sets {
		s := &sets[i]
		hasTargets := len(s.Targets.SNIDomains) > 0 || len(s.Targets.IPs) > 0
		carries := prog.Profiles[i].carriesStrategy()
		if escalationTarget[s.Id] && !hasTargets {
			if s.TCP.DPortFilter != "" || s.UDP.DPortFilter != "" {
				notes.extra = append(notes.extra, Note{
					Token:     s.Name,
					Synthetic: true,
					Profile:   i,
					Status:    StatusUnsupported,
					Reason:    "portFilterDroppedOnFallback",
					Fields:    []string{"tcp.dport_filter=", "udp.dport_filter="},
				})
			}
			s.TCP.DPortFilter = ""
			s.UDP.DPortFilter = ""
			s.Enabled = true
			continue
		}
		s.Enabled = hasTargets && carries && !prog.Profiles[i].Skip
	}
	disableShadowedSets(sets, notes)
}

func disableShadowedSets(sets []config.SetConfig, notes *noteSet) {
	owner := map[string]int{}
	for i := range sets {
		s := &sets[i]
		if !s.Enabled {
			continue
		}
		var stolenBy int
		var stolen string
		for _, d := range s.Targets.SNIDomains {
			if first, seen := owner[d]; seen {
				stolenBy, stolen = first, d
				break
			}
		}
		if stolen == "" {
			for _, d := range s.Targets.SNIDomains {
				owner[d] = i
			}
			continue
		}
		s.Enabled = false
		notes.extra = append(notes.extra, Note{
			Token:     s.Name,
			Profile:   i,
			Status:    StatusUnsupported,
			Reason:    "shadowedByEarlierSet",
			Synthetic: true,
			Fields:    []string{"enabled=false"},
			Params: map[string]any{
				"domain": stolen,
				"other":  sets[stolenBy].Name,
			},
		})
	}
}

var foolingToStrategy = map[string]string{
	"badsum": "tcp_check",
	"badseq": "pastseq",
	"ts":     "timestamp",
}

func applyFooling(set *config.SetConfig, prof *Profile, ti tokenIndex, notes *noteSet, defaults SpecDefaults) {
	switch {
	case prof.Fake.TTLSet && prof.Fake.TTL > 0:
		set.Faking.TTL = uint8(clamp(prof.Fake.TTL, 1, 255))
		set.Faking.ApplyTTL = true
	case prof.Fake.TTLSet:
		set.Faking.ApplyTTL = false
	case defaults.FakeTTL > 0:
		set.Faking.TTL = uint8(clamp(defaults.FakeTTL, 1, 255))
		set.Faking.ApplyTTL = defaults.FakeTTLForced
	}
	set.Faking.Strategy = "ttl"

	var chosen string
	var extra []string
	for _, f := range prof.Fake.Fooling {
		switch {
		case f == "md5sig":
			set.Faking.MD5OnFake = true
		case foolingToStrategy[f] != "":
			if chosen == "" {
				chosen = f
				set.Faking.Strategy = foolingToStrategy[f]
				continue
			}
			extra = append(extra, f)
		default:
			extra = append(extra, f)
		}
	}
	if chosen == "badseq" && prof.Fake.SeqIncrement != 0 {
		set.Faking.SeqOffset = int32(abs(prof.Fake.SeqIncrement))
	}
	if chosen == "ts" && prof.Fake.TSIncrement != 0 {
		set.Faking.TimestampDecrease = uint32(abs(prof.Fake.TSIncrement))
	}

	tok, ok := ti.first(prof.Index, "fooling")
	if !ok {
		return
	}
	fields := []string{"faking.strategy=" + set.Faking.Strategy}
	if set.Faking.MD5OnFake {
		fields = append(fields, "faking.md5_on_fake=true")
	}
	if len(extra) > 0 {
		n := notes.set(tok, StatusApproximated, "foolingPartial", fields...)
		n.Params = map[string]any{"kept": set.Faking.Strategy, "dropped": strings.Join(extra, ", ")}
		return
	}
	notes.set(tok, StatusMapped, "foolingMapped", fields...)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

var fakeOnlyKeys = []string{
	"ttl", "md5sig", "fake_data", "fake_sni", "tls_sni", "fake_tls_mod",
	"fake_offset", "fake_offset_pos", "ip_opt",
	"fooling", "desync_ttl", "repeats", "fake_tls", "badseq_inc", "ts_inc",
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
		Token:     fmt.Sprintf("profile %d", prof.Index+1),
		Profile:   prof.Index,
		Status:    StatusApproximated,
		Reason:    "profileWithoutDesync",
		Synthetic: true,
		Fields:    []string{"fragmentation.strategy=none", "faking.sni=false"},
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

func splitTokenList(ops []SplitOp, ti tokenIndex, profile int) string {
	seen := map[string]bool{}
	var out []string
	for _, op := range ops {
		raw := tokenByIndex(ti, profile, op.Token).Raw
		if raw == "" || seen[raw] {
			continue
		}
		seen[raw] = true
		out = append(out, raw)
	}
	return strings.Join(out, " ")
}
