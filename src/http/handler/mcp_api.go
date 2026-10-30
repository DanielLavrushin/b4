package handler

import (
	"context"
	"fmt"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/http/apispec"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	mcpAPISchemaBudget = 6000
	mcpAPIWritesSwitch = "'Allow configuration changes'"
	mcpAPIProbesSwitch = "'Allow active probes'"
)

var mcpAPILoad = apispec.Load

type mcpAPIHint struct {
	tool  string
	scope string
}

func (h mcpAPIHint) String() string {
	if h.scope == "" {
		return h.tool
	}
	return h.tool + ", " + h.scope
}

var mcpAPIPreferredTools = map[string]mcpAPIHint{
	"GET /api/config":                       {tool: "b4_get_config"},
	"PUT /api/config":                       {tool: "b4_set_config_value", scope: "one setting per call"},
	"GET /api/sets":                         {tool: "b4_list_sets"},
	"POST /api/sets":                        {tool: "b4_manage_set"},
	"GET /api/sets/{id}":                    {tool: "b4_get_set"},
	"PUT /api/sets/{id}":                    {tool: "b4_set_config_value", scope: "one setting per call"},
	"DELETE /api/sets/{id}":                 {tool: "b4_manage_set"},
	"POST /api/sets/batch-delete":           {tool: "b4_manage_set"},
	"POST /api/sets/batch-set-enabled":      {tool: "b4_manage_set"},
	"POST /api/sets/reorder":                {tool: "b4_manage_set"},
	"GET /api/sets/check-domain":            {tool: "b4_check_domain"},
	"POST /api/sets/{id}/add-domain":        {tool: "b4_edit_set_targets"},
	"PUT /api/geosite/domain":               {tool: "b4_edit_set_targets"},
	"PUT /api/geoip":                        {tool: "b4_edit_set_targets"},
	"GET /api/geosite":                      {tool: "b4_geo_lookup"},
	"GET /api/geosite/category":             {tool: "b4_geo_lookup"},
	"GET /api/geoip":                        {tool: "b4_geo_lookup"},
	"GET /api/metrics":                      {tool: "b4_metrics"},
	"GET /api/metrics/summary":              {tool: "b4_metrics"},
	"GET /api/system/diagnostics":           {tool: "b4_diagnostics"},
	"POST /api/discovery/start":             {tool: "b4_find_bypass_strategy", scope: "it also creates a set from the winner"},
	"GET /api/watchdog/status":              {tool: "b4_watchdog"},
	"POST /api/watchdog/enable":             {tool: "b4_watchdog"},
	"POST /api/watchdog/disable":            {tool: "b4_watchdog"},
	"POST /api/watchdog/check":              {tool: "b4_watchdog"},
	"POST /api/watchdog/domains":            {tool: "b4_watchdog"},
	"DELETE /api/watchdog/domains/{domain}": {tool: "b4_watchdog"},
	"PUT /api/watchdog/sets/{id}":           {tool: "b4_watchdog"},
	"POST /api/watchdog/sets/{id}/check":    {tool: "b4_watchdog"},
}

type mcpAPIIn struct {
	Operation string `json:"operation,omitempty" jsonschema:"One operation as 'METHOD /api/path', e.g. 'GET /api/sets'."`
	Query     string `json:"query,omitempty" jsonschema:"Words that must all appear in an operation's path, summary or description."`
	Schema    string `json:"schema,omitempty" jsonschema:"A type named in an earlier answer, e.g. 'config.SetConfig'."`
}

type mcpAPIOut struct {
	APIVersion  string              `json:"api_version"`
	B4Version   string              `json:"b4_version"`
	Count       int                 `json:"count,omitempty"`
	Operations  map[string][]string `json:"operations,omitempty"`
	Operation   *apispec.Operation  `json:"operation,omitempty"`
	PreferTool  string              `json:"prefer_tool,omitempty"`
	Schemas     map[string]any      `json:"schemas,omitempty"`
	MoreSchemas []string            `json:"more_schemas,omitempty"`
	Note        string              `json:"note"`
}

func (api *API) addMCPAPITools(srv *mcp.Server) {
	addTool(srv, &mcp.Tool{
		Name:  "b4_api_reference",
		Title: "Look up b4's REST API",
		Description: "b4's REST API, from the OpenAPI spec built into this b4, for work the b4_* tools do not cover. " +
			"No argument lists every operation; query searches them; operation ('PUT /api/sets/{id}') gives its parameters, body and responses; schema expands a type from an earlier answer. " +
			"Give at most one. It describes the API and never calls it.",
		Annotations: mcpReadOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpAPIIn) (*mcp.CallToolResult, any, error) {
		operation := strings.TrimSpace(in.Operation)
		query := strings.TrimSpace(in.Query)
		schema := strings.TrimSpace(in.Schema)
		given := 0
		for _, v := range []string{operation, query, schema} {
			if v != "" {
				given++
			}
		}
		if given > 1 {
			return nil, nil, fmt.Errorf("give at most one of operation, query or schema")
		}

		spec, err := mcpAPILoad()
		if err != nil {
			return nil, nil, err
		}
		cfg := api.getCfg()
		out := mcpAPIOut{APIVersion: spec.Version(), B4Version: Version}

		switch {
		case operation != "":
			op, err := spec.Find(operation)
			if err != nil {
				return nil, nil, fmt.Errorf("%w. Call again with query=<words> to search, or with no argument for the full list", err)
			}
			out.Operation = &op
			out.Schemas, out.MoreSchemas = spec.Schemas(op.Types(), mcpAPISchemaBudget)
			var note strings.Builder
			if hint, ok := mcpAPIPreferredTools[op.Method+" "+op.Path]; ok {
				out.PreferTool = hint.String()
				note.WriteString("Prefer " + hint.tool)
				if hint.scope != "" {
					note.WriteString(" (" + hint.scope + ")")
				}
				note.WriteString(" where it covers what you need: it goes through MCP and its checks. ")
				if gate := mcpAPISwitchOff(hint.tool, cfg); gate != "" {
					fmt.Fprintf(&note, "%s is not offered right now: it needs %s, switched on %s. %s. ", hint.tool, gate, mcpSwitchWhere, mcpSwitchReconnect)
				}
			}
			note.WriteString(mcpAPISchemaNote(out.Schemas, out.MoreSchemas))
			note.WriteString(mcpAPIRESTNote(cfg))
			out.Note = note.String()
		case schema != "":
			name, err := spec.SchemaName(schema)
			if err != nil {
				return nil, nil, fmt.Errorf("%w. Type names appear in an operation's parameters and responses", err)
			}
			out.Schemas, out.MoreSchemas = spec.Schemas([]string{name}, mcpAPISchemaBudget)
			out.Note = strings.TrimSpace(mcpAPISchemaNote(out.Schemas, out.MoreSchemas))
		default:
			ops := spec.Operations()
			if query != "" {
				ops = spec.Search(query)
			}
			out.Count = len(ops)
			out.Operations = mcpAPIIndex(ops)
			switch {
			case query == "":
				out.Note = fmt.Sprintf("%d operations, by tag. Call again with operation='METHOD /api/path' for one in full, or query=<words> to search. ", len(ops))
			case len(ops) == 0:
				out.Note = fmt.Sprintf("no operation matches %q: every word has to appear. Call again with fewer words, or with no argument for the full list. ", query)
			default:
				out.Note = fmt.Sprintf("%d operations match %q. Call again with operation='METHOD /api/path' for one in full. ", len(ops), query)
			}
			if mcpAPIAnySwitchOff(ops, cfg) {
				out.Note += "A b4_* tool named for an operation but missing from your tools is switched off " + mcpSwitchWhere + ". " + mcpSwitchReconnect + ". "
			}
			out.Note += mcpAPIRESTNote(cfg)
		}
		out.Note += mcpAPIVersionNote(out.APIVersion, out.B4Version)
		return nil, out, nil
	})
}

func mcpAPIIndex(ops []apispec.Operation) map[string][]string {
	out := map[string][]string{}
	for _, op := range ops {
		tag := "Other"
		if len(op.Tags) > 0 && op.Tags[0] != "" {
			tag = op.Tags[0]
		}
		line := op.Method + " " + op.Path
		if op.Summary != "" {
			line += " - " + op.Summary
		}
		if hint, ok := mcpAPIPreferredTools[op.Method+" "+op.Path]; ok {
			line += " (prefer " + hint.String() + ")"
		}
		out[tag] = append(out[tag], line)
	}
	return out
}

func mcpAPIToolNeeds(tool string) string {
	switch tool {
	case "b4_set_config_value", "b4_manage_set", "b4_edit_set_targets":
		return mcpAPIWritesSwitch
	case "b4_find_bypass_strategy":
		return mcpAPIProbesSwitch
	}
	return ""
}

func mcpAPISwitchOff(tool string, cfg *config.Config) string {
	switches := cfg.System.WebServer.MCP
	switch need := mcpAPIToolNeeds(tool); {
	case need == mcpAPIWritesSwitch && !switches.AllowWrites:
		return need
	case need == mcpAPIProbesSwitch && !switches.AllowActiveProbes:
		return need
	}
	return ""
}

func mcpAPIAnySwitchOff(ops []apispec.Operation, cfg *config.Config) bool {
	for _, op := range ops {
		if hint, ok := mcpAPIPreferredTools[op.Method+" "+op.Path]; ok && mcpAPISwitchOff(hint.tool, cfg) != "" {
			return true
		}
	}
	return false
}

func mcpAPISchemaNote(schemas map[string]any, more []string) string {
	var note strings.Builder
	if len(schemas) > 0 {
		note.WriteString("Fields read name: type, then b4's description where the spec has one. ")
	}
	settings := false
	for name := range schemas {
		settings = settings || strings.HasPrefix(name, "config.")
	}
	for _, name := range more {
		settings = settings || strings.HasPrefix(name, "config.")
	}
	if settings {
		note.WriteString("These field names do not tell what a setting means, its unit or its real default: read b4_get_topic for that, and the current values with b4_get_config or b4_get_set. ")
	}
	if len(more) > 0 {
		note.WriteString("Types in more_schemas were left out for size: call again with schema=<name> for any of them. ")
	}
	return note.String()
}

func mcpAPIRESTNote(cfg *config.Config) string {
	note := "REST paths sit beside /api/mcp on the same address. "
	if mcpAuthConfigured(cfg) {
		note += "Calls need the header 'Authorization: Bearer <token>'. POST /api/auth/login with {\"username\", \"password\"} of b4's web interface returns {\"token\"}: ask the user before using them, and do not retry a refused login, as failures are rate limited. " +
			"b4 keeps these tokens in memory, so a b4 restart ends them, and it refuses the MCP token outside /api/mcp. "
	} else {
		note += "b4's web login is off, so REST calls need no token. "
	}
	return note + "A REST write gets b4's own validation but not the extra checks and refusals of the b4_* tools, so prefer the b4_* tool named for an operation."
}

func mcpAPIVersionNote(apiVersion, running string) string {
	switch running {
	case "", "dev", "1.0.0":
		return ""
	}
	if apiVersion == "" || apiVersion == running {
		return ""
	}
	return fmt.Sprintf(" This reference was built for b4 %s and this b4 is %s: an operation added or changed in between may be missing or differ.", apiVersion, running)
}
