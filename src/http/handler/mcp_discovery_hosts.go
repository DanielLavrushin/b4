package handler

import (
	"fmt"
	"slices"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
)

const (
	mcpPageHostsListMax   = 12
	mcpPageHostsApplyWait = "the search is over and the confirmation pass has run, but the run is still testing the other hosts the confirmed pages load, for at most about two minutes, and apply waits for the run to end. " +
		"Call action=status once it has, or action=cancel to skip that step: the run then ends with every result kept"
	mcpPageHostsSkipNote = "the search was already over and the confirmation pass had run, so cancel skips only the check of the other hosts the confirmed pages load: " +
		"the run ends as complete with every result kept, and action=apply works once action=status reports it finished"
)

var mcpOfferedHostContexts = []discovery.HostContext{
	discovery.HostContextStylesheet,
	discovery.HostContextScript,
	discovery.HostContextImage,
	discovery.HostContextMedia,
	discovery.HostContextPreload,
	discovery.HostContextCSS,
}

var mcpHostContextLabels = map[discovery.HostContext]string{
	discovery.HostContextInline:     "only named in the page's code",
	discovery.HostContextFrame:      "an embedded frame",
	discovery.HostContextIcon:       "an icon",
	discovery.HostContextPreconnect: "a connection hint",
	discovery.HostContextBase:       "the page's base address",
	discovery.HostContextRedirect:   "where the page redirects",
}

func mcpRunning(status string) bool {
	switch discovery.CheckStatus(status) {
	case discovery.CheckStatusRunning, discovery.CheckStatusPending:
		return true
	}
	return false
}

func mcpCheckingPageHosts(status, phase string) bool {
	return mcpRunning(status) && phase == string(discovery.PhaseHosts)
}

func mcpSnapshotOf(id string) *mcpSuiteSnapshot {
	suite, ok := discovery.GetCheckSuite(id)
	if !ok || suite == nil {
		return nil
	}
	snap, err := mcpSuiteProjection(suite)
	if err != nil {
		return nil
	}
	return snap
}

func mcpCancelSkippedPageHosts(before, after *mcpSuiteSnapshot) bool {
	return before != nil && mcpRunning(before.Status) && after != nil &&
		after.CurrentPhase == string(discovery.PhaseHosts) && discovery.CheckStatus(after.Status) != discovery.CheckStatusCanceled
}

func mcpPageHostsRunningNote(snap *mcpSuiteSnapshot, rows []mcpDiscoveryDomain) string {
	found := 0
	for _, r := range rows {
		if r.Found {
			found++
		}
	}
	step := "reading the confirmed pages through their strategy to find the other hosts they load"
	switch {
	case snap.HostsSkipped:
		step = "ending the check of the other hosts the confirmed pages load, as asked"
	case snap.HostsTotal > 0:
		step = "testing the other hosts the confirmed pages load, without b4 and with the strategy"
	}
	note := fmt.Sprintf("the search is over and the confirmation pass has run (%d of %d domain(s) have a strategy). The run is now %s, which takes at most about two minutes, and action=apply works once it has ended. ",
		found, len(rows), step)
	if snap.SetId != "" {
		note += "The set verdict is published when the run ends. "
	}
	return note + "action=cancel skips only this step and keeps every result. Do not poll in a loop: check again only when the user asks."
}

func mcpNeededHosts(check *discovery.HostCheck, preset string) []discovery.HostResult {
	if check == nil || preset == "" || check.Preset != preset {
		return nil
	}
	var out []discovery.HostResult
	for _, h := range check.Hosts {
		if h.Verdict == discovery.HostNeeded {
			out = append(out, h)
		}
	}
	return out
}

func mcpNotePageHosts(row *mcpDiscoveryDomain, check *discovery.HostCheck, preset string) {
	if check == nil || preset == "" || check.Preset != preset {
		return
	}
	row.hostsPreset = preset
	row.pageHosts = mcpNeededHosts(check, preset)
	for _, h := range check.Hosts {
		if h.Verdict == discovery.HostNotFixed {
			row.unfixedHosts = append(row.unfixedHosts, h.Host)
		}
	}
}

func mcpGroupPageHosts(domains []string, preset string, pageHosts func(domain string) *discovery.HostCheck) []discovery.HostResult {
	var out []discovery.HostResult
	for _, d := range domains {
		for _, h := range mcpNeededHosts(pageHosts(d), preset) {
			if !slices.ContainsFunc(out, func(seen discovery.HostResult) bool { return seen.Host == h.Host }) {
				out = append(out, h)
			}
		}
	}
	return out
}

func mcpHostsOutside(set *config.SetConfig, hosts []discovery.HostResult) []discovery.HostResult {
	var out []discovery.HostResult
	for _, h := range hosts {
		if !setCoversDomain(set, h.Host) {
			out = append(out, h)
		}
	}
	return out
}

func mcpAppliedPreset(e discovery.HistoryEntry) string {
	if set := e.ApplicableSet(); set != nil && set.Name != "" {
		return set.Name
	}
	return e.BestPreset
}

func mcpPageHostLabel(h discovery.HostResult) string {
	switch {
	case h.Tracker && !h.Blocking:
		return "a tracker"
	case h.Service != "":
		return fmt.Sprintf("part of %s, better in a set of its own", h.Service)
	case slices.Contains(mcpOfferedHostContexts, h.Context):
		return ""
	}
	return mcpHostContextLabels[h.Context]
}

func mcpHostGrammar(n int) (is, load, them string) {
	if n == 1 {
		return "is", "loads", "it"
	}
	return "are", "load", "them"
}

func mcpHostNames(names []string) string {
	if len(names) <= mcpPageHostsListMax {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(names[:mcpPageHostsListMax], ", "), len(names)-mcpPageHostsListMax)
}

func mcpPageHostsAdvice(lead string, hosts []discovery.HostResult, preset, target string) string {
	var offered, other []string
	for _, h := range hosts {
		if label := mcpPageHostLabel(h); label != "" {
			other = append(other, fmt.Sprintf("%s (%s)", h.Host, label))
			continue
		}
		offered = append(offered, h.Host)
	}
	var parts []string
	if len(offered) > 0 {
		is, load, them := mcpHostGrammar(len(offered))
		parts = append(parts, fmt.Sprintf("%s %s, which %s blocked without b4 and %s with '%s': apply does not add %s, so add %s%s with b4_edit_set_targets (kind sni_domains) if the user wants the whole page",
			lead, mcpHostNames(offered), is, load, preset, them, them, target))
	}
	if len(other) > 0 {
		_, load, them := mcpHostGrammar(len(other))
		if len(offered) == 0 {
			parts = append(parts, fmt.Sprintf("%s %s, which failed without b4 and %s with '%s', but apply does not add %s and the web interface does not tick %s by default, so add %s%s with b4_edit_set_targets (kind sni_domains) only if the user asks",
				lead, mcpHostNames(other), load, preset, them, them, them, target))
		} else {
			parts = append(parts, fmt.Sprintf("%s also failed without b4 and %s with '%s', but the web interface does not tick %s by default, so add %s only if the user asks",
				mcpHostNames(other), load, preset, them, them))
		}
	}
	return strings.Join(parts, "; ")
}

func mcpPageHostsVerdict(d mcpDiscoveryDomain) string {
	var parts []string
	if advice := mcpPageHostsAdvice("its page also loads", d.pageHosts, d.hostsPreset, ""); advice != "" {
		parts = append(parts, advice)
	}
	if n := len(d.unfixedHosts); n > 0 {
		stays := "stays"
		if n > 1 {
			stays = "stay"
		}
		parts = append(parts, fmt.Sprintf("%s on its page %s blocked even with '%s'", mcpHostNames(d.unfixedHosts), stays, d.hostsPreset))
	}
	if len(parts) == 0 {
		return ""
	}
	return "; " + strings.Join(parts, "; ")
}

func mcpWithPageHostsNote(note string, rows []mcpDiscoveryDomain) string {
	var named []string
	for _, r := range rows {
		if len(r.pageHosts) > 0 {
			named = append(named, r.Domain)
		}
	}
	if len(named) == 0 {
		return note
	}
	if note != "" && !strings.HasSuffix(note, ".") {
		note += "."
	}
	if len(named) == 1 {
		return note + fmt.Sprintf(" The page of %s also needs other hosts, named in its verdict, which apply does not add.", named[0])
	}
	return note + fmt.Sprintf(" The pages of %s also need other hosts, named in their verdicts, which apply does not add.", strings.Join(named, ", "))
}

func mcpAppliedPageHostsNote(hosts []discovery.HostResult, preset, setName string, pages int) string {
	lead := "Its page also loads"
	if pages > 1 {
		lead = "Their pages also load"
	}
	advice := mcpPageHostsAdvice(lead, hosts, preset, fmt.Sprintf(" to set %q", setName))
	if advice == "" {
		return ""
	}
	return ". " + advice
}
