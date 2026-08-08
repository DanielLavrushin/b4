package convert

var normalizers = map[string]func(*Program, []Token, *noteSet){
	"zapret": normalizeZapret,
}

func runNormalizer(name string, prog *Program, tokens []Token, notes *noteSet) {
	if fn, ok := normalizers[name]; ok {
		fn(prog, tokens, notes)
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

func normalizeZapretProfile(prof *Profile, notes *noteSet) {
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
		case "fakedsplit":
			prof.Fake.Present = true
			appendSplits(prof, SplitPlain, positions[:1], token)
		case "fakeddisorder":
			prof.Fake.Present = true
			appendSplits(prof, SplitDisorder, positions[:1], token)
		case "hostfakesplit":
			prof.Fake.Present = true
			appendSplits(prof, SplitPlain, positions[:1], token)
		case "ipfrag1", "ipfrag2":
			appendSplits(prof, SplitIPFrag, positions[:1], token)
		}
	}
}

func appendSplits(prof *Profile, kind SplitKind, positions []Pos, token int) {
	for _, p := range positions {
		prof.Splits = append(prof.Splits, SplitOp{Kind: kind, Pos: p, Token: token})
	}
}
