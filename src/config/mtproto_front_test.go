package config

import (
	"strings"
	"testing"
)

func TestFrontNameIsValidatedEvenWithTheProxyOff(t *testing.T) {
	for _, tc := range []struct {
		value string
		ok    bool
	}{
		{"", true},
		{"off", true},
		{"OFF", true},
		{"sprinthost.ru", true},
		{"not a host", false},
		{"https://sprinthost.ru/", false},
		{"149.154.167.220", false},
	} {
		cfg := NewConfig()
		cfg.System.MTProto.Enabled = false
		cfg.System.MTProto.WSFrontSNI = tc.value
		err := cfg.Validate()
		hit := err != nil && strings.Contains(err.Error(), "ws_front_sni")
		if tc.ok && hit {
			t.Errorf("%q was refused: %v", tc.value, err)
		}
		if !tc.ok && !hit {
			t.Errorf("%q was accepted", tc.value)
		}
	}
}
