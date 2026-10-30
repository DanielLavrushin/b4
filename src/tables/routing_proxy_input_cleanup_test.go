package tables

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func onlyIPTablesBackend(t *testing.T) {
	t.Helper()
	stubBinaries(t, backendIPTables)
	for _, fam := range []string{backendIP6Tables, backendIPTablesLegacy, backendIP6TablesLegacy} {
		fam := fam
		prev, had := hasBinaryCache.Load(fam)
		t.Cleanup(func() {
			if had {
				hasBinaryCache.Store(fam, prev)
			} else {
				hasBinaryCache.Delete(fam)
			}
		})
		hasBinaryCache.Store(fam, false)
	}
}

func TestProxyInputAcceptIsInsertedAboveWhateverTheFirmwareLeftInInput(t *testing.T) {
	onlyIPTablesBackend(t)
	var inserts []string
	prevRun, prevLogged := run, runLogged
	t.Cleanup(func() {
		run, runLogged = prevRun, prevLogged
	})
	run = func(args ...string) (string, error) {
		return "", errors.New("no such rule")
	}
	runLogged = func(op string, args ...string) bool {
		inserts = append(inserts, strings.Join(args, " "))
		return true
	}
	var be routeBackend = &routeIptBackend{}
	addProxyInputAccept(be, 0x8000)
	want := []string{
		"iptables -w -t filter -I INPUT 1 -m mark --mark 0x8000/0x8000 -j ACCEPT",
		"iptables -w -t mangle -I INPUT 1 -m mark --mark 0x8000/0x8000 -j ACCEPT",
	}
	if !slices.Equal(inserts, want) {
		t.Errorf("the exemption must be inserted at the top of INPUT in both tables, because a firmware drop that lands in mangle INPUT ahead of ours kills the proxied connection before the filter rule is ever consulted:\ngot:\n%s\nwant:\n%s",
			strings.Join(inserts, "\n"), strings.Join(want, "\n"))
	}
}

func TestProxyInputAcceptInMangleSitsBelowB4sDNSCopyJumpOnlyWhenTheJumpHeadsTheChain(t *testing.T) {
	const header = "Chain INPUT (policy ACCEPT)\nnum  target     prot opt source               destination\n"
	const copyJump = "B4_DNSCOPY  udp  --  0.0.0.0/0            0.0.0.0/0            udp dpt:53\n"
	const firmware = "_NDM_HTTP_INPUT_TLS_  tcp  --  0.0.0.0/0            0.0.0.0/0            tcp dpt:443\n"
	for _, tc := range []struct {
		name    string
		listing string
		slot    string
	}{
		{"b4's copy jump heads the chain", header + "1    " + copyJump + "2    " + firmware, "2"},
		{"a firmware rule sits above b4's copy jump", header + "1    " + firmware + "2    " + copyJump, "1"},
		{"no copy jump", header + "1    " + firmware, "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			onlyIPTablesBackend(t)
			var inserts []string
			prevRun, prevLogged := run, runLogged
			t.Cleanup(func() {
				run, runLogged = prevRun, prevLogged
			})
			run = func(args ...string) (string, error) {
				if slices.Contains(args, "mangle") && slices.Contains(args, "-L") {
					return tc.listing, nil
				}
				return "", errors.New("no such rule")
			}
			runLogged = func(op string, args ...string) bool {
				inserts = append(inserts, strings.Join(args, " "))
				return true
			}
			var be routeBackend = &routeIptBackend{}
			addProxyInputAccept(be, 0x8000)
			want := []string{
				"iptables -w -t filter -I INPUT 1 -m mark --mark 0x8000/0x8000 -j ACCEPT",
				"iptables -w -t mangle -I INPUT " + tc.slot + " -m mark --mark 0x8000/0x8000 -j ACCEPT",
			}
			if !slices.Equal(inserts, want) {
				t.Errorf("in mangle INPUT the exemption must go right below b4's DNS copy jump when that jump heads the chain, so a proxied DNS query is still copied, and at the top otherwise, so no firmware rule ever sits above it:\ngot:\n%s\nwant:\n%s",
					strings.Join(inserts, "\n"), strings.Join(want, "\n"))
			}
		})
	}
}

func TestPerSetProxyCleanupRemovesTheInputExemptionFromEveryTable(t *testing.T) {
	onlyIPTablesBackend(t)
	var cmds []string
	prev := run
	t.Cleanup(func() { run = prev })
	seen := map[string]bool{}
	run = func(args ...string) (string, error) {
		joined := strings.Join(args, " ")
		if seen[joined] {
			return "", errors.New("rule not found")
		}
		seen[joined] = true
		cmds = append(cmds, joined)
		return "", nil
	}
	var be routeBackend = &routeIptBackend{}
	removeProxyInputAccept(be, 0x8000)
	want := []string{
		"iptables -w -t filter -D INPUT -m mark --mark 0x8000/0x8000 -j ACCEPT",
		"iptables -w -t mangle -D INPUT -m mark --mark 0x8000/0x8000 -j ACCEPT",
	}
	if !slices.Equal(cmds, want) {
		t.Errorf("disabling or deleting a proxy set must take the INPUT exemption out of every table it was inserted into; the per-mark cleanup used to touch only the default table, so the mangle rule outlived the set it belonged to:\ngot:\n%s\nwant:\n%s",
			strings.Join(cmds, "\n"), strings.Join(want, "\n"))
	}
}
