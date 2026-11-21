package tables

import (
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

const (
	netnsCollectedClampAddr = "203.0.113.77"
	netnsCollectedClampSize = 600
)

func netnsCollectedClampServer(t *testing.T) func(port int) int {
	t.Helper()
	ln, err := net.Listen("tcp4", net.JoinHostPort(netnsCollectedClampAddr, "443"))
	if err != nil {
		t.Fatalf("listen on %s:443: %v", netnsCollectedClampAddr, err)
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	seen := map[int]int{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			if mss, err := netnsTCPMaxSeg(c); err == nil {
				mu.Lock()
				seen[c.RemoteAddr().(*net.TCPAddr).Port] = mss
				mu.Unlock()
			}
			c.Close()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		wg.Wait()
	})
	return func(port int) int {
		deadline := time.Now().Add(2 * time.Second)
		for {
			mu.Lock()
			mss, ok := seen[port]
			mu.Unlock()
			if ok || time.Now().After(deadline) {
				return mss
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func netnsCollectedClampDial(t *testing.T, server func(int) int) (client, atServer int) {
	t.Helper()
	d := net.Dialer{Timeout: 3 * time.Second}
	c, err := d.Dial("tcp4", net.JoinHostPort(netnsCollectedClampAddr, "443"))
	if err != nil {
		t.Fatalf("dial %s:443: %v", netnsCollectedClampAddr, err)
	}
	defer c.Close()
	client, err = netnsTCPMaxSeg(c)
	if err != nil {
		t.Fatalf("read TCP_MAXSEG: %v", err)
	}
	return client, server(c.LocalAddr().(*net.TCPAddr).Port)
}

func netnsCollectedSetHolds(t *testing.T, engine, name, addr string) bool {
	t.Helper()
	var out string
	if engine == backendNFTables {
		out = netnsRun(t, "nft", "list", "set", "inet", nftTableName, name)
	} else {
		out = netnsRun(t, "ipset", "list", name)
	}
	for _, field := range strings.FieldsFunc(out, func(r rune) bool { return r == ' ' || r == '\n' || r == '\t' || r == ',' || r == '{' || r == '}' }) {
		if field == addr {
			return true
		}
	}
	return false
}

func netnsCollectedSetRules(t *testing.T, engine string, names ...string) []string {
	t.Helper()
	var listing string
	if engine == backendNFTables {
		listing = netnsRun(t, "nft", "list", "table", "inet", nftTableName)
	} else {
		for _, chain := range []string{"OUTPUT", "FORWARD", "PREROUTING", iptChainName} {
			out, _ := run(engine, "-w", "-t", "mangle", "-S", chain)
			listing += out
		}
	}
	var rules []string
	for _, line := range strings.Split(listing, "\n") {
		line = strings.TrimSpace(line)
		for _, name := range names {
			if strings.Contains(line, "@"+name+" ") || strings.Contains(line, "--match-set "+name+" ") {
				rules = append(rules, netnsRuleCounters.ReplaceAllString(line, "counter"))
				break
			}
		}
	}
	slices.Sort(rules)
	return rules
}

var netnsRuleCounters = regexp.MustCompile(`counter packets [0-9]+ bytes [0-9]+`)

func netnsNftSetRulePackets(t *testing.T, name string) int {
	t.Helper()
	total := 0
	for _, line := range strings.Split(netnsRun(t, "nft", "list", "table", "inet", nftTableName), "\n") {
		if !strings.Contains(line, "@"+name+" ") {
			continue
		}
		f := strings.Fields(line)
		for i := 0; i+1 < len(f); i++ {
			if f[i] == "packets" {
				n, _ := strconv.Atoi(f[i+1])
				total += n
			}
		}
	}
	return total
}

func TestNetnsCollectedAddressesGetTheSetsMSSClampWithoutARebuild(t *testing.T) {
	for _, engine := range []string{backendIPTables, backendNFTables} {
		t.Run(engine, func(t *testing.T) {
			netnsRequire(t)
			netnsRequireEngine(t, engine)
			if engine != backendNFTables {
				if err := ipsetMatchProbe(NewIPTablesManager(netnsConfig(engine), false), engine); err != nil {
					t.Skipf("the iptables set match does not work in this namespace (%v); the lab guests run this half", err)
				}
			}
			routeEngine = nil
			defer func() { routeEngine = nil }()
			netnsRun(t, "ip", "link", "set", "lo", "up")
			netnsRun(t, "ip", "addr", "add", netnsCollectedClampAddr+"/32", "dev", "lo")
			t.Cleanup(func() { _, _ = run("ip", "addr", "del", netnsCollectedClampAddr+"/32", "dev", "lo") })
			server := netnsCollectedClampServer(t)

			cfg := netnsConfig(engine)
			clamp := config.NewSetConfig()
			clamp.Id, clamp.Name, clamp.Enabled = "collected-clamp", "collected clamp", true
			clamp.Targets.UseCollectedIPs = true
			clamp.MSSClamp.Enabled, clamp.MSSClamp.Size = true, netnsCollectedClampSize
			dup := config.NewSetConfig()
			dup.Id, dup.Name, dup.Enabled = "collected-dup", "collected dup", true
			dup.Targets.UseCollectedIPs = true
			dup.TCP.Duplicate.Enabled, dup.TCP.Duplicate.Count = true, 2
			cfg.Sets = []*config.SetConfig{&clamp, &dup}
			if err := cfg.Validate(); err != nil {
				t.Fatalf("a clamp and a duplication set whose only addresses are collected ones must validate: %v", err)
			}
			if err := AddRules(cfg.WithCollectedIPs(nil)); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			t.Cleanup(func() { _ = ClearRules(cfg.WithCollectedIPs(nil)) })

			monitorAgrees := func(phase string, view *config.Config) {
				t.Helper()
				rulesMu.Lock()
				ok := (&Monitor{}).checkRules(view)
				rulesMu.Unlock()
				if !ok {
					t.Errorf("%s: the monitor finds the rules b4 just wrote missing, so it would rebuild the firewall on every tick", phase)
				}
			}
			monitorAgrees("with the pool empty", cfg.WithCollectedIPs(nil))

			mssName, dupName := captureCollectedMSSName(0, false), captureCollectedDupName(false)
			rules := netnsCollectedSetRules(t, engine, mssName, dupName)
			for _, name := range []string{mssName, dupName} {
				if netnsCollectedSetHolds(t, engine, name, netnsCollectedClampAddr) {
					t.Fatalf("%s holds %s before anything was collected", name, netnsCollectedClampAddr)
				}
				if !slices.ContainsFunc(rules, func(r string) bool { return strings.Contains(r, name+" ") }) {
					t.Fatalf("no rule matches %s with the pool empty, so a collected address could only join it with a rebuild: %q", name, rules)
				}
			}

			limit := netnsCollectedClampSize - netnsTimestampOption
			client, atServer := netnsCollectedClampDial(t, server)
			if client <= netnsCollectedClampSize || atServer <= netnsCollectedClampSize {
				t.Fatalf("before %s is collected its connection already has MSS %d at the client and %d at the server, at most the clamp of %d, so this test proves nothing", netnsCollectedClampAddr, client, atServer, netnsCollectedClampSize)
			}

			if err := SyncCollectedCaptureSets(cfg.WithCollectedIPs([]string{netnsCollectedClampAddr})); err != nil {
				t.Fatalf("adding the collected address: %v", err)
			}
			for _, name := range []string{mssName, dupName} {
				if !netnsCollectedSetHolds(t, engine, name, netnsCollectedClampAddr) {
					t.Errorf("%s does not hold the collected address %s", name, netnsCollectedClampAddr)
				}
			}
			monitorAgrees("with the address collected", cfg.WithCollectedIPs([]string{netnsCollectedClampAddr}))
			queuedBefore := 0
			if engine == backendNFTables {
				queuedBefore = netnsNftSetRulePackets(t, dupName)
			}
			clampedClient, clampedServer := netnsCollectedClampDial(t, server)
			if clampedClient > limit || clampedServer > limit {
				t.Errorf("a connection to the collected address has MSS %d at the client and %d at the server, want at most %d: the set's clamp of %d missed it", clampedClient, clampedServer, limit, netnsCollectedClampSize)
			}
			queuedCollected := 0
			if engine == backendNFTables {
				queuedCollected = netnsNftSetRulePackets(t, dupName) - queuedBefore
				if queuedCollected == 0 {
					t.Errorf("no packet of the connection to the collected address reached the duplication rule of %s", dupName)
				}
			}

			if err := SyncCollectedCaptureSets(cfg.WithCollectedIPs(nil)); err != nil {
				t.Fatalf("removing the collected address: %v", err)
			}
			for _, name := range []string{mssName, dupName} {
				if netnsCollectedSetHolds(t, engine, name, netnsCollectedClampAddr) {
					t.Errorf("%s still holds %s after it left the pool", name, netnsCollectedClampAddr)
				}
			}
			monitorAgrees("after the address left the pool", cfg.WithCollectedIPs(nil))
			if engine == backendNFTables {
				queuedBefore = netnsNftSetRulePackets(t, dupName)
			}
			afterClient, afterServer := netnsCollectedClampDial(t, server)
			if afterClient <= netnsCollectedClampSize || afterServer <= netnsCollectedClampSize {
				t.Errorf("after %s left the pool its connection still has MSS %d at the client and %d at the server", netnsCollectedClampAddr, afterClient, afterServer)
			}
			if engine == backendNFTables {
				if n := netnsNftSetRulePackets(t, dupName) - queuedBefore; n != 0 {
					t.Errorf("%d packets of a connection to an address that left the pool still reached the duplication rule of %s", n, dupName)
				}
			}

			if after := netnsCollectedSetRules(t, engine, mssName, dupName); !slices.Equal(after, rules) {
				t.Errorf("the rules that match the collected sets changed while the pool did: before %q, after %q", rules, after)
			}
			t.Logf("MSS client/server: before %d/%d, collected %d/%d, removed %d/%d (clamp %d); packets queued by the collected duplication rule while collected: %d", client, atServer, clampedClient, clampedServer, afterClient, afterServer, netnsCollectedClampSize, queuedCollected)
		})
	}
}
