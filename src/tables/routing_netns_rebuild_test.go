package tables

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

type rebuildSample struct {
	jump    bool
	rules   bool
	ipRule  bool
	route   bool
	members int
}

func rebuildConfig(engine string) *config.Config {
	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = true
	cfg.Queue.IPv6Enabled = false
	cfg.Queue.Threads = 1
	cfg.Queue.Mark = 0x8000
	cfg.System.Tables.Engine = engine
	cfg.System.Tables.SkipSetup = false

	quiet := config.NewSetConfig()
	quiet.Id = "netns-rebuild-quiet"
	quiet.Name = "quiet"
	quiet.Enabled = true
	quiet.Routing.Enabled = true
	quiet.Routing.Mode = config.RoutingModeInterface
	quiet.Routing.EgressInterface = netnsSecondary
	quiet.Targets.IPs = []string{netnsTarget}
	quiet.Targets.IpsToMatch = []string{netnsTarget}

	moving := config.NewSetConfig()
	moving.Id = "netns-rebuild-moving"
	moving.Name = "moving"
	moving.Enabled = true
	moving.Routing.Enabled = true
	moving.Routing.Mode = config.RoutingModeInterface
	moving.Routing.EgressInterface = netnsSecondary
	moving.Routing.KillSwitch = true
	moving.Targets.IPs = []string{"198.51.100.8"}
	moving.Targets.IpsToMatch = []string{"198.51.100.8"}

	cfg.Sets = []*config.SetConfig{&quiet, &moving}
	return &cfg
}

func rebuildProbe(engine, chain, setName string) rebuildSample {
	s := rebuildSample{}
	if engine == backendNFTables {
		if out, err := run("nft", "list", "chain", "inet", routeNftTable, routeNftPrerouting); err == nil {
			s.jump = strings.Contains(out, "jump "+chain)
		}
		if out, err := run("nft", "list", "chain", "inet", routeNftTable, chain); err == nil {
			for _, line := range strings.Split(out, "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "table ") || strings.HasPrefix(line, "chain ") || line == "}" {
					continue
				}
				s.rules = true
			}
		}
	} else {
		if out, err := run("iptables", "-t", "mangle", "-L", "PREROUTING", "-n"); err == nil {
			s.jump = strings.Contains(out, chain)
		}
		if out, err := run("iptables", "-t", "mangle", "-L", chain, "-n"); err == nil {
			for _, line := range strings.Split(out, "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "Chain ") || strings.HasPrefix(line, "target ") {
					continue
				}
				s.rules = true
			}
		}
		if out, err := run("ipset", "list", setName); err == nil {
			body := false
			for _, line := range strings.Split(out, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "Members") {
					body = true
					continue
				}
				if body && line != "" {
					s.members++
				}
			}
		} else {
			s.members = -1
		}
	}

	out, err := run("ip", "rule", "show")
	if err != nil {
		return s
	}
	var tables []string
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasSuffix(routeRuleField(line, "fwmark"), "/0x27fff") {
			continue
		}
		if lookup := routeRuleField(line, "lookup"); lookup != "" {
			tables = append(tables, lookup)
		}
	}
	if len(tables) == 0 {
		return s
	}
	s.ipRule = true
	s.route = true
	for _, table := range tables {
		routes, routeErr := run("ip", "route", "show", "table", table)
		if routeErr != nil {
			s.route = false
			continue
		}
		reachable := false
		for _, line := range strings.Split(routes, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "default") {
				reachable = true
			}
		}
		if !reachable {
			s.route = false
		}
	}
	return s
}

func netnsRebuildIsNeverDisarmed(t *testing.T, engine string) {
	t.Helper()
	netnsRequire(t)
	netnsSetupLinks(t)

	routeEngine = nil
	defer func() { routeEngine = nil }()

	cfg := rebuildConfig(engine)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	defer func() { _ = ClearRules(cfg) }()

	RoutingSyncConfig(cfg)
	defer RoutingClearAll()

	routeMu.Lock()
	st, cached := routeRuleCache["netns-rebuild-moving"]
	routeMu.Unlock()
	if !cached {
		t.Fatal("the set built no routing state")
	}

	learned := []string{"203.0.113.9", "203.0.113.10"}
	getRouteBackend(cfg).addElements(st.setV4, learned, 3600)

	setName := st.setV4
	if engine == backendNFTables {
		setName = ""
	}
	if base := rebuildProbe(engine, st.chainPre, setName); !base.jump || !base.rules || !base.ipRule || !base.route {
		t.Fatalf("the set is not fully installed before the test even starts: %+v", base)
	}

	var mu sync.Mutex
	var disarmed []rebuildSample
	minMembers := 1 << 30
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			s := rebuildProbe(engine, st.chainPre, setName)
			mu.Lock()
			if !s.jump || !s.rules || !s.ipRule || !s.route {
				disarmed = append(disarmed, s)
			}
			if setName != "" && s.members < minMembers {
				minMembers = s.members
			}
			mu.Unlock()
		}
	}()

	for cycle := 0; cycle < 2; cycle++ {
		cfg.Sets[1].Routing.EgressInterface = netnsPrimary
		RoutingSyncConfig(cfg)
		time.Sleep(200 * time.Millisecond)
		cfg.Sets[1].Routing.EgressInterface = netnsSecondary
		RoutingSyncConfig(cfg)
		time.Sleep(200 * time.Millisecond)
	}
	RoutingForceResync(cfg)
	time.Sleep(200 * time.Millisecond)
	cfg.Sets[0].Routing.EgressInterface = netnsPrimary
	RoutingSyncConfig(cfg)
	time.Sleep(200 * time.Millisecond)
	cfg.Sets[0].Routing.EgressInterface = netnsSecondary
	RoutingSyncConfig(cfg)
	time.Sleep(200 * time.Millisecond)

	close(stop)
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(disarmed) > 0 {
		t.Fatalf("reconfiguring a set left it matching nothing in %d samples, so every connection opened in that window leaves by the ordinary uplink and conntrack keeps it there for its whole life; first: %+v",
			len(disarmed), disarmed[0])
	}
	if setName != "" && minMembers < len(learned)+1 {
		t.Fatalf("the set was down to %d addresses during the rebuild, so the addresses it learned from DNS were thrown away and traffic to them stopped being routed until they were learned again",
			minMembers)
	}
}

func TestNetnsAReconfiguredSetIsNeverDisarmed(t *testing.T) {
	netnsRebuildIsNeverDisarmed(t, backendIPTables)
}

func TestNetnsAReconfiguredSetIsNeverDisarmedNft(t *testing.T) {
	netnsRebuildIsNeverDisarmed(t, backendNFTables)
}
