package handler

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"sort"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/geodat"
	"github.com/daniellavrushin/b4/sni"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	mcpGeoDefaultLimit = 100
	mcpGeoMaxLimit     = 500
	mcpGeoMaxHits      = 25
)

type mcpGeoIn struct {
	Action   string `json:"action" jsonschema:"One of: list, preview, find_domain, lookup_ip, status."`
	Kind     string `json:"kind,omitempty" jsonschema:"Which database: 'geosite' (domains, the default) or 'geoip' (addresses). Ignored by find_domain, lookup_ip and status."`
	Category string `json:"category,omitempty" jsonschema:"Category name, for action=preview."`
	Domain   string `json:"domain,omitempty" jsonschema:"Domain to locate, for action=find_domain."`
	IP       string `json:"ip,omitempty" jsonschema:"IPv4 or IPv6 address to locate, for action=lookup_ip."`
	Contains string `json:"contains,omitempty" jsonschema:"Substring filter on category names, for action=list."`
	Limit    int    `json:"limit,omitempty" jsonschema:"Maximum rows. Default 100, maximum 500."`
}

type mcpGeoCategory struct {
	Category string `json:"category"`
	Entry    string `json:"entry,omitempty"`
	Relation string `json:"relation,omitempty"`
	UsedBy   string `json:"used_by_set,omitempty"`
}

type mcpGeoDatabase struct {
	Kind       string `json:"kind"`
	Path       string `json:"path,omitempty"`
	Installed  bool   `json:"installed"`
	SizeBytes  int64  `json:"size_bytes,omitempty"`
	Modified   string `json:"modified,omitempty"`
	Categories int    `json:"categories,omitempty"`
	InUse      int    `json:"categories_used_by_sets"`
}

type mcpGeoOut struct {
	Action     string           `json:"action"`
	Categories []string         `json:"categories,omitempty"`
	Entries    []string         `json:"entries,omitempty"`
	Matches    []mcpGeoCategory `json:"matches,omitempty"`
	Total      int              `json:"total,omitempty"`
	Databases  []mcpGeoDatabase `json:"databases,omitempty"`
	Truncated  bool             `json:"truncated,omitempty"`
	Note       string           `json:"note"`
}

func mcpGeoLimit(v int) int {
	switch {
	case v <= 0:
		return mcpGeoDefaultLimit
	case v > mcpGeoMaxLimit:
		return mcpGeoMaxLimit
	default:
		return v
	}
}

func mcpGeoIsIP(kind string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "geosite", "geosite_categories":
		return false, nil
	case "geoip", "geoip_categories", "ip":
		return true, nil
	}
	return false, fmt.Errorf("unknown kind %q: expected geosite or geoip", kind)
}

func mcpGeoCategoriesInUse(cfg *config.Config, geoip bool) map[string]string {
	out := map[string]string{}
	for _, s := range cfg.Sets {
		list := s.Targets.GeoSiteCategories
		if geoip {
			list = s.Targets.GeoIpCategories
		}
		for _, c := range list {
			key := strings.ToLower(strings.TrimSpace(c))
			if key != "" && out[key] == "" {
				out[key] = s.Name
			}
		}
	}
	return out
}

func (api *API) addMCPGeoTools(srv *mcp.Server) {
	addTool(srv, &mcp.Tool{
		Name:  "b4_geo_lookup",
		Title: "Search the geosite and geoip databases",
		Description: "Read the installed geosite (domain) and geoip (address) databases. " +
			"status: what is installed. list: category names, filtered by 'contains'. preview: what one category holds. " +
			"find_domain: which categories cover a domain. lookup_ip: the same for an address. " +
			"Check a category here before using it: an unknown name matches nothing, silently. " +
			"find_domain and lookup_ip read the whole database and stop at 25 matches.",
		Annotations: mcpReadOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpGeoIn) (*mcp.CallToolResult, mcpGeoOut, error) {
		action := strings.ToLower(strings.TrimSpace(in.Action))
		out := mcpGeoOut{Action: action}

		switch action {
		case "status":
			return api.mcpGeoStatus(out)
		case "list":
			return api.mcpGeoList(out, in)
		case "preview":
			return api.mcpGeoPreview(ctx, out, in)
		case "find_domain":
			return api.mcpGeoFindDomain(ctx, out, in)
		case "lookup_ip":
			return api.mcpGeoLookupIP(ctx, out, in)
		case "":
			return nil, out, fmt.Errorf("action is required: status, list, preview, find_domain or lookup_ip")
		default:
			return nil, out, fmt.Errorf("unknown action %q: expected status, list, preview, find_domain or lookup_ip", action)
		}
	})
}

func (api *API) mcpGeoStatus(out mcpGeoOut) (*mcp.CallToolResult, mcpGeoOut, error) {
	cfg := api.getCfg()
	for _, spec := range []struct {
		kind  string
		path  string
		geoip bool
	}{
		{"geosite", api.geodataManager.GetGeositePath(), false},
		{"geoip", api.geodataManager.GetGeoipPath(), true},
	} {
		db := mcpGeoDatabase{Kind: spec.kind, Path: spec.path}
		db.InUse = len(mcpGeoCategoriesInUse(cfg, spec.geoip))
		if info, err := os.Stat(spec.path); err == nil && spec.path != "" {
			db.Installed = true
			db.SizeBytes = info.Size()
			db.Modified = info.ModTime().UTC().Format("2006-01-02T15:04:05Z")
			if tags, err := api.geodataManager.ListCategories(spec.path); err == nil {
				db.Categories = len(tags)
			}
		}
		out.Databases = append(out.Databases, db)
	}

	missing := []string{}
	for _, db := range out.Databases {
		if !db.Installed {
			missing = append(missing, db.Kind)
		}
	}
	switch len(missing) {
	case 0:
		out.Note = "both databases are installed"
	case 1:
		out.Note = fmt.Sprintf(
			"the %s database is not installed, so every %s category matches nothing and b4 refuses to save a set that selects one. Install it under Settings -> Geodata.",
			missing[0], missing[0])
	default:
		out.Note = "neither the geosite nor the geoip database is installed, so every geo category matches nothing and b4 refuses to save a set that selects one. Install them under Settings -> Geodata."
	}
	return nil, out, nil
}

func (api *API) mcpGeoList(out mcpGeoOut, in mcpGeoIn) (*mcp.CallToolResult, mcpGeoOut, error) {
	geoip, err := mcpGeoIsIP(in.Kind)
	if err != nil {
		return nil, out, err
	}
	path := api.geodataManager.GetGeositePath()
	if geoip {
		path = api.geodataManager.GetGeoipPath()
	}
	tags, lerr := api.geodataManager.ListCategories(path)
	if lerr != nil {
		return nil, out, fmt.Errorf("read %s database: %w", mcpGeoKindName(geoip), lerr)
	}

	if filter := strings.ToLower(strings.TrimSpace(in.Contains)); filter != "" {
		kept := tags[:0:0]
		for _, t := range tags {
			if strings.Contains(t, filter) {
				kept = append(kept, t)
			}
		}
		tags = kept
	}

	out.Total = len(tags)
	limit := mcpGeoLimit(in.Limit)
	if len(tags) > limit {
		tags = tags[:limit]
		out.Truncated = true
	}
	out.Categories = tags

	switch {
	case out.Total == 0 && strings.TrimSpace(in.Contains) != "":
		out.Note = fmt.Sprintf("no %s category name contains %q", mcpGeoKindName(geoip), in.Contains)
	case out.Total == 0:
		out.Note = fmt.Sprintf("the %s database holds no categories, or is not installed — call action=status", mcpGeoKindName(geoip))
	case out.Truncated:
		out.Note = fmt.Sprintf("%d %s categories match; the first %d are listed. Narrow it with 'contains'.",
			out.Total, mcpGeoKindName(geoip), len(tags))
	default:
		out.Note = fmt.Sprintf("%d %s categories", out.Total, mcpGeoKindName(geoip))
	}
	return nil, out, nil
}

func (api *API) mcpGeoPreview(ctx context.Context, out mcpGeoOut, in mcpGeoIn) (*mcp.CallToolResult, mcpGeoOut, error) {
	category := strings.ToLower(strings.TrimSpace(in.Category))
	if category == "" {
		return nil, out, fmt.Errorf("category is required for action=preview")
	}
	geoip, kerr := mcpGeoIsIP(in.Kind)
	if kerr != nil {
		return nil, out, kerr
	}
	limit := mcpGeoLimit(in.Limit)

	var entries []string
	var total int
	var err error
	if geoip {
		entries, total, err = api.geodataManager.PreviewGeoipCategory(ctx, category, limit)
	} else {
		entries, total, err = api.geodataManager.PreviewGeositeCategory(ctx, category, limit)
	}
	if err != nil {
		return nil, out, fmt.Errorf("preview %s category %q: %w", mcpGeoKindName(geoip), category, err)
	}

	out.Entries = entries
	out.Total = total
	out.Truncated = total > len(entries)
	switch {
	case total == 0:
		out.Note = fmt.Sprintf(
			"%q holds nothing. Either no such %s category exists — b4 accepts an unknown name and silently matches nothing — or the database is not installed. Call action=list with 'contains' to find the real name.",
			category, mcpGeoKindName(geoip))
	case out.Truncated:
		out.Note = fmt.Sprintf("%q holds %d entries; the first %d are shown", category, total, len(entries))
	default:
		out.Note = fmt.Sprintf("%q holds %d entries", category, total)
	}
	return nil, out, nil
}

func (api *API) mcpGeoFindDomain(ctx context.Context, out mcpGeoOut, in mcpGeoIn) (*mcp.CallToolResult, mcpGeoOut, error) {
	domain := sni.NormalizeDomain(in.Domain)
	if domain == "" {
		return nil, out, fmt.Errorf("domain is required for action=find_domain")
	}

	inUse := mcpGeoCategoriesInUse(api.getCfg(), false)
	best := map[string]mcpGeoCategory{}
	stopped := false

	err := api.geodataManager.ScanGeositeEntries(func(tag, entry string) error {
		if ctx.Err() != nil {
			return geodat.ErrStopScan
		}
		relation, matched := sni.MatchDomainEntry(entry, domain)
		if relation == sni.RelationNone || relation == sni.RelationCovers {
			return nil
		}
		prev, seen := best[tag]
		if seen && prev.Relation != "" &&
			sni.DomainRelation(prev.Relation).Priority() >= relation.Priority() {
			return nil
		}
		best[tag] = mcpGeoCategory{
			Category: tag,
			Entry:    matched,
			Relation: string(relation),
			UsedBy:   inUse[tag],
		}
		if !seen && len(best) >= mcpGeoMaxHits {
			stopped = true
			return geodat.ErrStopScan
		}
		return nil
	})
	if err != nil {
		return nil, out, fmt.Errorf("search the geosite database: %w", err)
	}
	if ctx.Err() != nil {
		return nil, out, fmt.Errorf("the geosite search was cut short after %d matches, so this is not a complete answer: %w", len(best), ctx.Err())
	}

	for _, m := range best {
		out.Matches = append(out.Matches, m)
	}
	sort.Slice(out.Matches, func(i, j int) bool {
		li := sni.DomainRelation(out.Matches[i].Relation).Priority()
		lj := sni.DomainRelation(out.Matches[j].Relation).Priority()
		if li != lj {
			return li > lj
		}
		return out.Matches[i].Category < out.Matches[j].Category
	})
	out.Total = len(out.Matches)
	out.Truncated = stopped

	used := []string{}
	for _, m := range out.Matches {
		if m.UsedBy != "" {
			used = append(used, fmt.Sprintf("%s (already in set %q)", m.Category, m.UsedBy))
		}
	}
	switch {
	case stopped:
		out.Note = fmt.Sprintf("stopped at %d categories covering %s; there are more", out.Total, domain)
	case out.Total == 0:
		out.Note = fmt.Sprintf(
			"no geosite category covers %s. Add it to a set as a plain domain in targets.sni_domains instead — b4 matches subdomains of it automatically.", domain)
	default:
		out.Note = fmt.Sprintf("%d categor%s cover %s", out.Total, plural(out.Total, "y", "ies"), domain)
	}
	if len(used) > 0 {
		out.Note += ". Already selected: " + strings.Join(used, ", ")
	}
	return nil, out, nil
}

func (api *API) mcpGeoLookupIP(ctx context.Context, out mcpGeoOut, in mcpGeoIn) (*mcp.CallToolResult, mcpGeoOut, error) {
	raw := strings.TrimSpace(in.IP)
	if raw == "" {
		return nil, out, fmt.Errorf("ip is required for action=lookup_ip")
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return nil, out, fmt.Errorf("%q is not an IP address: %w", raw, err)
	}
	addr = addr.Unmap()

	inUse := mcpGeoCategoriesInUse(api.getCfg(), true)
	seen := map[string]bool{}
	stopped := false

	err = api.geodataManager.ScanGeoipPrefixes(func(tag string, prefix netip.Prefix) error {
		if ctx.Err() != nil {
			return geodat.ErrStopScan
		}
		if seen[tag] || !prefix.Contains(addr) {
			return nil
		}
		seen[tag] = true
		out.Matches = append(out.Matches, mcpGeoCategory{
			Category: tag,
			Entry:    prefix.String(),
			UsedBy:   inUse[tag],
		})
		if len(out.Matches) >= mcpGeoMaxHits {
			stopped = true
			return geodat.ErrStopScan
		}
		return nil
	})
	if err != nil {
		return nil, out, fmt.Errorf("search the geoip database: %w", err)
	}
	if ctx.Err() != nil {
		return nil, out, fmt.Errorf("the geoip search was cut short after %d matches, so this is not a complete answer: %w", len(out.Matches), ctx.Err())
	}

	sort.Slice(out.Matches, func(i, j int) bool { return out.Matches[i].Category < out.Matches[j].Category })
	out.Total = len(out.Matches)
	out.Truncated = stopped
	switch {
	case stopped:
		out.Note = fmt.Sprintf("stopped at %d categories containing %s; there are more", out.Total, addr)
	case out.Total == 0:
		out.Note = fmt.Sprintf("no geoip category contains %s", addr)
	default:
		out.Note = fmt.Sprintf("%d categor%s contain %s", out.Total, plural(out.Total, "y", "ies"), addr)
	}
	return nil, out, nil
}

func mcpGeoKindName(geoip bool) string {
	if geoip {
		return "geoip"
	}
	return "geosite"
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
