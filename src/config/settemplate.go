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
	return sparse, nil
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
