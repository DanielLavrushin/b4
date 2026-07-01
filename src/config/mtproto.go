package config

import "strings"

type MTProtoSecret struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Secret  string `json:"secret"`
	Enabled bool   `json:"enabled"`
}

func (m *MTProtoConfig) EffectiveSecrets() []MTProtoSecret {
	if len(m.Secrets) > 0 {
		out := make([]MTProtoSecret, 0, len(m.Secrets))
		for _, s := range m.Secrets {
			if s.Enabled && strings.TrimSpace(s.Secret) != "" {
				out = append(out, s)
			}
		}
		return out
	}
	if strings.TrimSpace(m.Secret) != "" {
		return []MTProtoSecret{{ID: "legacy", Secret: m.Secret, Enabled: true}}
	}
	return nil
}

func (m *MTProtoConfig) FirstEnabledSecret() string {
	for _, s := range m.Secrets {
		if s.Enabled && strings.TrimSpace(s.Secret) != "" {
			return s.Secret
		}
	}
	return ""
}
