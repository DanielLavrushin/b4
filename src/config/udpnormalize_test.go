package config

import "testing"

func TestDefaultSetFilterQUICIsSNI(t *testing.T) {
	if DefaultSetConfig.UDP.FilterQUIC != QUICFilterSNI {
		t.Errorf("default filter_quic: want %q, got %q", QUICFilterSNI, DefaultSetConfig.UDP.FilterQUIC)
	}
	if NewSetConfig().UDP.FilterQUIC != QUICFilterSNI {
		t.Errorf("a new set must start on %q", QUICFilterSNI)
	}
}

func TestNormalizeQUICFilter(t *testing.T) {
	cases := map[string]string{
		"all":      QUICFilterAll,
		"sni":      QUICFilterSNI,
		"disabled": QUICFilterSNI,
		"parse":    QUICFilterSNI,
		"":         QUICFilterSNI,
		"ALL":      QUICFilterSNI,
		"nonsense": QUICFilterSNI,
	}
	for in, want := range cases {
		if got := NormalizeQUICFilter(in); got != want {
			t.Errorf("NormalizeQUICFilter(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeUDPMode(t *testing.T) {
	cases := map[string]string{
		ConfigOff:  ConfigOff,
		"fake":     UDPModeFake,
		"drop":     UDPModeDrop,
		"reject":   UDPModeReject,
		"":         DefaultSetConfig.UDP.Mode,
		"none":     DefaultSetConfig.UDP.Mode,
		"disabled": DefaultSetConfig.UDP.Mode,
		"Drop":     DefaultSetConfig.UDP.Mode,
	}
	for in, want := range cases {
		if got := NormalizeUDPMode(in); got != want {
			t.Errorf("NormalizeUDPMode(%q) = %q, want %q", in, got, want)
		}
	}
	if NormalizeUDPMode("garbage") == ConfigOff {
		t.Error("an unrecognised udp mode must not silently mean pass-through")
	}
}

func TestValidateNormalizesUDPMatchingAndAction(t *testing.T) {
	cfg := NewConfig()

	legacy := NewSetConfig()
	legacy.Id = "11111111-2222-3333-4444-555555555555"
	legacy.Name = "Legacy"
	legacy.UDP.FilterQUIC = "parse"
	legacy.UDP.Mode = "none"

	widened := NewSetConfig()
	widened.Id = "66666666-7777-8888-9999-aaaaaaaaaaaa"
	widened.Name = "Widened"
	widened.UDP.FilterQUIC = "all"
	widened.UDP.Mode = ConfigOff

	cfg.Sets = []*SetConfig{&legacy, &widened}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	if legacy.UDP.FilterQUIC != QUICFilterSNI {
		t.Errorf("legacy filter_quic: want %q, got %q", QUICFilterSNI, legacy.UDP.FilterQUIC)
	}
	if legacy.UDP.Mode != DefaultSetConfig.UDP.Mode {
		t.Errorf("unrecognised udp mode: want %q, got %q", DefaultSetConfig.UDP.Mode, legacy.UDP.Mode)
	}
	if widened.UDP.FilterQUIC != QUICFilterAll {
		t.Errorf("filter_quic %q must survive validation, got %q", QUICFilterAll, widened.UDP.FilterQUIC)
	}
	if widened.UDP.Mode != ConfigOff {
		t.Errorf("udp mode %q must survive validation, got %q", ConfigOff, widened.UDP.Mode)
	}
}
