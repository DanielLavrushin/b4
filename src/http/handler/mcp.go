package handler

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/daniellavrushin/b4/ai"
	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	mcpEndpoint     = "/api/mcp"
	mcpServerName   = "b4"
	mcpTopicScheme  = "b4://topics/"
	mcpMaxSetsInOut = 200
	mcpDefaultLines = 100
	mcpMaxLines     = 500
	mcpTailReadCap  = 512 << 10
)

var mcpUnauthWarnOnce sync.Once

func boolPtr(b bool) *bool { return &b }

func addTool[In, Out any](srv *mcp.Server, t *mcp.Tool, h mcp.ToolHandlerFor[In, Out]) {
	mcp.AddTool(srv, t, func(ctx context.Context, req *mcp.CallToolRequest, in In) (res *mcp.CallToolResult, out Out, err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Errorf("mcp: tool %q panicked: %v\n%s", t.Name, r, debug.Stack())
				var zero Out
				res, out, err = nil, zero, fmt.Errorf("internal error in tool %s", t.Name)
			}
		}()
		return h(ctx, req, in)
	})
}

var mcpReadOnly = &mcp.ToolAnnotations{
	ReadOnlyHint:    true,
	DestructiveHint: boolPtr(false),
	IdempotentHint:  true,
	OpenWorldHint:   boolPtr(false),
}

// @Summary Model Context Protocol endpoint
// @Description JSON-RPC 2.0 endpoint implementing the Model Context Protocol (Streamable HTTP, stateless).
// @Description This is not a REST resource: there is one endpoint and the operation is named in the body's "method"
// @Description field ("tools/list", "tools/call", "resources/list", "resources/read", "prompts/list", "prompts/get").
// @Description Tool names go in params.name — there are no per-tool URLs. Call tools/list for the authoritative,
// @Description self-describing tool catalogue and its JSON schemas.
// @Description The Accept header MUST list both application/json and text/event-stream; responses are Server-Sent Events.
// @Description Disabled unless system.web_server.mcp.enabled is true. Authentication follows the web server:
// @Description when a username and password are configured, the usual Bearer token is required.
// @Tags MCP
// @Accept json
// @Produce text/event-stream
// @Param body body object true "JSON-RPC 2.0 request, e.g. {\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\"}"
// @Success 200 {string} string "JSON-RPC response as an SSE stream"
// @Failure 400 {string} string "Accept header missing application/json or text/event-stream"
// @Failure 403 {object} map[string]string "Origin not allowed (DNS-rebinding protection)"
// @Failure 404 {object} map[string]string "MCP server is disabled"
// @Security BearerAuth
// @Router /mcp [post]
func (api *API) RegisterMCPApi() {
	srv := api.newMCPServer()
	streamable := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{Stateless: true},
	)
	api.mux.Handle(mcpEndpoint, api.mcpGate(streamable))
	api.mux.HandleFunc("/api/mcp/generate-token", api.handleMCPGenerateToken)
}

// @Summary Generate an MCP access token
// @Description Returns a fresh random token for MCP clients. It is not saved — store it via the config API to activate it.
// @Tags MCP
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @Router /mcp/generate-token [post]
func (api *API) handleMCPGenerateToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	token, err := GenerateMCPToken()
	if err != nil {
		writeJsonError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}
	sendResponse(w, map[string]interface{}{"success": true, "token": token})
}

func GenerateMCPToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// MCPEndpoint is the MCP route, exported so the auth middleware can recognise
// requests that carry an MCP token instead of a web session token.
const MCPEndpoint = mcpEndpoint

// MCPTokenAccepts reports whether the presented bearer token is the configured
// MCP token. It is constant-time and false when no MCP token is configured.
func MCPTokenAccepts(cfg *config.Config, token string) bool {
	want := cfg.System.WebServer.MCP.Token
	if want == "" || token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(token)) == 1
}

func mcpBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
}

func (api *API) mcpGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := api.getCfg()

		if !cfg.System.WebServer.MCP.Enabled {
			writeJsonError(w, http.StatusNotFound, "mcp server is disabled")
			return
		}

		if cfg.System.WebServer.MCP.Token != "" {
			if !MCPTokenAccepts(cfg, mcpBearerToken(r)) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="b4 MCP"`)
				writeJsonError(w, http.StatusUnauthorized, "invalid or missing MCP token")
				return
			}
		} else if !mcpAuthConfigured(cfg) {
			mcpUnauthWarnOnce.Do(func() {
				log.Warnf("mcp: serving without authentication — no MCP token is set and web_server.username/password are empty, so anyone who can reach %s can read b4 status, configuration and diagnostics", mcpEndpoint)
			})
		}

		if origin := r.Header.Get("Origin"); origin != "" && !mcpOriginAllowed(origin, r.Host, cfg.System.WebServer.MCP.AllowedOrigins) {
			writeJsonError(w, http.StatusForbidden, "origin not allowed")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func mcpAuthConfigured(cfg *config.Config) bool {
	return cfg.System.WebServer.Username != "" && cfg.System.WebServer.Password != ""
}

func mcpOriginAllowed(origin, host string, allowed []string) bool {
	for _, a := range allowed {
		a = strings.TrimSpace(a)
		if a == "*" || strings.EqualFold(a, origin) {
			return true
		}
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if strings.EqualFold(u.Host, host) {
		return true
	}

	oHost, oPort := splitHostPort(u.Host)
	hHost, hPort := splitHostPort(host)
	if !strings.EqualFold(oHost, hHost) {
		return false
	}
	// The names agree. Accept only when one side left the port implicit; two
	// different explicit ports are two different origins, and a page served
	// from another port on this same machine is not this server.
	return oPort == "" || hPort == ""
}

func splitHostPort(hostport string) (host, port string) {
	if i := strings.LastIndex(hostport, ":"); i > 0 && !strings.Contains(hostport[i:], "]") {
		return hostport[:i], hostport[i+1:]
	}
	return hostport, ""
}

func (api *API) newMCPServer() *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    mcpServerName,
		Title:   "B4 DPI-bypass control plane",
		Version: Version,
	}, &mcp.ServerOptions{
		Instructions: strings.Join([]string{
			"B4 is a Linux DPI-bypass daemon controlled through this server.",
			"Prefer b4_check_domain before changing sets: it tells you whether a domain is already covered and by which set.",
			"Read the b4://topics/<key> resources before explaining or tuning a setting — they are authoritative.",
			"Never infer a setting's unit or default from its name; several are deliberately misleading.",
		}, " "),
	})

	api.addMCPTools(srv)
	api.addMCPWriteTools(srv)
	api.addMCPResources(srv)
	api.addMCPPrompts(srv)
	return srv
}

type mcpEmpty struct{}

type mcpStatusOut struct {
	Version         string `json:"version"`
	Engine          string `json:"engine"`
	FirewallBackend string `json:"firewall_backend"`
	SetsTotal       int    `json:"sets_total"`
	SetsEnabled     int    `json:"sets_enabled"`
	Socks5Enabled   bool   `json:"socks5_enabled"`
	MTProtoOn       bool   `json:"mtproto_enabled"`
	Uptime          string `json:"uptime"`
	ConnectionsSeen int64  `json:"connections_seen"`
}

type mcpConfigIn struct {
	Section string `json:"section,omitempty" jsonschema:"Optional dotted path to return instead of the whole config, e.g. 'system.dns' or 'sets'."`
}

type mcpRawOut struct {
	JSON string `json:"json"`
}

type mcpCheckDomainIn struct {
	Domain string `json:"domain" jsonschema:"Domain to test, e.g. youtube.com. Several may be comma-separated."`
}

type mcpCheckDomainOut struct {
	Matches        []SetDomainMatch `json:"matches"`
	Checked        []string         `json:"checked"`
	Covered        bool             `json:"covered"`
	CoveredByDomain map[string]bool `json:"covered_by_domain"`
	Truncated       bool            `json:"truncated"`
	Note            string          `json:"note,omitempty"`
}

type mcpSetSummary struct {
	Id            string `json:"id"`
	Name          string `json:"name"`
	Enabled       bool   `json:"enabled"`
	Domains       int    `json:"domain_count"`
	ManualDomains int    `json:"manual_domain_count"`
	Strategy      string `json:"strategy,omitempty"`
}

type mcpSetsOut struct {
	Sets      []mcpSetSummary `json:"sets"`
	Truncated bool            `json:"truncated"`
}

type mcpGetSetIn struct {
	Set string `json:"set" jsonschema:"Set id or name, as reported by b4_list_sets."`
}

type mcpRecentConnIn struct {
	Limit    int    `json:"limit,omitempty" jsonschema:"Maximum lines to return, newest last. Default 100, maximum 500."`
	Contains string `json:"contains,omitempty" jsonschema:"Optional case-insensitive substring filter, e.g. a domain or set name."`
}

type mcpRecentConnOut struct {
	Connections []string `json:"connections"`
	Truncated   bool     `json:"truncated"`
}

type mcpLogsIn struct {
	Limit    int    `json:"limit,omitempty" jsonschema:"Maximum lines to return, newest last. Default 100, maximum 500."`
	Contains string `json:"contains,omitempty" jsonschema:"Optional case-insensitive substring filter."`
}

type mcpLogsOut struct {
	Path      string   `json:"path,omitempty"`
	Lines     []string `json:"lines"`
	Matched   int      `json:"matched"`
	Scanned   int      `json:"scanned"`
	Truncated bool     `json:"truncated"`
	Note      string   `json:"note"`
}

type tailResult struct {
	Lines     []string
	Matched   int
	Scanned   int
	Truncated bool
	Exists    bool
}

// mcpClampLimit keeps a caller-supplied line limit inside the documented
// range. An oversized request is clamped to the maximum rather than dropped
// back to the default, which would silently return fewer lines than asked for.
func mcpClampLimit(v int) int {
	switch {
	case v <= 0:
		return mcpDefaultLines
	case v > mcpMaxLines:
		return mcpMaxLines
	default:
		return v
	}
}

func (api *API) addMCPTools(srv *mcp.Server) {
	addTool(srv, &mcp.Tool{
		Name:        "b4_status",
		Title:       "B4 status",
		Description: "High-level health of the running b4 daemon: version, packet capture engine (nfqueue or tun), firewall backend, how many strategy sets exist and are enabled, which subsystems are on, uptime and how many connections b4 has processed. Call this first when diagnosing. 'connections_seen' is a running total since b4 started, not a live concurrency figure.",
		Annotations: mcpReadOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ mcpEmpty) (*mcp.CallToolResult, mcpStatusOut, error) {
		cfg := api.getCfg()
		snap := GetMetricsCollector().GetSnapshot()

		backend := cfg.System.Tables.Engine
		if backend == "" {
			backend = "auto"
		}

		out := mcpStatusOut{
			Version:         Version,
			Engine:          collectEngineInfo(cfg).Mode,
			FirewallBackend: backend,
			SetsTotal:       len(cfg.Sets),
			Socks5Enabled:   cfg.System.Socks5.Enabled,
			MTProtoOn:       cfg.System.MTProto.Enabled,
			Uptime:          snap.Uptime,
			ConnectionsSeen: int64(snap.ActiveFlows),
		}
		for _, s := range cfg.Sets {
			if s.Enabled {
				out.SetsEnabled++
			}
		}
		return nil, out, nil
	})

	addTool(srv, &mcp.Tool{
		Name:        "b4_get_config",
		Title:       "Read b4 configuration",
		Description: "Return the b4 configuration as JSON, with all credentials redacted. Pass 'section' to narrow it (e.g. 'system.dns', 'system.mtproto', 'sets') — the full config is large, so prefer a section.",
		Annotations: mcpReadOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpConfigIn) (*mcp.CallToolResult, mcpRawOut, error) {
		redacted := redactConfigForMCP(api.getCfg())
		raw, err := json.Marshal(redacted)
		if err != nil {
			return nil, mcpRawOut{}, fmt.Errorf("marshal config: %w", err)
		}
		if section := strings.TrimSpace(in.Section); section != "" {
			sub, err := extractJSONPath(raw, section)
			if err != nil {
				return nil, mcpRawOut{}, err
			}
			raw = sub
		}
		return nil, mcpRawOut{JSON: string(raw)}, nil
	})

	addTool(srv, &mcp.Tool{
		Name:        "b4_check_domain",
		Title:       "Check domain coverage",
		Description: "Report which strategy sets already target a domain, how the match was made (exact/wildcard/suffix), whether it came from a manual entry or a geosite category, and whether that set is enabled. Use before adding a domain to avoid duplicates.",
		Annotations: mcpReadOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpCheckDomainIn) (*mcp.CallToolResult, mcpCheckDomainOut, error) {
		domains, truncated := parseCheckDomains(in.Domain)
		if len(domains) == 0 {
			return nil, mcpCheckDomainOut{}, fmt.Errorf("domain is required")
		}
		matches := api.matchDomainsToSets(domains, "")

		byDomain := make(map[string]bool, len(domains))
		for _, d := range domains {
			byDomain[d] = false
		}
		for _, m := range matches {
			if m.Enabled {
				byDomain[m.Domain] = true
			}
		}

		covered := true
		uncovered := make([]string, 0, len(domains))
		for _, d := range domains {
			if !byDomain[d] {
				covered = false
				uncovered = append(uncovered, d)
			}
		}

		out := mcpCheckDomainOut{
			Matches:         matches,
			Checked:         domains,
			Covered:         covered,
			CoveredByDomain: byDomain,
			Truncated:       truncated,
		}
		switch {
		case truncated:
			out.Note = fmt.Sprintf(
				"only the first %d domains were checked; call again with the rest. Not covered so far: %s",
				maxCheckDomains, strings.Join(uncovered, ", "))
		case covered:
			out.Note = "every domain checked is matched by at least one enabled set"
		default:
			out.Note = "not matched by any enabled set: " + strings.Join(uncovered, ", ")
		}
		return nil, out, nil
	})

	addTool(srv, &mcp.Tool{
		Name:        "b4_list_sets",
		Title:       "List strategy sets",
		Description: "Summarise the configured DPI-bypass strategy sets in priority order: id, name, whether enabled, how many domains each targets, and the primary strategy. 'domain_count' is every domain the set matches, including those expanded from geosite categories; 'manual_domain_count' is only the ones listed by hand.",
		Annotations: mcpReadOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ mcpEmpty) (*mcp.CallToolResult, mcpSetsOut, error) {
		cfg := api.getCfg()
		out := mcpSetsOut{Sets: make([]mcpSetSummary, 0, len(cfg.Sets))}
		for i, s := range cfg.Sets {
			if i >= mcpMaxSetsInOut {
				out.Truncated = true
				break
			}
			// DomainsToMatch is the expanded list and already contains
			// SNIDomains; it is empty until the set's targets are loaded.
			total := len(s.Targets.DomainsToMatch)
			if total < len(s.Targets.SNIDomains) {
				total = len(s.Targets.SNIDomains)
			}
			out.Sets = append(out.Sets, mcpSetSummary{
				Id:            s.Id,
				Name:          s.Name,
				Enabled:       s.Enabled,
				Domains:       total,
				ManualDomains: len(s.Targets.SNIDomains),
				Strategy:      s.Fragmentation.Strategy,
			})
		}
		return nil, out, nil
	})

	addTool(srv, &mcp.Tool{
		Name:        "b4_metrics",
		Title:       "Traffic metrics",
		Description: "Live counters from the packet engine: connections/packets per second, dropped RSTs, blocked totals, uptime and memory use. 'total_connections' and 'connections_seen' are running totals since b4 started or since the counters were last reset; b4 does not track when a connection ends, so neither is a count of connections open right now.",
		Annotations: mcpReadOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ mcpEmpty) (*mcp.CallToolResult, mcpRawOut, error) {
		m := GetMetricsCollector().GetSnapshot()
		summary := map[string]any{
			"total_connections": m.TotalConnections,
			"connections_seen":  m.ActiveFlows,
			"current_cps":       m.CurrentCPS,
			"current_pps":       m.CurrentPPS,
			"rst_dropped":       m.RSTDropped,
			"blocked_total":     m.BlockedTotal,
			"uptime":            m.Uptime,
			"memory_percent":    m.MemoryUsage.Percent,
		}
		raw, err := json.Marshal(summary)
		if err != nil {
			return nil, mcpRawOut{}, err
		}
		return nil, mcpRawOut{JSON: string(raw)}, nil
	})

	addTool(srv, &mcp.Tool{
		Name:        "b4_get_set",
		Title:       "Read one strategy set",
		Description: "Return the full configuration of a single strategy set by id or name, with upstream proxy credentials redacted: targets, fragmentation, faking, TCP/UDP options, DNS and routing. Use this instead of b4_get_config when reasoning about one set.",
		Annotations: mcpReadOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpGetSetIn) (*mcp.CallToolResult, mcpRawOut, error) {
		want := strings.TrimSpace(in.Set)
		if want == "" {
			return nil, mcpRawOut{}, fmt.Errorf("set (id or name) is required")
		}
		for _, s := range api.getCfg().Sets {
			if !strings.EqualFold(s.Id, want) && !strings.EqualFold(s.Name, want) {
				continue
			}
			raw, err := marshalSetForMCP(s)
			if err != nil {
				return nil, mcpRawOut{}, err
			}
			return nil, mcpRawOut{JSON: string(raw)}, nil
		}
		return nil, mcpRawOut{}, fmt.Errorf("no set with id or name %q", want)
	})

	addTool(srv, &mcp.Tool{
		Name:        "b4_recent_connections",
		Title:       "Recent connections",
		Description: "Recent connections b4 actually processed, newest last: protocol, matched set, domain, source, destination and TLS version. This is the right tool for any question about a specific domain or site — it shows whether traffic reached b4 and which set matched it, which the error log does not record. Pass the domain in 'contains' to filter. Empty means no matching traffic has been seen since b4 started.",
		Annotations: mcpReadOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpRecentConnIn) (*mcp.CallToolResult, mcpRecentConnOut, error) {
		lines := log.GetConnectionHub().Snapshot()

		if filter := strings.ToLower(strings.TrimSpace(in.Contains)); filter != "" {
			kept := lines[:0:0]
			for _, l := range lines {
				if strings.Contains(strings.ToLower(l), filter) {
					kept = append(kept, l)
				}
			}
			lines = kept
		}

		limit := mcpClampLimit(in.Limit)
		truncated := false
		if len(lines) > limit {
			lines = lines[len(lines)-limit:]
			truncated = true
		}
		return nil, mcpRecentConnOut{Connections: lines, Truncated: truncated}, nil
	})

	addTool(srv, &mcp.Tool{
		Name:        "b4_logs_tail",
		Title:       "Tail the b4 log",
		Description: "Most recent lines from b4's error and system log, newest last. Use after b4_status and b4_diagnostics when something is configured correctly but still not working. This log holds b4's own errors and warnings — it does NOT record per-domain traffic, so do not filter it by a domain name; use b4_recent_connections to find out whether a domain is being matched. The 'note' field always explains the result, including why it is empty.",
		Annotations: mcpReadOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpLogsIn) (*mcp.CallToolResult, mcpLogsOut, error) {
		path := api.getCfg().System.Logging.ErrorFilePath()
		if path == "" {
			return nil, mcpLogsOut{
				Lines: []string{},
				Note:  "file logging is disabled (system.logging.directory is empty), so there is no log to read",
			}, nil
		}

		limit := mcpClampLimit(in.Limit)

		filter := strings.TrimSpace(in.Contains)
		res, err := tailLines(path, limit, filter)
		if err != nil {
			return nil, mcpLogsOut{}, fmt.Errorf("read %s: %w", path, err)
		}

		out := mcpLogsOut{
			Path:      path,
			Lines:     res.Lines,
			Matched:   res.Matched,
			Scanned:   res.Scanned,
			Truncated: res.Truncated,
		}
		switch {
		case !res.Exists:
			out.Note = "the log file does not exist yet — b4 has not written anything to it"
		case res.Scanned == 0:
			out.Note = "the log file exists but is empty"
		case filter != "" && res.Matched == 0:
			out.Note = fmt.Sprintf(
				"no lines matched %q among the %d most recent lines. This log contains b4's own errors, not per-domain traffic — to check whether a domain is being matched, call b4_recent_connections instead.",
				filter, res.Scanned)
		case filter != "" && res.Truncated:
			out.Note = fmt.Sprintf("%d of the %d most recent lines matched %q; the newest %d are returned", res.Matched, res.Scanned, filter, len(res.Lines))
		case filter != "":
			out.Note = fmt.Sprintf("%d of the %d most recent lines matched %q", res.Matched, res.Scanned, filter)
		case res.Truncated:
			out.Note = fmt.Sprintf("the newest %d of %d available lines", len(res.Lines), res.Matched)
		default:
			out.Note = fmt.Sprintf("%d most recent lines", len(res.Lines))
		}
		return nil, out, nil
	})

	addTool(srv, &mcp.Tool{
		Name:        "b4_diagnostics",
		Title:       "System diagnostics",
		Description: "Full environment report: OS and kernel, memory, b4 build and paths, detected firewall backend and the live nftables/iptables rule groups b4 installed, network interfaces, engine and TUN state. Use when the bypass appears not to be applied at all. This report includes the hostname, every interface address and the firewall ruleset, so treat the output as identifying information about the network it came from.",
		Annotations: mcpReadOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ mcpEmpty) (*mcp.CallToolResult, mcpRawOut, error) {
		raw, err := json.Marshal(api.buildDiagnostics())
		if err != nil {
			return nil, mcpRawOut{}, err
		}
		return nil, mcpRawOut{JSON: string(raw)}, nil
	})
}

func (api *API) addMCPResources(srv *mcp.Server) {
	for _, key := range ai.TopicKeys() {
		topic := key
		srv.AddResource(&mcp.Resource{
			URI:         mcpTopicScheme + topic,
			Name:        topic,
			Title:       "b4 setting: " + topic,
			Description: "Authoritative b4-specific behaviour notes for the " + topic + " setting. Trust this over inference from the field name.",
			MIMEType:    "text/plain",
		}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			facts := ai.TopicFacts(topic)
			if facts == "" {
				return nil, fmt.Errorf("no facts for topic %q", topic)
			}
			return &mcp.ReadResourceResult{
				Contents: []*mcp.ResourceContents{{
					URI:      mcpTopicScheme + topic,
					MIMEType: "text/plain",
					Text:     facts,
				}},
			}, nil
		})
	}
}

func (api *API) addMCPPrompts(srv *mcp.Server) {
	srv.AddPrompt(&mcp.Prompt{
		Name:        "diagnose_domain",
		Title:       "Diagnose a blocked domain",
		Description: "Walk through why a specific domain is not working and what to change in b4.",
		Arguments: []*mcp.PromptArgument{{
			Name:        "domain",
			Description: "The domain that is not working, e.g. youtube.com",
			Required:    true,
		}},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		domain := ""
		if req.Params != nil {
			domain = req.Params.Arguments["domain"]
		}
		if strings.TrimSpace(domain) == "" {
			return nil, fmt.Errorf("domain argument is required")
		}
		text := strings.Join([]string{
			fmt.Sprintf("The domain %q is not loading correctly through b4. Diagnose it.", domain),
			"",
			"Work in this order:",
			"1. Call b4_status to confirm the daemon is running and which engine is active.",
			fmt.Sprintf("2. Call b4_check_domain for %q to see whether any enabled set already covers it.", domain),
			"3. If nothing covers it, say so and propose which existing set it belongs in, or a new one.",
			"4. If a set does cover it, call b4_get_config for that set and read the relevant b4://topics/ resources before commenting on any setting.",
			"5. If the bypass looks configured but ineffective, call b4_diagnostics and check the firewall rule groups are actually installed.",
			"",
			"Do not guess a setting's unit or default from its name — read the matching b4://topics/ resource.",
			"State clearly which changes you are recommending and why; do not claim to have applied anything.",
		}, "\n")
		return &mcp.GetPromptResult{
			Description: fmt.Sprintf("Diagnostic plan for %s", domain),
			Messages: []*mcp.PromptMessage{{
				Role:    "user",
				Content: &mcp.TextContent{Text: text},
			}},
		}, nil
	})
}

func redactConfigForMCP(cfg *config.Config) *config.Config {
	clone := cfg.Clone()

	if clone.System.WebServer.Password != "" {
		clone.System.WebServer.PasswordSet = true
		clone.System.WebServer.Password = ""
	}
	clone.System.WebServer.Username = ""
	clone.System.WebServer.TLSKey = ""

	if clone.System.WebServer.MCP.Token != "" {
		clone.System.WebServer.MCP.Token = redactedMarker
	}

	if clone.System.Socks5.Password != "" {
		clone.System.Socks5.Password = redactedMarker
	}
	if clone.System.Socks5.Username != "" {
		clone.System.Socks5.Username = redactedMarker
	}

	for i := range clone.System.MTProto.Secrets {
		clone.System.MTProto.Secrets[i].Secret = redactedMarker
		if clone.System.MTProto.Secrets[i].Name != "" {
			clone.System.MTProto.Secrets[i].Name = redactedMarker
		}
	}

	if clone.System.API.IPInfoToken != "" {
		clone.System.API.IPInfoToken = redactedMarker
	}

	clone.System.AI.APIKeyRef = ""

	for _, s := range clone.Sets {
		redactSetSecrets(s)
	}

	return clone
}

// redactSetSecrets strips the credentials a set carries for its upstream proxy.
// Sets are reachable through b4_get_config and b4_get_set alike, so every path
// that returns one has to go through here.
func redactSetSecrets(set *config.SetConfig) {
	if set == nil {
		return
	}
	if set.Routing.Upstream.Username != "" {
		set.Routing.Upstream.Username = redactedMarker
	}
	if set.Routing.Upstream.Password != "" {
		set.Routing.Upstream.Password = redactedMarker
	}
}

// marshalSetForMCP renders one set as JSON with its credentials stripped,
// without disturbing the live config.
func marshalSetForMCP(set *config.SetConfig) ([]byte, error) {
	raw, err := json.Marshal(set)
	if err != nil {
		return nil, err
	}
	var clone config.SetConfig
	if err := json.Unmarshal(raw, &clone); err != nil {
		return nil, err
	}
	redactSetSecrets(&clone)
	return json.Marshal(&clone)
}

const redactedMarker = "[redacted]"

func tailLines(path string, limit int, contains string) (tailResult, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return tailResult{Lines: []string{}}, nil
		}
		return tailResult{}, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return tailResult{}, err
	}

	size := info.Size()
	offset := int64(0)
	if size > mcpTailReadCap {
		offset = size - mcpTailReadCap
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return tailResult{}, err
	}

	raw, err := io.ReadAll(io.LimitReader(f, mcpTailReadCap))
	if err != nil {
		return tailResult{}, err
	}
	if offset > 0 {
		if i := bytes.IndexByte(raw, '\n'); i >= 0 {
			raw = raw[i+1:]
		}
	}

	filter := strings.ToLower(contains)
	out := make([]string, 0, limit)
	scanned := 0
	matched := 0
	for _, l := range strings.Split(string(raw), "\n") {
		l = strings.TrimRight(l, "\r")
		if l == "" {
			continue
		}
		scanned++
		if filter != "" && !strings.Contains(strings.ToLower(l), filter) {
			continue
		}
		matched++
		out = append(out, l)
	}
	truncated := false
	if len(out) > limit {
		out = out[len(out)-limit:]
		truncated = true
	}
	return tailResult{Lines: out, Matched: matched, Scanned: scanned, Truncated: truncated, Exists: true}, nil
}

func extractJSONPath(raw []byte, path string) ([]byte, error) {
	var cur any
	if err := json.Unmarshal(raw, &cur); err != nil {
		return nil, err
	}
	for _, part := range strings.Split(path, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("section %q: %q is not an object", path, part)
		}
		next, ok := obj[part]
		if !ok {
			return nil, fmt.Errorf("section %q: no such key %q", path, part)
		}
		cur = next
	}
	return json.Marshal(cur)
}
