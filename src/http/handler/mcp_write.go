package handler

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var mcpWriteMu sync.Mutex

var mcpDestructive = &mcp.ToolAnnotations{
	ReadOnlyHint:    false,
	DestructiveHint: boolPtr(true),
	IdempotentHint:  true,
	OpenWorldHint:   boolPtr(false),
}

var mcpFragmentationStrategies = []string{
	"combo", "hybrid", "tcp", "ip", "tls", "oob", "disorder", "extsplit", "firstbyte", "none",
}

type mcpWritableField struct {
	Type   string
	Enum   []string
	PerSet bool
	Get    func(*config.Config, *config.SetConfig) string
	Set    func(*config.Config, *config.SetConfig, string) error
	// Check runs against the candidate config after Set and before anything is
	// saved, for preconditions that config.Validate does not cover.
	Check func(*config.Config, *config.SetConfig) error
}

var mcpWritableFields = map[string]mcpWritableField{
	"system.mtproto.enabled": {
		Type: "bool",
		Get: func(c *config.Config, _ *config.SetConfig) string {
			return strconv.FormatBool(c.System.MTProto.Enabled)
		},
		Set: func(c *config.Config, _ *config.SetConfig, v string) error {
			b, err := strconv.ParseBool(v)
			if err != nil {
				return err
			}
			c.System.MTProto.Enabled = b
			return nil
		},
		Check: func(c *config.Config, _ *config.SetConfig) error {
			mt := c.System.MTProto
			if mt.Enabled && len(mt.EffectiveSecrets()) == 0 && mt.FakeSNI == "" {
				return fmt.Errorf(
					"MTProto needs at least one secret or a fake SNI domain before it can be enabled; add one under Settings first")
			}
			return nil
		},
	},
	"system.socks5.enabled": {
		Type: "bool",
		Get:  func(c *config.Config, _ *config.SetConfig) string { return strconv.FormatBool(c.System.Socks5.Enabled) },
		Set: func(c *config.Config, _ *config.SetConfig, v string) error {
			b, err := strconv.ParseBool(v)
			if err != nil {
				return err
			}
			c.System.Socks5.Enabled = b
			return nil
		},
	},
	"sets[].enabled": {
		Type:   "bool",
		PerSet: true,
		Get:    func(_ *config.Config, s *config.SetConfig) string { return strconv.FormatBool(s.Enabled) },
		Set: func(_ *config.Config, s *config.SetConfig, v string) error {
			b, err := strconv.ParseBool(v)
			if err != nil {
				return err
			}
			s.Enabled = b
			return nil
		},
	},
	"sets[].fragmentation.strategy": {
		Type:   "string",
		Enum:   mcpFragmentationStrategies,
		PerSet: true,
		Get:    func(_ *config.Config, s *config.SetConfig) string { return s.Fragmentation.Strategy },
		Set: func(_ *config.Config, s *config.SetConfig, v string) error {
			s.Fragmentation.Strategy = v
			return nil
		},
	},
}

func mcpWritablePathList() []string {
	keys := make([]string, 0, len(mcpWritableFields))
	for k := range mcpWritableFields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func mcpWriteToolDescription() string {
	var sb strings.Builder
	sb.WriteString("Change one allow-listed b4 setting and apply it live (no restart). ")
	sb.WriteString("Only these paths are writable; anything else is refused, including every credential:\n")
	for _, k := range mcpWritablePathList() {
		f := mcpWritableFields[k]
		sb.WriteString("  - ")
		sb.WriteString(k)
		sb.WriteString(" (")
		sb.WriteString(f.Type)
		if len(f.Enum) > 0 {
			sb.WriteString(": ")
			sb.WriteString(strings.Join(f.Enum, ", "))
		}
		sb.WriteString(")\n")
	}
	sb.WriteString("For a per-set path put the set id or name in the brackets, e.g. sets[video].enabled. ")
	sb.WriteString("Requires system.web_server.mcp.allow_writes to be enabled. ")
	sb.WriteString("Confirm the change with the user before calling; report the returned previous/current values.")
	return sb.String()
}

func parseMCPWritePath(path string) (key, setRef string, err error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", "", fmt.Errorf("path is required")
	}
	open := strings.Index(path, "[")
	if open < 0 {
		return path, "", nil
	}
	closing := strings.Index(path, "]")
	if closing < open {
		return "", "", fmt.Errorf("malformed path %q: expected a closing ']'", path)
	}
	setRef = strings.TrimSpace(path[open+1 : closing])
	if setRef == "" {
		return "", "", fmt.Errorf("malformed path %q: put a set id or name in the brackets", path)
	}
	return path[:open+1] + "]" + path[closing+1:], setRef, nil
}

func mcpCoerceValue(f mcpWritableField, raw string) (string, error) {
	v := strings.TrimSpace(raw)
	switch f.Type {
	case "bool":
		b, err := strconv.ParseBool(v)
		if err != nil {
			return "", fmt.Errorf("value %q is not a boolean: use true or false", raw)
		}
		return strconv.FormatBool(b), nil
	case "string":
		if len(f.Enum) == 0 {
			return v, nil
		}
		for _, opt := range f.Enum {
			if strings.EqualFold(opt, v) {
				return opt, nil
			}
		}
		return "", fmt.Errorf("value %q is not allowed: expected one of %s", raw, strings.Join(f.Enum, ", "))
	default:
		return "", fmt.Errorf("unsupported field type %q", f.Type)
	}
}

func findSetIn(cfg *config.Config, ref string) *config.SetConfig {
	for _, s := range cfg.Sets {
		if strings.EqualFold(s.Id, ref) || strings.EqualFold(s.Name, ref) {
			return s
		}
	}
	return nil
}

type mcpSetValueIn struct {
	Path  string `json:"path" jsonschema:"Allow-listed setting to change, e.g. system.mtproto.enabled or sets[video].enabled. See the tool description for the full list."`
	Value string `json:"value" jsonschema:"New value as a string: 'true' or 'false' for booleans, or one of the listed options for enums."`
}

type mcpSetValueOut struct {
	Path     string `json:"path"`
	Previous string `json:"previous"`
	Current  string `json:"current"`
	Changed  bool   `json:"changed"`
	Note     string `json:"note,omitempty"`
}

func (api *API) addMCPWriteTools(srv *mcp.Server) {
	addTool(srv, &mcp.Tool{
		Name:        "b4_set_config_value",
		Title:       "Change a b4 setting",
		Description: mcpWriteToolDescription(),
		Annotations: mcpDestructive,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpSetValueIn) (*mcp.CallToolResult, mcpSetValueOut, error) {
		if !api.getCfg().System.WebServer.MCP.AllowWrites {
			return nil, mcpSetValueOut{}, fmt.Errorf(
				"configuration writes are disabled: set system.web_server.mcp.allow_writes to true to permit them")
		}

		key, setRef, err := parseMCPWritePath(in.Path)
		if err != nil {
			return nil, mcpSetValueOut{}, err
		}

		field, ok := mcpWritableFields[key]
		if !ok {
			return nil, mcpSetValueOut{}, fmt.Errorf(
				"path %q is not writable. Writable paths: %s", in.Path, strings.Join(mcpWritablePathList(), ", "))
		}

		value, err := mcpCoerceValue(field, in.Value)
		if err != nil {
			return nil, mcpSetValueOut{}, err
		}

		mcpWriteMu.Lock()
		defer mcpWriteMu.Unlock()

		oldCfg := api.getCfg()
		newCfg := oldCfg.Clone()

		var target *config.SetConfig
		if field.PerSet {
			if target = findSetIn(newCfg, setRef); target == nil {
				return nil, mcpSetValueOut{}, fmt.Errorf("no set with id or name %q", setRef)
			}
		}

		previous := field.Get(newCfg, target)
		if previous == value {
			return nil, mcpSetValueOut{
				Path: in.Path, Previous: previous, Current: previous, Changed: false,
				Note: "already set to this value; nothing was written",
			}, nil
		}

		if err := field.Set(newCfg, target, value); err != nil {
			return nil, mcpSetValueOut{}, err
		}

		if field.Check != nil {
			if err := field.Check(newCfg, target); err != nil {
				return nil, mcpSetValueOut{}, fmt.Errorf("rejected: %w", err)
			}
		}

		if err := api.saveAndPushConfig(newCfg); err != nil {
			return nil, mcpSetValueOut{}, fmt.Errorf("rejected: %w", err)
		}

		// Saving pushes the new config to the running subsystems, but the
		// firewall rules are derived from it separately: enabling a set can
		// change the ports b4 has to intercept or its MSS clamping, and none of
		// that reaches nftables/iptables without this call. Every REST write
		// path does the same thing.
		refreshed := api.PerformSoftRestart(newCfg, oldCfg)

		log.Infof("mcp: %s changed from %s to %s", in.Path, previous, value)

		note := "applied live; no restart required"
		if refreshed {
			note = "applied live; firewall rules were refreshed to match"
		}

		return nil, mcpSetValueOut{
			Path: in.Path, Previous: previous, Current: value, Changed: true,
			Note: note,
		}, nil
	})
}
