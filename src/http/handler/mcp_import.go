package handler

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/convert"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sni"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	mcpImportTool         = "b4_import_strategy"
	mcpImportPayloadFile  = "payload"
	mcpImportOptionWidth  = 96
	mcpImportOptionsShown = 6
	mcpImportDomainsShown = 5
	mcpImportMovedShown   = 10
	mcpImportRowsShown    = 40
)

type mcpImportIn struct {
	Text    string `json:"text" jsonschema:"The command line or config file, pasted as is."`
	Tool    string `json:"tool,omitempty" jsonschema:"zapret or byedpi. Default: detected from the text."`
	Version string `json:"version,omitempty" jsonschema:"Tool version, when the detected one is wrong; the preview lists the values."`
	Domains string `json:"domains,omitempty" jsonschema:"Comma-separated domains for the first set whose profile has no host list."`
	Apply   string `json:"apply,omitempty" jsonschema:"'true' creates the sets. Default: preview only."`
}

type mcpImportFidelity struct {
	Score         int `json:"score_of_100"`
	Options       int `json:"options"`
	Mapped        int `json:"mapped,omitempty"`
	Approximated  int `json:"approximated,omitempty"`
	Unsupported   int `json:"unsupported,omitempty"`
	NotApplicable int `json:"not_applicable,omitempty"`
	Degenerate    int `json:"degenerate,omitempty"`
	Unknown       int `json:"unknown,omitempty"`
	Invalid       int `json:"invalid,omitempty"`
}

type mcpImportSet struct {
	Position       int      `json:"position"`
	Id             string   `json:"id,omitempty"`
	Name           string   `json:"name"`
	Role           string   `json:"role"`
	FallbackFor    string   `json:"fallback_for,omitempty"`
	Enabled        bool     `json:"enabled"`
	DisabledWhy    string   `json:"disabled_because,omitempty"`
	Strategy       string   `json:"strategy"`
	Faking         bool     `json:"faking"`
	UDPMode        string   `json:"udp_mode"`
	TCPPorts       string   `json:"tcp_ports,omitempty"`
	UDPPorts       string   `json:"udp_ports,omitempty"`
	AcceptsTargets bool     `json:"accepts_targets"`
	NeedsTargets   bool     `json:"needs_targets,omitempty"`
	DomainCount    int      `json:"domain_count"`
	Domains        []string `json:"domains,omitempty"`
	IPCount        int      `json:"ip_count"`
	Calls          []string `json:"calls,omitempty"`
}

type mcpImportOption struct {
	Status      string   `json:"status,omitempty"`
	Options     []string `json:"options,omitempty"`
	MoreOptions int      `json:"more_options,omitempty"`
	Sets        []int    `json:"sets"`
	Why         string   `json:"why"`
	Settings    []string `json:"settings,omitempty"`
}

type mcpImportFile struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
	Sets []int  `json:"sets"`
}

type mcpImportMove struct {
	Set     string   `json:"set"`
	SetId   string   `json:"set_id"`
	Count   int      `json:"count"`
	Domains []string `json:"domains"`
}

type mcpImportOut struct {
	Applied         bool              `json:"applied"`
	Tool            string            `json:"tool"`
	ToolLabel       string            `json:"tool_label"`
	Version         string            `json:"version"`
	VersionGuessed  bool              `json:"version_guessed,omitempty"`
	Versions        []string          `json:"versions,omitempty"`
	Fidelity        mcpImportFidelity `json:"fidelity"`
	CanApply        bool              `json:"can_apply"`
	Blocked         string            `json:"blocked,omitempty"`
	Sets            []mcpImportSet    `json:"sets"`
	NotConverted    []mcpImportOption `json:"not_converted,omitempty"`
	Approximated    []mcpImportOption `json:"approximated,omitempty"`
	UnresolvedFiles []mcpImportFile   `json:"unresolved_files,omitempty"`
	Warnings        []string          `json:"warnings,omitempty"`
	WouldMoveFrom   []mcpImportMove   `json:"would_move_from,omitempty"`
	MovedFrom       []mcpImportMove   `json:"moved_from,omitempty"`
	TotalSets       int               `json:"total_sets"`
	Note            string            `json:"note"`
}

func mcpImportApply(raw string) (bool, error) {
	if strings.TrimSpace(raw) == "" {
		return false, nil
	}
	apply, err := mcpParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("apply must be true or false, got %q", raw)
	}
	return apply, nil
}

func mcpImportToolRef(raw string) (string, error) {
	want := strings.ToLower(strings.TrimSpace(raw))
	if want == "" {
		return "", nil
	}
	tools, err := convert.Tools()
	if err != nil {
		return "", fmt.Errorf("the converter rules could not be loaded: %w", err)
	}
	ids := make([]string, 0, len(tools))
	for _, t := range tools {
		if t.Tool == want {
			return t.Tool, nil
		}
		ids = append(ids, t.Tool)
	}
	return "", fmt.Errorf("unknown tool %q: expected one of %s, or omit tool to detect it from the text", raw, strings.Join(ids, ", "))
}

func mcpImportVersions(tool string) []string {
	tools, err := convert.Tools()
	if err != nil {
		return nil
	}
	for _, t := range tools {
		if t.Tool == tool {
			return t.Versions
		}
	}
	return nil
}

func (api *API) mcpImportDomains(raw string) ([]string, error) {
	entries := mcpSplitEntries(raw)
	if len(entries) > mcpTargetMaxEntries {
		return nil, fmt.Errorf("too many domains in one call (%d): send at most %d, and add the rest with b4_edit_set_targets", len(entries), mcpTargetMaxEntries)
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		canonical, _, err := api.mcpCanonicalTarget(mcpTargetKindDomains, entry)
		if err == nil {
			err = mcpASCIIPlainDomain(canonical)
		}
		if err != nil {
			return nil, err
		}
		if _, isRegex := sni.ParseDomainEntry(canonical); isRegex {
			return nil, fmt.Errorf("%q is a pattern, and domains takes plain domains only: add it to the created set with b4_edit_set_targets", entry)
		}
		out = append(out, canonical)
	}
	return out, nil
}

func mcpImportSourceOption(notes []convert.Note, profile int, path string) string {
	field := strings.TrimPrefix(path, mcpSetPathPrefix+".") + "="
	for _, n := range notes {
		if n.Profile != profile || n.Synthetic {
			continue
		}
		for _, f := range n.Fields {
			if strings.HasPrefix(f, field) {
				return fmt.Sprintf(", from the option %s", mcpImportOptionText(n.Token))
			}
		}
	}
	return ""
}

func mcpImportSetRefusal(set, defaults *config.SetConfig, source func(path string) string) error {
	cfg := &config.Config{Sets: []*config.SetConfig{set}}
	base := &config.Config{Sets: []*config.SetConfig{defaults}}
	refuse := func(path, value, accepted string) error {
		return fmt.Errorf("the set %q would get %s=%s%s, and b4 accepts %s for it over MCP. Change that option in the text and import again",
			set.Name, strings.TrimPrefix(path, mcpSetPathPrefix+"."), value, source(path), accepted)
	}
	changed := func(path string) (reflect.Value, bool) {
		if !strings.HasPrefix(path, mcpSetPathPrefix+".") {
			return reflect.Value{}, false
		}
		field, err := mcpResolvePath(cfg, path, set.Id)
		if err != nil {
			return reflect.Value{}, false
		}
		def, err := mcpResolvePath(base, path, defaults.Id)
		if err != nil || reflect.DeepEqual(field.Interface(), def.Interface()) {
			return reflect.Value{}, false
		}
		return field, true
	}

	for _, path := range slices.Sorted(maps.Keys(mcpRanges)) {
		field, ok := changed(path)
		if !ok || !field.CanInt() {
			continue
		}
		n := field.Int()
		if mcpCheckRange(path, "", n) != nil {
			return refuse(path, strconv.FormatInt(n, 10), mcpRanges[path].String())
		}
	}
	for _, path := range slices.Sorted(maps.Keys(mcpEnums)) {
		field, ok := changed(path)
		if !ok || field.Kind() != reflect.String {
			continue
		}
		if opts := mcpEnums[path]; !slices.Contains(opts, field.String()) {
			return refuse(path, strconv.Quote(field.String()), "one of "+strings.Join(mcpQuoteEmpty(opts), ", "))
		}
	}
	for _, path := range slices.Sorted(maps.Keys(mcpListEntryEnums)) {
		field, ok := changed(path)
		if !ok || field.Kind() != reflect.Slice {
			continue
		}
		opts := mcpListEntryEnums[path]
		for i := 0; i < field.Len(); i++ {
			if entry := field.Index(i).String(); !slices.Contains(opts, entry) {
				return refuse(path, strconv.Quote(entry), "a list of "+strings.Join(opts, ", "))
			}
		}
	}
	return nil
}

func (api *API) mcpImportRefusal(cfg *config.Config, res *convert.Result) error {
	if !res.Applicable {
		return fmt.Errorf("%sThe options that were not understood are listed in the preview's not_converted", convertNothingApplicable)
	}
	if total := len(cfg.Sets) + len(res.Sets); total > mcpMaxSets {
		return fmt.Errorf("the text converts into %d sets and there are already %d; b4 will not go past %d sets from here",
			len(res.Sets), len(cfg.Sets), mcpMaxSets)
	}

	defaults := config.NewSetConfig()
	defaults.Id = "defaults"
	config.ApplySetDefaults(&defaults)

	staged := assignSetIDs(res.Sets)
	uniqueSetNames(staged, cfg.Sets)
	for i, set := range staged {
		config.ApplySetDefaults(set)
		for _, entry := range set.Targets.SNIDomains {
			canonical, _, err := api.mcpCanonicalTarget(mcpTargetKindDomains, entry)
			if err == nil {
				err = mcpASCIIPlainDomain(canonical)
			}
			if err != nil {
				return fmt.Errorf("the set %q would list a domain b4 cannot match: %w. Remove it from the text and import again", set.Name, err)
			}
			if sni.CanonicalDomainEntry(entry) != canonical {
				return fmt.Errorf("the set %q would list %q, which b4 matches as written, so it would match nothing: b4 matches the subdomains of a domain itself. "+
					"Write it as %s in the text and import again", set.Name, entry, canonical)
			}
		}
		for _, entry := range set.Targets.IPs {
			if _, _, err := api.mcpCanonicalTarget(mcpTargetKindIPs, entry); err != nil {
				return fmt.Errorf("the set %q would list an address b4 cannot match: %w. Remove it from the text and import again", set.Name, err)
			}
		}
		if err := mcpImportSetRefusal(set, &defaults, func(path string) string {
			return mcpImportSourceOption(res.Notes, i, path)
		}); err != nil {
			return err
		}
	}
	return nil
}

func mcpImportUseFinalNames(res *convert.Result, cfg *config.Config) {
	named := make([]*config.SetConfig, len(res.Sets))
	for i := range res.Sets {
		set := res.Sets[i]
		named[i] = &set
	}
	uniqueSetNames(named, cfg.Sets)
	names := make([]string, len(named))
	for i, set := range named {
		names[i] = set.Name
	}
	res.RenameSets(names)
}

func mcpImportOptionText(token string) string {
	token = strings.Join(strings.Fields(token), " ")
	if len(token) <= mcpImportOptionWidth {
		return token
	}
	return mcpTruncate(token, mcpImportOptionWidth) + "…"
}

func mcpImportOptions(notes []convert.Note) (notConverted, approximated []mcpImportOption) {
	type group struct {
		row     mcpImportOption
		status  convert.Status
		options map[string]bool
		sets    map[int]bool
	}
	var order []*group
	byKey := map[string]*group{}

	for _, n := range notes {
		if n.Status == convert.StatusMapped {
			continue
		}
		why := n.English()
		key := string(n.Status) + "\x00" + why + "\x00" + strings.Join(n.Fields, "\x00")
		g := byKey[key]
		if g == nil {
			g = &group{
				row:     mcpImportOption{Why: why, Settings: n.Fields},
				status:  n.Status,
				options: map[string]bool{}, sets: map[int]bool{},
			}
			byKey[key] = g
			order = append(order, g)
		}
		if !g.sets[n.Profile] {
			g.sets[n.Profile] = true
			g.row.Sets = append(g.row.Sets, n.Profile+1)
		}
		if !n.Synthetic {
			if option := mcpImportOptionText(n.Token); !g.options[option] {
				g.options[option] = true
				if len(g.row.Options) < mcpImportOptionsShown {
					g.row.Options = append(g.row.Options, option)
				} else {
					g.row.MoreOptions++
				}
			}
		}
	}

	for _, g := range order {
		sort.Ints(g.row.Sets)
		if g.status == convert.StatusApproximated {
			approximated = append(approximated, g.row)
			continue
		}
		g.row.Status = string(g.status)
		notConverted = append(notConverted, g.row)
	}
	return notConverted, approximated
}

func mcpImportFiles(unresolved []convert.Unresolved) []mcpImportFile {
	var out []mcpImportFile
	at := map[string]int{}
	for _, u := range unresolved {
		key := u.Kind + "\x00" + u.Path
		i, ok := at[key]
		if !ok {
			i = len(out)
			at[key] = i
			out = append(out, mcpImportFile{Kind: u.Kind, Path: u.Path})
		}
		if !slices.Contains(out[i].Sets, u.Profile+1) {
			out[i].Sets = append(out[i].Sets, u.Profile+1)
		}
	}
	return out
}

func mcpImportMoves(moved []DomainReassignment) []mcpImportMove {
	var out []mcpImportMove
	at := map[string]int{}
	for _, m := range moved {
		i, ok := at[m.SetId]
		if !ok {
			i = len(out)
			at[m.SetId] = i
			out = append(out, mcpImportMove{Set: m.SetName, SetId: m.SetId})
		}
		out[i].Count++
		if len(out[i].Domains) < mcpImportMovedShown {
			out[i].Domains = append(out[i].Domains, m.Domain)
		}
	}
	return out
}

func mcpImportSets(res *convert.Result, sets []*config.SetConfig, withIDs bool) []mcpImportSet {
	rows := make([]mcpImportSet, 0, len(sets))
	for i, set := range sets {
		row := mcpImportSet{
			Position:    i + 1,
			Name:        set.Name,
			Role:        "entry",
			Enabled:     set.Enabled,
			Strategy:    set.Fragmentation.Strategy,
			Faking:      set.Faking.SNI,
			UDPMode:     set.UDP.Mode,
			TCPPorts:    set.TCP.DPortFilter,
			UDPPorts:    set.UDP.DPortFilter,
			DomainCount: len(set.Targets.SNIDomains),
			IPCount:     len(set.Targets.IPs),
			Calls:       set.UDP.FilterL7,
		}
		if withIDs {
			row.Id = set.Id
		}
		if len(set.Targets.SNIDomains) > mcpImportDomainsShown {
			row.Domains = set.Targets.SNIDomains[:mcpImportDomainsShown]
		} else {
			row.Domains = set.Targets.SNIDomains
		}
		if i < len(res.Plan) {
			plan := res.Plan[i]
			row.Role = plan.Role
			row.AcceptsTargets = plan.AcceptsTargets
			row.NeedsTargets = plan.DisabledFor == convert.DisabledNeedsTargets
			row.DisabledWhy = res.DisabledEnglish(i)
			if plan.FallbackFor >= 0 && plan.FallbackFor < len(sets) {
				row.FallbackFor = sets[plan.FallbackFor].Name
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func mcpImportPendingPayloads(sets []config.SetConfig) int {
	n := 0
	for _, s := range sets {
		for _, file := range []string{s.Faking.PayloadFile, s.UDP.FakePayloadFile} {
			if strings.HasPrefix(file, convert.PendingPayloadPrefix) {
				n++
			}
		}
	}
	return n
}

func mcpImportReport(res *convert.Result) mcpImportOut {
	versions := mcpImportVersions(res.Tool)
	out := mcpImportOut{
		Tool:           res.Tool,
		ToolLabel:      res.ToolLabel,
		Version:        res.Version,
		VersionGuessed: res.VersionInferred && len(versions) > 1,
		Fidelity: mcpImportFidelity{
			Score:         res.Fidelity.Score,
			Options:       res.Fidelity.Total,
			Mapped:        res.Fidelity.Mapped,
			Approximated:  res.Fidelity.Approximated,
			Unsupported:   res.Fidelity.Unsupported,
			NotApplicable: res.Fidelity.NotApplicable,
			Degenerate:    res.Fidelity.Degenerate,
			Unknown:       res.Fidelity.Unknown,
			Invalid:       res.Fidelity.Invalid,
		},
		UnresolvedFiles: mcpImportFiles(res.Unresolved),
	}
	if len(versions) > 1 {
		out.Versions = versions
	}
	out.NotConverted, out.Approximated = mcpImportOptions(res.Notes)
	for _, w := range res.Warnings {
		out.Warnings = append(out.Warnings, w.English())
	}
	return out
}

func mcpImportCap[T any](rows []T, what string, left *[]string) []T {
	if len(rows) <= mcpImportRowsShown {
		return rows
	}
	*left = append(*left, fmt.Sprintf("%d %s", len(rows)-mcpImportRowsShown, what))
	return rows[:mcpImportRowsShown]
}

func mcpImportTrim(out *mcpImportOut) string {
	var left []string
	out.Sets = mcpImportCap(out.Sets, "more set row(s)", &left)
	out.NotConverted = mcpImportCap(out.NotConverted, "more row(s) of not_converted", &left)
	out.Approximated = mcpImportCap(out.Approximated, "more row(s) of approximated", &left)
	out.UnresolvedFiles = mcpImportCap(out.UnresolvedFiles, "more unresolved file(s)", &left)
	out.WouldMoveFrom = mcpImportCap(out.WouldMoveFrom, "more set(s) in would_move_from", &left)
	out.MovedFrom = mcpImportCap(out.MovedFrom, "more set(s) in moved_from", &left)
	if len(left) == 0 {
		return ""
	}
	return " Left out of this answer for size: " + strings.Join(left, ", ") + "."
}

func mcpImportFollowUp(out mcpImportOut, label string, applied bool) string {
	var note strings.Builder
	disabled, needTargets := 0, 0
	for _, s := range out.Sets {
		if !s.Enabled {
			disabled++
		}
		if s.NeedsTargets {
			needTargets++
		}
	}
	if disabled > 0 {
		lead := "Would be created DISABLED"
		if applied {
			lead = "Created DISABLED"
		}
		fmt.Fprintf(&note, " %s: %d of the %d set(s). A disabled set matches nothing. "+
			"Its row says why in disabled_because: read that before enabling the set, because targets are not always what it lacks.",
			lead, disabled, len(out.Sets))
	}
	if needTargets > 0 {
		fmt.Fprintf(&note, " Sets marked needs_targets (%d) lack only targets: add them with b4_edit_set_targets, then enable the set with b4_manage_set action=set_enabled.", needTargets)
	}
	lists, payloads := 0, 0
	for _, f := range out.UnresolvedFiles {
		if f.Kind == mcpImportPayloadFile {
			payloads++
		} else {
			lists++
		}
	}
	if lists+payloads > 0 {
		fmt.Fprintf(&note, " b4 cannot read the files in unresolved_files: they are on the machine that ran %s.", label)
	}
	if lists > 0 {
		note.WriteString(" Ask the user for the contents of the hostlist and ipset files and add those to the sets named there with b4_edit_set_targets.")
	}
	if payloads > 0 {
		note.WriteString(" A file of kind payload holds the bytes of a fake packet, not a list: its set uses a built-in payload instead. " +
			"Only the user can bring the original in, by uploading it under Settings -> Payloads in b4's web interface and choosing it in the set.")
	}
	note.WriteString(" This is a best-effort translation, not an exact copy, and it does not test whether any site loads.")
	return note.String()
}

func (api *API) addMCPImportTools(srv *mcp.Server) {
	addTool(srv, &mcp.Tool{
		Name:  mcpImportTool,
		Title: "Import a zapret or byedpi strategy",
		Description: "Turns a zapret (nfqws, winws) or byedpi command line or config file into b4 sets; use it instead of translating their options yourself. " +
			"Without apply it writes nothing and previews: the sets, each option that did not convert and why, and the files b4 cannot read. " +
			"apply='true' creates the sets above the existing ones and takes their domains from other enabled sets; each such call adds another copy.",
		Annotations: mcpDestructiveEachCall,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpImportIn) (*mcp.CallToolResult, any, error) {
		if !api.getCfg().System.WebServer.MCP.AllowWrites {
			return nil, nil, errMCPWritesOff
		}
		apply, err := mcpImportApply(in.Apply)
		if err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(in.Text) == "" {
			return nil, nil, fmt.Errorf("text is required: the command line or the config file to convert")
		}
		tool, err := mcpImportToolRef(in.Tool)
		if err != nil {
			return nil, nil, err
		}
		domains, err := api.mcpImportDomains(in.Domains)
		if err != nil {
			return nil, nil, err
		}

		res, err := api.runConvert(convertRequest{Text: in.Text, Tool: tool, Version: strings.TrimSpace(in.Version), Domains: domains})
		if err != nil {
			return nil, nil, err
		}
		if want := strings.TrimSpace(in.Version); want != "" && res.Version != want {
			return nil, nil, fmt.Errorf("version %q is not a %s version: expected one of %s, or omit version to detect it",
				in.Version, res.ToolLabel, strings.Join(mcpImportVersions(res.Tool), ", "))
		}

		if !apply {
			cfg := api.getCfg()
			mcpImportUseFinalNames(res, cfg)
			out := mcpImportReport(res)
			staged := assignSetIDs(res.Sets)
			out.Sets = mcpImportSets(res, staged, false)
			out.TotalSets = len(cfg.Sets)
			out.WouldMoveFrom = mcpImportMoves(domainsHeldByEnabledSets(cfg.Sets, releasableDomains(staged)))

			var note strings.Builder
			note.WriteString("Preview only: nothing was written. ")
			if refusal := api.mcpImportRefusal(cfg, res); refusal != nil {
				out.Blocked = refusal.Error()
				note.WriteString("apply would be refused, for the reason in blocked.")
			} else {
				out.CanApply = true
				fmt.Fprintf(&note, "Calling again with the same arguments and apply='true' creates these %d set(s) at the top of the list, above the %d existing ones, so they match first. "+
					"Show the user the sets, what did not convert and the warnings before that.", len(staged), len(cfg.Sets))
				if len(out.WouldMoveFrom) > 0 {
					note.WriteString(" A domain belongs to one enabled set, so applying takes the domains in would_move_from away from the sets that hold them now.")
				}
			}
			note.WriteString(mcpImportFollowUp(out, res.ToolLabel, false))
			note.WriteString(mcpImportTrim(&out))
			out.Note = note.String()
			return nil, out, nil
		}

		mcpWriteMu.Lock()
		defer mcpWriteMu.Unlock()

		oldCfg := api.getCfg()
		mcpImportUseFinalNames(res, oldCfg)
		if err := api.mcpImportRefusal(oldCfg, res); err != nil {
			return nil, nil, err
		}
		out := mcpImportReport(res)
		payloads := mcpImportPendingPayloads(res.Sets)

		newCfg := oldCfg.Clone()
		created, moved, err := api.stageConvertedSets(newCfg, res.Sets)
		if err != nil {
			return nil, nil, err
		}
		if err := mcpValidateCandidate(oldCfg, newCfg); err != nil {
			return nil, nil, fmt.Errorf("rejected: %w", err)
		}

		snapshot := oldCfg.Clone()
		if err := api.mcpSave(oldCfg, newCfg); err != nil {
			return nil, nil, fmt.Errorf("rejected: %w", err)
		}
		api.applyRuntimeChanges(newCfg, oldCfg)
		refreshed := api.PerformSoftRestart(newCfg, oldCfg)

		mcpRecordChange(mcpChange{
			Path:     "sets",
			Previous: fmt.Sprintf("%d sets", len(oldCfg.Sets)),
			Current:  fmt.Sprintf("%d sets", len(newCfg.Sets)),
			When:     time.Now(), Snapshot: snapshot,
		}, oldCfg, newCfg)
		log.Infof("mcp: imported a %s configuration as %d set(s) (now %d sets)", res.Tool, len(created), len(newCfg.Sets))

		out.Applied = true
		out.CanApply = true
		out.Sets = mcpImportSets(res, created, true)
		out.TotalSets = len(newCfg.Sets)
		out.MovedFrom = mcpImportMoves(moved)

		var note strings.Builder
		fmt.Fprintf(&note, "applied live; %d set(s) created at positions 1 to %d of %d, so they match before the older sets. Undo with b4_revert_last_change",
			len(created), len(created), len(newCfg.Sets))
		if refreshed {
			note.WriteString(" (firewall rules were refreshed to match)")
		}
		note.WriteString(". Calling this again with apply='true' creates another copy of the sets.")
		if len(out.MovedFrom) > 0 {
			note.WriteString(" A domain belongs to one enabled set, so b4 took the domains in moved_from away from the sets that held them.")
		}
		if payloads > 0 {
			fmt.Fprintf(&note, " %d fake payload(s) written out in the text were saved as payload files; an undo removes the sets and leaves those files, which the web interface lists under Settings, Payloads.", payloads)
		}
		note.WriteString(mcpImportFollowUp(out, res.ToolLabel, true))
		if left := mcpImportTrim(&out); left != "" {
			note.WriteString(left + " b4_list_sets shows every set.")
		}
		out.Note = note.String()
		return nil, out, nil
	})
}
