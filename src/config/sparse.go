package config

import (
	"encoding/json"
	"reflect"
)

func toMap(v interface{}) (map[string]interface{}, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func isEmptyOrNil(v interface{}) bool {
	if v == nil {
		return true
	}
	if arr, ok := v.([]interface{}); ok && len(arr) == 0 {
		return true
	}
	return false
}

func sparsifyMap(current, defaults map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for key, val := range current {
		defVal, hasDefault := defaults[key]
		if !hasDefault {
			result[key] = val
			continue
		}

		curMap, curIsMap := val.(map[string]interface{})
		defMap, defIsMap := defVal.(map[string]interface{})
		if curIsMap && defIsMap {
			sparse := sparsifyMap(curMap, defMap)
			if len(sparse) > 0 {
				result[key] = sparse
			}
			continue
		}

		if isEmptyOrNil(val) && isEmptyOrNil(defVal) {
			continue
		}

		if !reflect.DeepEqual(val, defVal) {
			result[key] = val
		}
	}
	return result
}

func SparsifySetMap(setMap map[string]interface{}) (map[string]interface{}, error) {
	defaultSet := NewSetConfig()
	defSetMap, err := toMap(&defaultSet)
	if err != nil {
		return nil, err
	}
	return sparsifyMap(setMap, defSetMap), nil
}

func SetToMap(set *SetConfig) (map[string]interface{}, error) {
	return toMap(set)
}

func MarshalSparse(cfg *Config) ([]byte, error) {
	cfgMap, err := toMap(cfg)
	if err != nil {
		return nil, err
	}

	defaultCfg := NewConfig()
	defMap, err := toMap(&defaultCfg)
	if err != nil {
		return nil, err
	}

	cfgSets, _ := cfgMap["sets"].([]interface{})
	delete(cfgMap, "sets")
	delete(defMap, "sets")

	sparse := sparsifyMap(cfgMap, defMap)

	sparse["version"] = cfg.Version

	defaultSet := NewSetConfig()
	defSetMap, err := toMap(&defaultSet)
	if err != nil {
		return nil, err
	}

	sparseSets := make([]interface{}, 0, len(cfgSets))
	for _, rawSet := range cfgSets {
		setMap, ok := rawSet.(map[string]interface{})
		if !ok {
			sparseSets = append(sparseSets, rawSet)
			continue
		}
		sparseSet := sparsifyMap(setMap, defSetMap)
		sparseSet["id"] = setMap["id"]
		sparseSet["name"] = setMap["name"]
		if enabled, ok := setMap["enabled"]; ok {
			sparseSet["enabled"] = enabled
		}
		sparseSets = append(sparseSets, sparseSet)
	}
	sparse["sets"] = sparseSets

	if sys, ok := sparse["system"].(map[string]interface{}); ok {
		if checker, ok := sys["checker"].(map[string]interface{}); ok {
			mark := cfg.MainInjectedMark()
			if v, ok := checker["discovery_flow_mark"].(float64); ok && uint(v) == mark+1 {
				delete(checker, "discovery_flow_mark")
			}
			if v, ok := checker["discovery_injected_mark"].(float64); ok && uint(v) == mark+2 {
				delete(checker, "discovery_injected_mark")
			}
			if len(checker) == 0 {
				delete(sys, "checker")
			}
			if len(sys) == 0 {
				delete(sparse, "system")
			}
		}
	}

	return json.MarshalIndent(sparse, "", "  ")
}
