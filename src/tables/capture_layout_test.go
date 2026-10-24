package tables

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

type captureLayout struct {
	name         string
	devices      bool
	whiteIsBlack bool
	manual       bool
}

var captureLayouts = []captureLayout{
	{name: "postrouting"},
	{name: "device filter", devices: true},
	{name: "device filter excluding devices", devices: true, whiteIsBlack: true},
	{name: "device filter of a device added by hand", devices: true, manual: true},
	{name: "device filter excluding a device added by hand", devices: true, whiteIsBlack: true, manual: true},
}

func (l captureLayout) apply(cfg *config.Config, mac, ip string) {
	cfg.Queue.Devices.Enabled = l.devices
	cfg.Queue.Devices.WhiteIsBlack = l.whiteIsBlack
	device := config.Device{MAC: mac, Selected: true}
	if l.manual {
		device = config.Device{MAC: "02:B4:00:00:00:01", IP: ip, Selected: true, IsManual: true}
	}
	cfg.Queue.Devices.Devices = append(cfg.Queue.Devices.Devices, device)
}

func captureLayoutConfig(backend string, l captureLayout) *config.Config {
	cfg := config.NewConfig()
	cfg.System.Tables.Engine = backend
	cfg.Queue.IPv4Enabled = true
	cfg.Queue.MSSClamp.Enabled, cfg.Queue.MSSClamp.Size = true, 600
	cfg.Queue.Devices.Devices = []config.Device{{MAC: "AA:BB:CC:DD:EE:02", MSSClamp: 700}}
	l.apply(&cfg, "AA:BB:CC:DD:EE:01", "192.168.51.2")
	cfg.Sets = []*config.SetConfig{mssClampSet("mss", 800, []string{"203.0.113.7"}, nil)}
	return &cfg
}

func clampRulesLead(t *testing.T, where string, rules []string, isClamp func(string) bool) {
	t.Helper()
	clamps, other := 0, ""
	for _, rule := range rules {
		if !isClamp(rule) {
			if other == "" {
				other = rule
			}
			continue
		}
		clamps++
		if other != "" {
			t.Errorf("%s: the MSS clamp %q comes after %q; a packet that rule queues or accepts skips the rest of the chain and leaves unclamped", where, rule, other)
		}
	}
	if clamps == 0 {
		t.Errorf("%s holds no MSS clamp rule, so this check proves nothing: %q", where, rules)
	}
}

func iptJumpsTo(c *goldenIptChain, target string) int {
	if c == nil {
		return 0
	}
	n := 0
	for _, rule := range c.rules {
		if goldenIptTarget(rule) == target {
			n++
		}
	}
	return n
}

func iptJumpsToB4(c *goldenIptChain) int {
	return iptJumpsTo(c, iptChainName)
}

func TestTheRouterOwnPacketsMeetB4OnceOnIptables(t *testing.T) {
	for _, l := range captureLayouts {
		t.Run(l.name, func(t *testing.T) {
			h := newGoldenHost(t)
			goldenIsolate(t, h)
			cfg := captureLayoutConfig(backendIPTables, l)
			if err := AddRules(cfg); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			mangle := h.iptTable(backendIPTables, "mangle")
			output, post := iptJumpsToB4(mangle.chain("OUTPUT")), iptJumpsToB4(mangle.chain("POSTROUTING"))
			forward, gated := iptJumpsTo(mangle.chain("FORWARD"), iptForwardChainName), iptJumpsToB4(mangle.chain(iptForwardChainName))
			switch {
			case !l.devices && (output != 0 || post != 1 || forward != 0 || iptJumpsToB4(mangle.chain("FORWARD")) != 0):
				t.Errorf("jumps to %s: OUTPUT %d, POSTROUTING %d, FORWARD %d; without a device filter POSTROUTING alone must jump there, since the router's own packets pass POSTROUTING too and a jump from OUTPUT queues each of them twice", iptChainName, output, post, forward)
			case l.devices && (output != 1 || post != 0 || forward != 1 || gated == 0 || iptJumpsToB4(mangle.chain("FORWARD")) != 0):
				t.Errorf("jumps to %s: OUTPUT %d, POSTROUTING %d, from %s %d, FORWARD to %s %d; with a device filter FORWARD jumps to %s once, which jumps there for the devices, and OUTPUT for the router's own packets, which never cross FORWARD", iptChainName, output, post, iptForwardChainName, gated, iptForwardChainName, forward, iptForwardChainName)
			}
			if l.manual {
				want := []string{"-s 192.168.51.2 -j " + iptChainName}
				if l.whiteIsBlack {
					want = []string{"-s 192.168.51.2 -j RETURN", "-j " + iptChainName}
				}
				if got := mangle.chain(iptForwardChainName).rules; !slices.Equal(got, want) {
					t.Errorf("%s holds %q, want %q: a device added by hand has no MAC address on the wire, so only its address matches it", iptForwardChainName, got, want)
				}
			}

			rules := mangle.chain("OUTPUT").rules
			accept := slices.Index(rules, "-m mark --mark 0x8000/0x8000 -j ACCEPT")
			for what, present := range map[string]bool{
				"the ACCEPT for b4's own packets": accept >= 0,
				"the CONNMARK save":               slices.ContainsFunc(rules, func(r string) bool { return goldenIptTarget(r) == "CONNMARK" }),
				"the DNS answer queue rule":       slices.ContainsFunc(rules, func(r string) bool { return strings.Contains(r, "--sport 53") && goldenIptTarget(r) == "NFQUEUE" }),
			} {
				if !present {
					t.Errorf("mangle OUTPUT lost %s: %q", what, rules)
				}
			}
			if jump := slices.IndexFunc(rules, func(r string) bool { return goldenIptTarget(r) == iptChainName }); jump >= 0 && jump < accept {
				t.Errorf("mangle OUTPUT jumps to %s above the ACCEPT for b4's own packets: %q", iptChainName, rules)
			}

			isTCPMSS := func(r string) bool { return goldenIptTarget(r) == "TCPMSS" }
			for _, chain := range []string{"OUTPUT", "FORWARD", "B4_PREROUTING"} {
				clampRulesLead(t, "mangle "+chain, mangle.chain(chain).rules, isTCPMSS)
			}

			m := &Monitor{}
			if !m.checkRules(cfg) {
				t.Fatalf("the monitor finds rules missing right after they were installed, so it rebuilds them on every check")
			}
			if !l.devices {
				return
			}
			out := mangle.chain("OUTPUT")
			out.rules = slices.DeleteFunc(out.rules, func(r string) bool { return goldenIptTarget(r) == iptChainName })
			if m.checkRules(cfg) {
				t.Errorf("with a device filter the router's own packets reach %s only from mangle OUTPUT, and that jump is gone, yet the monitor finds nothing missing", iptChainName)
			}
			if err := AddRules(cfg); err != nil {
				t.Fatalf("restore: %v", err)
			}
			if got := iptJumpsToB4(out); got != 1 {
				t.Errorf("after the restore mangle OUTPUT jumps to %s %d times, want once", iptChainName, got)
			}
			if !m.checkRules(cfg) {
				t.Errorf("the monitor still finds rules missing after the restore")
			}
		})
	}
}

func TestAnUpgradeTakesAwayTheOutputJumpEarlierVersionsInstalled(t *testing.T) {
	h := newGoldenHost(t)
	goldenIsolate(t, h)
	plain := captureLayoutConfig(backendIPTables, captureLayouts[0])
	devices := captureLayoutConfig(backendIPTables, captureLayouts[1])
	output := func() *goldenIptChain { return h.iptTable(backendIPTables, "mangle").chain("OUTPUT") }

	if err := AddRules(plain); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	output().rules = append(output().rules, "-j "+iptChainName)
	if err := ClearRules(plain); err != nil {
		t.Fatalf("ClearRules: %v", err)
	}
	if got := iptJumpsToB4(output()); got != 0 {
		t.Errorf("the clear left %d jumps from mangle OUTPUT to %s that an earlier version installed; they keep queueing the router's own packets a second time", got, iptChainName)
	}
	if h.iptTable(backendIPTables, "mangle").chain(iptChainName) != nil {
		t.Errorf("the clear left the %s chain behind", iptChainName)
	}
	if err := AddRules(plain); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	if got := iptJumpsToB4(output()); got != 0 {
		t.Errorf("mangle OUTPUT jumps to %s %d times without a device filter", iptChainName, got)
	}

	if err := RefreshRules(devices); err != nil {
		t.Fatalf("RefreshRules to the device filter: %v", err)
	}
	if got := iptJumpsToB4(output()); got != 1 {
		t.Errorf("after turning the device filter on mangle OUTPUT jumps to %s %d times, want once", iptChainName, got)
	}
	if err := RefreshRules(plain); err != nil {
		t.Fatalf("RefreshRules back: %v", err)
	}
	if got := iptJumpsToB4(output()); got != 0 {
		t.Errorf("after turning the device filter off mangle OUTPUT still jumps to %s %d times", iptChainName, got)
	}
}

type nftEmitted struct {
	order  []string
	heads  map[string]string
	chains map[string][]string
}

func nftEmittedTable(lines []string) nftEmitted {
	fresh := func() nftEmitted { return nftEmitted{heads: map[string]string{}, chains: map[string][]string{}} }
	e := fresh()
	addChain := "exec nft add chain inet " + nftTableName + " "
	addRule := "exec nft add rule inet " + nftTableName + " "
	insertRule := "exec nft insert rule inet " + nftTableName + " "
	for _, line := range lines {
		switch {
		case line == "exec nft delete table inet "+nftTableName:
			e = fresh()
		case strings.HasPrefix(line, addChain):
			name, head, _ := strings.Cut(strings.TrimPrefix(line, addChain), " ")
			if _, ok := e.chains[name]; !ok {
				e.order = append(e.order, name)
				e.chains[name] = nil
			}
			e.heads[name] = strings.Trim(head, "{} ")
		case strings.HasPrefix(line, addRule):
			chain, rule, _ := strings.Cut(strings.TrimPrefix(line, addRule), " ")
			e.chains[chain] = append(e.chains[chain], rule)
		case strings.HasPrefix(line, insertRule):
			chain, rule, _ := strings.Cut(strings.TrimPrefix(line, insertRule), " ")
			e.chains[chain] = append([]string{rule}, e.chains[chain]...)
		}
	}
	return e
}

func serveNftListings(t *testing.T, e nftEmitted) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range e.order {
		var b strings.Builder
		fmt.Fprintf(&b, "table inet %s {\n\tchain %s {\n", nftTableName, name)
		if head := e.heads[name]; head != "" {
			fmt.Fprintf(&b, "\t\t%s\n", head)
		}
		for _, rule := range e.chains[name] {
			fmt.Fprintf(&b, "\t\t%s\n", rule)
		}
		b.WriteString("\t}\n}\n")
		if err := os.WriteFile(filepath.Join(dir, "chain."+name), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stub := "#!/bin/sh\nd='" + dir + "'\n" + `case "$1 $2" in
"list tables") printf 'table inet %s\n' "` + nftTableName + `" ;;
"list chain")
	if [ ! -f "$d/chain.$5" ]; then
		printf 'Error: No such file or directory\n' >&2
		exit 1
	fi
	while IFS= read -r line; do
		printf '%s\n' "$line"
	done < "$d/chain.$5"
	;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(dir, "nft"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestNFTablesTheRouterOwnPacketsMeetB4OnceAndTheClampComesFirst(t *testing.T) {
	for _, l := range captureLayouts {
		t.Run(l.name, func(t *testing.T) {
			h := newGoldenHost(t)
			goldenIsolate(t, h)
			cfg := captureLayoutConfig(backendNFTables, l)
			if err := AddRules(cfg); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			table := nftEmittedTable(h.take())
			jumps := func(chain string) int {
				n := 0
				for _, rule := range table.chains[chain] {
					if strings.HasSuffix(rule, "jump "+nftChainName) {
						n++
					}
				}
				return n
			}
			output, post, forward := jumps("output"), jumps("postrouting"), jumps("forward")
			switch {
			case !l.devices && (output != 0 || post != 1 || forward != 0):
				t.Errorf("jumps to %s: output %d, postrouting %d, forward %d; without a device filter postrouting alone must jump there, since the router's own packets pass postrouting too and a jump from output queues each of them twice", nftChainName, output, post, forward)
			case l.devices && (output != 1 || post != 0 || forward == 0):
				t.Errorf("jumps to %s: output %d, postrouting %d, forward %d; with a device filter forward jumps there for the devices and output for the router's own packets, which never cross forward", nftChainName, output, post, forward)
			}

			rules := table.chains["output"]
			accept := slices.Index(rules, "meta mark & 0x8000 == 0x8000 accept")
			for what, present := range map[string]bool{
				"the return for loopback":         slices.Contains(rules, `oifname "lo" return`),
				"the accept for b4's own packets": accept >= 0,
				"the connection mark save":        slices.ContainsFunc(rules, func(r string) bool { return strings.Contains(r, "ct mark set ct mark | 0x8000") }),
				"the DNS answer queue rule":       slices.ContainsFunc(rules, func(r string) bool { return strings.Contains(r, "udp sport 53") && strings.Contains(r, " queue ") }),
			} {
				if !present {
					t.Errorf("output lost %s: %q", what, rules)
				}
			}
			if jump := slices.Index(rules, "jump "+nftChainName); jump >= 0 && jump < accept {
				t.Errorf("output jumps to %s above the accept for b4's own packets: %q", nftChainName, rules)
			}

			isClamp := func(r string) bool { return strings.Contains(r, "maxseg size set") }
			for _, chain := range []string{"prerouting", "output", "forward"} {
				clampRulesLead(t, chain, table.chains[chain], isClamp)
			}
			if l.manual {
				want := []string{"ip saddr 192.168.51.2 jump " + nftChainName}
				if l.whiteIsBlack {
					want = []string{"ip saddr 192.168.51.2 return", "jump " + nftChainName}
				}
				if got := slices.DeleteFunc(slices.Clone(table.chains["forward"]), isClamp); !slices.Equal(got, want) {
					t.Errorf("forward holds %q besides the MSS clamp, want %q: a device added by hand has no MAC address on the wire, so only its address matches it", got, want)
				}
			}

			serveNftListings(t, table)
			m := &Monitor{}
			if !m.checkRules(cfg) {
				t.Fatalf("the monitor finds rules missing right after they were installed, so it rebuilds them on every check")
			}
			if !l.devices {
				return
			}
			table.chains["output"] = slices.DeleteFunc(slices.Clone(rules), func(r string) bool { return r == "jump "+nftChainName })
			serveNftListings(t, table)
			if m.checkRules(cfg) {
				t.Errorf("with a device filter the router's own packets reach %s only from output, and that jump is gone, yet the monitor finds nothing missing", nftChainName)
			}
		})
	}
}
