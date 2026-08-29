package convert

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

//go:embed rules/*.json
var ruleFS embed.FS

const (
	ArgNone     = "none"
	ArgRequired = "required"
	ArgOptional = "optional"
)

const (
	ScopeGlobal  = "global"
	ScopeProfile = "profile"
	ScopeBreak   = "break"
)

type OptionSpec struct {
	Key      string            `json:"key"`
	Short    string            `json:"short"`
	Long     string            `json:"long"`
	Arg      string            `json:"arg"`
	Scope    string            `json:"scope"`
	Grammar  string            `json:"grammar"`
	Target   string            `json:"target"`
	Const    map[string]string `json:"const"`
	Versions []string          `json:"versions"`
	Note     string            `json:"note"`
	Default  string            `json:"default"`
}

func (o OptionSpec) appliesTo(version string) bool {
	if len(o.Versions) == 0 {
		return true
	}
	for _, v := range o.Versions {
		if v == version {
			return true
		}
	}
	return false
}

func (o OptionSpec) display() string {
	if o.Short != "" {
		return "-" + o.Short
	}
	return "--" + o.Long
}

type VersionSpec struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Markers []string `json:"markers"`
	PosHint bool     `json:"pos_hint"`
	Default bool     `json:"default"`
}

type DetectSpec struct {
	Markers   []string `json:"markers"`
	Signature []string `json:"signature"`
	Reject    []string `json:"reject"`
	EnvVars   []string `json:"env_vars"`
}

type SourceGroup struct {
	Vars    []string `json:"vars"`
	Require []string `json:"require"`
	Append  []string `json:"append"`
}

type SourceLayout struct {
	ID      string        `json:"id"`
	Require []string      `json:"require"`
	Groups  []SourceGroup `json:"groups"`
}

type SpecSources struct {
	Vars         []string          `json:"vars"`
	Foreign      []string          `json:"foreign"`
	ForeignNote  string            `json:"foreign_note"`
	Placeholders map[string]string `json:"placeholders"`
	Layouts      []SourceLayout    `json:"layouts"`
}

type SpecDefaults struct {
	FakeTTL       int  `json:"fake_ttl"`
	FakeTTLForced bool `json:"fake_ttl_forced"`
	OOBByte       int  `json:"oob_byte"`
}

type Spec struct {
	Tool         string        `json:"tool"`
	Label        string        `json:"label"`
	Style        string        `json:"style"`
	Convertible  *bool         `json:"convertible"`
	Homepage     string        `json:"homepage"`
	Defaults     SpecDefaults  `json:"defaults"`
	Detect       DetectSpec    `json:"detect"`
	Sources      SpecSources   `json:"sources"`
	Versions     []VersionSpec `json:"versions"`
	Ambiguous    []string      `json:"ambiguous"`
	Normalize    string        `json:"normalize"`
	ProfileModel string        `json:"profile_model"`
	ProfileBreak []string      `json:"profile_break"`
	Options      []OptionSpec  `json:"options"`
}

func (s *Spec) convertible() bool {
	return s.Convertible == nil || *s.Convertible
}

func (s *Spec) defaultVersion() string {
	for _, v := range s.Versions {
		if v.Default {
			return v.ID
		}
	}
	if len(s.Versions) > 0 {
		return s.Versions[0].ID
	}
	return ""
}

func (s *Spec) versionLabel(id string) string {
	for _, v := range s.Versions {
		if v.ID == id {
			if v.Label != "" {
				return v.Label
			}
			return v.ID
		}
	}
	return id
}

func (s *Spec) hasVersion(id string) bool {
	for _, v := range s.Versions {
		if v.ID == id {
			return true
		}
	}
	return false
}

func (s *Spec) isBreak(key string) bool {
	for _, k := range s.ProfileBreak {
		if k == key {
			return true
		}
	}
	return false
}

type optionTable struct {
	short map[string]OptionSpec
	long  map[string]OptionSpec
	byKey map[string]OptionSpec
}

func (s *Spec) tableFor(version string) *optionTable {
	t := &optionTable{
		short: map[string]OptionSpec{},
		long:  map[string]OptionSpec{},
		byKey: map[string]OptionSpec{},
	}
	for _, o := range s.Options {
		if !o.appliesTo(version) {
			continue
		}
		if o.Short != "" {
			t.short[o.Short] = o
		}
		if o.Long != "" {
			t.long[o.Long] = o
		}
		t.byKey[o.Key] = o
	}
	return t
}

func (t *optionTable) longMatch(name string) (OptionSpec, bool, bool) {
	if o, ok := t.long[name]; ok {
		return o, true, false
	}
	var found []OptionSpec
	for k, o := range t.long {
		if len(name) < len(k) && k[:len(name)] == name {
			found = append(found, o)
		}
	}
	if len(found) == 1 {
		return found[0], true, false
	}
	if len(found) > 1 {
		return OptionSpec{}, false, true
	}
	return OptionSpec{}, false, false
}

var (
	specsOnce sync.Once
	specs     map[string]*Spec
	specsErr  error
)

func loadSpecs() (map[string]*Spec, error) {
	specsOnce.Do(func() {
		entries, err := ruleFS.ReadDir("rules")
		if err != nil {
			specsErr = err
			return
		}
		out := map[string]*Spec{}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			raw, err := ruleFS.ReadFile("rules/" + e.Name())
			if err != nil {
				specsErr = fmt.Errorf("read %s: %w", e.Name(), err)
				return
			}
			var sp Spec
			if err := json.Unmarshal(raw, &sp); err != nil {
				specsErr = fmt.Errorf("parse %s: %w", e.Name(), err)
				return
			}
			if sp.Tool == "" {
				specsErr = fmt.Errorf("%s: missing tool name", e.Name())
				return
			}
			out[sp.Tool] = &sp
		}
		specs = out
	})
	return specs, specsErr
}

type ToolInfo struct {
	Tool     string   `json:"tool"`
	Label    string   `json:"label"`
	Homepage string   `json:"homepage"`
	Versions []string `json:"versions"`
}

func Tools() ([]ToolInfo, error) {
	all, err := loadSpecs()
	if err != nil {
		return nil, err
	}
	out := make([]ToolInfo, 0, len(all))
	for _, s := range all {
		if !s.convertible() {
			continue
		}
		vs := make([]string, 0, len(s.Versions))
		for _, v := range s.Versions {
			vs = append(vs, v.ID)
		}
		out = append(out, ToolInfo{Tool: s.Tool, Label: s.Label, Homepage: s.Homepage, Versions: vs})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tool < out[j].Tool })
	return out, nil
}
