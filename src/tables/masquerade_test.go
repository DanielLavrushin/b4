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

	t.Run("empty and whitespace entries are trimmed and skipped", func(t *testing.T) {
		cfg := config.NewConfig()
		cfg.System.Tables.Masquerade.Interfaces = []string{"", "  eth0 ", "\t", "ppp0"}

		specs := masqueradeSpecs(&cfg)
		if len(specs) != 2 {
			t.Fatalf("expected 2 specs after filtering, got %d (%v)", len(specs), specs)
		}
		if strings.Join(specs[0], " ") != "-o eth0 -j MASQUERADE" {
			t.Errorf("spec 0 = %v, want trimmed eth0", specs[0])
		}
		if strings.Join(specs[1], " ") != "-o ppp0 -j MASQUERADE" {
			t.Errorf("spec 1 = %v, want ppp0", specs[1])
		}
	})

	t.Run("duplicate interfaces are collapsed", func(t *testing.T) {
		cfg := config.NewConfig()
		cfg.System.Tables.Masquerade.Interfaces = []string{"eth0", " eth0 ", "ppp0", "eth0"}

		specs := masqueradeSpecs(&cfg)
		if len(specs) != 2 {
			t.Fatalf("expected 2 deduped specs, got %d (%v)", len(specs), specs)
		}
		if strings.Join(specs[0], " ") != "-o eth0 -j MASQUERADE" || strings.Join(specs[1], " ") != "-o ppp0 -j MASQUERADE" {
			t.Errorf("unexpected deduped specs: %v", specs)
		}
	})

	t.Run("all-empty list falls back to global masquerade", func(t *testing.T) {
		cfg := config.NewConfig()
		cfg.System.Tables.Masquerade.Interfaces = []string{"", "   ", "\t"}

		specs := masqueradeSpecs(&cfg)
		if len(specs) != 1 || strings.Join(specs[0], " ") != "-j MASQUERADE" {
			t.Errorf("expected global masquerade fallback, got %v", specs)
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

	var jumps, postroutingMasq, postroutingReturn int
	returnIdx, jumpIdx := -1, -1
	for i, r := range rules {
		if r.Chain == "POSTROUTING" {
			if specContains(r.Spec, "MASQUERADE") {
				postroutingMasq++
			}
			if specContains(r.Spec, "RETURN") {
				postroutingReturn++
				returnIdx = i
				// The mark-bypass must short-circuit the whole built-in chain, so it
				// has to be inserted (-I), not appended after the jump.
				if r.Action != "I" {
					t.Errorf("mark-bypass RETURN must use -I to sit above the jump, got action %q", r.Action)
				}
			}
			if specContains(r.Spec, masqChainName) {
				jumps++
				jumpIdx = i
			}
			continue
		}
		if r.Chain != masqChainName {
			t.Errorf("rule in unexpected chain %q: %v", r.Chain, r.Spec)
		}
		if specContains(r.Spec, "RETURN") {
			t.Errorf("mark-bypass RETURN must live in POSTROUTING, not inside %s", masqChainName)
		}
	}

	if jumps != 1 {
		t.Errorf("expected exactly one POSTROUTING jump to %s, got %d", masqChainName, jumps)
	}
	if postroutingReturn != 1 {
		t.Errorf("expected exactly one mark-bypass RETURN in POSTROUTING, got %d", postroutingReturn)
	}
	if postroutingMasq != 0 {
		t.Errorf("expected no MASQUERADE rule directly in POSTROUTING, got %d", postroutingMasq)
	}
	if returnIdx == -1 || jumpIdx == -1 || returnIdx > jumpIdx {
		t.Errorf("mark-bypass RETURN must be emitted before the jump (return idx %d, jump idx %d)", returnIdx, jumpIdx)
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

func TestMasqueradeRulesPresent(t *testing.T) {
	postrouting := strings.Join([]string{
		"-P POSTROUTING ACCEPT",
		"-A POSTROUTING -m mark --mark 0x20000000/0x20000000 -j RETURN",
		"-A POSTROUTING -j B4_MASQ",
	}, "\n")
	masqChain := strings.Join([]string{
		"-N B4_MASQ",
		"-A B4_MASQ -j MASQUERADE",
	}, "\n")

	t.Run("manifest layout passes the monitor check", func(t *testing.T) {
		if !masqueradeRulesPresent(postrouting, masqChain) {
			t.Error("expected masquerade layout with B4_MASQ chain to be detected as present")
		}
	})

	t.Run("missing POSTROUTING jump detected", func(t *testing.T) {
		if masqueradeRulesPresent("-P POSTROUTING ACCEPT", masqChain) {
			t.Error("expected missing POSTROUTING jump to be detected")
		}
	})

	t.Run("empty masquerade chain detected", func(t *testing.T) {
		if masqueradeRulesPresent(postrouting, "-N B4_MASQ") {
			t.Error("expected empty masquerade chain to be detected")
		}
	})

	t.Run("per-interface rules pass", func(t *testing.T) {
		perIface := "-N B4_MASQ\n-A B4_MASQ -o eth0 -j MASQUERADE\n-A B4_MASQ -o ppp0 -j MASQUERADE"
		if !masqueradeRulesPresent(postrouting, perIface) {
			t.Error("expected per-interface masquerade rules to be detected as present")
		}
	})
}
