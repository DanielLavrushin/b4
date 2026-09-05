package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sni"
	"github.com/daniellavrushin/b4/watchdog"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	mcpDiscoveryMaxDomains = 5
	mcpDiscoveryMaxGroups  = 20
)

var (
	mcpLastSuiteMu   sync.Mutex
	mcpLastSuiteID   string
	mcpAppliedGroups = map[string]string{}
)

func mcpRememberSuite(id string) {
	mcpLastSuiteMu.Lock()
	mcpLastSuiteID = id
	mcpAppliedGroups = map[string]string{}
	mcpLastSuiteMu.Unlock()
}

func mcpMarkGroupApplied(suiteID, preset, setID string) {
	mcpLastSuiteMu.Lock()
	mcpAppliedGroups[suiteID+"|"+preset] = setID
	mcpLastSuiteMu.Unlock()
}

func mcpGroupAppliedAs(suiteID, preset string) string {
	mcpLastSuiteMu.Lock()
	defer mcpLastSuiteMu.Unlock()
	return mcpAppliedGroups[suiteID+"|"+preset]
}

func mcpResolveSuiteID(explicit string) string {
	if id := strings.TrimSpace(explicit); id != "" {
		return id
	}
	if cur, ok := discovery.GetCurrentSuite(); ok && cur != nil {
		return cur.Id
	}
	mcpLastSuiteMu.Lock()
	defer mcpLastSuiteMu.Unlock()
	return mcpLastSuiteID
}

type mcpDiscoveryIn struct {
	Action  string `json:"action" jsonschema:"One of: start, status, cancel, apply."`
	Domains string `json:"domains,omitempty" jsonschema:"Comma-separated domains to find a strategy for, at most 5. For action=start."`
	Id      string `json:"id,omitempty" jsonschema:"Suite id from start. If omitted, the run in progress is used, then the last run this server started."`
	Domain  string `json:"domain,omitempty" jsonschema:"For action=apply: which domain's winning strategy to turn into a set."`
	Name    string `json:"name,omitempty" jsonschema:"Optional name for the set created by apply."`
	SkipDNS string `json:"skip_dns,omitempty" jsonschema:"'true' to skip the DNS poisoning probe on start."`
}

type mcpSuiteSnapshot struct {
	Id              string `json:"id"`
	Status          string `json:"status"`
	CurrentPhase    string `json:"current_phase"`
	CurrentDomain   string `json:"current_domain"`
	TotalChecks     int    `json:"total_checks"`
	CompletedChecks int    `json:"completed_checks"`
	DomainResults   map[string]struct {
		Domain        string  `json:"domain"`
		BestPreset    string  `json:"best_preset"`
		BestSpeed     float64 `json:"best_speed"`
		BestSuccess   bool    `json:"best_success"`
		BaselineWorks bool    `json:"baseline_works"`
		Confirmed     int     `json:"confirmed"`
		Outcome       string  `json:"outcome"`
		Unconfirmed   bool    `json:"unconfirmed"`
		DNSResult     *struct {
			IsPoisoned       bool `json:"is_poisoned"`
			TransportBlocked bool `json:"transport_blocked"`
		} `json:"dns_result"`
	} `json:"domain_discovery_results"`
	StrategyGroups []struct {
		WinnerPreset string            `json:"winner_preset"`
		Family       string            `json:"family"`
		Domains      []string          `json:"domains"`
		Set          *config.SetConfig `json:"set"`
	} `json:"strategy_groups"`
}

func mcpSuiteProjection(suite *discovery.CheckSuite) (*mcpSuiteSnapshot, error) {
	raw, err := json.Marshal(suite)
	if err != nil {
		return nil, fmt.Errorf("read discovery run: %w", err)
	}
	var snap mcpSuiteSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, fmt.Errorf("decode discovery run: %w", err)
	}
	return &snap, nil
}

type mcpDiscoveryDomain struct {
	Domain        string  `json:"domain"`
	BestPreset    string  `json:"best_preset,omitempty"`
	Family        string  `json:"family,omitempty"`
	BestKBPerSec  float64 `json:"best_kb_per_sec,omitempty"`
	Found         bool    `json:"strategy_found"`
	BaselineWorks bool    `json:"works_without_b4"`
	DNSPoisoned   bool    `json:"dns_poisoned,omitempty"`
	Blocked       bool    `json:"transport_blocked,omitempty"`
	Confirmed     int     `json:"confirmed,omitempty"`
	Provisional   bool    `json:"provisional,omitempty"`
	Verdict       string  `json:"verdict"`
}

type mcpDiscoveryOut struct {
	Id        string               `json:"suite_id,omitempty"`
	Status    string               `json:"status,omitempty"`
	Phase     string               `json:"phase,omitempty"`
	Progress  string               `json:"progress,omitempty"`
	Source    string               `json:"source,omitempty"`
	Domains   []mcpDiscoveryDomain `json:"domains,omitempty"`
	Applied   *mcpSetRow           `json:"applied_set,omitempty"`
	MovedFrom []DomainReassignment `json:"moved_from_other_sets,omitempty"`
	Changed   bool                 `json:"changed,omitempty"`
	Note      string               `json:"note"`
}

func mcpDiscoveryVerdict(d mcpDiscoveryDomain, running bool) string {
	switch {
	case d.BaselineWorks:
		return "works without b4 - do not create a set for it"
	case d.Blocked:
		return "the address itself is unreachable, so no packet strategy can help; only a proxy or VPN route would"
	case d.Found && running:
		return fmt.Sprintf("%s is the best so far, but the run is still testing and this is PROVISIONAL: it has not been confirmed, a better one may still win, and nothing can be applied until the run finishes or is cancelled", d.BestPreset)
	case d.Found:
		return fmt.Sprintf("a working strategy was found (%s)", d.BestPreset)
	default:
		return "no strategy tried made it work"
	}
}

func (api *API) mcpDiscoverySuiteRows(snap *mcpSuiteSnapshot, running bool) []mcpDiscoveryDomain {
	family := map[string]string{}
	for _, g := range snap.StrategyGroups {
		for _, d := range g.Domains {
			family[sni.NormalizeDomain(d)] = g.Family
		}
	}

	rows := make([]mcpDiscoveryDomain, 0, len(snap.DomainResults))
	for key, r := range snap.DomainResults {
		name := r.Domain
		if name == "" {
			name = key
		}
		row := mcpDiscoveryDomain{
			Domain:        name,
			BestPreset:    r.BestPreset,
			Family:        family[sni.NormalizeDomain(name)],
			BestKBPerSec:  r.BestSpeed / 1024,
			Found:         r.BestSuccess && !r.BaselineWorks,
			BaselineWorks: r.BaselineWorks,
			Confirmed:     r.Confirmed,
		}
		if r.DNSResult != nil {
			row.DNSPoisoned = r.DNSResult.IsPoisoned
			row.Blocked = r.DNSResult.TransportBlocked
		}
		mcpApplyOutcome(&row, discovery.Outcome(r.Outcome))
		row.Provisional = running && row.Found
		row.Verdict = mcpDiscoveryVerdict(row, running)
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Domain < rows[j].Domain })
	return rows
}

func mcpApplyOutcome(row *mcpDiscoveryDomain, outcome discovery.Outcome) {
	switch outcome {
	case discovery.OutcomeFound:
		row.Found, row.BaselineWorks, row.Blocked = true, false, false
	case discovery.OutcomeWorksWithoutBypass:
		row.Found, row.BaselineWorks = false, true
	case discovery.OutcomeAddressBlocked:
		row.Found, row.Blocked = false, true
	case discovery.OutcomeNotFound:
		row.Found = false
	}
}

func (api *API) addMCPDiscoveryTools(srv *mcp.Server) {
	addTool(srv, &mcp.Tool{
		Name:  "b4_find_bypass_strategy",
		Title: "Search for a working bypass strategy",
		Description: "Brute-forces b4's strategies against a domain until one makes it load, then turns the winner into a set. " +
			"action=start begins a run and returns immediately; status polls it; cancel stops it; apply creates the set. " +
			"A run takes MINUTES, emits heavy traffic from the router, holds firewall rules, and only one can be in flight - do not poll in a loop, tell the user it is running and check once when they ask. " +
			"A finished run is written to b4's discovery history, so status and apply keep working for it afterwards, across restarts, addressed by suite_id or just by domain.",
		Annotations: mcpProbe,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpDiscoveryIn) (*mcp.CallToolResult, mcpDiscoveryOut, error) {
		action := strings.ToLower(strings.TrimSpace(in.Action))
		switch action {
		case "start":
			return api.mcpDiscoveryStart(in)
		case "status":
			return api.mcpDiscoveryStatus(in)
		case "cancel":
			return api.mcpDiscoveryCancel(in)
		case "apply":
			return api.mcpDiscoveryApply(in)
		case "":
			return nil, mcpDiscoveryOut{}, fmt.Errorf("action is required: start, status, cancel or apply")
		default:
			return nil, mcpDiscoveryOut{}, fmt.Errorf("unknown action %q: expected start, status, cancel or apply", action)
		}
	})
}

func (api *API) mcpDiscoveryStart(in mcpDiscoveryIn) (*mcp.CallToolResult, mcpDiscoveryOut, error) {
	cfg := api.getCfg()
	if !cfg.System.WebServer.MCP.AllowActiveProbes {
		return nil, mcpDiscoveryOut{}, fmt.Errorf(
			"active probes are disabled: a discovery run is the heaviest traffic b4 can generate, so it needs 'Allow active probes' under Settings -> Integrations -> MCP server")
	}
	if api.discoveryRT == nil {
		return nil, mcpDiscoveryOut{}, fmt.Errorf("the discovery runtime is not configured in this process")
	}

	var urls []string
	seen := map[string]bool{}
	for _, raw := range strings.Split(in.Domains, ",") {
		host := sni.NormalizeDomain(raw)
		if host == "" || seen[host] {
			continue
		}
		seen[host] = true
		urls = append(urls, host)
	}
	if len(urls) == 0 {
		return nil, mcpDiscoveryOut{}, fmt.Errorf("domains is required for action=start")
	}
	if len(urls) > mcpDiscoveryMaxDomains {
		return nil, mcpDiscoveryOut{}, fmt.Errorf(
			"at most %d domains per run; each one multiplies the traffic and the time", mcpDiscoveryMaxDomains)
	}
	for _, host := range urls {
		if watchdog.IsReservedHost(host) {
			return nil, mcpDiscoveryOut{}, fmt.Errorf(
				"%s is a private or local address: discovery fires hundreds of fetches from the router, and aiming them at the network b4 runs on tells you nothing about censorship", host)
		}
	}

	opts := discovery.StartSuiteOptions{ValidationTries: 1, Source: discovery.SourceMCP}
	if strings.EqualFold(strings.TrimSpace(in.SkipDNS), "true") {
		opts.SkipDNS = true
	}

	suite, err := api.discoveryRT.StartSuite(cfg, urls, opts)
	if err != nil {
		if errors.Is(err, discovery.ErrDiscoveryAlreadyRunning) {
			return nil, mcpDiscoveryOut{}, fmt.Errorf(
				"a discovery run is already in progress - only one can run at a time, and the watchdog's self-healing shares the same runtime. Poll it with action=status, or stop it with action=cancel")
		}
		return nil, mcpDiscoveryOut{}, fmt.Errorf("could not start discovery: %w", err)
	}

	mcpRememberSuite(suite.Id)
	log.Infof("mcp: discovery started for %v (suite %s)", urls, suite.Id)
	out := mcpDiscoveryOut{
		Id:     suite.Id,
		Status: string(suite.Status),
		Note: fmt.Sprintf(
			"started for %s. This runs for minutes, not seconds: it opens with %d strategies per domain and then explores the family that looked best, "+
				"which can be another hundred or more, each preceded by a config-propagation pause. It stops early for a domain that turns out to work without b4. "+
				"Do NOT poll in a loop - tell the user it is running and call action=status once when they ask. "+
				"While it runs, the watchdog cannot heal and a firewall refresh will block.",
			strings.Join(urls, ", "), len(discovery.GetPhase1Presets())),
	}
	return nil, out, nil
}

func (api *API) mcpDiscoveryStatus(in mcpDiscoveryIn) (*mcp.CallToolResult, mcpDiscoveryOut, error) {
	id := mcpResolveSuiteID(in.Id)
	if id != "" {
		if suite, ok := discovery.GetCheckSuite(id); ok && suite != nil {
			return api.mcpDiscoverySuiteStatus(suite)
		}
	}

	hist := discovery.GetHistory(api.getCfg().ConfigPath)
	if hist == nil {
		return nil, mcpDiscoveryOut{}, fmt.Errorf("no run with id %q is in memory and no history is available", id)
	}
	entries := hist.Entries
	if id != "" {
		if matched := mcpHistoryForSuite(hist, id); len(matched) > 0 {
			entries = matched
		}
	}

	out := mcpDiscoveryOut{Id: id, Source: "history", Status: "complete"}
	applicable := 0
	for _, e := range entries {
		row := mcpDiscoveryDomain{
			Domain:        e.Domain,
			BestPreset:    e.BestPreset,
			Family:        string(e.BestFamily),
			BestKBPerSec:  e.BestSpeed / 1024,
			Found:         e.BestSuccess && !e.BaselineWorks,
			BaselineWorks: e.BaselineWorks,
			Confirmed:     e.Confirmed,
		}
		mcpApplyOutcome(&row, e.Outcome)
		row.Verdict = mcpDiscoveryVerdict(row, false)
		if row.Found && e.ApplicableSet() != nil {
			applicable++
		}
		out.Domains = append(out.Domains, row)
	}
	sort.Slice(out.Domains, func(i, j int) bool { return out.Domains[i].Domain < out.Domains[j].Domain })
	if len(out.Domains) > mcpDiscoveryMaxGroups {
		out.Domains = out.Domains[:mcpDiscoveryMaxGroups]
	}
	out.Note = "this is the saved history rather than a run still in memory: one entry per domain, the newest result kept, and it survives a restart."
	switch {
	case len(out.Domains) == 0:
		out.Note = "no run is in progress and nothing has been discovered yet"
	case applicable > 0:
		out.Note += fmt.Sprintf(" %d of them still hold a strategy you can install with action=apply and the domain; there is no time limit on that.", applicable)
	default:
		out.Note += " None of them holds a strategy worth installing, so run discovery again for the domain you care about."
	}
	return nil, out, nil
}

func mcpHistoryForSuite(hist *discovery.DiscoveryHistory, id string) []discovery.HistoryEntry {
	var out []discovery.HistoryEntry
	for _, e := range hist.Entries {
		if e.SuiteId == id {
			out = append(out, e)
		}
	}
	return out
}

func mcpHistoryEntryFor(hist *discovery.DiscoveryHistory, domain string) (discovery.HistoryEntry, bool) {
	for _, e := range hist.Entries {
		if sni.NormalizeDomain(e.Domain) == domain {
			return e, true
		}
	}
	return discovery.HistoryEntry{}, false
}

func (api *API) mcpDiscoverySuiteStatus(suite *discovery.CheckSuite) (*mcp.CallToolResult, mcpDiscoveryOut, error) {
	snap, err := mcpSuiteProjection(suite)
	if err != nil {
		return nil, mcpDiscoveryOut{}, err
	}
	running := false
	switch discovery.CheckStatus(snap.Status) {
	case discovery.CheckStatusRunning, discovery.CheckStatusPending:
		running = true
	}
	out := mcpDiscoveryOut{
		Id:      snap.Id,
		Status:  snap.Status,
		Source:  "run",
		Domains: api.mcpDiscoverySuiteRows(snap, running),
	}
	switch discovery.CheckStatus(snap.Status) {
	case discovery.CheckStatusRunning, discovery.CheckStatusPending:
		out.Phase = snap.CurrentPhase
	}
	if snap.TotalChecks > 0 {
		out.Progress = fmt.Sprintf("%d/%d checks", snap.CompletedChecks, snap.TotalChecks)
	}

	switch discovery.CheckStatus(snap.Status) {
	case discovery.CheckStatusRunning, discovery.CheckStatusPending:
		out.Note = fmt.Sprintf("still running (%s, %s). Any strategy listed is the BEST SO FAR, not a result: it is unconfirmed, a better one may still win, and action=apply is refused until the run ends. "+
			"If the user wants to stop and keep what has been found, action=cancel ends the run and saves it. Do not poll in a loop: check again only when the user asks.",
			out.Phase, out.Progress)
		if snap.CurrentDomain != "" {
			out.Note = fmt.Sprintf("still running on %s (%s, %s). Do not poll in a loop: check again only when the user asks.",
				snap.CurrentDomain, out.Phase, out.Progress)
		}
	case discovery.CheckStatusCanceled:
		out.Note = "the run was cancelled; any results below are partial"
	default:
		applicable := 0
		for _, d := range out.Domains {
			if d.Found {
				applicable++
			}
		}
		if api.discoveryRT != nil && api.discoveryRT.IsActive() {
			out.Status = "finishing"
			out.Note = fmt.Sprintf("the search is done (%d of %d domain(s) have a strategy) but the run is still tearing down its firewall rules. "+
				"apply is refused until that finishes - call status once more in a few seconds.",
				applicable, len(out.Domains))
			break
		}
		out.Note = fmt.Sprintf("finished. %d of %d domain(s) have a strategy worth applying, with action=apply and the domain. "+
			"The result is saved, so there is no hurry: this suite_id and the domain still work later, and after a restart.",
			applicable, len(out.Domains))
	}
	return nil, out, nil
}

func (api *API) mcpDiscoveryCancel(in mcpDiscoveryIn) (*mcp.CallToolResult, mcpDiscoveryOut, error) {
	id := mcpResolveSuiteID(in.Id)
	if id == "" {
		return nil, mcpDiscoveryOut{}, fmt.Errorf("no discovery run is in progress")
	}
	if err := discovery.CancelCheckSuite(id); err != nil {
		return nil, mcpDiscoveryOut{}, fmt.Errorf("could not cancel %q: %w", id, err)
	}
	log.Infof("mcp: discovery run %s cancelled", id)
	return nil, mcpDiscoveryOut{
		Id:     id,
		Status: "canceled",
		Note: "cancellation requested. A run that had already finished is left alone, and the firewall rules it installed " +
			"are torn down once the run goroutine stops, which is not instant.",
	}, nil
}

func (api *API) mcpDiscoveryApply(in mcpDiscoveryIn) (*mcp.CallToolResult, mcpDiscoveryOut, error) {
	if !api.getCfg().System.WebServer.MCP.AllowWrites {
		return nil, mcpDiscoveryOut{}, fmt.Errorf(
			"configuration writes are disabled: turn on 'Allow configuration changes' under Settings -> Integrations -> MCP server to permit them")
	}
	domain := sni.NormalizeDomain(in.Domain)
	if domain == "" {
		return nil, mcpDiscoveryOut{}, fmt.Errorf("domain is required for action=apply")
	}
	if api.discoveryRT != nil && api.discoveryRT.IsActive() {
		if cur, ok := discovery.GetCurrentSuite(); ok && cur != nil {
			return nil, mcpDiscoveryOut{}, fmt.Errorf(
				"a discovery run is still going, so there is nothing settled to apply: whatever it has found is the best so far, unconfirmed, and may still be beaten. " +
					"Call action=status to see how far it has got, or action=cancel to stop it and keep what it has found, which can then be applied")
		}
		return nil, mcpDiscoveryOut{}, fmt.Errorf(
			"a discovery run is still tearing down its firewall rules: applying now would block on a firewall refresh for up to five minutes. Call action=status until it reports finished, then apply")
	}

	id := mcpResolveSuiteID(in.Id)
	var chosen *config.SetConfig
	var preset string
	var groupDomains []string
	suiteID := id
	source := "run"
	unconfirmed := false

	if suite, ok := discovery.GetCheckSuite(id); ok && suite != nil {
		snap, err := mcpSuiteProjection(suite)
		if err != nil {
			return nil, mcpDiscoveryOut{}, err
		}
		status := discovery.CheckStatus(strings.ToLower(snap.Status))
		switch status {
		case discovery.CheckStatusComplete:
		case discovery.CheckStatusCanceled:
			unconfirmed = true
		default:
			return nil, mcpDiscoveryOut{}, fmt.Errorf(
				"that run is %s, so it holds nothing settled to apply. Call action=status, and action=cancel if you want to stop it and keep what it has found", status)
		}
		suiteID = snap.Id
		for _, g := range snap.StrategyGroups {
			for _, d := range g.Domains {
				if sni.NormalizeDomain(d) == domain && g.Set != nil {
					chosen, preset, groupDomains = g.Set, g.WinnerPreset, g.Domains
				}
			}
		}
		if chosen == nil {
			if r, ok := snap.DomainResults[domain]; ok {
				if r.BaselineWorks {
					return nil, mcpDiscoveryOut{}, fmt.Errorf(
						"%s works without b4, so the run deliberately produced no strategy for it. Creating a set would be wrong", domain)
				}
				if !r.BestSuccess {
					return nil, mcpDiscoveryOut{}, fmt.Errorf(
						"the run found no working strategy for %s, so there is nothing to apply", domain)
				}
			}
			return nil, mcpDiscoveryOut{}, fmt.Errorf("that run holds no applicable strategy for %s", domain)
		}
	} else {
		source = "history"
		hist := discovery.GetHistory(api.getCfg().ConfigPath)
		if hist == nil {
			return nil, mcpDiscoveryOut{}, fmt.Errorf("no run is in memory and no saved history is available for %s", domain)
		}
		entry, found := mcpHistoryEntryFor(hist, domain)
		if !found {
			return nil, mcpDiscoveryOut{}, fmt.Errorf(
				"no run in memory and nothing saved for %s. Call action=status to see which domains have a saved result", domain)
		}
		if entry.BaselineWorks {
			return nil, mcpDiscoveryOut{}, fmt.Errorf(
				"%s works without b4, so the run deliberately produced no strategy for it. Creating a set would be wrong", domain)
		}
		if !entry.BestSuccess {
			return nil, mcpDiscoveryOut{}, fmt.Errorf(
				"the saved run found no working strategy for %s, so there is nothing to apply. Run discovery for it again", domain)
		}
		unconfirmed = entry.Status == discovery.CheckStatusCanceled || entry.Confirmed == 0
		chosen = entry.ApplicableSet()
		if chosen == nil {
			return nil, mcpDiscoveryOut{}, fmt.Errorf(
				"the saved result for %s predates b4 recording the set it built, so only the summary survives. Run discovery for it again", domain)
		}
		preset = entry.BestPreset
		groupDomains = chosen.Targets.SNIDomains
		if len(groupDomains) == 0 {
			groupDomains = []string{domain}
		}
		if entry.SuiteId != "" {
			suiteID = entry.SuiteId
		}
	}

	if prior := mcpGroupAppliedAs(suiteID, preset); prior != "" {
		if existing := findSetIn(api.getCfg(), prior); existing != nil {
			out := mcpDiscoveryOut{Id: suiteID, Source: source}
			for i, s := range api.getCfg().Sets {
				if s.Id == prior {
					out.Applied = &mcpSetRow{Position: i + 1, Id: s.Id, Name: s.Name, Enabled: s.Enabled}
					break
				}
			}
			out.Note = fmt.Sprintf(
				"%s is already covered by set %q. The run grouped %s under one winning strategy, so a single apply created a set for all of them: applying again per domain would only duplicate it",
				domain, existing.Name, mcpSummarizeList(groupDomains))
			return nil, out, nil
		}
	}

	mcpWriteMu.Lock()
	defer mcpWriteMu.Unlock()

	oldCfg := api.getCfg()
	newCfg := oldCfg.Clone()

	if len(newCfg.Sets) >= mcpMaxSets {
		return nil, mcpDiscoveryOut{}, fmt.Errorf("there are already %d sets; b4 will not add more from here", len(newCfg.Sets))
	}

	copied, err := redactedSetForMCP(chosen)
	if err != nil {
		return nil, mcpDiscoveryOut{}, err
	}
	set := *copied
	set.Id = uuid.New().String()
	set.Enabled = true
	if name := strings.TrimSpace(in.Name); name != "" {
		if mcpNameTaken(newCfg, name) {
			return nil, mcpDiscoveryOut{}, fmt.Errorf(
				"a set named %q already exists; pass a different name. Two sets sharing a name make every later reference to it ambiguous", name)
		}
		set.Name = name
	} else {
		if set.Name == "" {
			set.Name = domain
		}
		base := set.Name
		for i := 2; mcpNameTaken(newCfg, set.Name); i++ {
			set.Name = fmt.Sprintf("%s %d", base, i)
		}
	}
	set.Routing.Upstream.Username = ""
	set.Routing.Upstream.Password = ""
	api.initializeSetDefaults(&set)

	moved := api.releaseDomainsFromOtherSets(newCfg.Sets, set.Id, set.Targets.SNIDomains)
	newCfg.Sets = append([]*config.SetConfig{&set}, newCfg.Sets...)
	api.loadTargetsForSetCached(&set)

	if err := mcpValidateCandidate(oldCfg, newCfg); err != nil {
		return nil, mcpDiscoveryOut{}, fmt.Errorf("rejected: %w", err)
	}
	snapshot := oldCfg.Clone()
	if err := api.saveAndPushConfig(newCfg); err != nil {
		return nil, mcpDiscoveryOut{}, fmt.Errorf("rejected: %w", err)
	}
	api.applyRuntimeChanges(newCfg, oldCfg)
	api.PerformSoftRestart(newCfg, oldCfg)

	live := api.getCfg()
	out := mcpDiscoveryOut{Id: suiteID, Source: source, Changed: true, MovedFrom: moved}
	for i, s := range live.Sets {
		if s.Id == set.Id {
			out.Applied = &mcpSetRow{Position: i + 1, Id: s.Id, Name: s.Name, Enabled: s.Enabled}
			break
		}
	}
	if out.Applied == nil {
		return nil, mcpDiscoveryOut{}, fmt.Errorf("set %q disappeared while saving", set.Name)
	}

	mcpRecordChange(mcpChange{
		Path:     "sets",
		Previous: fmt.Sprintf("%d sets", len(oldCfg.Sets)),
		Current:  fmt.Sprintf("%d sets", len(live.Sets)),
		When:     time.Now(), Snapshot: snapshot,
	})
	mcpMarkGroupApplied(suiteID, preset, set.Id)
	log.Infof("mcp: applied discovery strategy %q for %s as set %q", preset, domain, set.Name)

	out.Note = fmt.Sprintf("created set %q from the %s strategy, at position %d of %d, so it matches before the sets already there. Undo with b4_revert_last_change",
		set.Name, preset, out.Applied.Position, len(live.Sets))
	if source == "history" {
		out.Note += ". This came from the saved history rather than a run still in memory, so it carries the strategy that won but not any geo categories or addresses a live apply would have added"
	}
	if unconfirmed {
		out.Note += ". The run it came from was stopped before it finished, so this strategy never went through the confirmation pass: it worked at least once, which is not the same as reliably. Check it with b4_test_domain_now and re-run discovery if it disappoints"
	}
	if len(groupDomains) > 1 {
		out.Note += fmt.Sprintf(". That one strategy won for %s, so the set targets all of them and there is nothing left to apply for the others",
			mcpSummarizeList(groupDomains))
	}
	if len(moved) > 0 {
		names := make([]string, 0, len(moved))
		for _, m := range moved {
			names = append(names, fmt.Sprintf("%s from %q", m.Domain, m.SetName))
		}
		out.Note += ". A domain belongs to one enabled set, so b4 took " + strings.Join(names, ", ")
	}
	out.Note += ". Confirm it with b4_test_domain_now before telling the user it is fixed"
	return nil, out, nil
}
