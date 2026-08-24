package handler

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

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

// mcpSetPathPrefix is the canonical form of a per-set path, with the set
// reference removed. Callers write sets[video].enabled; the allow-list, the
// enum table and b4_list_writable_paths all speak sets[].enabled.
const mcpSetPathPrefix = "sets[]"

// mcpWritableRoots are the only subtrees b4_set_config_value can reach. This
// is deliberately an allow-list rather than a deny-list: a config field added
// later is unreachable until someone puts its subtree here on purpose.
//
// Everything outside these roots is refused because a wrong value there can
// leave the machine unreachable with no way to undo it: system.web_server.*
// moves or locks the web interface, system.web_server.mcp.* would let the
// model widen its own permissions, queue.mode and queue.tun.* switch the
// capture engine underneath a live network, system.tables.* can stop the
// firewall rules being installed at all, and the packet marks are load-bearing
// for b4's own traffic.
var mcpWritableRoots = []string{
	mcpSetPathPrefix,
	"system.mtproto",
	"system.socks5",
	"system.logging",
}

// mcpEnums lists the accepted values for string fields whose set of options is
// not enforced by config.Validate. Keyed by canonical path.
var mcpEnums = map[string][]string{
	mcpSetPathPrefix + ".fragmentation.strategy": {
		"combo", "hybrid", "tcp", "ip", "tls", "oob", "disorder", "extsplit", "firstbyte", "none",
	},
	mcpSetPathPrefix + ".tcp.win.mode":          {"off", "oscillate", "zero", "random", "escalate"},
	mcpSetPathPrefix + ".tcp.desync.mode":       {"off", "rst", "fin", "ack", "combo", "full"},
	mcpSetPathPrefix + ".tcp.incoming.mode":     {"off", "fake", "reset", "fin", "desync"},
	mcpSetPathPrefix + ".tcp.incoming.strategy": {"badsum", "badseq", "badack", "rand", "all"},
	mcpSetPathPrefix + ".faking.sni_mutation.mode": {
		"off", "duplicate", "grease", "padding", "reorder", "full",
	},
	mcpSetPathPrefix + ".faking.fake_len_mode": {"", "match"},
	mcpSetPathPrefix + ".routing.mode": {
		config.RoutingModeInterface, config.RoutingModeProxy, config.RoutingModeMTProtoWS, config.RoutingModeBlock,
	},
	mcpSetPathPrefix + ".targets.tls":        {"", "1.2", "1.3"},
	mcpSetPathPrefix + ".targets.ip_version": {"", "4", "6"},
	"system.mtproto.upstream_mode":           {"tcp", "ws", "auto"},
}

// mcpAlias gives a numeric setting a name a model can write. Order is the
// order the options are reported in.
type mcpAlias struct{ Name, Value string }

// mcpValueAliases maps readable names onto the numbers b4 stores. The log
// level is an integer in the config file, and its order is not the usual one:
// trace is 2 and debug is 3, so debug is the most verbose of the four.
var mcpValueAliases = map[string][]mcpAlias{
	"system.logging.level": {
		{"error", "0"}, {"info", "1"}, {"trace", "2"}, {"debug", "3"},
	},
}

func mcpAliasNames(canonical string) []string {
	aliases, ok := mcpValueAliases[canonical]
	if !ok {
		return nil
	}
	names := make([]string, 0, len(aliases))
	for _, a := range aliases {
		names = append(names, a.Name)
	}
	return names
}

// mcpApplyAlias turns a name into the stored value, and reports whether the
// path has aliases at all so an unknown name can be refused rather than parsed
// as a number.
func mcpApplyAlias(canonical, raw string) (string, bool, error) {
	aliases, ok := mcpValueAliases[canonical]
	if !ok {
		return raw, false, nil
	}
	for _, a := range aliases {
		if strings.EqualFold(a.Name, raw) || a.Value == raw {
			return a.Value, true, nil
		}
	}
	return "", true, fmt.Errorf("value %q is not allowed for %s: expected one of %s",
		raw, canonical, strings.Join(mcpAliasNames(canonical), ", "))
}

// mcpDeniedPathHint explains why a path outside the writable roots is refused,
// so the model reports something useful instead of retrying.
func mcpDeniedPathHint(path string) string {
	switch {
	case strings.HasPrefix(path, "system.web_server.mcp"):
		return "the MCP server's own settings are never writable, so the AI cannot widen its own permissions"
	case strings.HasPrefix(path, "system.web_server"):
		return "changing the web server would move or lock the interface used to undo the change"
	case strings.HasPrefix(path, "system.tables"):
		return "the firewall backend and rule installation are not writable: a wrong value leaves the machine with no rules at all"
	case strings.HasPrefix(path, "queue.tun") || path == "queue.mode":
		return "the packet capture engine is not writable: switching it underneath a live network can cut the machine off"
	case strings.HasPrefix(path, "queue."):
		return "the queue settings carry b4's own packet marks and are not writable"
	case strings.HasPrefix(path, "system.geo"):
		return "the geo data file locations are not writable: pointing them at a missing file empties every geosite category at once"
	case strings.HasPrefix(path, "system.ai") || strings.HasPrefix(path, "system.api"):
		return "provider settings and API credentials are not writable"
	default:
		return "only strategy sets, the MTProto and SOCKS5 subsystems and the logging settings are writable"
	}
}

// mcpChange is one applied write, kept so it can be undone.
type mcpChange struct {
	Path     string
	Previous string
	Current  string
	When     time.Time
	// Snapshot is the whole configuration as it stood before the write. Undo
	// restores it wholesale rather than re-writing the single field, so a write
	// that b4 normalised on the way in is still fully reversed.
	Snapshot *config.Config
}

const mcpHistoryLimit = 20

// mcpHistory holds recent writes, oldest first. Guarded by mcpWriteMu. It is
// in memory only: a restart clears it, which is fine because a restart is
// itself a way back to the saved configuration.
var mcpHistory []mcpChange

func mcpRecordChange(c mcpChange) {
	mcpHistory = append(mcpHistory, c)
	if len(mcpHistory) > mcpHistoryLimit {
		mcpHistory = mcpHistory[len(mcpHistory)-mcpHistoryLimit:]
	}
}

func mcpPopChange() (mcpChange, bool) {
	if len(mcpHistory) == 0 {
		return mcpChange{}, false
	}
	last := mcpHistory[len(mcpHistory)-1]
	mcpHistory = mcpHistory[:len(mcpHistory)-1]
	return last, true
}

// mcpResetHistory exists for tests, which share the package-level history.
func mcpResetHistory() {
	mcpWriteMu.Lock()
	defer mcpWriteMu.Unlock()
	mcpHistory = nil
}

// parseMCPWritePath splits a caller-supplied path into its canonical form and,
// for a per-set path, the set the caller named.
func parseMCPWritePath(path string) (canonical, setRef string, err error) {
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
		return "", "", fmt.Errorf("malformed path %q: put a set id or name in the brackets, e.g. sets[video].enabled", path)
	}
	return path[:open+1] + "]" + path[closing+1:], setRef, nil
}

func mcpHasSourceEntries(entries []string) bool {
	for _, e := range entries {
		if strings.TrimSpace(e) != "" {
			return true
		}
	}
	return false
}

func mcpPathTouchesTargets(canonical string) bool {
	return strings.HasPrefix(canonical, mcpSetPathPrefix+".targets")
}

func mcpExpansionNote(e *mcpTargetExpansion) string {
	if e == nil {
		return ""
	}
	var parts []string
	if len(e.EmptyGeoSite) > 0 {
		parts = append(parts, fmt.Sprintf(
			"geosite %s matched no domains — an unknown category name is skipped silently, so check the spelling or whether a geosite database is installed",
			strings.Join(e.EmptyGeoSite, ", ")))
	}
	if len(e.EmptyGeoIP) > 0 {
		parts = append(parts, fmt.Sprintf(
			"geoip %s matched no addresses — an unknown category name is skipped silently, so check the spelling or whether a geoip database is installed",
			strings.Join(e.EmptyGeoIP, ", ")))
	}
	if len(parts) == 0 {
		return fmt.Sprintf("the set now matches %d domains and %d addresses.", e.Domains, e.IPs)
	}
	return fmt.Sprintf("The set now matches %d domains and %d addresses, but %s.",
		e.Domains, e.IPs, strings.Join(parts, "; "))
}

func mcpPathAllowed(canonical string) bool {
	for _, root := range mcpWritableRoots {
		if canonical == root || strings.HasPrefix(canonical, root+".") {
			return true
		}
	}
	return false
}

func findSetIn(cfg *config.Config, ref string) *config.SetConfig {
	for _, s := range cfg.Sets {
		if strings.EqualFold(s.Id, ref) || strings.EqualFold(s.Name, ref) {
			return s
		}
	}
	return nil
}

// mcpFieldByJSONName finds a struct field by its JSON name. It reports fields
// carrying mcp:"deny" as forbidden rather than missing, so the refusal is
// explicit, and skips json:"-" fields, which are runtime state rather than
// configuration.
func mcpFieldByJSONName(v reflect.Value, name string) (reflect.Value, error) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		jsonName := strings.Split(f.Tag.Get("json"), ",")[0]
		if jsonName == "-" || jsonName == "" || jsonName != name {
			continue
		}
		if f.Tag.Get("mcp") == "deny" {
			return reflect.Value{}, fmt.Errorf("%q is not writable over MCP", name)
		}
		return v.Field(i), nil
	}
	return reflect.Value{}, fmt.Errorf("no such setting %q", name)
}

// mcpResolvePath walks cfg to the field the canonical path names and returns
// it as a settable value.
func mcpResolvePath(cfg *config.Config, canonical, setRef string) (reflect.Value, error) {
	var cur reflect.Value
	var rest string

	switch {
	case strings.HasPrefix(canonical, mcpSetPathPrefix):
		set := findSetIn(cfg, setRef)
		if set == nil {
			return reflect.Value{}, fmt.Errorf("no set with id or name %q", setRef)
		}
		cur = reflect.ValueOf(set).Elem()
		rest = strings.TrimPrefix(canonical, mcpSetPathPrefix)
	case strings.HasPrefix(canonical, "system."):
		cur = reflect.ValueOf(cfg).Elem()
		rest = "." + canonical
	default:
		return reflect.Value{}, fmt.Errorf("path %q is not writable: %s", canonical, mcpDeniedPathHint(canonical))
	}

	rest = strings.TrimPrefix(rest, ".")
	if rest == "" {
		return reflect.Value{}, fmt.Errorf("path %q names a section, not a setting", canonical)
	}

	for _, part := range strings.Split(rest, ".") {
		for cur.Kind() == reflect.Ptr {
			if cur.IsNil() {
				return reflect.Value{}, fmt.Errorf("path %q: %q is not set", canonical, part)
			}
			cur = cur.Elem()
		}
		if cur.Kind() != reflect.Struct {
			return reflect.Value{}, fmt.Errorf("path %q: %q is a value, not a section", canonical, part)
		}
		next, err := mcpFieldByJSONName(cur, part)
		if err != nil {
			return reflect.Value{}, fmt.Errorf("path %q: %w", canonical, err)
		}
		cur = next
	}
	return cur, nil
}

// mcpDescribeKind names a field's type in terms the model can act on.
func mcpDescribeKind(t reflect.Type) (string, bool) {
	switch t.Kind() {
	case reflect.Bool:
		return "bool", true
	case reflect.String:
		return "string", true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "number", true
	case reflect.Float32, reflect.Float64:
		return "number", true
	case reflect.Slice:
		if t.Elem().Kind() == reflect.String {
			return "list", true
		}
	}
	return "", false
}

// mcpFormatCurrent reports a value the way a caller would write it, so an
// aliased setting reads back as its name rather than the stored number.
func mcpFormatCurrent(canonical string, v reflect.Value) string {
	raw := mcpFormatValue(v)
	for _, a := range mcpValueAliases[canonical] {
		if a.Value == raw {
			return a.Name
		}
	}
	return raw
}

func mcpFormatValue(v reflect.Value) string {
	switch v.Kind() {
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.String:
		return v.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'g', -1, 64)
	case reflect.Slice:
		parts := make([]string, 0, v.Len())
		for i := 0; i < v.Len(); i++ {
			parts = append(parts, v.Index(i).String())
		}
		return strings.Join(parts, ",")
	}
	return ""
}

// mcpAssignValue parses raw according to the field's type and assigns it.
func mcpAssignValue(v reflect.Value, canonical, raw string) error {
	val := strings.TrimSpace(raw)

	aliased, hasAliases, err := mcpApplyAlias(canonical, val)
	if err != nil {
		return err
	}
	if hasAliases {
		val = aliased
	}

	if opts, ok := mcpEnums[canonical]; ok {
		matched := false
		for _, opt := range opts {
			if strings.EqualFold(opt, val) {
				val = opt
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("value %q is not allowed for %s: expected one of %s",
				raw, canonical, strings.Join(mcpQuoteEmpty(opts), ", "))
		}
	}

	switch v.Kind() {
	case reflect.Bool:
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("value %q is not a boolean: use true or false", raw)
		}
		v.SetBool(b)
	case reflect.String:
		v.SetString(val)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(val, 10, v.Type().Bits())
		if err != nil {
			return fmt.Errorf("value %q is not a whole number that fits %s", raw, canonical)
		}
		v.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(val, 10, v.Type().Bits())
		if err != nil {
			return fmt.Errorf("value %q is not a positive whole number that fits %s", raw, canonical)
		}
		v.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(val, v.Type().Bits())
		if err != nil {
			return fmt.Errorf("value %q is not a number", raw)
		}
		v.SetFloat(f)
	case reflect.Slice:
		if v.Type().Elem().Kind() != reflect.String {
			return fmt.Errorf("%s holds a structured list and cannot be written as text", canonical)
		}
		items := []string{}
		for _, part := range strings.Split(val, ",") {
			if p := strings.TrimSpace(part); p != "" {
				items = append(items, p)
			}
		}
		v.Set(reflect.ValueOf(items))
	default:
		return fmt.Errorf("%s is a section, not a value that can be written", canonical)
	}
	return nil
}

func mcpQuoteEmpty(opts []string) []string {
	out := make([]string, 0, len(opts))
	for _, o := range opts {
		if o == "" {
			out = append(out, `"" (empty)`)
			continue
		}
		out = append(out, o)
	}
	return out
}

// mcpValidateCandidate holds the preconditions the web interface enforces in
// its handlers rather than in config.Validate, so an MCP write cannot reach a
// state the Settings page refuses.
//
// Each rule is checked against the old config as well, and only reported when
// the write is what broke it. A configuration that already violates a rule,
// which is reachable by editing the file by hand, must not make every
// unrelated setting unwritable.
func mcpValidateCandidate(oldCfg, newCfg *config.Config) error {
	for _, rule := range []struct {
		ok  func(*config.Config) bool
		err string
	}{
		{
			ok: func(c *config.Config) bool {
				mt := c.System.MTProto
				return !mt.Enabled || len(mt.EffectiveSecrets()) > 0 || mt.FakeSNI != ""
			},
			err: "MTProto needs at least one secret or a fake SNI domain before it can be enabled",
		},
		{
			ok: func(c *config.Config) bool {
				return (c.System.Socks5.Username == "") == (c.System.Socks5.Password == "")
			},
			err: "the SOCKS5 username and password must both be set or both be empty",
		},
		{
			ok: func(c *config.Config) bool {
				s5 := c.System.Socks5
				return !s5.Enabled || s5.Username != "" || mcpHasSourceEntries(s5.AllowedSources)
			},
			err: "the SOCKS5 server would accept every client without a password: set a username and password, or an allowed_sources list, in the web interface before enabling it. Both are refused over MCP, so they cannot be set from here",
		},
	} {
		if !rule.ok(newCfg) && rule.ok(oldCfg) {
			return fmt.Errorf("%s", rule.err)
		}
	}
	return nil
}

// mcpWritablePaths walks the writable roots and lists every leaf a caller can
// address, with its type and the values it accepts.
func mcpWritablePaths(cfg *config.Config, sample *config.SetConfig) []mcpPathInfo {
	var out []mcpPathInfo

	if sample != nil {
		out = append(out, mcpCollectPaths(reflect.ValueOf(sample).Elem(), mcpSetPathPrefix)...)
	}
	sys := reflect.ValueOf(cfg).Elem()
	for _, root := range []string{"system.mtproto", "system.socks5", "system.logging"} {
		parts := strings.Split(root, ".")
		cur := sys
		ok := true
		for _, p := range parts {
			next, err := mcpFieldByJSONName(cur, p)
			if err != nil {
				ok = false
				break
			}
			cur = next
		}
		if ok {
			out = append(out, mcpCollectPaths(cur, root)...)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func mcpCollectPaths(v reflect.Value, prefix string) []mcpPathInfo {
	var out []mcpPathInfo
	if v.Kind() != reflect.Struct {
		return out
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" || f.Tag.Get("mcp") == "deny" {
			continue
		}
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "-" || name == "" {
			continue
		}
		path := prefix + "." + name
		fv := v.Field(i)
		if fv.Kind() == reflect.Struct {
			out = append(out, mcpCollectPaths(fv, path)...)
			continue
		}
		kind, ok := mcpDescribeKind(f.Type)
		if !ok {
			continue
		}
		options := mcpEnums[path]
		if names := mcpAliasNames(path); names != nil {
			options = names
		}
		out = append(out, mcpPathInfo{
			Path:    path,
			Type:    kind,
			Current: mcpFormatCurrent(path, fv),
			Options: options,
		})
	}
	return out
}

type mcpPathInfo struct {
	Path    string   `json:"path"`
	Type    string   `json:"type"`
	Current string   `json:"current,omitempty"`
	Options []string `json:"options,omitempty"`
}

type mcpListPathsIn struct {
	Prefix string `json:"prefix,omitempty" jsonschema:"Optional filter, e.g. 'sets[].tcp' or 'system.mtproto'. Omit to list everything."`
	Set    string `json:"set,omitempty" jsonschema:"Set id or name whose current values should be shown for the sets[] paths. Defaults to the first set."`
}

type mcpListPathsOut struct {
	Paths []mcpPathInfo `json:"paths"`
	Note  string        `json:"note"`
}

type mcpSetValueIn struct {
	Path  string `json:"path" jsonschema:"Setting to change, e.g. sets[video].tcp.seg2delay or system.mtproto.port. Call b4_list_writable_paths for the full list."`
	Value string `json:"value" jsonschema:"New value as a string: 'true'/'false' for booleans, digits for numbers, a comma-separated list for list settings."`
}

type mcpTargetExpansion struct {
	Domains      int      `json:"domain_count"`
	IPs          int      `json:"ip_count"`
	EmptyGeoSite []string `json:"geosite_categories_matching_nothing,omitempty"`
	EmptyGeoIP   []string `json:"geoip_categories_matching_nothing,omitempty"`
}

type mcpSetValueOut struct {
	Path      string              `json:"path"`
	Previous  string              `json:"previous"`
	Current   string              `json:"current"`
	Changed   bool                `json:"changed"`
	Expansion *mcpTargetExpansion `json:"expansion,omitempty"`
	Note      string              `json:"note,omitempty"`
}

type mcpRevertOut struct {
	Reverted    bool   `json:"reverted"`
	Path        string `json:"path,omitempty"`
	RestoredTo  string `json:"restored_to,omitempty"`
	UndoneValue string `json:"undone_value,omitempty"`
	Remaining   int    `json:"remaining_changes"`
	Note        string `json:"note"`
}

func mcpWriteToolDescription() string {
	var sb strings.Builder
	sb.WriteString("Change one b4 setting and apply it live (no restart). ")
	sb.WriteString("Writable: every setting inside a strategy set (targets, fragmentation, faking, TCP/UDP, DNS, escalation, routing), ")
	sb.WriteString("the MTProto and SOCKS5 subsystems, and the logging settings - system.logging.level takes ")
	sb.WriteString("error, info, trace or debug, and debug is the most verbose. Address a per-set setting as sets[<id or name>].<path>, ")
	sb.WriteString("for example sets[video].fragmentation.strategy or sets[video].tcp.seg2delay.\n")
	sb.WriteString("Refused, always: every credential, the web server, the MCP settings themselves, the packet capture engine, ")
	sb.WriteString("the firewall backend and the packet marks — a wrong value there can leave the machine unreachable.\n")
	sb.WriteString("Call b4_list_writable_paths for the exact paths, types and accepted values rather than guessing a path. ")
	sb.WriteString("A list setting is replaced wholesale, so read its current value first and send the full list back. ")
	sb.WriteString("Requires the 'Allow configuration changes' setting to be enabled. ")
	sb.WriteString("Confirm with the user before calling, report the returned previous/current values, ")
	sb.WriteString("and use b4_revert_last_change if the result is not what was intended.")
	return sb.String()
}

func (api *API) addMCPWriteTools(srv *mcp.Server) {
	addTool(srv, &mcp.Tool{
		Name:        "b4_list_writable_paths",
		Title:       "List writable settings",
		Description: "Every setting b4_set_config_value can change, with its type, current value and accepted values. Call this before writing rather than guessing a path. Read-only.",
		Annotations: mcpReadOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpListPathsIn) (*mcp.CallToolResult, mcpListPathsOut, error) {
		cfg := api.getCfg()

		var sample *config.SetConfig
		if ref := strings.TrimSpace(in.Set); ref != "" {
			if sample = findSetIn(cfg, ref); sample == nil {
				return nil, mcpListPathsOut{}, fmt.Errorf("no set with id or name %q", ref)
			}
		} else if len(cfg.Sets) > 0 {
			sample = cfg.Sets[0]
		}

		paths := mcpWritablePaths(cfg, sample)
		if prefix := strings.TrimSpace(in.Prefix); prefix != "" {
			kept := paths[:0:0]
			for _, p := range paths {
				if strings.HasPrefix(p.Path, prefix) {
					kept = append(kept, p)
				}
			}
			paths = kept
		}

		note := fmt.Sprintf("%d writable settings", len(paths))
		if sample != nil {
			note += fmt.Sprintf("; the sets[] current values are those of set %q, and the same paths apply to any set", sample.Name)
		} else {
			note += "; no sets are configured, so the sets[] paths are not listed"
		}
		return nil, mcpListPathsOut{Paths: paths, Note: note}, nil
	})

	addTool(srv, &mcp.Tool{
		Name:        "b4_set_config_value",
		Title:       "Change a b4 setting",
		Description: mcpWriteToolDescription(),
		Annotations: mcpDestructive,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpSetValueIn) (*mcp.CallToolResult, mcpSetValueOut, error) {
		if !api.getCfg().System.WebServer.MCP.AllowWrites {
			return nil, mcpSetValueOut{}, fmt.Errorf(
				"configuration writes are disabled: turn on 'Allow configuration changes' under Settings -> API -> MCP server to permit them")
		}

		canonical, setRef, err := parseMCPWritePath(in.Path)
		if err != nil {
			return nil, mcpSetValueOut{}, err
		}
		if !mcpPathAllowed(canonical) {
			return nil, mcpSetValueOut{}, fmt.Errorf(
				"path %q is not writable: %s", in.Path, mcpDeniedPathHint(canonical))
		}

		mcpWriteMu.Lock()
		defer mcpWriteMu.Unlock()

		oldCfg := api.getCfg()
		newCfg := oldCfg.Clone()

		field, err := mcpResolvePath(newCfg, canonical, setRef)
		if err != nil {
			return nil, mcpSetValueOut{}, err
		}
		if !field.CanSet() {
			return nil, mcpSetValueOut{}, fmt.Errorf("path %q cannot be written", in.Path)
		}

		readRef := setRef
		if set := findSetIn(newCfg, setRef); set != nil && set.Id != "" {
			readRef = set.Id
		}

		previous := mcpFormatCurrent(canonical, field)
		if err := mcpAssignValue(field, canonical, in.Value); err != nil {
			return nil, mcpSetValueOut{}, err
		}
		requested := mcpFormatCurrent(canonical, field)

		if previous == requested {
			return nil, mcpSetValueOut{
				Path: in.Path, Previous: previous, Current: previous, Changed: false,
				Note: "already set to this value; nothing was written",
			}, nil
		}

		if err := mcpValidateCandidate(oldCfg, newCfg); err != nil {
			return nil, mcpSetValueOut{}, fmt.Errorf("rejected: %w", err)
		}

		var expansion *mcpTargetExpansion
		if mcpPathTouchesTargets(canonical) {
			if set := findSetIn(newCfg, readRef); set != nil {
				report := api.loadTargetsForSetCached(set)
				expansion = &mcpTargetExpansion{
					Domains:      report.Domains,
					IPs:          report.IPs,
					EmptyGeoSite: report.EmptyGeoSite,
					EmptyGeoIP:   report.EmptyGeoIP,
				}
			}
		}

		snapshot := oldCfg.Clone()

		if err := api.saveAndPushConfig(newCfg); err != nil {
			return nil, mcpSetValueOut{}, fmt.Errorf("rejected: %w", err)
		}

		// Saving pushes the new config to the running subsystems, but two
		// things sit outside that: settings applied to the process itself
		// (log level, timezone, geodata paths), and the firewall rules, which
		// are derived from the config separately and only reach
		// nftables/iptables through PerformSoftRestart. Every REST write path
		// does both.
		api.applyRuntimeChanges(newCfg, oldCfg)
		refreshed := api.PerformSoftRestart(newCfg, oldCfg)

		current := requested
		if liveField, err := mcpResolvePath(api.getCfg(), canonical, readRef); err == nil {
			current = mcpFormatCurrent(canonical, liveField)
		}

		out := mcpSetValueOut{
			Path: in.Path, Previous: previous, Current: current,
			Changed: current != previous, Expansion: expansion,
		}

		if current == previous {
			out.Note = fmt.Sprintf(
				"not applied: b4 reset %s back to %q while validating the saved configuration, so nothing changed and nothing was recorded for undo. "+
					"Call b4_list_writable_paths for the accepted form of this setting and read its b4://topics resource before retrying.",
				in.Path, previous)
			log.Infof("mcp: %s rejected on save, still %s", in.Path, previous)
			return nil, out, nil
		}

		mcpRecordChange(mcpChange{
			Path: in.Path, Previous: previous, Current: current,
			When: time.Now(), Snapshot: snapshot,
		})

		log.Infof("mcp: %s changed from %s to %s", in.Path, previous, current)

		out.Note = "applied live; no restart required. Undo with b4_revert_last_change"
		if refreshed {
			out.Note = "applied live; firewall rules were refreshed to match. Undo with b4_revert_last_change"
		}
		if current != requested {
			out.Note = fmt.Sprintf("b4 normalised the value on save: %s is now %q, not the requested %q. ", in.Path, current, requested) + out.Note
		}
		if hint := mcpExpansionNote(expansion); hint != "" {
			out.Note = out.Note + " " + hint
		}

		return nil, out, nil
	})

	addTool(srv, &mcp.Tool{
		Name:  "b4_revert_last_change",
		Title: "Undo the last b4 setting change",
		Description: "Restore the configuration as it stood before the most recent b4_set_config_value call, and apply it live. " +
			"Call this as soon as a change turns out to be wrong. Repeating it walks further back, one change at a time. " +
			"Only changes made through MCP since b4 last started can be undone; edits made in the web interface cannot.",
		Annotations: mcpDestructive,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ mcpEmpty) (*mcp.CallToolResult, mcpRevertOut, error) {
		if !api.getCfg().System.WebServer.MCP.AllowWrites {
			return nil, mcpRevertOut{}, fmt.Errorf(
				"configuration writes are disabled: turn on 'Allow configuration changes' under Settings -> API -> MCP server to permit them")
		}

		mcpWriteMu.Lock()
		defer mcpWriteMu.Unlock()

		last, ok := mcpPopChange()
		if !ok {
			return nil, mcpRevertOut{
				Reverted: false,
				Note:     "nothing to undo: no setting has been changed through MCP since b4 started",
			}, nil
		}

		oldCfg := api.getCfg()
		if err := api.saveAndPushConfig(last.Snapshot); err != nil {
			mcpRecordChange(last)
			return nil, mcpRevertOut{}, fmt.Errorf("could not restore the previous configuration: %w", err)
		}
		api.applyRuntimeChanges(last.Snapshot, oldCfg)
		api.PerformSoftRestart(last.Snapshot, oldCfg)

		log.Infof("mcp: reverted %s back to %s", last.Path, last.Previous)

		return nil, mcpRevertOut{
			Reverted:    true,
			Path:        last.Path,
			RestoredTo:  last.Previous,
			UndoneValue: last.Current,
			Remaining:   len(mcpHistory),
			Note: fmt.Sprintf("%s restored to %q, undoing the change to %q; applied live",
				last.Path, last.Previous, last.Current),
		}, nil
	})
}
