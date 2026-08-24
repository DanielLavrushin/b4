package handler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpMaxSets = 200

type mcpManageSetIn struct {
	Action   string `json:"action" jsonschema:"One of: create, duplicate, move, set_enabled, delete, reset."`
	Set      string `json:"set,omitempty" jsonschema:"Set id or name to act on. Comma-separated for set_enabled."`
	Name     string `json:"name,omitempty" jsonschema:"Name for the new set, for create and duplicate."`
	Position string `json:"position,omitempty" jsonschema:"Where it lands in match priority: 'last' (default), 'first', 'before:<set>' or 'after:<set>'. First enabled match wins."`
	Enabled  string `json:"enabled,omitempty" jsonschema:"'true' or 'false', for set_enabled."`
	Confirm  string `json:"confirm_name,omitempty" jsonschema:"For delete and reset: the exact current name of the set, as a guard against acting on the wrong one."`
}

type mcpSetRow struct {
	Position int    `json:"position"`
	Id       string `json:"id"`
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
}

type mcpManageSetOut struct {
	Action    string      `json:"action"`
	Set       *mcpSetRow  `json:"set,omitempty"`
	Order     []mcpSetRow `json:"order,omitempty"`
	TotalSets int         `json:"total_sets"`
	Changed   bool        `json:"changed"`
	Note      string      `json:"note"`
}

func mcpSetRows(cfg *config.Config) []mcpSetRow {
	rows := make([]mcpSetRow, 0, len(cfg.Sets))
	for i, s := range cfg.Sets {
		rows = append(rows, mcpSetRow{Position: i + 1, Id: s.Id, Name: s.Name, Enabled: s.Enabled})
	}
	return rows
}

func mcpFindSetIndex(cfg *config.Config, ref string) int {
	for i, s := range cfg.Sets {
		if strings.EqualFold(s.Id, ref) || strings.EqualFold(s.Name, ref) {
			return i
		}
	}
	return -1
}

func mcpMatchingSetIndexes(cfg *config.Config, ref string) []int {
	var out []int
	for i, s := range cfg.Sets {
		if strings.EqualFold(s.Id, ref) {
			return []int{i}
		}
		if strings.EqualFold(s.Name, ref) {
			out = append(out, i)
		}
	}
	return out
}

func mcpNameTaken(cfg *config.Config, name string) bool {
	for _, s := range cfg.Sets {
		if strings.EqualFold(s.Name, name) {
			return true
		}
	}
	return false
}

func mcpPlaceSet(cfg *config.Config, from int, position string) (int, error) {
	set := cfg.Sets[from]
	rest := make([]*config.SetConfig, 0, len(cfg.Sets)-1)
	rest = append(rest, cfg.Sets[:from]...)
	rest = append(rest, cfg.Sets[from+1:]...)

	spec := strings.ToLower(strings.TrimSpace(position))
	if spec == "" {
		spec = "last"
	}

	insert := len(rest)
	switch {
	case spec == "last":
	case spec == "first":
		insert = 0
	case strings.HasPrefix(spec, "before:"), strings.HasPrefix(spec, "after:"):
		ref := strings.TrimSpace(position[strings.Index(position, ":")+1:])
		if ref == "" {
			return -1, fmt.Errorf("position %q names no set", position)
		}
		anchor := -1
		for i, s := range rest {
			if strings.EqualFold(s.Id, ref) || strings.EqualFold(s.Name, ref) {
				anchor = i
				break
			}
		}
		if anchor < 0 {
			return -1, fmt.Errorf("position %q: no other set with id or name %q", position, ref)
		}
		insert = anchor
		if strings.HasPrefix(spec, "after:") {
			insert = anchor + 1
		}
	default:
		return -1, fmt.Errorf("unknown position %q: expected first, last, before:<set> or after:<set>", position)
	}

	out := make([]*config.SetConfig, 0, len(cfg.Sets))
	out = append(out, rest[:insert]...)
	out = append(out, set)
	out = append(out, rest[insert:]...)
	cfg.Sets = out
	return insert, nil
}

func mcpSetHasProtectedSecrets(set *config.SetConfig) []string {
	var held []string
	if set.Routing.Upstream.Username != "" || set.Routing.Upstream.Password != "" {
		held = append(held, "upstream proxy credentials")
	}
	if len(set.DNS.Pins) > 0 {
		held = append(held, fmt.Sprintf("%d DNS pin(s)", len(set.DNS.Pins)))
	}
	return held
}

func mcpEscalationsPointingAt(cfg *config.Config, id string) []string {
	var from []string
	for _, s := range cfg.Sets {
		if s.Id != id && s.Escalate.To == id {
			from = append(from, s.Name)
		}
	}
	return from
}

func (api *API) addMCPSetTools(srv *mcp.Server) {
	addTool(srv, &mcp.Tool{
		Name:  "b4_manage_set",
		Title: "Create, move, enable or remove a strategy set",
		Description: "Set lifecycle: create, duplicate, move, set_enabled, delete, reset. " +
			"List position IS match priority and the first enabled match wins; new sets go last unless 'position' says otherwise. " +
			"delete and reset need confirm_name to equal the set's current name, and are refused on a set holding upstream credentials or DNS pins. " +
			"Targets belong to b4_edit_set_targets, settings to b4_set_config_value.",
		Annotations: mcpDestructive,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpManageSetIn) (*mcp.CallToolResult, mcpManageSetOut, error) {
		if !api.getCfg().System.WebServer.MCP.AllowWrites {
			return nil, mcpManageSetOut{}, fmt.Errorf(
				"configuration writes are disabled: turn on 'Allow configuration changes' under Settings -> API -> MCP server to permit them")
		}

		action := strings.ToLower(strings.TrimSpace(in.Action))
		if action == "" {
			return nil, mcpManageSetOut{}, fmt.Errorf("action is required: create, duplicate, move, set_enabled, delete or reset")
		}

		mcpWriteMu.Lock()
		defer mcpWriteMu.Unlock()

		oldCfg := api.getCfg()
		newCfg := oldCfg.Clone()
		out := mcpManageSetOut{Action: action}

		var focusID string
		var routingDropped bool
		var copiedFrom string
		var copiedDomains, copiedIPs int

		switch action {
		case "create", "duplicate":
			if len(newCfg.Sets) >= mcpMaxSets {
				return nil, mcpManageSetOut{}, fmt.Errorf("there are already %d sets; b4 will not add more from here", len(newCfg.Sets))
			}
			name := strings.TrimSpace(in.Name)

			var fresh config.SetConfig
			if action == "duplicate" {
				src := findSetIn(newCfg, strings.TrimSpace(in.Set))
				if src == nil {
					return nil, mcpManageSetOut{}, fmt.Errorf("no set with id or name %q to duplicate", in.Set)
				}
				clone, err := redactedSetForMCP(src)
				if err != nil {
					return nil, mcpManageSetOut{}, err
				}
				fresh = *clone
				fresh.Routing.Upstream.Username = ""
				fresh.Routing.Upstream.Password = ""
				fresh.DNS.Pins = nil
				routingDropped = fresh.Routing.Enabled
				fresh.Routing = config.RoutingConfig{}
				api.initializeSetDefaults(&fresh)
				copiedFrom = src.Name
				copiedDomains = len(fresh.Targets.SNIDomains)
				copiedIPs = len(fresh.Targets.IPs)
				if name == "" {
					name = src.Name + " copy"
					for i := 2; mcpNameTaken(newCfg, name); i++ {
						name = fmt.Sprintf("%s copy %d", src.Name, i)
					}
				}
			} else {
				fresh = config.NewSetConfig()
				fresh.Enabled = true
				if name == "" {
					name = fmt.Sprintf("Set %d", len(newCfg.Sets)+1)
				}
			}
			if mcpNameTaken(newCfg, name) {
				return nil, mcpManageSetOut{}, fmt.Errorf(
					"a set named %q already exists; pass a different name. Two sets sharing a name make every later reference to it ambiguous", name)
			}
			fresh.Id = uuid.New().String()
			fresh.Name = name
			api.initializeSetDefaults(&fresh)

			newCfg.Sets = append(newCfg.Sets, &fresh)
			if _, err := mcpPlaceSet(newCfg, len(newCfg.Sets)-1, in.Position); err != nil {
				return nil, mcpManageSetOut{}, err
			}
			api.loadTargetsForSetCached(&fresh)
			focusID = fresh.Id

		case "move":
			idx := mcpFindSetIndex(newCfg, strings.TrimSpace(in.Set))
			if idx < 0 {
				return nil, mcpManageSetOut{}, fmt.Errorf("no set with id or name %q", in.Set)
			}
			if strings.TrimSpace(in.Position) == "" {
				return nil, mcpManageSetOut{}, fmt.Errorf("position is required for action=move: first, last, before:<set> or after:<set>")
			}
			focusID = newCfg.Sets[idx].Id
			if _, err := mcpPlaceSet(newCfg, idx, in.Position); err != nil {
				return nil, mcpManageSetOut{}, err
			}

		case "set_enabled":
			want, err := mcpParseBool(in.Enabled)
			if err != nil {
				return nil, mcpManageSetOut{}, err
			}
			refs := mcpSplitEntries(in.Set)
			if len(refs) == 0 {
				return nil, mcpManageSetOut{}, fmt.Errorf("set is required for action=set_enabled")
			}
			touched := 0
			for _, ref := range refs {
				s := findSetIn(newCfg, ref)
				if s == nil {
					return nil, mcpManageSetOut{}, fmt.Errorf("no set with id or name %q", ref)
				}
				if s.Enabled != want {
					s.Enabled = want
					touched++
				}
				focusID = s.Id
			}
			if touched == 0 {
				out.TotalSets = len(newCfg.Sets)
				out.Order = mcpSetRows(newCfg)
				out.Note = fmt.Sprintf("every named set is already %s", map[bool]string{true: "enabled", false: "disabled"}[want])
				return nil, out, nil
			}

		case "delete", "reset":
			ref := strings.TrimSpace(in.Set)
			matches := mcpMatchingSetIndexes(newCfg, ref)
			if len(matches) == 0 {
				return nil, mcpManageSetOut{}, fmt.Errorf("no set with id or name %q", ref)
			}
			if len(matches) > 1 {
				ids := make([]string, 0, len(matches))
				for _, i := range matches {
					ids = append(ids, newCfg.Sets[i].Id)
				}
				return nil, mcpManageSetOut{}, fmt.Errorf(
					"%d sets are named %q, so %s would act on the wrong one. Pass the id instead: %s",
					len(matches), ref, action, strings.Join(ids, ", "))
			}
			idx := matches[0]
			target := newCfg.Sets[idx]
			if strings.TrimSpace(in.Confirm) != target.Name {
				return nil, mcpManageSetOut{}, fmt.Errorf(
					"%s needs confirm_name to equal the set's current name: pass confirm_name=%q", action, target.Name)
			}
			if held := mcpSetHasProtectedSecrets(target); len(held) > 0 {
				return nil, mcpManageSetOut{}, fmt.Errorf(
					"refusing to %s %q: it holds %s, which this tool cannot read back or restore. Do it in the web interface",
					action, target.Name, strings.Join(held, " and "))
			}
			if action == "delete" {
				if linked := mcpEscalationsPointingAt(newCfg, target.Id); len(linked) > 0 {
					return nil, mcpManageSetOut{}, fmt.Errorf(
						"refusing to delete %q: %s escalate to it, and b4 clears a dangling escalate.to silently on the next save. Repoint them first",
						target.Name, strings.Join(linked, ", "))
				}
				newCfg.Sets = append(newCfg.Sets[:idx], newCfg.Sets[idx+1:]...)
			} else {
				wasEnabled := target.Enabled
				target.ResetToDefaults()
				target.Enabled = wasEnabled
				api.loadTargetsForSetCached(target)
				focusID = target.Id
			}

		default:
			return nil, mcpManageSetOut{}, fmt.Errorf("unknown action %q: expected create, duplicate, move, set_enabled, delete or reset", action)
		}

		if err := mcpValidateCandidate(oldCfg, newCfg); err != nil {
			return nil, mcpManageSetOut{}, fmt.Errorf("rejected: %w", err)
		}

		snapshot := oldCfg.Clone()
		if err := api.saveAndPushConfig(newCfg); err != nil {
			return nil, mcpManageSetOut{}, fmt.Errorf("rejected: %w", err)
		}
		api.applyRuntimeChanges(newCfg, oldCfg)
		refreshed := api.PerformSoftRestart(newCfg, oldCfg)

		live := api.getCfg()
		out.TotalSets = len(live.Sets)
		out.Order = mcpSetRows(live)
		out.Changed = true

		if focusID != "" {
			for i, s := range live.Sets {
				if s.Id == focusID {
					out.Set = &mcpSetRow{Position: i + 1, Id: s.Id, Name: s.Name, Enabled: s.Enabled}
					break
				}
			}
		}

		mcpRecordChange(mcpChange{
			Path:     "sets",
			Previous: fmt.Sprintf("%d sets", len(oldCfg.Sets)),
			Current:  fmt.Sprintf("%d sets", len(live.Sets)),
			When:     time.Now(), Snapshot: snapshot,
		})
		log.Infof("mcp: sets %s (now %d sets)", action, len(live.Sets))

		out.Note = fmt.Sprintf("applied live; %d set(s) configured. Undo with b4_revert_last_change", len(live.Sets))
		if refreshed {
			out.Note += " (firewall rules were refreshed to match)"
		}
		if out.Set != nil {
			out.Note += fmt.Sprintf(". %q is at position %d of %d — earlier sets match first",
				out.Set.Name, out.Set.Position, out.TotalSets)
			switch action {
			case "create":
				out.Note += ". It targets nothing yet, so it matches nothing: add targets with b4_edit_set_targets"
			case "duplicate":
				out.Note += fmt.Sprintf(". It copied %d domain(s) and %d address(es) from %q, so both sets claim them and whichever sits earlier wins",
					copiedDomains, copiedIPs, copiedFrom)
				if routingDropped {
					out.Note += ". Routing was NOT copied: the upstream credentials cannot be read here, and a copy pointing at a proxy without them would misroute"
				}
			}
		}
		if action == "reset" {
			out.Note += ". Targets and the enabled switch were kept; every strategy setting is back to its default"
		}
		return nil, out, nil
	})
}

func mcpParseBool(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "yes", "on", "1":
		return true, nil
	case "false", "no", "off", "0":
		return false, nil
	}
	return false, fmt.Errorf("enabled must be true or false, got %q", raw)
}
