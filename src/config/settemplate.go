package config

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/daniellavrushin/b4/log"
)

const (
	SetTemplatePath        = "ui.new_set_template"
	setTemplateValidateID  = "new-set-template"
	setTemplateValidateFor = "new set template"
	setTemplateInvalidCode = "invalid_set_template"
)

var SetTemplateOwnKeys = []string{"id", "name", "enabled", "targets", "hub", "discovery", "revision", "stats", "hub_state", "b4_version"}

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
	dns, ok := template["dns"].(map[string]interface{})
	if !ok {
		return template
	}
	maskedDNS := make(map[string]interface{}, len(dns))
	for key, value := range dns {
		maskedDNS[key] = value
	}
	if doh, ok := maskedDNS["doh_url"].(string); ok {
		maskedDNS["doh_url"] = maskDoHURL(doh)
	}
	if upstreams, ok := maskedDNS["upstreams"].([]interface{}); ok {
		masked := make([]interface{}, len(upstreams))
		for i, upstream := range upstreams {
			if server, ok := upstream.(string); ok {
				masked[i] = maskDNSServer(server)
			} else {
				masked[i] = upstream
			}
		}
		maskedDNS["upstreams"] = masked
	}
	out := make(map[string]interface{}, len(template))
	for key, value := range template {
		out[key] = value
	}
	out["dns"] = maskedDNS
	return out
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
