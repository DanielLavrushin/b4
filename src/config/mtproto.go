package config

import "strings"

type MTProtoSecret struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Secret  string `json:"secret"`
	Enabled bool   `json:"enabled"`
}

func (m *MTProtoConfig) EffectiveSecrets() []MTProtoSecret {
	out := make([]MTProtoSecret, 0, len(m.Secrets))
	for _, s := range m.Secrets {
		if s.Enabled && strings.TrimSpace(s.Secret) != "" {
			out = append(out, s)
		}
	}
	return out
}

func (m *MTProtoConfig) FirstEnabledSecret() string {
	for _, s := range m.Secrets {
		if s.Enabled && strings.TrimSpace(s.Secret) != "" {
			return s.Secret
		}
	}
	return ""
}
