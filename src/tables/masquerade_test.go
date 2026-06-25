package tables

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func specContains(spec []string, token string) bool {
	for _, s := range spec {
		if s == token {
			return true
		}
	}
	return false
}

func TestMasqueradeSpecs(t *testing.T) {
	t.Run("no interfaces falls back to global masquerade", func(t *testing.T) {
		cfg := config.NewConfig()
		cfg.System.Tables.Masquerade.Interfaces = nil

		specs := masqueradeSpecs(&cfg)
		if len(specs) != 1 {
			t.Fatalf("expected 1 spec, got %d", len(specs))
		}
		if strings.Join(specs[0], " ") != "-j MASQUERADE" {
			t.Errorf("expected global masquerade spec, got %v", specs[0])
		}
	})

	t.Run("per-interface specs", func(t *testing.T) {
		cfg := config.NewConfig()
		cfg.System.Tables.Masquerade.Interfaces = []string{"eth0", "ppp0"}

		specs := masqueradeSpecs(&cfg)
		if len(specs) != 2 {
			t.Fatalf("expected 2 specs, got %d", len(specs))
		}
		want := [][]string{
			{"-o", "eth0", "-j", "MASQUERADE"},
			{"-o", "ppp0", "-j", "MASQUERADE"},
		}
		for i, w := range want {
			if strings.Join(specs[i], " ") != strings.Join(w, " ") {
				t.Errorf("spec %d = %v, want %v", i, specs[i], w)
			}
		}
	})
}

func TestMasqueradeLogLabel(t *testing.T) {
	cfg := config.NewConfig()

	cfg.System.Tables.Masquerade.Interfaces = nil
	if got := masqueradeLogLabel(&cfg); got != "all" {
		t.Errorf("empty interfaces label = %q, want all", got)
	}

	cfg.System.Tables.Masquerade.Interfaces = []string{"eth0", "ppp0"}
	if got := masqueradeLogLabel(&cfg); got != "eth0, ppp0" {
		t.Errorf("label = %q, want \"eth0, ppp0\"", got)
	}
}

func TestMasqChainName(t *testing.T) {
	if masqChainName != "B4_MASQ" {
		t.Errorf("masqChainName = %q, want B4_MASQ (clear/apply must agree on the chain name)", masqChainName)
	}
}

func TestBuildMasqueradeManifest_GlobalStructure(t *testing.T) {
	cfg := config.NewConfig()
	cfg.System.Tables.Masquerade.Enabled = true
	cfg.System.Tables.Masquerade.Interfaces = nil
	manager := NewIPTablesManager(&cfg, false)

	chains, rules := manager.buildMasqueradeManifest("iptables")

	if len(chains) != 1 || chains[0].Name != masqChainName || chains[0].Table != "nat" {
		t.Fatalf("expected one nat %s chain, got %+v", masqChainName, chains)
	}

	var jumps, postroutingMasq int
	for _, r := range rules {
		if r.Chain == "POSTROUTING" {
			if !specContains(r.Spec, masqChainName) {
				t.Errorf("unexpected rule directly in POSTROUTING: %v", r.Spec)
			}
			if specContains(r.Spec, "MASQUERADE") {
				postroutingMasq++
			}
			jumps++
			continue
		}
		if r.Chain != masqChainName {
			t.Errorf("rule in unexpected chain %q: %v", r.Chain, r.Spec)
		}
	}

	if jumps != 1 {
		t.Errorf("expected exactly one POSTROUTING jump to %s, got %d", masqChainName, jumps)
	}
	if postroutingMasq != 0 {
		t.Errorf("expected no MASQUERADE rule directly in POSTROUTING, got %d", postroutingMasq)
	}

	// First rule inside the chain must be the mark-bypass RETURN, ahead of any MASQUERADE.
	var firstChainRule []string
	for _, r := range rules {
		if r.Chain == masqChainName {
			firstChainRule = r.Spec
			break
		}
	}
	if !specContains(firstChainRule, "RETURN") {
		t.Errorf("expected mark-bypass RETURN first in chain, got %v", firstChainRule)
	}
}

func TestBuildMasqueradeManifest_PerInterface(t *testing.T) {
	cfg := config.NewConfig()
	cfg.System.Tables.Masquerade.Enabled = true
	cfg.System.Tables.Masquerade.Interfaces = []string{"eth0", "ppp0"}
	manager := NewIPTablesManager(&cfg, false)

	_, rules := manager.buildMasqueradeManifest("iptables")

	seen := map[string]bool{}
	for _, r := range rules {
		if r.Chain == masqChainName && specContains(r.Spec, "MASQUERADE") && specContains(r.Spec, "-o") {
			for i, s := range r.Spec {
				if s == "-o" && i+1 < len(r.Spec) {
					seen[r.Spec[i+1]] = true
				}
			}
		}
	}

	for _, iface := range []string{"eth0", "ppp0"} {
		if !seen[iface] {
			t.Errorf("expected a MASQUERADE rule for interface %q inside %s", iface, masqChainName)
		}
	}
}
