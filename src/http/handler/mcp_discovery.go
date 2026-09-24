package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sni"
	"github.com/daniellavrushin/b4/utils"
	"github.com/daniellavrushin/b4/watchdog"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	mcpDiscoveryMaxDomains = 5
	mcpDiscoveryMaxGroups  = 20
)

var (
	mcpLastSuiteMu sync.Mutex
	mcpLastSuiteID string
)

func mcpRememberSuite(id string) {
	mcpLastSuiteMu.Lock()
	mcpLastSuiteID = id
	mcpLastSuiteMu.Unlock()
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
	Domains string `json:"domains,omitempty" jsonschema:"Comma-separated domains to find a strategy for, at most 5. For action=start. With set it may be left empty to probe the set's stored discovery URLs."`
	Id      string `json:"id,omitempty" jsonschema:"Suite id from start. If omitted, the run in progress is used, then the last run this server started."`
	Domain  string `json:"domain,omitempty" jsonschema:"For action=apply without set: which domain's winning strategy to turn into a new set."`
	Name    string `json:"name,omitempty" jsonschema:"Optional name for the set created by apply without set."`
	SkipDNS string `json:"skip_dns,omitempty" jsonschema:"'true' to skip the DNS poisoning probe on start."`
	Set     string `json:"set,omitempty" jsonschema:"Optional existing set, by id or exact name, the run is for. On start: probes domains, or the set's stored discovery URLs when domains is empty (at most 5), tests the set's current strategy first, and stops once one strategy passes every confirmation try on every address. On apply: writes the result into this set instead of creating one, strategy only with its domains untouched, and only when the run's set verdict is covered. Sets with routing enabled are refused."`
}

type mcpSuiteSnapshot struct {
	Id              string                  `json:"id"`
	Status          string                  `json:"status"`
	CurrentPhase    string                  `json:"current_phase"`
	CurrentDomain   string                  `json:"current_domain"`
	TotalChecks     int                     `json:"total_checks"`
	CompletedChecks int                     `json:"completed_checks"`
	Domains         []discovery.DomainInput `json:"domains"`
	DomainResults   map[string]struct {
		Domain        string  `json:"domain"`
		Url           string  `json:"url"`
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
	SetId          string                `json:"set_id"`
	SetVerdict     *discovery.SetVerdict `json:"set_verdict"`
	StoppedCovered bool                  `json:"stopped_covered"`
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
	Gateway       bool    `json:"gateway_intercepted,omitempty"`
	Confirmed     int     `json:"confirmed,omitempty"`
	Provisional   bool    `json:"provisional,omitempty"`
	Unconfirmed   bool    `json:"unconfirmed,omitempty"`
	Verdict       string  `json:"verdict"`
}

type mcpDiscoveryOut struct {
	Id         string               `json:"suite_id,omitempty"`
	Status     string               `json:"status,omitempty"`
	Phase      string               `json:"phase,omitempty"`
	Progress   string               `json:"progress,omitempty"`
	Source     string               `json:"source,omitempty"`
	Domains    []mcpDiscoveryDomain `json:"domains,omitempty"`
	SetVerdict *mcpSetVerdictOut    `json:"set_verdict,omitempty"`
	Applied    *mcpSetRow           `json:"applied_set,omitempty"`
	MovedFrom  []DomainReassignment `json:"moved_from_other_sets,omitempty"`
	Changed    bool                 `json:"changed,omitempty"`
	Note       string               `json:"note"`
}

type mcpSetVerdictOut struct {
	SetId          string   `json:"set_id"`
	SetName        string   `json:"set_name,omitempty"`
	Status         string   `json:"status"`
	WinnerPreset   string   `json:"winner_preset,omitempty"`
	Family         string   `json:"family,omitempty"`
	Covered        []string `json:"covered,omitempty"`
	Uncovered      []string `json:"uncovered,omitempty"`
	NoBypass       []string `json:"works_without_b4,omitempty"`
	Confirmed      bool     `json:"confirmed,omitempty"`
	StoppedCovered bool     `json:"stopped_when_covered,omitempty"`
	Meaning        string   `json:"meaning"`
}

func mcpDiscoveryVerdict(d mcpDiscoveryDomain, running bool) string {
	switch {
	case d.BaselineWorks:
		return "works without b4 - do not create a set for it"
	case d.Gateway:
		return "TCP to every known address is answered by the first hop in front of this host (a transparent proxy on the gateway), so packets from this host never reach the ISP; run b4 on that gateway or exclude this host from its redirect; if this host is the router itself, the ISP does this at its edge and only a proxy route helps"
	case d.Blocked:
		return "the address itself is unreachable, so no packet strategy can help; only a proxy or VPN route would"
	case d.Found && running:
		return fmt.Sprintf("%s is the best so far, but the run is still testing and this is PROVISIONAL: it has not been confirmed, a better one may still win, and nothing can be applied until the run finishes or is cancelled", d.BestPreset)
	case d.Found && d.Unconfirmed:
		return fmt.Sprintf("%s loaded the site at least once, but the run ended before the confirmation pass re-checked it, so it is UNCONFIRMED: apply it if the user accepts that, and verify with b4_test_domain_now", d.BestPreset)
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
		row.Unconfirmed = row.Found && r.Unconfirmed && !running
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
		row.Found, row.BaselineWorks, row.Blocked = false, true, false
	case discovery.OutcomeAddressBlocked:
		row.Found, row.BaselineWorks, row.Blocked = false, false, true
	case discovery.OutcomeGatewayIntercepted:
		row.Found, row.BaselineWorks, row.Blocked, row.Gateway = false, false, false, true
	case discovery.OutcomeNotFound:
		row.Found, row.BaselineWorks, row.Blocked = false, false, false
	}
}

func (api *API) addMCPDiscoveryTools(srv *mcp.Server) {
	addTool(srv, &mcp.Tool{
		Name:  "b4_find_bypass_strategy",
		Title: "Search for a working bypass strategy",
		Description: "Brute-forces b4's strategies against a domain until one makes it load, then turns the winner into a set. " +
			"action=start begins a run and returns immediately; status polls it; cancel stops it; apply creates the set. " +
			"With set (an existing set's id or exact name) the run is for that set: it probes the given domains or the set's stored discovery URLs, tests the set's current strategy first, stops once one strategy passes every confirmation try on every address, and ends with a set verdict " +
			"(covered, current_works, partial, not_needed, none or incomplete); apply then writes the strategy into that set, leaving its domains alone, and only for the verdict covered. " +
			"A run takes MINUTES, emits heavy traffic from the router, holds firewall rules, and only one can be in flight - do not poll in a loop, tell the user it is running and check once when they ask. " +
			"A finished run is written to b4's discovery history, so status and apply keep working for it afterwards, across restarts, addressed by suite_id, by set or just by domain.",
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
	var target *config.SetConfig
	if ref := strings.TrimSpace(in.Set); ref != "" {
		var err error
		if target, err = mcpDiscoverySet(cfg, ref); err != nil {
			return nil, mcpDiscoveryOut{}, err
		}
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
	if len(urls) == 0 && target != nil {
		urls = slices.Clone(target.Discovery.URLs)
	}
	if len(urls) == 0 {
		if target != nil {
			return nil, mcpDiscoveryOut{}, fmt.Errorf(
				"set %q stores no discovery URLs, so pass the domains to probe for it", target.Name)
		}
		return nil, mcpDiscoveryOut{}, fmt.Errorf("domains is required for action=start")
	}
	if len(urls) > mcpDiscoveryMaxDomains {
		return nil, mcpDiscoveryOut{}, fmt.Errorf(
			"at most %d domains per run; each one multiplies the traffic and the time", mcpDiscoveryMaxDomains)
	}
	for _, raw := range urls {
		if host := probeInputHost(raw); watchdog.IsReservedHost(host) {
			return nil, mcpDiscoveryOut{}, fmt.Errorf(
				"%s is a private or local address: discovery fires hundreds of fetches from the router, and aiming them at the network b4 runs on tells you nothing about censorship", host)
		}
	}

	opts := discovery.StartSuiteOptions{ValidationTries: 1, Source: discovery.SourceMCP}
	if strings.EqualFold(strings.TrimSpace(in.SkipDNS), "true") {
		opts.SkipDNS = true
	}
	if target != nil {
		if urls = utils.SanitizeProbeURLs(urls, nil); len(urls) == 0 {
			return nil, mcpDiscoveryOut{}, fmt.Errorf("none of the addresses for set %q can be probed: only public http and https addresses work", target.Name)
		}
		opts.SetId = target.Id
		opts.SetStrategy = discovery.SetRunStrategy(target)
		opts.StopWhenCovered = true
		opts.TLSVersion, opts.IPVersion = discovery.SetRunVersions(target, "", "")
	}
	if api.discoveryRT == nil {
		return nil, mcpDiscoveryOut{}, fmt.Errorf("the discovery runtime is not configured in this process")
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
	if target != nil {
		out.Note = fmt.Sprintf(
			"started for set %q with %s. The set's current strategy is tested first; the search stops as soon as one strategy passes every confirmation try on every address, "+
				"otherwise it runs for minutes like any discovery. It ends with a set verdict that action=status reports and action=apply with set=%q acts on. "+
				"Do NOT poll in a loop - tell the user it is running and call action=status once when they ask. "+
				"While it runs, the watchdog cannot heal and a firewall refresh will block.",
			target.Name, strings.Join(urls, ", "), target.Name)
	}
	return nil, out, nil
}

func mcpDiscoverySet(cfg *config.Config, ref string) (*config.SetConfig, error) {
	set, err := mcpFindDiscoverySet(cfg, ref)
	if err != nil {
		return nil, err
	}
	return mcpDiscoverableSet(set)
}

func mcpFindDiscoverySet(cfg *config.Config, ref string) (*config.SetConfig, error) {
	for _, s := range cfg.Sets {
		if s != nil && strings.EqualFold(s.Id, ref) {
			return s, nil
		}
	}
	var exact, folded []*config.SetConfig
	for _, s := range cfg.Sets {
		switch {
		case s == nil:
		case s.Name == ref:
			exact = append(exact, s)
		case strings.EqualFold(s.Name, ref):
			folded = append(folded, s)
		}
	}
	matches := exact
	if len(matches) == 0 {
		matches = folded
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no set with id or name %q; b4_list_sets shows them", ref)
	case 1:
		return matches[0], nil
	default:
		return nil, fmt.Errorf("%d sets are named %q; pass the set id instead", len(matches), ref)
	}
}

func mcpDiscoverableSet(set *config.SetConfig) (*config.SetConfig, error) {
	if set.Routing.Enabled {
		return nil, fmt.Errorf(
			"set %q has routing enabled: its traffic takes the routed path, not the direct one a discovery run tests strategies on, so a run for it is refused", set.Name)
	}
	return set, nil
}

func (api *API) mcpDiscoveryStatus(in mcpDiscoveryIn) (*mcp.CallToolResult, mcpDiscoveryOut, error) {
	cfg := api.getCfg()
	var target *config.SetConfig
	if ref := strings.TrimSpace(in.Set); ref != "" {
		var err error
		if target, err = mcpFindDiscoverySet(cfg, ref); err != nil {
			return nil, mcpDiscoveryOut{}, err
		}
	}

	id := mcpResolveSuiteID(in.Id)
	if id != "" {
		if suite, ok := discovery.GetCheckSuite(id); ok && suite != nil {
			snap, err := mcpSuiteProjection(suite)
			if err != nil {
				return nil, mcpDiscoveryOut{}, err
			}
			if target == nil || snap.SetId == target.Id {
				return api.mcpDiscoverySuiteStatus(snap)
			}
		}
	}

	hist := discovery.GetHistory(cfg.ConfigPath)
	if hist == nil {
		return nil, mcpDiscoveryOut{}, fmt.Errorf("no run with id %q is in memory and no history is available", id)
	}
	var record *discovery.SetRunRecord
	if target != nil {
		rec, ok := hist.SetRuns[target.Id]
		if !ok {
			return nil, mcpDiscoveryOut{Source: "history", Note: fmt.Sprintf(
				"no discovery run for set %q is in memory or saved; start one with action=start and set=%q", target.Name, target.Name)}, nil
		}
		record = &rec
		if strings.TrimSpace(in.Id) == "" {
			id = rec.SuiteId
		}
	} else if rec, ok := hist.SetRunForSuite(id); ok {
		record = &rec
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
		mcpApplyOutcome(&row, e.EffectiveOutcome())
		row.Unconfirmed = row.Found && (e.Unconfirmed || e.Status == discovery.CheckStatusCanceled)
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
	case record != nil:
		out.SetVerdict = mcpSetVerdictView(cfg, record.SetId, &record.Verdict, false)
		out.Note += " This run was for a set: " + out.SetVerdict.Meaning
	case len(out.Domains) == 0:
		out.Note = "no run is in progress and nothing has been discovered yet"
	case applicable > 0:
		out.Note += fmt.Sprintf(" %d of them still hold a strategy you can install with action=apply and the domain; there is no time limit on that.", applicable)
	default:
		out.Note += " None of them holds a strategy worth installing, so run discovery again for the domain you care about."
	}
	return nil, out, nil
}

func mcpSetVerdictView(cfg *config.Config, setID string, v *discovery.SetVerdict, stoppedCovered bool) *mcpSetVerdictOut {
	out := &mcpSetVerdictOut{SetId: setID, StoppedCovered: stoppedCovered}
	name := setID
	if set := cfg.GetSetById(setID); set != nil {
		out.SetName = set.Name
		name = set.Name
	}
	if v == nil {
		out.Status = "pending"
		out.Meaning = "the run is still going; the set verdict comes when it ends"
		return out
	}
	out.Status = string(v.Status)
	out.WinnerPreset = v.WinnerPreset
	out.Family = string(v.Family)
	out.Covered = v.Covered
	out.Uncovered = v.Uncovered
	out.NoBypass = v.NoBypass
	out.Confirmed = v.Confirmed
	out.Meaning = mcpSetVerdictMeaning(name, v)
	return out
}

func mcpSetVerdictMeaning(name string, v *discovery.SetVerdict) string {
	switch v.Status {
	case discovery.SetVerdictCovered:
		return fmt.Sprintf("'%s' passed every confirmation try on every address of set %q in one configuration; action=apply with set writes it into the set, leaving its domains alone", v.WinnerPreset, name)
	case discovery.SetVerdictCurrentWorks:
		return fmt.Sprintf("the current strategy of set %q passed every confirmation try on every address; there is nothing to write", name)
	case discovery.SetVerdictPartial:
		return fmt.Sprintf("no single strategy works for every address of set %q: '%s' covers %s, nothing found covers %s. apply is refused; those addresses need a set of their own",
			name, v.WinnerPreset, mcpListOrNone(v.Covered), mcpListOrNone(v.Uncovered))
	case discovery.SetVerdictNotNeeded:
		return fmt.Sprintf("every address of set %q loads without b4; it needs no strategy for them", name)
	case discovery.SetVerdictNone:
		return fmt.Sprintf("no strategy tried made %s load; a packet strategy does not help there, a proxy route might", mcpListOrNone(v.Uncovered))
	case discovery.SetVerdictIncomplete:
		return fmt.Sprintf("the run for set %q was cancelled before it reached a verdict; start it again", name)
	default:
		return fmt.Sprintf("unknown set verdict %q", v.Status)
	}
}

func mcpListOrNone(list []string) string {
	if len(list) == 0 {
		return "none"
	}
	return mcpSummarizeList(list)
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

func mcpRememberCheckURL(checkURLs map[string]string, domain, checkURL string) {
	key := sni.NormalizeDomain(domain)
	if key == "" || checkURL == "" || checkURLs[key] != "" {
		return
	}
	checkURLs[key] = checkURL
}

func mcpProbeURLsFor(domain string, groupDomains []string, checkURLs map[string]string) []string {
	raw := make([]string, 0, len(groupDomains)+1)
	for _, d := range append([]string{domain}, groupDomains...) {
		if u := checkURLs[sni.NormalizeDomain(d)]; u != "" {
			raw = append(raw, u)
		}
	}
	if len(raw) == 0 {
		raw = append(raw, "https://"+domain+"/")
	}
	return utils.SanitizeProbeURLs(raw, nil)
}

func mcpHistoryEntryFor(hist *discovery.DiscoveryHistory, domain string) (discovery.HistoryEntry, bool) {
	for _, e := range hist.Entries {
		if sni.NormalizeDomain(e.Domain) == domain {
			return e, true
		}
	}
	return discovery.HistoryEntry{}, false
}

func (api *API) mcpDiscoverySuiteStatus(snap *mcpSuiteSnapshot) (*mcp.CallToolResult, mcpDiscoveryOut, error) {
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
	if snap.SetId != "" {
		out.SetVerdict = mcpSetVerdictView(api.getCfg(), snap.SetId, snap.SetVerdict, snap.StoppedCovered)
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
		if out.SetVerdict != nil {
			out.Note = "finished. This run was for a set: " + out.SetVerdict.Meaning +
				". The result is saved, so there is no hurry: this suite_id and the set still work later, and after a restart."
		}
	}
	if discovery.CheckStatus(snap.Status) == discovery.CheckStatusCanceled && out.SetVerdict != nil {
		out.Note += ". " + out.SetVerdict.Meaning
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
	var target *config.SetConfig
	if ref := strings.TrimSpace(in.Set); ref != "" {
		var err error
		if target, err = mcpDiscoverySet(api.getCfg(), ref); err != nil {
			return nil, mcpDiscoveryOut{}, err
		}
	}
	domain := sni.NormalizeDomain(in.Domain)
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
	run, found, err := api.mcpSetRunFor(id, target)
	if err != nil {
		return nil, mcpDiscoveryOut{}, err
	}
	if target != nil || found {
		return api.mcpApplySetRun(target, run, found)
	}
	if domain == "" {
		return nil, mcpDiscoveryOut{}, fmt.Errorf("domain is required for action=apply, or set to write a set run's result into that set")
	}

	var chosen *config.SetConfig
	var preset string
	var groupDomains []string
	suiteID := id
	source := "run"
	unconfirmed := false
	checkURLs := map[string]string{}

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
		for _, di := range snap.Domains {
			mcpRememberCheckURL(checkURLs, di.Domain, di.CheckURL)
		}
		for key, r := range snap.DomainResults {
			mcpRememberCheckURL(checkURLs, key, r.Url)
		}
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
		for _, e := range hist.Entries {
			mcpRememberCheckURL(checkURLs, e.Domain, e.Url)
		}
	}

	if existing := api.setCoveringDomainWith(domain, chosen); existing != nil {
		out := mcpDiscoveryOut{Id: suiteID, Source: source}
		for i, s := range api.getCfg().Sets {
			if s.Id == existing.Id {
				out.Applied = &mcpSetRow{Position: i + 1, Id: s.Id, Name: s.Name, Enabled: s.Enabled}
				break
			}
		}
		out.Note = fmt.Sprintf(
			"%s is already covered by set %q, which carries the same strategy. The run grouped %s under one winner, so a single apply created a set for all of them: applying again per domain would only duplicate it",
			domain, existing.Name, mcpSummarizeList(groupDomains))
		return nil, out, nil
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
	set.Discovery.URLs = mcpProbeURLsFor(domain, groupDomains, checkURLs)
	api.initializeSetDefaults(&set)

	moved := api.releaseDomainsFromOtherSets(newCfg.Sets, set.Id, set.Targets.SNIDomains)
	newCfg.Sets = append([]*config.SetConfig{&set}, newCfg.Sets...)
	api.loadTargetsForSetCached(&set)

	if err := mcpValidateCandidate(oldCfg, newCfg); err != nil {
		return nil, mcpDiscoveryOut{}, fmt.Errorf("rejected: %w", err)
	}
	snapshot := oldCfg.Clone()
	if err := api.mcpSave(oldCfg, newCfg); err != nil {
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
	}, oldCfg, newCfg)
	if err := discovery.MarkAppliedInHistory(api.getCfg().ConfigPath, groupDomains, preset, set.Id); err != nil {
		log.Errorf("Failed to record applied strategy in discovery history: %v", err)
	}
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

type mcpSetRun struct {
	setID          string
	suiteID        string
	status         discovery.CheckStatus
	verdict        *discovery.SetVerdict
	stoppedCovered bool
	source         string
}

func (api *API) mcpSetRunFor(id string, target *config.SetConfig) (mcpSetRun, bool, error) {
	if id != "" {
		if suite, ok := discovery.GetCheckSuite(id); ok && suite != nil {
			snap, err := mcpSuiteProjection(suite)
			if err != nil {
				return mcpSetRun{}, false, err
			}
			if snap.SetId != "" && (target == nil || snap.SetId == target.Id) {
				return mcpSetRun{
					setID:          snap.SetId,
					suiteID:        snap.Id,
					status:         discovery.CheckStatus(strings.ToLower(snap.Status)),
					verdict:        snap.SetVerdict,
					stoppedCovered: snap.StoppedCovered,
					source:         "run",
				}, true, nil
			}
		}
	}

	hist := discovery.GetHistory(api.getCfg().ConfigPath)
	var rec discovery.SetRunRecord
	var ok bool
	if target != nil {
		rec, ok = hist.SetRuns[target.Id]
	} else {
		rec, ok = hist.SetRunForSuite(id)
	}
	if !ok {
		return mcpSetRun{}, false, nil
	}
	status := discovery.CheckStatusComplete
	if rec.Verdict.Status == discovery.SetVerdictIncomplete {
		status = discovery.CheckStatusCanceled
	}
	verdict := rec.Verdict
	return mcpSetRun{setID: rec.SetId, suiteID: rec.SuiteId, status: status, verdict: &verdict, source: "history"}, true, nil
}

func (api *API) mcpApplySetRun(target *config.SetConfig, run mcpSetRun, found bool) (*mcp.CallToolResult, mcpDiscoveryOut, error) {
	cfg := api.getCfg()
	if !found {
		return nil, mcpDiscoveryOut{}, fmt.Errorf(
			"no discovery run for set %q is in memory or saved, so there is nothing to write into it; start one with action=start and set=%q", target.Name, target.Name)
	}
	if target == nil {
		live := cfg.GetSetById(run.setID)
		if live == nil {
			return nil, mcpDiscoveryOut{}, fmt.Errorf("the set that run was for (id %s) no longer exists, so there is nothing to write into", run.setID)
		}
		var err error
		if target, err = mcpDiscoverableSet(live); err != nil {
			return nil, mcpDiscoveryOut{}, err
		}
	}
	switch run.status {
	case discovery.CheckStatusComplete, discovery.CheckStatusCanceled:
	default:
		return nil, mcpDiscoveryOut{}, fmt.Errorf(
			"the run for set %q is %s, so it holds no verdict yet. Call action=status, and action=cancel if you want to stop it", target.Name, run.status)
	}
	v := run.verdict
	if v == nil {
		return nil, mcpDiscoveryOut{}, fmt.Errorf("the run for set %q ended without a set verdict; start it again", target.Name)
	}

	out := mcpDiscoveryOut{Id: run.suiteID, Source: run.source, SetVerdict: mcpSetVerdictView(cfg, target.Id, v, run.stoppedCovered)}
	switch v.Status {
	case discovery.SetVerdictCovered:
	case discovery.SetVerdictCurrentWorks:
		out.Note = fmt.Sprintf("nothing written: the current strategy of set %q passed every confirmation try on %s, so it already works for every address the run probed",
			target.Name, mcpListOrNone(v.Covered))
		return nil, out, nil
	default:
		return nil, mcpDiscoveryOut{}, fmt.Errorf("nothing written: %s. Covered: %s. Uncovered: %s",
			mcpSetVerdictMeaning(target.Name, v), mcpListOrNone(v.Covered), mcpListOrNone(v.Uncovered))
	}
	if v.Set == nil {
		return nil, mcpDiscoveryOut{}, fmt.Errorf("the verdict for set %q names '%s' but carries no strategy to write; start the run again", target.Name, v.WinnerPreset)
	}

	mcpWriteMu.Lock()
	defer mcpWriteMu.Unlock()

	oldCfg := api.getCfg()
	newCfg := oldCfg.Clone()
	live := newCfg.GetSetById(target.Id)
	if live == nil {
		return nil, mcpDiscoveryOut{}, fmt.Errorf("set %q disappeared before the write", target.Name)
	}
	live.AdoptStrategy(v.Set)
	if pins := mcpVerdictPins(v); len(pins) > 0 {
		replacePins(live, pinDomains(pins), pins)
	}

	if err := mcpValidateCandidate(oldCfg, newCfg); err != nil {
		return nil, mcpDiscoveryOut{}, fmt.Errorf("rejected: %w", err)
	}
	snapshot := oldCfg.Clone()
	if err := api.mcpSave(oldCfg, newCfg); err != nil {
		return nil, mcpDiscoveryOut{}, fmt.Errorf("rejected: %w", err)
	}
	api.PerformSoftRestart(newCfg, oldCfg)

	for i, s := range api.getCfg().Sets {
		if s.Id == live.Id {
			out.Applied = &mcpSetRow{Position: i + 1, Id: s.Id, Name: s.Name, Enabled: s.Enabled}
			break
		}
	}
	if out.Applied == nil {
		return nil, mcpDiscoveryOut{}, fmt.Errorf("set %q disappeared while saving", live.Name)
	}
	out.Changed = true

	mcpRecordChange(mcpChange{
		Path:     fmt.Sprintf("sets[%s].strategy", live.Name),
		Previous: "the strategy before discovery",
		Current:  v.WinnerPreset,
		When:     time.Now(), Snapshot: snapshot,
	}, oldCfg, newCfg)
	if err := discovery.MarkAppliedInHistory(api.getCfg().ConfigPath, v.Covered, v.WinnerPreset, live.Id); err != nil {
		log.Errorf("Failed to record applied strategy in discovery history: %v", err)
	}
	log.Infof("mcp: wrote discovery strategy %q into set %q", v.WinnerPreset, live.Name)

	out.Note = fmt.Sprintf("wrote the '%s' strategy into set %q; its domains and addresses are unchanged. It passed every confirmation try on %s together. Undo with b4_revert_last_change",
		v.WinnerPreset, live.Name, mcpListOrNone(v.Covered))
	if !out.Applied.Enabled {
		out.Note += fmt.Sprintf(". Set %q is DISABLED, so nothing matches it until it is enabled", live.Name)
	}
	out.Note += ". Confirm it with b4_test_domain_now before telling the user it is fixed"
	return nil, out, nil
}

func mcpVerdictPins(v *discovery.SetVerdict) map[string][]string {
	if v == nil || v.Set == nil || len(v.Set.DNS.Pins) == 0 {
		return nil
	}
	covered := make(map[string]bool, len(v.Covered))
	for _, d := range v.Covered {
		covered[config.NormalizePinDomain(d)] = true
	}
	var pins map[string][]string
	for domain, ips := range v.Set.DNS.Pins {
		if len(ips) == 0 || !covered[config.NormalizePinDomain(domain)] {
			continue
		}
		if pins == nil {
			pins = map[string][]string{}
		}
		pins[domain] = slices.Clone(ips)
	}
	return pins
}
