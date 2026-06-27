package config

import (
	"bytes"
	"encoding/json"
)

func decodeBoolOrObject(data []byte, onBool func(bool), decodeObject func([]byte) error) error {
	t := bytes.TrimSpace(data)
	if len(t) > 0 && (t[0] == 't' || t[0] == 'f') {
		var b bool
		if err := json.Unmarshal(t, &b); err != nil {
			return err
		}
		onBool(b)
		return nil
	}
	return decodeObject(data)
}

func (m *MasqueradeConfig) UnmarshalJSON(data []byte) error {
	type raw MasqueradeConfig
	return decodeBoolOrObject(data,
		func(b bool) { m.Enabled = b },
		func(d []byte) error { return json.Unmarshal(d, (*raw)(m)) },
	)
}

func (m MasqueradeConfig) Equal(o MasqueradeConfig) bool {
	return m.Enabled == o.Enabled && equalStringSet(m.Interfaces, o.Interfaces)
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, s := range a {
		counts[s]++
	}
	for _, s := range b {
		counts[s]--
		if counts[s] < 0 {
			return false
		}
	}
	return true
}
