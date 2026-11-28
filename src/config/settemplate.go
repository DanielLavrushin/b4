package config

import (
	"encoding/json"
	"errors"
	"maps"
	"strings"

	"github.com/daniellavrushin/b4/log"
)

const (
	SetTemplatePath        = "ui.new_set_template"
	setTemplateValidateID  = "new-set-template"
	setTemplateValidateFor = "new set template"
	setTemplateInvalidCode = "invalid_set_template"
	setTemplateScopeDevice = "02:00:00:00:00:00"
)

var SetTemplateOwnKeys = []string{"id", "name", "group", "enabled", "targets", "hub", "discovery", "revision", "stats", "hub_state", "b4_version"}

func NormalizeSetTemplate(raw map[string]interface{}) (map[string]interface{}, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	fields := make(map[string]interface{}, len(raw))
	for key, value := range raw {
		fields[key] = value
	}
	dropSetTemplateOwnKeys(fields)
	data, err := json.Marshal(fields)
	if err != nil {
		return nil, setTemplateError(err)
	}

	set := NewSetConfig()
	if err := json.Unmarshal(data, &set); err != nil {
		return nil, setTemplateError(err)
	}
	set.Id = setTemplateValidateID
	set.Name = setTemplateValidateFor
	set.Enabled = true
	set.Routing.Upstream.Username = ""
	set.Routing.Upstream.Password = ""
	if set.MSSClamp.Enabled {
		set.Targets.SourceDevices = []string{setTemplateScopeDevice}
		set.Targets.SourceDevicesExclude = false
	}

	cfg := NewConfig()
	cfg.System.Geo.GeoSitePath = "/dev/null"
	cfg.System.Geo.GeoIpPath = "/dev/null"
	cfg.Sets = []*SetConfig{&set}
	if err := cfg.Validate(); err != nil {
		return nil, setTemplateValidationError(err)
	}

	full, err := SetToMap(cfg.Sets[0])
	if err != nil {
		return nil, setTemplateError(err)
	}
	sparse, err := SparsifySetMap(full)
	if err != nil {
		return nil, setTemplateError(err)
	}
	dropSetTemplateOwnKeys(sparse)
	if len(sparse) == 0 {
		return nil, nil
	}
	if err := setTemplatePlaceholderError(sparse); err != nil {
		return nil, err
	}
	return sparse, nil
}

func setTemplatePlaceholderError(template map[string]interface{}) error {
	paths := placeholderValuePaths(map[string]interface{}{"ui": map[string]interface{}{"new_set_template": template}})
	if len(paths) == 0 {
		return nil
	}
	out := &ValidationError{Fields: make([]ValidationField, 0, len(paths))}
	for _, path := range paths {
		out.Fields = append(out.Fields, ValidationField{
			Path:    path,
			Code:    "redacted_placeholder",
			Message: path + " holds the " + RedactedMarker + " placeholder of a safe copy instead of a real value",
			Params:  map[string]any{"path": path},
		})
	}
	return out
}

func redactSetTemplateForSharing(template map[string]interface{}) map[string]interface{} {
	dns, _ := template["dns"].(map[string]interface{})
	routing, _ := template["routing"].(map[string]interface{})
	upstream, _ := routing["upstream"].(map[string]interface{})
	if dns == nil && upstream == nil {
		return template
	}
	out := maps.Clone(template)
	if dns != nil {
		out["dns"] = redactTemplateDNS(dns)
	}
	if host, ok := upstream["host"].(string); ok {
		maskedUpstream, maskedRouting := maps.Clone(upstream), maps.Clone(routing)
		maskedUpstream["host"] = maskProxyHost(host)
		maskedRouting["upstream"] = maskedUpstream
		out["routing"] = maskedRouting
	}
	return out
}

func redactTemplateDNS(dns map[string]interface{}) map[string]interface{} {
	masked := maps.Clone(dns)
	if doh, ok := masked["doh_url"].(string); ok {
		masked["doh_url"] = maskDoHURL(doh)
	}
	if target, ok := masked["target_dns"].(string); ok {
		masked["target_dns"] = maskDNSServer(target)
	}
	if upstreams, ok := masked["upstreams"].([]interface{}); ok {
		servers := make([]interface{}, len(upstreams))
		for i, upstream := range upstreams {
			if server, ok := upstream.(string); ok {
				servers[i] = maskDNSServer(server)
			} else {
				servers[i] = upstream
			}
		}
		masked["upstreams"] = servers
	}
	return masked
}

func (c *Config) loadSetTemplate() {
	template, err := NormalizeSetTemplate(c.UI.NewSetTemplate)
	if err != nil {
		log.Warnf("Ignoring %s from the config file, it does not describe a valid set: %v", SetTemplatePath, err)
		c.UI.NewSetTemplate = nil
		return
	}
	c.UI.NewSetTemplate = template
}

func dropSetTemplateOwnKeys(m map[string]interface{}) {
	for _, key := range SetTemplateOwnKeys {
		delete(m, key)
	}
}

func setTemplateError(err error) error {
	return &ValidationError{Fields: []ValidationField{{
		Path:    SetTemplatePath,
		Code:    setTemplateInvalidCode,
		Message: "the new set template is not a valid set: " + err.Error(),
		Params:  map[string]any{"error": err.Error()},
	}}}
}

func setTemplateValidationError(err error) error {
	var ve *ValidationError
	if !errors.As(err, &ve) {
		return setTemplateError(err)
	}
	out := &ValidationError{Fields: make([]ValidationField, 0, len(ve.Fields))}
	for _, f := range ve.Fields {
		if rest, ok := strings.CutPrefix(f.Path, "sets[0]"); ok {
			f.Path = SetTemplatePath + rest
		}
		out.Fields = append(out.Fields, f)
	}
	return out
}
