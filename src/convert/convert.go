package convert

import (
	"errors"
	"sort"
	"strings"

	"github.com/daniellavrushin/b4/config"
)

var (
	ErrNothingToParse  = errors.New("no options found in the supplied text")
	ErrUnsupportedTool = errors.New("the options look like a tool b4 cannot convert yet")
)

type Options struct {
	Tool           string           `json:"tool"`
	Version        string           `json:"version"`
	NamePrefix     string           `json:"name_prefix"`
	Domains        []string         `json:"domains"`
	ProfileDomains map[int][]string `json:"profile_domains"`
}

type Warning struct {
	Code   string         `json:"code"`
	Params map[string]any `json:"params,omitempty"`
}

type Unresolved struct {
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Profile int    `json:"profile"`
}

type Fidelity struct {
	Mapped        int `json:"mapped"`
	Approximated  int `json:"approximated"`
	Unsupported   int `json:"unsupported"`
	NotApplicable int `json:"not_applicable"`
	Degenerate    int `json:"degenerate"`
	Unknown       int `json:"unknown"`
	Invalid       int `json:"invalid"`
	Total         int `json:"total"`
	Score         int `json:"score"`
}

type SetPlan struct {
	Profile        int      `json:"profile"`
	Name           string   `json:"name"`
	Role           string   `json:"role"`
	FallbackFor    int      `json:"fallback_for"`
	AcceptsTargets bool     `json:"accepts_targets"`
	Domains        []string `json:"domains"`
	IPs            []string `json:"ips"`
	Strategy       string   `json:"strategy"`
	Faking         bool     `json:"faking"`
	Enabled        bool     `json:"enabled"`
}

type Result struct {
	Tool            string             `json:"tool"`
	ToolLabel       string             `json:"tool_label"`
	Version         string             `json:"version"`
	VersionLabel    string             `json:"version_label"`
	VersionInferred bool               `json:"version_inferred"`
	Confidence      float64            `json:"confidence"`
	Argv            []string           `json:"argv"`
	Sets            []config.SetConfig `json:"sets"`
	Notes           []Note             `json:"notes"`
	Warnings        []Warning          `json:"warnings"`
	Unresolved      []Unresolved       `json:"unresolved"`
	Fidelity        Fidelity           `json:"fidelity"`
	Plan            []SetPlan          `json:"plan"`
	Applicable      bool               `json:"applicable"`
}

func anyProfileCarriesStrategy(prog *Program) bool {
	for _, p := range prog.Profiles {
		if p.carriesStrategy() {
			return true
		}
	}
	return false
}

func flagUnrecognisedProfiles(prog *Program, sets []config.SetConfig, notes *noteSet) {
	lost := map[int]int{}
	for _, n := range notes.list() {
		if n.Status == StatusUnknown || n.Status == StatusInvalid {
			lost[n.Profile]++
		}
	}
	for i := range sets {
		if i >= len(prog.Profiles) || lost[i] == 0 || prog.Profiles[i].carriesStrategy() {
			continue
		}
		sets[i].Enabled = false
		notes.extra = append(notes.extra, Note{
			Token:     sets[i].Name,
			Profile:   i,
			Status:    StatusInvalid,
			Reason:    "profileNotUnderstood",
			Synthetic: true,
			Fields:    []string{"enabled=false"},
			Params:    map[string]any{"count": lost[i]},
		})
	}
}

func Analyze(input string, opts Options) (*Result, error) {
	src := scanSource(input)
	probe := src.probeArgv()
	if len(probe) == 0 {
		return nil, ErrNothingToParse
	}

	all, err := loadSpecs()
	if err != nil {
		return nil, err
	}

	var spec *Spec
	confidence := 1.0
	if opts.Tool != "" {
		s, ok := all[opts.Tool]
		if !ok {
			return nil, errors.New("unknown tool: " + opts.Tool)
		}
		spec = s
	} else {
		spec, confidence = detectTool(input, probe, all)
		if spec == nil || confidence < minToolConfidence {
			return nil, ErrUnsupportedTool
		}
	}
	if spec == nil {
		return nil, ErrUnsupportedTool
	}

	argv, srcRep := src.assemble(spec)
	if len(argv) == 0 {
		argv = probe
	}

	version := opts.Version
	inferred := false
	if version == "" || !spec.hasVersion(version) {
		version, inferred = detectVersion(spec, argv)
		inferred = !inferred
	}

	table := spec.tableFor(version)
	tokens := getoptLong(argv, table, spec.Style == "long_only")

	notes := newNoteSet()
	prog, resolved := buildProgram(spec, version, tokens, notes)
	runNormalizer(spec.Normalize, prog, resolved, notes)
	resolved = dropTrailing(prog, resolved, spec.ProfileModel)
	resolved = foldUDPProfiles(prog, resolved, notes)
	sets := emit(prog, resolved, notes, emitOpts{
		NamePrefix:     opts.NamePrefix,
		Domains:        opts.Domains,
		ProfileDomains: opts.ProfileDomains,
		ProfileModel:   spec.ProfileModel,
		BreakKeys:      spec.ProfileBreak,
		Defaults:       spec.Defaults,
	})
	reconcileRepeats(resolved, notes)
	noteUnaccounted(resolved, notes)

	res := &Result{
		Tool:            spec.Tool,
		ToolLabel:       spec.Label,
		Version:         version,
		VersionLabel:    spec.versionLabel(version),
		VersionInferred: inferred,
		Confidence:      confidence,
		Argv:            argv,
		Sets:            sets,
		Notes:           notes.list(),
		Unresolved:      collectUnresolved(prog),
	}
	flagUnrecognisedProfiles(prog, sets, notes)
	res.Notes = notes.list()
	res.Applicable = anyProfileCarriesStrategy(prog)
	res.Plan = buildPlan(prog, sets)
	res.Warnings = buildWarnings(spec, argv, prog, sets, inferred, srcRep)
	res.Fidelity = score(res.Notes)
	return res, nil
}

func buildPlan(prog *Program, sets []config.SetConfig) []SetPlan {
	fallbackFor := make(map[string]int, len(sets))
	for i, s := range sets {
		if s.Escalate.To != "" {
			fallbackFor[s.Escalate.To] = i
		}
	}
	plan := make([]SetPlan, 0, len(sets))
	for i, s := range sets {
		entry := prog.Profiles[i].IsEntry()
		role := "entry"
		from := -1
		if !entry {
			role = "fallback"
			if idx, ok := fallbackFor[s.Id]; ok {
				from = idx
			}
		}
		plan = append(plan, SetPlan{
			Profile:        i,
			Name:           s.Name,
			Role:           role,
			FallbackFor:    from,
			AcceptsTargets: entry,
			Domains:        append([]string{}, s.Targets.SNIDomains...),
			IPs:            append([]string{}, s.Targets.IPs...),
			Strategy:       s.Fragmentation.Strategy,
			Faking:         s.Faking.SNI,
			Enabled:        s.Enabled,
		})
	}
	return plan
}

func collectUnresolved(prog *Program) []Unresolved {
	out := []Unresolved{}
	for _, p := range prog.Profiles {
		if p.Filters.HostsRef != "" {
			out = append(out, Unresolved{Kind: "hostlist", Path: p.Filters.HostsRef, Profile: p.Index})
		}
		if p.Filters.IPsRef != "" {
			out = append(out, Unresolved{Kind: "ipset", Path: p.Filters.IPsRef, Profile: p.Index})
		}
		if p.Fake.DataRef != "" {
			out = append(out, Unresolved{Kind: "payload", Path: p.Fake.DataRef, Profile: p.Index})
		}
	}
	return out
}

const minToolConfidence = 0.35

func buildWarnings(spec *Spec, argv []string, prog *Program, sets []config.SetConfig, inferred bool, src sourceReport) []Warning {
	out := []Warning{}
	if inferred {
		if amb := ambiguousFlags(spec, argv); len(amb) > 0 {
			out = append(out, Warning{Code: "ambiguousVersion", Params: map[string]any{"flags": amb}})
		}
	}
	if src.Layout != "" {
		out = append(out, Warning{Code: "sourceLayout", Params: map[string]any{
			"layout": src.Layout, "vars": strings.Join(uniqueStrings(src.Used), ", "),
		}})
	}
	if len(src.Skipped) > 0 {
		out = append(out, Warning{Code: "unusedVars", Params: map[string]any{
			"vars": strings.Join(src.Skipped, ", "), "tool": spec.Label,
		}})
	}
	if len(src.Foreign) > 0 {
		out = append(out, Warning{Code: "foreignDaemonVars", Params: map[string]any{
			"vars": strings.Join(src.Foreign, ", "), "tool": spec.Label,
		}})
	}
	if names := sortedKeys(src.Alternate); len(names) > 0 {
		n := 0
		for _, k := range names {
			n += src.Alternate[k]
		}
		out = append(out, Warning{Code: "alternativeStrategies", Params: map[string]any{
			"count": n, "vars": strings.Join(names, ", "),
		}})
	}
	enabled := 0
	for _, s := range sets {
		if s.Enabled && (len(s.Targets.SNIDomains) > 0 || len(s.Targets.IPs) > 0) {
			enabled++
		}
	}
	if enabled == 0 {
		out = append(out, Warning{Code: "needsTargets", Params: map[string]any{
			"sets": len(sets), "tool": spec.Label,
		}})
	}
	if !anyProfileCarriesStrategy(prog) {
		out = append(out, Warning{Code: "nothingRecognized", Params: map[string]any{"tool": spec.Label}})
	}
	if shadowed := countShadowed(sets); shadowed > 0 {
		out = append(out, Warning{Code: "shadowedSets", Params: map[string]any{
			"count": shadowed, "tool": spec.Label,
		}})
	}
	if strategyless := countStrategyless(prog, sets); strategyless > 0 {
		out = append(out, Warning{Code: "setsWithoutStrategy", Params: map[string]any{"count": strategyless}})
	}
	if prog.Globals.NoUDP {
		out = append(out, Warning{Code: "udpDisabledUpstream"})
	}
	if len(prog.Profiles) > 1 {
		out = append(out, Warning{Code: "profilesAsSets", Params: map[string]any{"count": len(prog.Profiles)}})
	}
	return out
}

func countStrategyless(prog *Program, sets []config.SetConfig) int {
	n := 0
	for i := range sets {
		if i < len(prog.Profiles) && !prog.Profiles[i].carriesStrategy() {
			n++
		}
	}
	return n
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func countShadowed(sets []config.SetConfig) int {
	n := 0
	for _, s := range sets {
		if !s.Enabled && len(s.Targets.SNIDomains) > 0 {
			n++
		}
	}
	return n
}

var unscoredReasons = map[string]bool{
	"strayArgument": true, "templatePlaceholder": true, "profileNotUnderstood": true,
}

func score(notes []Note) Fidelity {
	var f Fidelity
	var scored Fidelity
	for _, n := range notes {
		counted := !n.Synthetic && !unscoredReasons[n.Reason]
		switch n.Status {
		case StatusMapped:
			f.Mapped++
			if counted {
				scored.Mapped++
			}
		case StatusApproximated:
			f.Approximated++
			if counted {
				scored.Approximated++
			}
		case StatusUnsupported:
			f.Unsupported++
			if counted {
				scored.Unsupported++
			}
		case StatusNotApplicable:
			f.NotApplicable++
		case StatusDegenerate:
			f.Degenerate++
			if counted {
				scored.Degenerate++
			}
		case StatusUnknown:
			f.Unknown++
			if counted {
				scored.Unknown++
			}
		case StatusInvalid:
			f.Invalid++
			if counted {
				scored.Invalid++
			}
		}
	}
	f.Total = f.Mapped + f.Approximated + f.Unsupported + f.NotApplicable + f.Degenerate + f.Unknown + f.Invalid
	denom := scored.Mapped + scored.Approximated + scored.Unsupported + scored.Degenerate + scored.Unknown + scored.Invalid
	if denom == 0 {
		f.Score = 0
		return f
	}
	weighted := float64(scored.Mapped) + 0.6*float64(scored.Approximated) + 0.3*float64(scored.Degenerate)
	f.Score = int((weighted/float64(denom))*100 + 0.5)
	return f
}
