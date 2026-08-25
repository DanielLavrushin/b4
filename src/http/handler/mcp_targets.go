package handler

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sni"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	mcpTargetKindDomains  = "sni_domains"
	mcpTargetKindIPs      = "ip"
	mcpTargetKindGeoSite  = "geosite_categories"
	mcpTargetKindGeoIP    = "geoip_categories"
	mcpTargetKindDevices  = "source_devices"
	mcpTargetSummaryLimit = 120
	mcpTargetMaxEntries   = 200
)

var mcpTargetKinds = []string{
	mcpTargetKindDomains, mcpTargetKindIPs,
	mcpTargetKindGeoSite, mcpTargetKindGeoIP, mcpTargetKindDevices,
}

type mcpEditTargetsIn struct {
	Set    string `json:"set" jsonschema:"Set id or name, as reported by b4_list_sets."`
	Kind   string `json:"kind" jsonschema:"Which selector to edit: sni_domains, ip, geosite_categories, geoip_categories or source_devices. These are the config field names."`
	Add    string `json:"add,omitempty" jsonschema:"Comma-separated entries to add."`
	Remove string `json:"remove,omitempty" jsonschema:"Comma-separated entries to remove. Matched case-insensitively against what is stored."`
}

type mcpEditTargetsOut struct {
	Set        string               `json:"set"`
	Kind       string               `json:"kind"`
	Added      []string             `json:"added,omitempty"`
	Removed    []string             `json:"removed,omitempty"`
	Rewritten  []string             `json:"rewritten,omitempty"`
	AlreadySet []string             `json:"already_present,omitempty"`
	NotPresent []string             `json:"not_present,omitempty"`
	MovedFrom  []DomainReassignment `json:"moved_from_other_sets,omitempty"`
	EntryCount int                  `json:"entry_count"`
	Expansion  *mcpTargetExpansion  `json:"expansion,omitempty"`
	Changed    bool                 `json:"changed"`
	Note       string               `json:"note"`
}

func mcpTargetList(t *config.TargetsConfig, kind string) []string {
	switch kind {
	case mcpTargetKindDomains:
		return t.SNIDomains
	case mcpTargetKindIPs:
		return t.IPs
	case mcpTargetKindGeoSite:
		return t.GeoSiteCategories
	case mcpTargetKindGeoIP:
		return t.GeoIpCategories
	case mcpTargetKindDevices:
		return t.SourceDevices
	}
	return nil
}

func mcpTargetSetList(t *config.TargetsConfig, kind string, v []string) {
	switch kind {
	case mcpTargetKindDomains:
		t.SNIDomains = v
	case mcpTargetKindIPs:
		t.IPs = v
	case mcpTargetKindGeoSite:
		t.GeoSiteCategories = v
	case mcpTargetKindGeoIP:
		t.GeoIpCategories = v
	case mcpTargetKindDevices:
		t.SourceDevices = v
	}
}

func mcpSplitEntries(raw string) []string {
	out := []string{}
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func mcpSummarizeList(items []string) string {
	if len(items) == 0 {
		return "(empty)"
	}
	joined := strings.Join(items, ",")
	if len(joined) <= mcpTargetSummaryLimit {
		return joined
	}
	return fmt.Sprintf("%s… (%d entries)", mcpTruncate(joined, mcpTargetSummaryLimit), len(items))
}

func (api *API) mcpCanonicalTarget(kind, raw string) (canonical, rewritten string, err error) {
	value := strings.TrimSpace(raw)
	switch kind {
	case mcpTargetKindDomains:
		note := ""
		if trimmed := strings.TrimPrefix(value, "*."); trimmed != value {
			note = "the wildcard is dropped, b4 already matches subdomains"
			value = trimmed
		}
		canonical = sni.CanonicalDomainEntry(value)
		if canonical == "" {
			return "", "", fmt.Errorf("%q is not a usable domain entry", raw)
		}
		if pattern, isRegex := sni.ParseDomainEntry(canonical); isRegex {
			if _, cerr := regexp.Compile(pattern); cerr != nil {
				return "", "", fmt.Errorf("%q is not a valid regular expression: %w", raw, cerr)
			}
		} else if err := mcpValidPlainDomain(canonical); err != nil {
			return "", "", err
		}
		if canonical != strings.TrimSpace(raw) {
			if note != "" {
				return canonical, fmt.Sprintf("%s -> %s (%s)", raw, canonical, note), nil
			}
			return canonical, fmt.Sprintf("%s -> %s", raw, canonical), nil
		}
		return canonical, "", nil

	case mcpTargetKindIPs:
		if strings.Contains(value, "/") {
			p, perr := netip.ParsePrefix(value)
			if perr != nil {
				return "", "", fmt.Errorf("%q is not a valid CIDR: %w", raw, perr)
			}
			masked := p.Masked()
			if masked.String() != value {
				return masked.String(), fmt.Sprintf("%s -> %s (host bits cleared)", raw, masked), nil
			}
			return masked.String(), "", nil
		}
		addr, aerr := netip.ParseAddr(value)
		if aerr != nil {
			return "", "", fmt.Errorf("%q is neither an IP address nor a CIDR: %w", raw, aerr)
		}
		return addr.Unmap().String(), "", nil

	case mcpTargetKindGeoSite, mcpTargetKindGeoIP:
		lower := strings.ToLower(value)
		if lower != value {
			return lower, fmt.Sprintf("%s -> %s (category names are lower case)", raw, lower), nil
		}
		return lower, "", nil

	case mcpTargetKindDevices:
		hw, herr := net.ParseMAC(value)
		if herr != nil {
			return "", "", fmt.Errorf(
				"%q is not a MAC address. b4 scopes a set by the client's MAC, not its IP - a device configured by address carries a generated MAC, which is what belongs here", raw)
		}
		upper := strings.ToUpper(hw.String())
		if upper != value {
			return upper, fmt.Sprintf("%s -> %s", raw, upper), nil
		}
		return upper, "", nil
	}
	return "", "", fmt.Errorf("unknown kind %q", kind)
}

func mcpValidPlainDomain(entry string) error {
	if strings.ContainsAny(entry, " \t/@?#") {
		return fmt.Errorf("%q is not a domain: it looks like a URL or a phrase. Pass the host on its own, for example rutracker.org", entry)
	}
	if strings.Contains(entry, ":") {
		return fmt.Errorf("%q is not a domain: drop the port or scheme", entry)
	}
	if !strings.Contains(entry, ".") {
		return fmt.Errorf("%q has no dot, so it matches nothing. Pass a full host such as example.com, or a regexp: pattern", entry)
	}
	return nil
}

func mcpTargetIsCatchAll(kind, canonical string) bool {
	switch kind {
	case mcpTargetKindIPs:
		return canonical == "0.0.0.0/0" || canonical == "::/0"
	case mcpTargetKindDomains:
		pattern, isRegex := sni.ParseDomainEntry(canonical)
		if !isRegex {
			return false
		}
		switch pattern {
		case ".*", "^.*$", ".+", "^.+$", "(.*)":
			return true
		}
	}
	return false
}

func (api *API) mcpTargetKey(kind, stored string) string {
	if canonical, _, err := api.mcpCanonicalTarget(kind, stored); err == nil {
		return strings.ToLower(canonical)
	}
	return strings.ToLower(strings.TrimSpace(stored))
}

func (api *API) mcpUnknownCategories(kind string, names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	geoip := kind == mcpTargetKindGeoIP
	path := api.geodataManager.GetGeositePath()
	if geoip {
		path = api.geodataManager.GetGeoipPath()
	}
	tags, err := api.geodataManager.ListCategories(path)
	if err != nil {
		return nil, fmt.Errorf("no %s database is readable, so %s cannot be verified or expanded: %w",
			mcpGeoKindName(geoip), strings.Join(names, ", "), err)
	}
	known := make(map[string]bool, len(tags))
	for _, t := range tags {
		known[t] = true
	}
	var unknown []string
	for _, n := range names {
		if !known[n] {
			unknown = append(unknown, n)
		}
	}
	return unknown, nil
}

func (api *API) mcpNearestCategories(kind, name string) []string {
	geoip := kind == mcpTargetKindGeoIP
	path := api.geodataManager.GetGeositePath()
	if geoip {
		path = api.geodataManager.GetGeoipPath()
	}
	tags, err := api.geodataManager.ListCategories(path)
	if err != nil {
		return nil
	}
	var near []string
	for _, t := range tags {
		if strings.Contains(t, name) || strings.Contains(name, t) {
			near = append(near, t)
			if len(near) == 5 {
				break
			}
		}
	}
	return near
}

func (api *API) addMCPTargetTools(srv *mcp.Server) {
	addTool(srv, &mcp.Tool{
		Name:  "b4_edit_set_targets",
		Title: "Add or remove what a set matches",
		Description: "Add or remove entries in one of a set's selectors, without rewriting the whole list. " +
			"Use this rather than b4_set_config_value, which replaces a target list wholesale. " +
			"Entries are validated first: an unparseable address or an unknown geo category is refused here, because nothing else in b4 rejects them. " +
			"Adding a domain REMOVES it from every other enabled set; the result lists what moved.",
		Annotations: mcpDestructive,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpEditTargetsIn) (*mcp.CallToolResult, mcpEditTargetsOut, error) {
		if !api.getCfg().System.WebServer.MCP.AllowWrites {
			return nil, mcpEditTargetsOut{}, fmt.Errorf(
				"configuration writes are disabled: turn on 'Allow configuration changes' under Settings -> Integrations -> MCP server to permit them")
		}

		kind := strings.ToLower(strings.TrimSpace(in.Kind))
		if kind == "" {
			return nil, mcpEditTargetsOut{}, fmt.Errorf("kind is required: one of %s", strings.Join(mcpTargetKinds, ", "))
		}
		valid := false
		for _, k := range mcpTargetKinds {
			if k == kind {
				valid = true
				break
			}
		}
		if !valid {
			return nil, mcpEditTargetsOut{}, fmt.Errorf("unknown kind %q: expected one of %s", in.Kind, strings.Join(mcpTargetKinds, ", "))
		}

		setRef := strings.TrimSpace(in.Set)
		if setRef == "" {
			return nil, mcpEditTargetsOut{}, fmt.Errorf("set (id or name) is required")
		}
		adds := mcpSplitEntries(in.Add)
		removes := mcpSplitEntries(in.Remove)
		if len(adds) == 0 && len(removes) == 0 {
			return nil, mcpEditTargetsOut{}, fmt.Errorf("nothing to do: pass 'add', 'remove' or both")
		}
		if len(adds)+len(removes) > mcpTargetMaxEntries {
			return nil, mcpEditTargetsOut{}, fmt.Errorf(
				"too many entries in one call (%d): send at most %d, in batches", len(adds)+len(removes), mcpTargetMaxEntries)
		}

		mcpWriteMu.Lock()
		defer mcpWriteMu.Unlock()

		oldCfg := api.getCfg()
		newCfg := oldCfg.Clone()

		set := findSetIn(newCfg, setRef)
		if set == nil {
			return nil, mcpEditTargetsOut{}, fmt.Errorf("no set with id or name %q", setRef)
		}

		out := mcpEditTargetsOut{Set: set.Name, Kind: kind}

		canonAdds := make([]string, 0, len(adds))
		for _, raw := range adds {
			canonical, rewritten, err := api.mcpCanonicalTarget(kind, raw)
			if err != nil {
				return nil, mcpEditTargetsOut{}, err
			}
			if rewritten != "" {
				out.Rewritten = append(out.Rewritten, rewritten)
			}
			canonAdds = append(canonAdds, canonical)
		}
		canonRemoves := make([]string, 0, len(removes))
		for _, raw := range removes {
			canonical, _, err := api.mcpCanonicalTarget(kind, raw)
			if err != nil {
				canonical = strings.ToLower(strings.TrimSpace(raw))
			}
			canonRemoves = append(canonRemoves, canonical)
		}

		addKeys := make(map[string]bool, len(canonAdds))
		for _, e := range canonAdds {
			addKeys[strings.ToLower(e)] = true
		}
		for _, e := range canonRemoves {
			if addKeys[strings.ToLower(e)] {
				return nil, mcpEditTargetsOut{}, fmt.Errorf(
					"%q is in both add and remove; send one or the other", e)
			}
		}

		if kind == mcpTargetKindGeoSite || kind == mcpTargetKindGeoIP {
			unknown, err := api.mcpUnknownCategories(kind, canonAdds)
			if err != nil {
				return nil, mcpEditTargetsOut{}, err
			}
			if len(unknown) > 0 {
				msg := fmt.Sprintf("no such %s category: %s", mcpGeoKindName(kind == mcpTargetKindGeoIP), strings.Join(unknown, ", "))
				if near := api.mcpNearestCategories(kind, unknown[0]); len(near) > 0 {
					msg += fmt.Sprintf(". Did you mean %s? Call b4_geo_lookup with action=list to search.", strings.Join(near, ", "))
				} else {
					msg += ". Call b4_geo_lookup with action=list to find the right name."
				}
				return nil, mcpEditTargetsOut{}, fmt.Errorf("%s", msg)
			}
		}

		current := mcpTargetList(&set.Targets, kind)
		previous := append([]string(nil), current...)
		previousSummary := mcpSummarizeList(previous)

		present := make(map[string]bool, len(current))
		for _, e := range current {
			present[api.mcpTargetKey(kind, e)] = true
		}

		next := append([]string(nil), current...)
		for _, e := range canonAdds {
			key := api.mcpTargetKey(kind, e)
			if present[key] {
				out.AlreadySet = append(out.AlreadySet, e)
				continue
			}
			present[key] = true
			next = append(next, e)
			out.Added = append(out.Added, e)
		}
		if len(canonRemoves) > 0 {
			drop := make(map[string]bool, len(canonRemoves))
			for _, e := range canonRemoves {
				drop[api.mcpTargetKey(kind, e)] = true
			}
			kept := next[:0:0]
			seen := map[string]bool{}
			for _, e := range next {
				key := api.mcpTargetKey(kind, e)
				if drop[key] {
					out.Removed = append(out.Removed, e)
					seen[key] = true
					continue
				}
				kept = append(kept, e)
			}
			for _, e := range canonRemoves {
				if !seen[api.mcpTargetKey(kind, e)] {
					out.NotPresent = append(out.NotPresent, e)
				}
			}
			next = kept
		}

		out.EntryCount = len(next)
		mcpTargetSetList(&set.Targets, kind, next)

		if kind == mcpTargetKindDomains && len(canonAdds) > 0 {
			out.MovedFrom = api.releaseDomainsFromOtherSets(newCfg.Sets, set.Id, canonAdds)
		}

		if len(out.Added) == 0 && len(out.Removed) == 0 && len(out.MovedFrom) == 0 {
			out.Note = "nothing changed: " + mcpTargetsNoChangeReason(out)
			return nil, out, nil
		}

		report := api.loadTargetsForSetCached(set)
		out.Expansion = &mcpTargetExpansion{
			Domains:      report.Domains,
			IPs:          report.IPs,
			EmptyGeoSite: report.EmptyGeoSite,
			EmptyGeoIP:   report.EmptyGeoIP,
		}

		if err := mcpValidateCandidate(oldCfg, newCfg); err != nil {
			return nil, mcpEditTargetsOut{}, fmt.Errorf("rejected: %w", err)
		}

		snapshot := oldCfg.Clone()
		if err := api.saveAndPushConfig(newCfg); err != nil {
			return nil, mcpEditTargetsOut{}, fmt.Errorf("rejected: %w", err)
		}
		api.applyRuntimeChanges(newCfg, oldCfg)
		refreshed := api.PerformSoftRestart(newCfg, oldCfg)

		path := fmt.Sprintf("sets[%s].targets.%s", set.Name, kind)

		live := findSetIn(api.getCfg(), set.Id)
		if live == nil {
			return nil, mcpEditTargetsOut{}, fmt.Errorf("set %q disappeared while saving", set.Name)
		}
		stored := mcpTargetList(&live.Targets, kind)
		currentSummary := mcpSummarizeList(stored)
		out.EntryCount = len(stored)
		out.Changed = !slices.Equal(stored, previous) || len(out.MovedFrom) > 0

		if !out.Changed {
			out.Note = fmt.Sprintf(
				"not applied: %s is unchanged after saving, and nothing was recorded for undo.", path)
			log.Infof("mcp: %s unchanged after save", path)
			return nil, out, nil
		}

		mcpRecordChange(mcpChange{
			Path: path, Previous: previousSummary, Current: currentSummary,
			When: time.Now(), Snapshot: snapshot,
		})
		log.Infof("mcp: %s +%d -%d (now %d entries)", path, len(out.Added), len(out.Removed), len(stored))

		out.Note = fmt.Sprintf("applied live; %s now holds %d entries. Undo with b4_revert_last_change",
			path, len(stored))
		if refreshed {
			out.Note += " (firewall rules were refreshed to match)"
		}
		if len(out.MovedFrom) > 0 {
			names := make([]string, 0, len(out.MovedFrom))
			for _, m := range out.MovedFrom {
				names = append(names, fmt.Sprintf("%s from %q", m.Domain, m.SetName))
			}
			out.Note += ". A domain belongs to one enabled set, so b4 took " + strings.Join(names, ", ")
		}
		if !live.Enabled {
			out.Note += fmt.Sprintf(". Set %q is DISABLED, so nothing here matches any traffic until it is enabled", live.Name)
		}
		if kind == mcpTargetKindDevices && len(current) > 0 && len(stored) == 0 {
			out.Note += ". source_devices is now empty, which does NOT mean no devices: an empty list matches EVERY device on the network, so this set just widened from one device to all of them"
		}
		if catch := mcpCatchAllAdded(kind, out.Added); len(catch) > 0 {
			out.Note += fmt.Sprintf(". %s is a catch-all that matches everything", strings.Join(catch, ", "))
			if live.Routing.Enabled {
				out.Note += fmt.Sprintf(", and routing is enabled on this set in %q mode - every matched packet now takes that path", live.Routing.Mode)
			}
		}
		if hint := mcpExpansionNote(out.Expansion); hint != "" {
			out.Note += ". " + hint
		}
		return nil, out, nil
	})
}

func mcpCatchAllAdded(kind string, added []string) []string {
	var out []string
	for _, e := range added {
		if mcpTargetIsCatchAll(kind, e) {
			out = append(out, e)
		}
	}
	return out
}

func mcpTargetsNoChangeReason(out mcpEditTargetsOut) string {
	switch {
	case len(out.AlreadySet) > 0 && len(out.NotPresent) > 0:
		return fmt.Sprintf("%s already present, %s not present",
			strings.Join(out.AlreadySet, ", "), strings.Join(out.NotPresent, ", "))
	case len(out.AlreadySet) > 0:
		return "already present: " + strings.Join(out.AlreadySet, ", ")
	case len(out.NotPresent) > 0:
		return "not present: " + strings.Join(out.NotPresent, ", ")
	default:
		return "nothing matched"
	}
}
