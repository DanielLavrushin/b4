package tables

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

type dscpNftFakeTable struct {
	chains map[string][]string
	maps   map[string][]string
	sets   map[string][]string
}

func (t *dscpNftFakeTable) clone() *dscpNftFakeTable {
	if t == nil {
		return nil
	}
	c := &dscpNftFakeTable{chains: map[string][]string{}, maps: map[string][]string{}, sets: map[string][]string{}}
	for k, v := range t.chains {
		c.chains[k] = slices.Clone(v)
	}
	for k, v := range t.maps {
		c.maps[k] = slices.Clone(v)
	}
	for k, v := range t.sets {
		c.sets[k] = slices.Clone(v)
	}
	return c
}

func (t *dscpNftFakeTable) setReferenced(name string) bool {
	for _, rules := range t.chains {
		for _, rule := range rules {
			if slices.Contains(dscpNftFakeRefs(rule), name) {
				return true
			}
		}
	}
	return false
}

func (t *dscpNftFakeTable) chainReferenced(name string) bool {
	for _, elems := range t.maps {
		for _, e := range elems {
			if dscpNftFakeTarget(e) == name {
				return true
			}
		}
	}
	return false
}

func (t *dscpNftFakeTable) lookup(name string, addr netip.Addr) (string, bool) {
	for _, e := range t.maps[name] {
		key, _, _ := strings.Cut(e, " : ")
		from, to, ok := dscpNftFakeRange(key)
		if ok && from.BitLen() == addr.BitLen() && !addr.Less(from) && !to.Less(addr) {
			return dscpNftFakeTarget(e), true
		}
	}
	return "", false
}

func dscpNftFakeRefs(rule string) []string {
	var refs []string
	for _, field := range strings.Fields(rule) {
		if strings.HasPrefix(field, "@") {
			refs = append(refs, field[1:])
		}
	}
	return refs
}

func dscpNftFakeTarget(element string) string {
	_, target, _ := strings.Cut(element, " : jump ")
	return target
}

func dscpNftFakeRange(text string) (netip.Addr, netip.Addr, bool) {
	if from, to, ok := strings.Cut(text, "-"); ok {
		a, errA := netip.ParseAddr(from)
		b, errB := netip.ParseAddr(to)
		return a, b, errA == nil && errB == nil
	}
	if strings.Contains(text, "/") {
		p, err := netip.ParsePrefix(text)
		if err != nil || p.Masked() != p {
			return netip.Addr{}, netip.Addr{}, false
		}
		width := p.Addr().BitLen()
		last := dscpU128Of(p.Addr()).or(dscpLowBits(width - p.Bits())).addr(width)
		return p.Addr(), last, true
	}
	a, err := netip.ParseAddr(text)
	return a, a, err == nil
}

type dscpNftFakeTx struct {
	table        *dscpNftFakeTable
	late         bool
	pinnedSets   map[string]bool
	pinnedChains map[string]bool
	elements     int
}

var (
	errDSCPNftFakeMissing = errors.New("No such file or directory")
	errDSCPNftFakeBusy    = errors.New("Device or resource busy")
)

func (tx *dscpNftFakeTx) apply(line string) error {
	fields := strings.Fields(line)
	if len(fields) < 4 || fields[2] != "inet" || fields[3] != dscpNftTable {
		return fmt.Errorf("unexpected command %q", line)
	}
	verb, kind := fields[0], fields[1]
	if kind == "table" {
		switch verb {
		case "add":
			if tx.table == nil {
				tx.table = &dscpNftFakeTable{chains: map[string][]string{}, maps: map[string][]string{}, sets: map[string][]string{}}
			}
		case "delete":
			if tx.table == nil {
				return errDSCPNftFakeMissing
			}
			tx.table = nil
			tx.pinnedSets, tx.pinnedChains = map[string]bool{}, map[string]bool{}
		default:
			return fmt.Errorf("unexpected command %q", line)
		}
		return nil
	}
	if tx.table == nil {
		return errDSCPNftFakeMissing
	}
	if len(fields) < 5 {
		return fmt.Errorf("unexpected command %q", line)
	}
	t, name := tx.table, fields[4]
	switch verb + " " + kind {
	case "add chain":
		if _, ok := t.chains[name]; !ok {
			t.chains[name] = []string{}
		}
	case "flush chain":
		rules, ok := t.chains[name]
		if !ok {
			return errDSCPNftFakeMissing
		}
		if tx.late {
			for _, rule := range rules {
				for _, ref := range dscpNftFakeRefs(rule) {
					tx.pinnedSets[ref] = true
				}
			}
		}
		t.chains[name] = []string{}
	case "delete chain":
		rules, ok := t.chains[name]
		if !ok {
			return errDSCPNftFakeMissing
		}
		if len(rules) > 0 || tx.pinnedChains[name] || t.chainReferenced(name) {
			return errDSCPNftFakeBusy
		}
		delete(t.chains, name)
	case "add rule":
		if _, ok := t.chains[name]; !ok {
			return errDSCPNftFakeMissing
		}
		body := strings.Join(fields[5:], " ")
		for _, ref := range dscpNftFakeRefs(body) {
			_, isMap := t.maps[ref]
			_, isSet := t.sets[ref]
			if !isMap && !isSet {
				return errDSCPNftFakeMissing
			}
		}
		t.chains[name] = append(t.chains[name], body)
	case "add map":
		if _, ok := t.maps[name]; !ok {
			t.maps[name] = []string{}
		}
	case "flush map":
		elems, ok := t.maps[name]
		if !ok {
			return errDSCPNftFakeMissing
		}
		tx.release(elems)
		t.maps[name] = []string{}
	case "delete map":
		elems, ok := t.maps[name]
		if !ok {
			return errDSCPNftFakeMissing
		}
		if tx.pinnedSets[name] || t.setReferenced(name) {
			return errDSCPNftFakeBusy
		}
		tx.release(elems)
		delete(t.maps, name)
	case "add set":
		if _, ok := t.sets[name]; !ok {
			t.sets[name] = []string{}
		}
	case "delete set":
		if _, ok := t.sets[name]; !ok {
			return errDSCPNftFakeMissing
		}
		if tx.pinnedSets[name] || t.setReferenced(name) {
			return errDSCPNftFakeBusy
		}
		delete(t.sets, name)
	case "add element":
		start, end := strings.Index(line, "{ "), strings.LastIndex(line, " }")
		if start < 0 || end < start {
			return fmt.Errorf("unexpected command %q", line)
		}
		elems := strings.Split(line[start+2:end], ", ")
		tx.elements += len(elems)
		if existing, ok := t.maps[name]; ok {
			var spans [][2]netip.Addr
			for _, have := range existing {
				key, _, _ := strings.Cut(have, " : ")
				from, to, _ := dscpNftFakeRange(key)
				spans = append(spans, [2]netip.Addr{from, to})
			}
			for _, e := range elems {
				if _, ok := t.chains[dscpNftFakeTarget(e)]; !ok {
					return errDSCPNftFakeMissing
				}
				key, _, _ := strings.Cut(e, " : ")
				from, to, ok := dscpNftFakeRange(key)
				if !ok {
					return fmt.Errorf("syntax error in %q", e)
				}
				for _, span := range spans {
					if span[0].BitLen() == from.BitLen() && !to.Less(span[0]) && !span[1].Less(from) {
						return errors.New("interval overlaps with an existing one")
					}
				}
				spans = append(spans, [2]netip.Addr{from, to})
				existing = append(existing, e)
			}
			t.maps[name] = existing
			return nil
		}
		if _, ok := t.sets[name]; !ok {
			return errDSCPNftFakeMissing
		}
		for _, e := range elems {
			t.sets[name] = append(t.sets[name], strings.Fields(e)[0])
		}
	default:
		return fmt.Errorf("unexpected command %q", line)
	}
	return nil
}

func (tx *dscpNftFakeTx) release(elems []string) {
	if !tx.late {
		return
	}
	for _, e := range elems {
		tx.pinnedChains[dscpNftFakeTarget(e)] = true
	}
}

type dscpNftFake struct {
	table       *dscpNftFakeTable
	scripts     []string
	commands    []string
	maxElements int
	lateRelease bool
	fail        func(script string) (string, error)
	committed   func()
}

func installDSCPNftFake(t *testing.T) *dscpNftFake {
	t.Helper()
	resetDSCPState(t)
	stubBinaryPresence(t, map[string]bool{"nft": true})
	f := &dscpNftFake{}
	origRun, origStdin := run, runNftStdin
	t.Cleanup(func() { run, runNftStdin = origRun, origStdin })
	run, runNftStdin = f.run, f.load
	return f
}

func (f *dscpNftFake) load(script string) (string, error) {
	f.scripts = append(f.scripts, script)
	if f.fail != nil {
		if out, err := f.fail(script); err != nil {
			return out, err
		}
	}
	tx := &dscpNftFakeTx{table: f.table.clone(), late: f.lateRelease, pinnedSets: map[string]bool{}, pinnedChains: map[string]bool{}}
	for _, line := range strings.Split(strings.TrimRight(script, "\n"), "\n") {
		if err := tx.apply(line); err != nil {
			out := fmt.Sprintf("Error: Could not process rule: %v\n%s", err, line)
			return out, fmt.Errorf("command [nft -f -] failed: exit status 1 (%s)", out)
		}
	}
	if f.maxElements > 0 && tx.elements > f.maxElements {
		out := "netlink: Error: Could not process rule: Message too long"
		return out, fmt.Errorf("command [nft -f -] failed: exit status 1 (%s)", out)
	}
	f.table = tx.table
	if f.committed != nil {
		f.committed()
	}
	return "", nil
}

func (f *dscpNftFake) run(args ...string) (string, error) {
	cmd := strings.Join(args, " ")
	f.commands = append(f.commands, cmd)
	switch cmd {
	case "nft list tables":
		if f.table == nil {
			return "", nil
		}
		return "table inet " + dscpNftTable + "\n", nil
	case "nft delete table inet " + dscpNftTable:
		if f.table == nil {
			return "Error: No such file or directory", errors.New("exit status 1")
		}
		f.table = nil
		return "", nil
	case "nft list chain inet " + dscpNftTable + " " + dscpNftChain:
		if f.table == nil {
			return "Error: No such file or directory", errors.New("exit status 1")
		}
		var b strings.Builder
		fmt.Fprintf(&b, "table inet %s {\n\tchain %s {\n\t\ttype filter hook postrouting priority 150; policy accept;\n", dscpNftTable, dscpNftChain)
		for _, rule := range f.table.chains[dscpNftChain] {
			fmt.Fprintf(&b, "\t\t%s\n", rule)
		}
		b.WriteString("\t}\n}\n")
		return b.String(), nil
	}
	return "", fmt.Errorf("unexpected command %q", cmd)
}

func (f *dscpNftFake) stamp(addr netip.Addr, oif string) (int, bool) {
	if f.table == nil {
		return 0, false
	}
	t, v6 := f.table, addr.Is6()
	value, stamped := 0, false
	set := func(rule string) {
		if n, err := strconv.Atoi(rule[strings.LastIndexByte(rule, ' ')+1:]); err == nil {
			value, stamped = n, true
		}
	}
	for _, rule := range t.chains[dscpNftChain] {
		if strings.HasPrefix(rule, "oifname ") {
			parts := strings.SplitN(rule, " ", 3)
			if strings.Trim(parts[1], `"`) != oif {
				continue
			}
			rule = parts[2]
		}
		fields := strings.Fields(rule)
		switch {
		case strings.HasSuffix(rule, "return"):
		case strings.HasPrefix(rule, "meta nfproto ipv4 "):
			if !v6 {
				set(rule)
			}
		case strings.HasPrefix(rule, "meta nfproto ipv6 "):
			if v6 {
				set(rule)
			}
		case len(fields) == 4 && fields[2] == "vmap":
			if (fields[0] == "ip6") != v6 {
				continue
			}
			if chain, ok := t.lookup(strings.TrimPrefix(fields[3], "@"), addr); ok {
				for _, inner := range t.chains[chain] {
					if strings.HasPrefix(inner, "ip6 ") == v6 {
						set(inner)
					}
				}
			}
		case len(fields) == 7 && strings.HasPrefix(fields[2], "@"):
			if (fields[0] == "ip6") == v6 && slices.Contains(t.sets[fields[2][1:]], addr.String()) {
				set(rule)
			}
		}
	}
	return value, stamped
}

func (f *dscpNftFake) learn(name string, addrs ...string) {
	f.table.sets[name] = append(f.table.sets[name], addrs...)
}

func (f *dscpNftFake) objects(kind string) []string {
	if f.table == nil {
		return nil
	}
	var names []string
	switch kind {
	case "chain":
		for name := range f.table.chains {
			names = append(names, name)
		}
	case "map":
		for name := range f.table.maps {
			names = append(names, name)
		}
	case "set":
		for name := range f.table.sets {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

func (f *dscpNftFake) since(n int) []string {
	return slices.Clone(f.scripts[n:])
}

func dscpNftTestPlan(global config.DSCPConfig, sets ...*config.SetConfig) *dscpPlan {
	cfg := dscpPlanTestConfig(sets...)
	cfg.System.Tables.DSCP = global
	return dscpPlanFor(cfg)
}

func dscpNftTestPlanOne() *dscpPlan {
	return dscpNftTestPlan(config.DSCPConfig{Enabled: true, Value: 7},
		dscpPlanTestSet("a", 31, "10.0.0.0/8", "10.1.2.0/24", "fd00::/16"),
		dscpPlanTestSet("b", 6, "10.1.0.0/16", "fd00:1::/32"),
		dscpPlanTestSet("c", 25, "172.16.0.0/12"),
	)
}

func dscpNftTestPlanTwo(global config.DSCPConfig) *dscpPlan {
	return dscpNftTestPlan(global,
		dscpPlanTestSet("a", 31, "10.0.0.0/8"),
		dscpPlanTestSet("b", 6, "10.1.0.0/16"),
		dscpPlanTestSet("d", 46, "172.16.0.0/12", "10.1.2.0/24"),
	)
}

func dscpNftTestRanges(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("100.%d.%d.0/24", 64+(2*i)/256, (2*i)%256)
	}
	return out
}

func dscpNftTestElements(script string) int {
	n := 0
	for _, line := range strings.Split(script, "\n") {
		if strings.HasPrefix(line, "add element ") {
			n += strings.Count(line, " : jump ")
		}
	}
	return n
}

func dscpNftApplyOK(t *testing.T, plan *dscpPlan, prev *dscpNftLayout) *dscpNftLayout {
	t.Helper()
	out, err := dscpNftApplyPlan(plan, prev)
	if err != nil {
		t.Fatalf("dscpNftApplyPlan: %v", err)
	}
	if out.pending || !out.layout.perSet() {
		t.Fatalf("expected the per-set layout to be applied, got %+v", out)
	}
	return out.layout
}

func TestSetDSCPNftBatchGolden(t *testing.T) {
	plan1 := dscpNftTestPlanOne()
	wantReplace := `add table inet b4_dscp
delete table inet b4_dscp
add table inet b4_dscp
add chain inet b4_dscp postrouting { type filter hook postrouting priority 150 ; policy accept ; }
add chain inet b4_dscp v6
flush chain inet b4_dscp v6
add rule inet b4_dscp v6 ip dscp set 6
add rule inet b4_dscp v6 ip6 dscp set 6
add chain inet b4_dscp v25
flush chain inet b4_dscp v25
add rule inet b4_dscp v25 ip dscp set 25
add rule inet b4_dscp v25 ip6 dscp set 25
add chain inet b4_dscp v31
flush chain inet b4_dscp v31
add rule inet b4_dscp v31 ip dscp set 31
add rule inet b4_dscp v31 ip6 dscp set 31
add map inet b4_dscp s4_1 { type ipv4_addr : verdict ; flags interval ; }
add map inet b4_dscp s6_1 { type ipv6_addr : verdict ; flags interval ; }
add element inet b4_dscp s4_1 { 10.0.0.0/16 : jump v31, 10.1.0.0/23 : jump v6, 10.1.2.0/24 : jump v31, 10.1.3.0-10.1.255.255 : jump v6, 10.2.0.0-10.255.255.255 : jump v31, 172.16.0.0/12 : jump v25 }
add element inet b4_dscp s6_1 { fd00::/32 : jump v31, fd00:1::/32 : jump v6, fd00:2::-fd00:ffff:ffff:ffff:ffff:ffff:ffff:ffff : jump v31 }
add set inet b4_dscp l_a_d39_4 { type ipv4_addr ; flags timeout ; }
add set inet b4_dscp l_a_d39_6 { type ipv6_addr ; flags timeout ; }
add set inet b4_dscp l_b_14f2_4 { type ipv4_addr ; flags timeout ; }
add set inet b4_dscp l_b_14f2_6 { type ipv6_addr ; flags timeout ; }
add set inet b4_dscp l_c_125f_4 { type ipv4_addr ; flags timeout ; }
add set inet b4_dscp l_c_125f_6 { type ipv6_addr ; flags timeout ; }
add rule inet b4_dscp postrouting oifname "lo" return
add rule inet b4_dscp postrouting meta mark & 0x20000000 == 0x20000000 return
add rule inet b4_dscp postrouting ct direction reply return
add rule inet b4_dscp postrouting meta nfproto ipv4 ip dscp set 7
add rule inet b4_dscp postrouting meta nfproto ipv6 ip6 dscp set 7
add rule inet b4_dscp postrouting ip daddr vmap @s4_1
add rule inet b4_dscp postrouting ip6 daddr vmap @s6_1
add rule inet b4_dscp postrouting ip daddr @l_c_125f_4 ip dscp set 25
add rule inet b4_dscp postrouting ip6 daddr @l_c_125f_6 ip6 dscp set 25
add rule inet b4_dscp postrouting ip daddr @l_b_14f2_4 ip dscp set 6
add rule inet b4_dscp postrouting ip6 daddr @l_b_14f2_6 ip6 dscp set 6
add rule inet b4_dscp postrouting ip daddr @l_a_d39_4 ip dscp set 31
add rule inet b4_dscp postrouting ip6 daddr @l_a_d39_6 ip6 dscp set 31
`
	wantFill := `add chain inet b4_dscp v46
flush chain inet b4_dscp v46
add rule inet b4_dscp v46 ip dscp set 46
add rule inet b4_dscp v46 ip6 dscp set 46
add map inet b4_dscp s4_2 { type ipv4_addr : verdict ; flags interval ; }
flush map inet b4_dscp s4_2
add map inet b4_dscp s6_2 { type ipv6_addr : verdict ; flags interval ; }
flush map inet b4_dscp s6_2
add element inet b4_dscp s4_2 { 10.0.0.0/16 : jump v31, 10.1.0.0/23 : jump v6, 10.1.2.0/24 : jump v46, 10.1.3.0-10.1.255.255 : jump v6, 10.2.0.0-10.255.255.255 : jump v31, 172.16.0.0/12 : jump v46 }
`
	wantSwap := `add set inet b4_dscp l_a_d39_4 { type ipv4_addr ; flags timeout ; }
add set inet b4_dscp l_a_d39_6 { type ipv6_addr ; flags timeout ; }
add set inet b4_dscp l_b_14f2_4 { type ipv4_addr ; flags timeout ; }
add set inet b4_dscp l_b_14f2_6 { type ipv6_addr ; flags timeout ; }
add set inet b4_dscp l_d_580_4 { type ipv4_addr ; flags timeout ; }
add set inet b4_dscp l_d_580_6 { type ipv6_addr ; flags timeout ; }
flush chain inet b4_dscp postrouting
add rule inet b4_dscp postrouting oifname "lo" return
add rule inet b4_dscp postrouting meta mark & 0x20000000 == 0x20000000 return
add rule inet b4_dscp postrouting ct direction reply return
add rule inet b4_dscp postrouting oifname "dummy0" meta nfproto ipv4 ip dscp set 7
add rule inet b4_dscp postrouting oifname "dummy0" meta nfproto ipv6 ip6 dscp set 7
add rule inet b4_dscp postrouting oifname "dummy0" ip daddr vmap @s4_2
add rule inet b4_dscp postrouting oifname "dummy0" ip6 daddr vmap @s6_2
add rule inet b4_dscp postrouting oifname "dummy0" ip daddr @l_d_580_4 ip dscp set 46
add rule inet b4_dscp postrouting oifname "dummy0" ip6 daddr @l_d_580_6 ip6 dscp set 46
add rule inet b4_dscp postrouting oifname "dummy0" ip daddr @l_b_14f2_4 ip dscp set 6
add rule inet b4_dscp postrouting oifname "dummy0" ip6 daddr @l_b_14f2_6 ip6 dscp set 6
add rule inet b4_dscp postrouting oifname "dummy0" ip daddr @l_a_d39_4 ip dscp set 31
add rule inet b4_dscp postrouting oifname "dummy0" ip6 daddr @l_a_d39_6 ip6 dscp set 31
`
	wantDrop := `add map inet b4_dscp s4_1 { type ipv4_addr : verdict ; flags interval ; }
delete map inet b4_dscp s4_1
add map inet b4_dscp s6_1 { type ipv6_addr : verdict ; flags interval ; }
delete map inet b4_dscp s6_1
add set inet b4_dscp l_c_125f_4 { type ipv4_addr ; flags timeout ; }
delete set inet b4_dscp l_c_125f_4
add set inet b4_dscp l_c_125f_6 { type ipv6_addr ; flags timeout ; }
delete set inet b4_dscp l_c_125f_6
`
	wantDropChains := `add chain inet b4_dscp v25
flush chain inet b4_dscp v25
delete chain inet b4_dscp v25
`
	if got := dscpNftReplaceScript(plan1, true); got != wantReplace {
		t.Errorf("full replace script:\n%s\nwant:\n%s", got, wantReplace)
	}

	f := installDSCPNftFake(t)
	layout1 := dscpNftApplyOK(t, plan1, nil)
	if !reflect.DeepEqual(f.scripts, []string{wantReplace}) {
		t.Fatalf("the first apply must be one full replace, got:\n%s", strings.Join(f.scripts, "----\n"))
	}
	want1 := &dscpNftLayout{generation: 1, staticKey: plan1.staticKey, maps: []int{1}, values: []int{6, 25, 31},
		learned: []string{"a_d39", "b_14f2", "c_125f"}, stamps: 8, vmaps: 2}
	if !reflect.DeepEqual(layout1, want1) {
		t.Errorf("layout after the full replace:\n got %+v\nwant %+v", layout1, want1)
	}

	plan2 := dscpNftTestPlanTwo(config.DSCPConfig{Enabled: true, Value: 7, Interfaces: []string{"dummy0"}})
	mark := len(f.scripts)
	layout2 := dscpNftApplyOK(t, plan2, layout1)
	if got, want := f.since(mark), []string{wantFill, wantSwap, wantDrop, wantDropChains}; !reflect.DeepEqual(got, want) {
		t.Fatalf("the generation change must fill, swap, drop the maps and sets, then drop the chains; got:\n%s\nwant:\n%s",
			strings.Join(got, "----\n"), strings.Join(want, "----\n"))
	}
	want2 := &dscpNftLayout{generation: 2, staticKey: plan2.staticKey, maps: []int{2}, values: []int{6, 31, 46},
		learned: []string{"a_d39", "b_14f2", "d_580"}, stamps: 8, vmaps: 2}
	if !reflect.DeepEqual(layout2, want2) {
		t.Errorf("layout after the generation change:\n got %+v\nwant %+v", layout2, want2)
	}

	chunks := dscpNftElementScripts(dscpNftTestPlan(config.DSCPConfig{}, dscpPlanTestSet("big", 12, dscpNftTestRanges(3)...)), 5, 2)
	wantChunks := []string{
		"add element inet b4_dscp s4_5 { 100.64.0.0/24 : jump v12, 100.64.2.0/24 : jump v12 }\n",
		"add element inet b4_dscp s4_5 { 100.64.4.0/24 : jump v12 }\n",
	}
	if !reflect.DeepEqual(chunks, wantChunks) {
		t.Errorf("element chunks:\n got %q\nwant %q", chunks, wantChunks)
	}
}

func TestSetDSCPNftCounts(t *testing.T) {
	pinned := dscpPlanTestSet("d", 46, "192.0.2.0/24")
	pinned.Targets.DomainOnly = true
	sets := []*config.SetConfig{dscpPlanTestSet("a", 31, "10.0.0.0/8"), dscpPlanTestSet("b", 6, "10.1.0.0/16"), dscpPlanTestSet("c", 25, "fd00::/16"), pinned}
	cases := []struct {
		global        config.DSCPConfig
		stamps, vmaps int
	}{
		{config.DSCPConfig{Enabled: true, Value: 7}, 2 + 2*3, 2},
		{config.DSCPConfig{Enabled: false, Value: 7}, 2 * 3, 2},
		{config.DSCPConfig{Enabled: true, Value: 7, Interfaces: []string{"eth0", "wan"}}, 2*2 + 2*2*3, 4},
		{config.DSCPConfig{Interfaces: []string{"eth0", "wan", "ppp0"}}, 2 * 3 * 3, 6},
	}
	for _, tc := range cases {
		plan := dscpNftTestPlan(tc.global, sets...)
		if got := dscpNftStampCount(plan); got != tc.stamps {
			t.Errorf("%+v: stamps %d, want %d", tc.global, got, tc.stamps)
		}
		if got := dscpNftVmapCount(plan); got != tc.vmaps {
			t.Errorf("%+v: vmaps %d, want %d", tc.global, got, tc.vmaps)
		}
		rules := dscpNftRules(plan, 1)
		stamps, vmaps := 0, 0
		for _, r := range rules {
			if strings.Contains(r, "dscp set") {
				stamps++
			}
			if strings.Contains(r, "daddr vmap @s") {
				vmaps++
			}
		}
		if stamps != tc.stamps || vmaps != tc.vmaps {
			t.Errorf("%+v: the rendered rules hold %d stamps and %d vmap lookups, the counts say %d and %d", tc.global, stamps, vmaps, tc.stamps, tc.vmaps)
		}
	}
}

func TestSetDSCPNftDomainOnlyHasNoLearnedObjects(t *testing.T) {
	f, _ := dscpLearnNftSetup(t)
	pinned := func(domainOnly bool) *config.SetConfig {
		set := dscpPlanTestSet("d", 25, "172.16.0.0/12")
		set.Targets.SNIDomains = []string{"example.net"}
		set.Targets.DomainOnly = domainOnly
		return set
	}
	build := func(domainOnly bool) *config.Config {
		return dscpSyncNftConfig(config.DSCPConfig{Enabled: true, Value: 7}, dscpPlanTestSet("a", 31, "10.0.0.0/8"), pinned(domainOnly))
	}
	sa, sd := routeSanitizeSetID("a"), routeSanitizeSetID("d")
	learnedSets := func(ids ...string) []string {
		var names []string
		for _, id := range ids {
			names = append(names, dscpNftLearnedSet(id, false), dscpNftLearnedSet(id, true))
		}
		slices.Sort(names)
		return names
	}
	expect := func(stage string, learned []string, stamps int) *dscpState {
		t.Helper()
		st := dscpApplied.Load()
		if st == nil || st.pending || !st.nft.perSet() {
			t.Fatalf("%s: the per-set layout is not applied: %+v", stage, st)
		}
		if got := f.objects("set"); !slices.Equal(got, learnedSets(learned...)) {
			t.Errorf("%s: learned sets %v, want %v", stage, got, learnedSets(learned...))
		}
		if !slices.Equal(st.nft.learned, learned) || st.nft.stamps != stamps || st.nft.vmaps != 2 {
			t.Errorf("%s: layout learned %v, stamps %d, vmaps %d; want %v, %d, 2", stage, st.nft.learned, st.nft.stamps, st.nft.vmaps, learned, stamps)
		}
		if !dscpNftLayoutIntact(st.nft) {
			t.Errorf("%s: the monitor's shape check rejects the chain that was just loaded", stage)
		}
		mark := len(f.scripts)
		if ensureDSCPLocked(st.cfg, false) || len(f.scripts) != mark {
			t.Errorf("%s: the monitor rebuilt an intact chain", stage)
		}
		return st
	}

	plan := dscpPlanFor(build(true))
	if got := dscpNftLearnedIDs(plan); !slices.Equal(got, []string{sa}) {
		t.Errorf("learned ids %v, want only the set that learns", got)
	}
	var iptLearned []string
	for _, w := range dscpIptWanted(plan, false) {
		if w.learned && w.name != dscpIptLearnedUnionSet(false) {
			iptLearned = append(iptLearned, w.name)
		}
	}
	if !slices.Equal(iptLearned, []string{dscpIptLearnedSet(sa, false)}) {
		t.Errorf("iptables wants the learned ipsets %v, nftables the learned sets of %v", iptLearned, dscpNftLearnedIDs(plan))
	}
	for name, script := range map[string]string{"full replace": dscpNftReplaceScript(plan, true), "swap": dscpNftSwapScript(plan, 1)} {
		if strings.Contains(script, "l_"+sd+"_") {
			t.Errorf("the %s script renders learned objects for a domain-only set:\n%s", name, script)
		}
	}

	dscpLearnApply(t, build(true), backendNFTables)
	st := expect("a domain-only set", []string{sa}, 2*(1+1))
	if got, _ := f.stamp(netip.MustParseAddr("172.16.1.1"), "wan"); got != 25 {
		t.Errorf("the domain-only set's own range leaves with %d, want 25", got)
	}
	if _, ok := dscpLearnCur.Load().target("d"); ok || !st.learnsInto(sa, false) || st.learnsInto(sd, false) {
		t.Errorf("only the set that learns may be written to, view %+v", dscpLearnCur.Load())
	}

	dscpLearnApply(t, build(false), backendNFTables)
	st = expect("the set starts learning", []string{sa, sd}, 2*(2+1))
	if _, ok := dscpLearnCur.Load().target("d"); !ok || !st.learnsInto(sd, true) {
		t.Errorf("a set that learns again must be written to, view %+v", dscpLearnCur.Load())
	}

	f.learn(dscpNftLearnedSet(sd, false), "198.51.100.7")
	dscpLearnApply(t, build(true), backendNFTables)
	expect("the set is domain-only again", []string{sa}, 2*(1+1))
	if got, _ := f.stamp(netip.MustParseAddr("198.51.100.7"), "wan"); got != 7 {
		t.Errorf("an address the set learned before it turned domain-only leaves with %d, want the global 7", got)
	}

	dscpLearnApply(t, dscpSyncNftConfig(config.DSCPConfig{}, pinned(true)), backendNFTables)
	expect("only a domain-only set, global off", []string{}, 0)
	if got, ok := f.stamp(netip.MustParseAddr("172.16.1.1"), "wan"); !ok || got != 25 {
		t.Errorf("the domain-only set's range leaves with %d (stamped %v), want 25", got, ok)
	}
	if view := dscpLearnCur.Load(); view != nil {
		t.Errorf("no set learns, yet the learner may write: %+v", view)
	}
}

func TestSetDSCPNftGenerationSwap(t *testing.T) {
	f := installDSCPNftFake(t)
	plan1 := dscpNftTestPlanOne()
	layout1 := dscpNftApplyOK(t, plan1, nil)
	if !dscpNftLayoutIntact(layout1) {
		t.Fatalf("the full replace does not pass its own shape check")
	}
	f.learn(dscpNftLearnedSet(routeSanitizeSetID("b"), false), "10.1.2.7")
	f.learn(dscpNftLearnedSet(routeSanitizeSetID("c"), false), "10.3.0.7")
	f.learn(dscpNftLearnedSet(routeSanitizeSetID("a"), true), "fd00:1::9")

	plan2 := dscpNftTestPlan(config.DSCPConfig{Enabled: true, Value: 7},
		dscpPlanTestSet("a", 31, "10.0.0.0/8", "fd00::/16"),
		dscpPlanTestSet("b", 6, "10.1.0.0/16", "10.1.2.0/24", "fd00:1::/32"),
		dscpPlanTestSet("d", 46, "172.16.0.0/12"),
	)
	probes := []struct {
		addr          string
		before, after int
	}{
		{"10.0.0.9", 31, 31},
		{"10.1.2.5", 31, 6},
		{"10.1.0.5", 6, 6},
		{"172.16.1.1", 25, 46},
		{"9.9.9.9", 7, 7},
		{"fd00:1::5", 6, 6},
		{"fd00:1::9", 31, 31},
		{"fd01::1", 7, 7},
		{"10.1.2.7", 6, 6},
		{"10.3.0.7", 25, 31},
	}
	check := func(stage string, pick func(before, after int) []int) {
		t.Helper()
		for _, p := range probes {
			got, ok := f.stamp(netip.MustParseAddr(p.addr), "wan")
			if !ok || !slices.Contains(pick(p.before, p.after), got) {
				t.Errorf("%s: %s left with DSCP %d (stamped %v), want one of %v", stage, p.addr, got, ok, pick(p.before, p.after))
			}
		}
	}
	check("before the change", func(before, _ int) []int { return []int{before} })
	commits := 0
	f.committed = func() {
		commits++
		check(fmt.Sprintf("after transaction %d of the change", commits), func(before, after int) []int { return []int{before, after} })
	}
	mark := len(f.scripts)
	layout2 := dscpNftApplyOK(t, plan2, layout1)
	f.committed = nil
	check("after the change", func(_, after int) []int { return []int{after} })

	scripts := f.since(mark)
	if len(scripts) != 4 || commits != 4 {
		t.Fatalf("a static change that retires value 25 is a fill, a swap, a drop of generation 1 and a drop of v25, got %d scripts:\n%s", len(scripts), strings.Join(scripts, "----\n"))
	}
	if scripts[3] != dscpNftDropChainsScript([]int{25}) {
		t.Errorf("the last transaction must drop only the retired value chain:\n%s", scripts[3])
	}
	for _, s := range scripts {
		if strings.Contains(s, "delete table") {
			t.Errorf("the generation swap deleted the table:\n%s", s)
		}
	}
	if !strings.Contains(scripts[0], "add map inet b4_dscp s4_2 ") || strings.Contains(scripts[0], dscpNftChain) || strings.Contains(scripts[0], "s4_1") || strings.Contains(scripts[0], "s6_1") {
		t.Errorf("the fill must build generation 2 without touching postrouting or the live generation:\n%s", scripts[0])
	}
	if !strings.Contains(scripts[1], "flush chain inet b4_dscp postrouting\n") || !strings.Contains(scripts[1], "ip daddr vmap @s4_2\n") {
		t.Errorf("the swap must rewrite postrouting onto generation 2:\n%s", scripts[1])
	}
	if !strings.Contains(scripts[2], "delete map inet b4_dscp s4_1\n") || !strings.Contains(scripts[2], "delete set inet b4_dscp l_c_125f_4\n") {
		t.Errorf("the drop must remove generation 1 and the departed set's learned sets:\n%s", scripts[2])
	}
	if layout2.generation != 2 || !reflect.DeepEqual(layout2.maps, []int{2}) || layout2.staticKey != plan2.staticKey {
		t.Errorf("layout after the swap: %+v", layout2)
	}
	if got := f.objects("map"); !reflect.DeepEqual(got, []string{"s4_2", "s6_2"}) {
		t.Errorf("maps after the swap: %v", got)
	}
	if got := f.table.sets[dscpNftLearnedSet(routeSanitizeSetID("b"), false)]; !reflect.DeepEqual(got, []string{"10.1.2.7"}) {
		t.Errorf("a learned address of a set that stayed was lost: %v", got)
	}
	if !dscpNftLayoutIntact(layout2) {
		t.Errorf("the swapped chain does not pass its own shape check")
	}

	plan3 := dscpNftTestPlan(config.DSCPConfig{Enabled: true, Value: 9},
		dscpPlanTestSet("a", 31, "10.0.0.0/8", "fd00::/16"),
		dscpPlanTestSet("b", 6, "10.1.0.0/16", "10.1.2.0/24", "fd00:1::/32"),
		dscpPlanTestSet("d", 46, "172.16.0.0/12"),
	)
	mark = len(f.scripts)
	layout3 := dscpNftApplyOK(t, plan3, layout2)
	if scripts := f.since(mark); len(scripts) != 1 || strings.Contains(scripts[0], "add map") || !strings.Contains(scripts[0], "ip dscp set 9\n") {
		t.Errorf("a global value change must be one swap on the same generation, got:\n%s", strings.Join(scripts, "----\n"))
	}
	if layout3.generation != 2 {
		t.Errorf("a change outside the static part moved the generation to %d", layout3.generation)
	}
	if got, _ := f.stamp(netip.MustParseAddr("9.9.9.9"), "wan"); got != 9 {
		t.Errorf("the new global value is not written, got %d", got)
	}

	mark = len(f.scripts)
	dscpNftApplyOK(t, plan3, layout3)
	if scripts := f.since(mark); len(scripts) != 1 || !strings.Contains(scripts[0], "flush chain inet b4_dscp postrouting\n") {
		t.Errorf("an unchanged plan must be one swap that rewrites the chain in place, got:\n%s", strings.Join(scripts, "----\n"))
	}
}

func TestSetDSCPNftLeftoverGenerationIsReset(t *testing.T) {
	f := installDSCPNftFake(t)
	layout := dscpNftApplyOK(t, dscpNftTestPlanOne(), nil)
	f.table.maps["s4_2"] = []string{"10.0.0.0/8 : jump v31"}
	f.table.maps["s6_2"] = []string{"fd00::/16 : jump v6"}
	mark := len(f.scripts)
	layout = dscpNftApplyOK(t, dscpNftTestPlanTwo(config.DSCPConfig{Enabled: true, Value: 7}), layout)
	for _, s := range f.since(mark) {
		if strings.Contains(s, "delete table") {
			t.Fatalf("a leftover map of the next generation forced a full replace:\n%s", s)
		}
	}
	if layout.generation != 2 {
		t.Errorf("generation %d, want 2", layout.generation)
	}
	if got := f.table.maps["s6_2"]; len(got) != 0 {
		t.Errorf("the leftover IPv6 elements survived the fill: %v", got)
	}
	if got, _ := f.stamp(netip.MustParseAddr("10.1.2.5"), "wan"); got != 46 {
		t.Errorf("10.1.2.5 leaves with %d after the fill over a leftover map, want 46", got)
	}
}

func TestSetDSCPNftChunkedFill(t *testing.T) {
	small := dscpNftTestPlan(config.DSCPConfig{}, dscpPlanTestSet("a", 31, "10.0.0.0/8"))
	bigPlan := func(n int) *dscpPlan {
		v6 := []string{"2001:db8::/48", "2001:db8:2::/48"}
		return dscpNftTestPlan(config.DSCPConfig{}, dscpPlanTestSet("a", 31, "10.0.0.0/8"), dscpPlanTestSet("big", 12, append(dscpNftTestRanges(n), v6...)...))
	}
	limits := func(t *testing.T, scripts []string, limit int) int {
		t.Helper()
		total := 0
		for _, s := range scripts {
			n := dscpNftTestElements(s)
			if n > limit {
				t.Errorf("a transaction carries %d elements, more than %d", n, limit)
			}
			total += n
		}
		return total
	}

	t.Run("one transaction up to the batch size", func(t *testing.T) {
		f := installDSCPNftFake(t)
		layout := dscpNftApplyOK(t, small, nil)
		plan := bigPlan(1500)
		mark := len(f.scripts)
		dscpNftApplyOK(t, plan, layout)
		scripts := f.since(mark)
		if dscpNftTestElements(scripts[0]) != dscpNftElementCount(plan) || !strings.Contains(scripts[0], "flush map inet b4_dscp s4_2\n") {
			t.Errorf("%d elements must go into generation 2 in one transaction, the first script holds %d", dscpNftElementCount(plan), dscpNftTestElements(scripts[0]))
		}
	})

	t.Run("batches above the batch size", func(t *testing.T) {
		f := installDSCPNftFake(t)
		layout := dscpNftApplyOK(t, small, nil)
		plan := bigPlan(2500)
		mark := len(f.scripts)
		layout = dscpNftApplyOK(t, plan, layout)
		scripts := f.since(mark)
		if dscpNftTestElements(scripts[0]) != 0 || !strings.Contains(scripts[0], "add map inet b4_dscp s4_2 ") || !strings.Contains(scripts[0], "add chain inet b4_dscp v12\n") {
			t.Errorf("the first transaction must create the maps and the value chain without elements:\n%s", scripts[0])
		}
		fills := 0
		for _, s := range scripts[1:] {
			if strings.HasPrefix(s, "add element ") {
				fills++
			}
		}
		if fills != 4 {
			t.Errorf("%d IPv4 and %d IPv6 elements in batches of %d are 4 transactions, got %d", len(plan.static4), len(plan.static6), dscpNftFillChunk, fills)
		}
		if total := limits(t, scripts, dscpNftFillChunk); total != dscpNftElementCount(plan) {
			t.Errorf("loaded %d elements, want %d", total, dscpNftElementCount(plan))
		}
		if got := len(f.table.maps["s4_2"]) + len(f.table.maps["s6_2"]); got != dscpNftElementCount(plan) || layout.generation != 2 {
			t.Errorf("generation %d holds %d elements, want generation 2 with %d", layout.generation, got, dscpNftElementCount(plan))
		}
		if got, _ := f.stamp(netip.MustParseAddr("100.83.134.9"), "wan"); got != 12 {
			t.Errorf("the last loaded range is not stamped, got %d", got)
		}
	})

	t.Run("message too long falls back to batches", func(t *testing.T) {
		f := installDSCPNftFake(t)
		layout := dscpNftApplyOK(t, small, nil)
		f.maxElements = 1200
		plan := bigPlan(1500)
		mark := len(f.scripts)
		layout = dscpNftApplyOK(t, plan, layout)
		scripts := f.since(mark)
		if dscpNftTestElements(scripts[0]) != dscpNftElementCount(plan) {
			t.Fatalf("the first attempt must be one transaction, it held %d elements", dscpNftTestElements(scripts[0]))
		}
		if total := limits(t, scripts[1:], dscpNftFillChunk); total != dscpNftElementCount(plan) {
			t.Errorf("after the refusal %d elements were loaded, want %d", total, dscpNftElementCount(plan))
		}
		if got := len(f.table.maps["s4_2"]) + len(f.table.maps["s6_2"]); got != dscpNftElementCount(plan) || layout.generation != 2 {
			t.Errorf("generation %d holds %d elements, want generation 2 with %d", layout.generation, got, dscpNftElementCount(plan))
		}
	})

	t.Run("full replace above the batch size", func(t *testing.T) {
		f := installDSCPNftFake(t)
		plan := bigPlan(2500)
		layout := dscpNftApplyOK(t, plan, nil)
		if !strings.Contains(f.scripts[0], "delete table inet b4_dscp\n") || dscpNftTestElements(f.scripts[0]) != 0 {
			t.Errorf("the full replace must build the table without elements first:\n%.400s", f.scripts[0])
		}
		if total := limits(t, f.scripts[1:], dscpNftFillChunk); total != dscpNftElementCount(plan) {
			t.Errorf("loaded %d elements after the replace, want %d", total, dscpNftElementCount(plan))
		}
		if layout.generation != 1 || len(f.table.maps["s4_1"]) != 2501 {
			t.Errorf("generation %d with %d IPv4 elements", layout.generation, len(f.table.maps["s4_1"]))
		}
	})

	t.Run("full replace message too long", func(t *testing.T) {
		f := installDSCPNftFake(t)
		f.maxElements = 1200
		plan := bigPlan(1500)
		layout := dscpNftApplyOK(t, plan, nil)
		if len(f.scripts) < 3 || dscpNftTestElements(f.scripts[0]) != dscpNftElementCount(plan) || dscpNftTestElements(f.scripts[1]) != 0 {
			t.Fatalf("expected the inline replace, then the replace without elements, then batches; got %d scripts", len(f.scripts))
		}
		if got := len(f.table.maps["s4_1"]) + len(f.table.maps["s6_1"]); got != dscpNftElementCount(plan) || layout.generation != 1 {
			t.Errorf("generation %d holds %d elements, want %d", layout.generation, got, dscpNftElementCount(plan))
		}
	})
}

func TestSetDSCPNftFallbackLadder(t *testing.T) {
	rejectOnce := func(f *dscpNftFake, marker string) {
		done := false
		f.fail = func(script string) (string, error) {
			if !done && strings.Contains(script, marker) {
				done = true
				return "Error: Could not process rule: Operation not supported", errors.New("command [nft -f -] failed: exit status 1 (Error: Could not process rule: Operation not supported)")
			}
			return "", nil
		}
	}
	rejectAll := func(f *dscpNftFake, marker string) {
		f.fail = func(script string) (string, error) {
			if strings.Contains(script, marker) {
				return "Error: syntax error", errors.New("command [nft -f -] failed: exit status 1 (Error: syntax error)")
			}
			return "", nil
		}
	}
	plan2 := func(on bool) *dscpPlan {
		return dscpNftTestPlanTwo(config.DSCPConfig{Enabled: on, Value: 7, Interfaces: []string{"wan"}})
	}

	t.Run("a rejected update rebuilds the table", func(t *testing.T) {
		f := installDSCPNftFake(t)
		layout := dscpNftApplyOK(t, dscpNftTestPlanOne(), nil)
		f.learn(dscpNftLearnedSet(routeSanitizeSetID("a"), false), "10.9.9.9")
		rejectOnce(f, "flush chain inet b4_dscp postrouting\n")
		mark := len(f.scripts)
		out, err := dscpNftApplyPlan(plan2(true), layout)
		if err == nil || !strings.Contains(err.Error(), "builds the table again") {
			t.Fatalf("the rejected update must be reported, got %v", err)
		}
		if out.pending || !out.rebuilt || !out.layout.perSet() || out.layout.generation != 1 {
			t.Fatalf("the rebuild must leave the per-set layout at generation 1, got %+v", out)
		}
		scripts := f.since(mark)
		if !strings.Contains(scripts[len(scripts)-1], "delete table inet b4_dscp\n") {
			t.Errorf("the last step must be the full replace, got:\n%s", scripts[len(scripts)-1])
		}
		if got := f.table.sets[dscpNftLearnedSet(routeSanitizeSetID("a"), false)]; len(got) != 0 {
			t.Errorf("a rebuilt table kept learned addresses %v", got)
		}
		if got, _ := f.stamp(netip.MustParseAddr("172.16.1.1"), "wan"); got != 46 {
			t.Errorf("after the rebuild 172.16.1.1 leaves with %d, want 46", got)
		}
	})

	t.Run("a rejected rebuild falls back to the global stamp", func(t *testing.T) {
		f := installDSCPNftFake(t)
		layout := dscpNftApplyOK(t, dscpNftTestPlanOne(), nil)
		rejectAll(f, "vmap")
		plan := plan2(true)
		mark := len(f.scripts)
		out, err := dscpNftApplyPlan(plan, layout)
		if err == nil || !strings.Contains(err.Error(), "get DSCP 7 like all others") {
			t.Fatalf("the per-set refusal must be reported, got %v", err)
		}
		global, stamps := dscpNftScript(7, []string{"wan"})
		scripts := f.since(mark)
		if scripts[len(scripts)-1] != global {
			t.Errorf("the fallback must load today's global-only script byte for byte, got:\n%s\nwant:\n%s", scripts[len(scripts)-1], global)
		}
		if out.pending || out.layout == nil || out.layout.perSet() || out.layout.stamps != stamps || out.layout.vmaps != 0 {
			t.Fatalf("the global-only fallback must be recorded as applied without per-set objects, got %+v", out.layout)
		}
		if !dscpNftLayoutIntact(out.layout) {
			t.Errorf("the global-only fallback does not pass the shape check it is recorded with")
		}
		if got, _ := f.stamp(netip.MustParseAddr("172.16.1.1"), "wan"); got != 7 {
			t.Errorf("after the fallback 172.16.1.1 leaves with %d, want the global 7", got)
		}
		mark = len(f.scripts)
		if _, err := dscpNftApplyPlan(plan, out.layout); err == nil || strings.Contains(strings.Join(f.since(mark), ""), "flush chain inet b4_dscp postrouting") {
			t.Errorf("after a fallback the next apply must start from a full replace, err %v", err)
		}
	})

	t.Run("with the global stamp off the table goes", func(t *testing.T) {
		f := installDSCPNftFake(t)
		layout := dscpNftApplyOK(t, dscpNftTestPlanOne(), nil)
		rejectAll(f, "vmap")
		f.commands = nil
		out, err := dscpNftApplyPlan(plan2(false), layout)
		if err == nil || !strings.Contains(err.Error(), "without the sets' DSCP values") {
			t.Fatalf("the refusal must be reported, got %v", err)
		}
		for _, s := range f.scripts {
			if strings.Contains(s, "ip dscp set 7") && !strings.Contains(s, "vmap") {
				t.Errorf("with the global stamp off no global-only script may run:\n%s", s)
			}
		}
		if !slices.Contains(f.commands, "nft delete table inet b4_dscp") || f.table != nil {
			t.Errorf("the failure path must delete the table, commands %v", f.commands)
		}
		if out.layout != nil || out.pending || !out.gone {
			t.Errorf("nothing may be recorded as applied, got %+v", out)
		}
	})

	t.Run("everything rejected", func(t *testing.T) {
		f := installDSCPNftFake(t)
		layout := dscpNftApplyOK(t, dscpNftTestPlanOne(), nil)
		rejectAll(f, "add rule")
		out, err := dscpNftApplyPlan(plan2(true), layout)
		if err == nil || !strings.Contains(err.Error(), "neither IPv4 nor IPv6 packets get the DSCP value") {
			t.Fatalf("the last refusal must be reported, got %v", err)
		}
		if out.layout != nil || out.pending || !out.gone || f.table != nil {
			t.Errorf("the failure path must leave nothing applied and no table, got %+v", out)
		}
	})

	t.Run("a timeout while filling is retried on a fresh generation", func(t *testing.T) {
		f := installDSCPNftFake(t)
		layout := dscpNftApplyOK(t, dscpNftTestPlanOne(), nil)
		stall := true
		f.fail = func(script string) (string, error) {
			if stall && strings.Contains(script, "add map inet b4_dscp s4_2 ") {
				return "", fmt.Errorf("command [nft -f -] gave up after 15s: %w", context.DeadlineExceeded)
			}
			return "", nil
		}
		plan := plan2(true)
		out, err := dscpNftApplyPlan(plan, layout)
		if err == nil || !out.pending || out.rebuilt || out.layout.generation != 1 || !reflect.DeepEqual(out.layout.maps, []int{1, 2}) {
			t.Fatalf("a timed-out fill must stay pending on generation 1 and note generation 2, got %+v (%v)", out.layout, err)
		}
		if out.layout.staticKey != layout.staticKey || !reflect.DeepEqual(out.layout.values, layout.values) {
			t.Errorf("a timed-out fill changed the recorded static part: %+v", out.layout)
		}
		stall = false
		next := dscpNftApplyOK(t, plan, out.layout)
		if next.generation != 3 || !reflect.DeepEqual(f.objects("map"), []string{"s4_3", "s6_3"}) {
			t.Errorf("the retry must use generation 3 and leave only its maps, got %+v with maps %v", next, f.objects("map"))
		}
	})

	t.Run("a timeout is retried, not rebuilt", func(t *testing.T) {
		f := installDSCPNftFake(t)
		layout := dscpNftApplyOK(t, dscpNftTestPlanOne(), nil)
		f.learn(dscpNftLearnedSet(routeSanitizeSetID("b"), false), "10.1.2.7")
		stall := true
		f.fail = func(script string) (string, error) {
			if stall && strings.Contains(script, "flush chain inet b4_dscp postrouting\n") {
				return "", fmt.Errorf("command [nft -f -] gave up after 15s: %w", context.DeadlineExceeded)
			}
			return "", nil
		}
		plan := plan2(true)
		mark := len(f.scripts)
		out, err := dscpNftApplyPlan(plan, layout)
		if err == nil || !strings.Contains(err.Error(), "did not answer") {
			t.Fatalf("the timeout must be reported, got %v", err)
		}
		if !out.pending || out.rebuilt || out.layout.generation != 1 || !reflect.DeepEqual(out.layout.maps, []int{1, 2}) {
			t.Fatalf("a timeout must keep the known layout and note the generation it may have left, got %+v", out.layout)
		}
		for _, s := range f.since(mark) {
			if strings.Contains(s, "delete table") {
				t.Errorf("a timeout rebuilt the table:\n%s", s)
			}
		}
		stall = false
		next := dscpNftApplyOK(t, plan, out.layout)
		if next.generation != 3 || !reflect.DeepEqual(next.maps, []int{3}) || !reflect.DeepEqual(f.objects("map"), []string{"s4_3", "s6_3"}) {
			t.Errorf("the retry must fill a fresh generation and drop both older ones, got %+v with maps %v", next, f.objects("map"))
		}
		if got := f.table.sets[dscpNftLearnedSet(routeSanitizeSetID("b"), false)]; !reflect.DeepEqual(got, []string{"10.1.2.7"}) {
			t.Errorf("the retry lost a learned address: %v", got)
		}
	})

	t.Run("a rejected element batch stays out of the warning", func(t *testing.T) {
		f := installDSCPNftFake(t)
		layout := dscpNftApplyOK(t, dscpNftTestPlanOne(), nil)
		f.fail = func(script string) (string, error) {
			if !strings.Contains(script, "add element") {
				return "", nil
			}
			out := "/dev/stdin:5:1-27593: Error: Could not process rule: No such file or directory\n" + script
			return out, fmt.Errorf("command [nft -f -] failed: exit status 1 (%s)", out)
		}
		plan := dscpNftTestPlan(config.DSCPConfig{Enabled: true, Value: 7}, dscpPlanTestSet("big", 12, dscpNftTestRanges(1500)...))
		out, err := dscpNftApplyPlan(plan, layout)
		if err == nil || !strings.Contains(err.Error(), "builds the table again") || !strings.Contains(err.Error(), "like all others") {
			t.Fatalf("both refusals must be reported, got %v", err)
		}
		if len(err.Error()) > 1000 || strings.Contains(err.Error(), "100.64.2.0/24") {
			t.Errorf("the warning carries the rejected elements, %d bytes:\n%.400s", len(err.Error()), err.Error())
		}
		if out.layout == nil || out.layout.perSet() {
			t.Errorf("expected the global-only fallback, got %+v", out)
		}
	})

	commitThenStall := func(f *dscpNftFake, marker string) *bool {
		stall := true
		runNftStdin = func(script string) (string, error) {
			out, err := f.load(script)
			if err == nil && stall && strings.Contains(script, marker) {
				return "", fmt.Errorf("command [nft -f -] gave up after 15s: %w", context.DeadlineExceeded)
			}
			return out, err
		}
		return &stall
	}

	t.Run("a chain drop that stalled after deleting is not counted on", func(t *testing.T) {
		f := installDSCPNftFake(t)
		plan1 := dscpNftTestPlanOne()
		layout := dscpNftApplyOK(t, plan1, nil)
		stall := commitThenStall(f, "delete chain")
		layout = dscpNftApplyOK(t, dscpNftTestPlanTwo(config.DSCPConfig{Enabled: true, Value: 7}), layout)
		*stall = false
		if slices.Contains(f.objects("chain"), "v25") || slices.Contains(layout.values, 25) {
			t.Fatalf("the stalled drop deleted v25, the layout must not count on it: chains %v, recorded %v", f.objects("chain"), layout.values)
		}
		mark := len(f.scripts)
		out, err := dscpNftApplyPlan(plan1, layout)
		if err != nil || out.rebuilt || out.pending {
			t.Fatalf("a value whose chain went with a stalled drop must be filled again, not rebuilt: %v %+v", err, out)
		}
		if !strings.Contains(f.since(mark)[0], "add chain inet b4_dscp v25\n") {
			t.Errorf("the fill must create v25 again:\n%s", f.since(mark)[0])
		}
		if got, _ := f.stamp(netip.MustParseAddr("172.16.1.1"), "wan"); got != 25 {
			t.Errorf("172.16.1.1 leaves with %d, want 25", got)
		}
	})

	t.Run("a stalled swap keeps what the fill made", func(t *testing.T) {
		f := installDSCPNftFake(t)
		plan1 := dscpNftTestPlanOne()
		layout := dscpNftApplyOK(t, plan1, nil)
		stall := commitThenStall(f, "flush chain inet b4_dscp postrouting\n")
		out, err := dscpNftApplyPlan(dscpNftTestPlanTwo(config.DSCPConfig{Enabled: true, Value: 7}), layout)
		*stall = false
		if err == nil || !out.pending || out.layout.generation != 1 {
			t.Fatalf("a stalled swap must stay pending on generation 1, got %+v (%v)", out.layout, err)
		}
		if !slices.Contains(out.layout.values, 46) || !slices.Contains(out.layout.learned, routeSanitizeSetID("d")) {
			t.Errorf("the pending layout must keep the chain and sets the change made, got %+v", out.layout)
		}
		layout = dscpNftApplyOK(t, plan1, out.layout)
		chains, sets := f.objects("chain"), f.objects("set")
		if !reflect.DeepEqual(chains, []string{"postrouting", "v25", "v31", "v6"}) || slices.Contains(sets, dscpNftLearnedSet(routeSanitizeSetID("d"), false)) {
			t.Errorf("going back after a stalled swap left objects behind: chains %v, sets %v", chains, sets)
		}
		if !reflect.DeepEqual(f.objects("map"), []string{"s4_1", "s6_1"}) || !reflect.DeepEqual(layout.maps, []int{1}) {
			t.Errorf("maps %v, recorded %v", f.objects("map"), layout.maps)
		}
	})
}

const dscpNftListingGlobal = `table inet b4_dscp {
	chain postrouting {
		type filter hook postrouting priority 150; policy accept;
		oifname "lo" return
		meta mark & 0x20000000 == 0x20000000 return
		ct direction reply return
		ip dscp set 0x07
		ip6 dscp set 0x07
		ip daddr vmap @s4_1
		ip6 daddr vmap @s6_1
		ip daddr @l_c_125f_4 ip dscp set 0x19
		ip6 daddr @l_c_125f_6 ip6 dscp set 0x19
		ip daddr @l_b_14f2_4 ip dscp set 0x06
		ip6 daddr @l_b_14f2_6 ip6 dscp set 0x06
		ip daddr @l_a_d39_4 ip dscp set 0x1f
		ip6 daddr @l_a_d39_6 ip6 dscp set 0x1f
	}
}
`

const dscpNftListingScoped = `table inet b4_dscp {
	chain postrouting {
		type filter hook postrouting priority 150; policy accept;
		oifname "lo" return
		meta mark & 0x20000000 == 0x20000000 return
		ct direction reply return
		oifname "dummy0" ip daddr vmap @s4_3
		oifname "dummy0" ip6 daddr vmap @s6_3
		oifname "wan9" ip daddr vmap @s4_3
		oifname "wan9" ip6 daddr vmap @s6_3
		oifname "dummy0" ip daddr @l_big_2184_4 ip dscp set af12
		oifname "dummy0" ip6 daddr @l_big_2184_6 ip6 dscp set af12
		oifname "dummy0" ip daddr @l_d_580_4 ip dscp set ef
		oifname "dummy0" ip6 daddr @l_d_580_6 ip6 dscp set ef
		oifname "dummy0" ip daddr @l_b_14f2_4 ip dscp set 0x06
		oifname "dummy0" ip6 daddr @l_b_14f2_6 ip6 dscp set 0x06
		oifname "dummy0" ip daddr @l_a_d39_4 ip dscp set 0x1f
		oifname "dummy0" ip6 daddr @l_a_d39_6 ip6 dscp set 0x1f
		oifname "wan9" ip daddr @l_big_2184_4 ip dscp set af12
		oifname "wan9" ip6 daddr @l_big_2184_6 ip6 dscp set af12
		oifname "wan9" ip daddr @l_d_580_4 ip dscp set ef
		oifname "wan9" ip6 daddr @l_d_580_6 ip6 dscp set ef
		oifname "wan9" ip daddr @l_b_14f2_4 ip dscp set 0x06
		oifname "wan9" ip6 daddr @l_b_14f2_6 ip6 dscp set 0x06
		oifname "wan9" ip daddr @l_a_d39_4 ip dscp set 0x1f
		oifname "wan9" ip6 daddr @l_a_d39_6 ip6 dscp set 0x1f
	}
}
`

const dscpNftListingGlobalOnly = `table inet b4_dscp {
	chain postrouting {
		type filter hook postrouting priority 150; policy accept;
		oifname "lo" return
		meta mark & 0x20000000 == 0x20000000 return
		ct direction reply return
		ip dscp set 0x07
		ip6 dscp set 0x07
	}
}
`

func TestSetDSCPNftShape(t *testing.T) {
	without := func(listing string, drop string) string {
		return strings.Replace(listing, "\t\t"+drop+"\n", "", 1)
	}
	insertAfter := func(listing, anchor, line string) string {
		return strings.Replace(listing, "\t\t"+anchor+"\n", "\t\t"+anchor+"\n\t\t"+line+"\n", 1)
	}
	cases := []struct {
		name          string
		listing       string
		stamps, vmaps int
		want          bool
	}{
		{"nft 0.9.8 and 1.1.6, global on, three sets", dscpNftListingGlobal, 8, 2, true},
		{"nft 0.9.8 and 1.1.6, two interfaces, four sets, global off", dscpNftListingScoped, 16, 4, true},
		{"nft 0.9.8 and 1.1.6, global-only fallback", dscpNftListingGlobalOnly, 2, 0, true},
		{"global-only listing for a per-set layout", dscpNftListingGlobalOnly, 2, 2, false},
		{"a learned rule missing", without(dscpNftListingGlobal, "ip6 daddr @l_a_d39_6 ip6 dscp set 0x1f"), 8, 2, false},
		{"a vmap lookup missing", without(dscpNftListingGlobal, "ip6 daddr vmap @s6_1"), 8, 2, false},
		{"an interface's lookups missing", without(without(dscpNftListingScoped, `oifname "wan9" ip daddr vmap @s4_3`), `oifname "wan9" ip6 daddr vmap @s6_3`), 16, 4, false},
		{"a return missing", without(dscpNftListingGlobal, "ct direction reply return"), 8, 2, false},
		{"a vmap lookup before the returns", insertAfter(without(dscpNftListingGlobal, "ip daddr vmap @s4_1"), `oifname "lo" return`, "ip daddr vmap @s4_1"), 8, 2, false},
		{"a return after the stamps", insertAfter(dscpNftListingGlobal, "ip6 daddr @l_a_d39_6 ip6 dscp set 0x1f", "return"), 8, 2, false},
		{"a foreign stamp", insertAfter(dscpNftListingGlobal, "ip6 daddr vmap @s6_1", "ip dscp set 0x2e"), 8, 2, false},
		{"a vmap lookup twice", insertAfter(dscpNftListingGlobal, "ip6 daddr vmap @s6_1", "ip6 daddr vmap @s6_1"), 8, 2, false},
	}
	for _, tc := range cases {
		if got := dscpNftSetsChainShape(tc.listing, tc.stamps, tc.vmaps); got != tc.want {
			t.Errorf("%s: shape=%v, want %v", tc.name, got, tc.want)
		}
	}

	global := dscpNftTestPlanOne()
	if s, v := dscpNftStampCount(global), dscpNftVmapCount(global); !dscpNftSetsChainShape(dscpNftListingGlobal, s, v) {
		t.Errorf("the listing of the plan it was loaded from fails with counts %d and %d", s, v)
	}
	scoped := dscpNftTestPlan(config.DSCPConfig{Interfaces: []string{"dummy0", "wan9"}},
		dscpPlanTestSet("a", 31, "10.0.0.0/8"), dscpPlanTestSet("b", 6, "10.1.0.0/16"),
		dscpPlanTestSet("d", 46, "172.16.0.0/12"), dscpPlanTestSet("big", 12, dscpNftTestRanges(4)...))
	if s, v := dscpNftStampCount(scoped), dscpNftVmapCount(scoped); !dscpNftSetsChainShape(dscpNftListingScoped, s, v) {
		t.Errorf("the scoped listing fails with the plan's counts %d and %d", s, v)
	}

	origRun := run
	t.Cleanup(func() { run = origRun })
	listing := dscpNftListingGlobal
	run = func(args ...string) (string, error) {
		if strings.Join(args, " ") != "nft list chain inet b4_dscp postrouting" {
			return "", errors.New("unexpected")
		}
		if listing == "" {
			return "Error: No such file or directory", errors.New("exit status 1")
		}
		return listing, nil
	}
	layout := &dscpNftLayout{generation: 1, stamps: 8, vmaps: 2}
	if !dscpNftLayoutIntact(layout) {
		t.Errorf("an intact chain reads as broken")
	}
	listing = ""
	if dscpNftLayoutIntact(layout) {
		t.Errorf("a missing table reads as intact")
	}
}

func TestSetDSCPNftValueChainLifecycle(t *testing.T) {
	f := installDSCPNftFake(t)
	f.lateRelease = true
	layout := dscpNftApplyOK(t, dscpNftTestPlanOne(), nil)
	if got := f.objects("chain"); !reflect.DeepEqual(got, []string{"postrouting", "v25", "v31", "v6"}) {
		t.Fatalf("chains after the first apply: %v", got)
	}

	plan2 := dscpNftTestPlanTwo(config.DSCPConfig{Enabled: true, Value: 7})
	mark := len(f.scripts)
	layout = dscpNftApplyOK(t, plan2, layout)
	scripts := f.since(mark)
	if len(scripts) != 4 {
		t.Fatalf("expected fill, swap, drop and chain drop, got:\n%s", strings.Join(scripts, "----\n"))
	}
	if !strings.Contains(scripts[0], "add chain inet b4_dscp v46\n") || strings.Contains(scripts[0], "add chain inet b4_dscp v31\n") || strings.Contains(scripts[0], "add chain inet b4_dscp v6\n") {
		t.Errorf("the fill must create only the new value's chain:\n%s", scripts[0])
	}
	for i, s := range scripts {
		if strings.Contains(s, "delete chain") && (strings.Contains(s, "delete map") || i != 3) {
			t.Errorf("value chains must go in their own transaction after the maps that used them:\n%s", s)
		}
	}
	if scripts[3] != dscpNftDropChainsScript([]int{25}) {
		t.Errorf("only the departed value's chain may be dropped:\n%s", scripts[3])
	}
	if got := f.objects("chain"); !reflect.DeepEqual(got, []string{"postrouting", "v31", "v46", "v6"}) {
		t.Errorf("chains after the change: %v", got)
	}
	if !reflect.DeepEqual(layout.values, []int{6, 31, 46}) {
		t.Errorf("recorded value chains %v", layout.values)
	}

	probe := *f
	probe.table = f.table.clone()
	probe.scripts, probe.committed, probe.fail = nil, nil, nil
	combined := dscpNftDropScript([]int{2}, nil) + dscpNftDropChainsScript([]int{46})
	probe.table.chains[dscpNftChain] = nil
	if _, err := probe.load(combined); err == nil {
		t.Errorf("the fake must refuse a value chain deleted with its map in one transaction, as kernels before 6.4 do")
	}

	refuse := true
	f.fail = func(script string) (string, error) {
		if refuse && strings.Contains(script, "delete chain") {
			return "Error: Could not process rule: Device or resource busy", errors.New("command [nft -f -] failed: exit status 1 (Error: Could not process rule: Device or resource busy)")
		}
		return "", nil
	}
	plan3 := dscpNftTestPlan(config.DSCPConfig{Enabled: true, Value: 7},
		dscpPlanTestSet("a", 31, "10.0.0.0/8"),
		dscpPlanTestSet("b", 6, "10.1.0.0/16", "172.16.0.0/12", "10.1.2.0/24"),
	)
	layout = dscpNftApplyOK(t, plan3, layout)
	if !reflect.DeepEqual(layout.values, []int{6, 31, 46}) || !slices.Contains(f.objects("chain"), "v46") {
		t.Errorf("a chain that could not be deleted must stay recorded, got %v", layout.values)
	}
	if !reflect.DeepEqual(layout.maps, []int{3}) {
		t.Errorf("a refused chain drop must not keep the maps, got %v", layout.maps)
	}

	refuse = false
	mark = len(f.scripts)
	layout = dscpNftApplyOK(t, plan3, layout)
	if scripts := f.since(mark); len(scripts) != 2 || scripts[1] != dscpNftDropChainsScript([]int{46}) {
		t.Errorf("the next apply must retry the chain drop, got:\n%s", strings.Join(scripts, "----\n"))
	}
	if !reflect.DeepEqual(layout.values, []int{6, 31}) || slices.Contains(f.objects("chain"), "v46") {
		t.Errorf("after the retry the chains are %v, recorded %v", f.objects("chain"), layout.values)
	}

	refuse = true
	layout = dscpNftApplyOK(t, plan2, layout)
	layout = dscpNftApplyOK(t, plan3, layout)
	if !reflect.DeepEqual(layout.values, []int{6, 31, 46}) || !slices.Contains(f.objects("chain"), "v46") {
		t.Fatalf("the refused drop must leave v46 recorded and in place, got %v", layout.values)
	}
	refuse = false
	mark = len(f.scripts)
	layout = dscpNftApplyOK(t, plan2, layout)
	scripts = f.since(mark)
	if strings.Contains(scripts[0], "add chain inet b4_dscp v46\n") || !strings.Contains(scripts[0], ": jump v46") {
		t.Errorf("a value whose chain is still in place must reuse it, not create it again:\n%s", scripts[0])
	}
	if got, _ := f.stamp(netip.MustParseAddr("172.16.1.1"), "wan"); got != 46 {
		t.Errorf("the value that came back is not written, got %d", got)
	}
	if !reflect.DeepEqual(layout.values, []int{6, 31, 46}) {
		t.Errorf("recorded value chains %v", layout.values)
	}

	plan4 := dscpNftTestPlan(config.DSCPConfig{Enabled: true, Value: 7},
		dscpPlanTestSet("a", 31, "10.0.0.0/8"),
		dscpPlanTestSet("b", 6, "10.1.0.0/16"),
		dscpPlanTestSet("d", 46, "172.16.0.0/12"),
		dscpPlanTestSet("e", 25, "192.0.2.0/24"),
	)
	mark = len(f.scripts)
	layout = dscpNftApplyOK(t, plan4, layout)
	for _, s := range f.since(mark) {
		if strings.Contains(s, "delete table") {
			t.Errorf("the value chain lifecycle fell back to a full replace:\n%s", s)
		}
	}
	if got, _ := f.stamp(netip.MustParseAddr("192.0.2.1"), "wan"); got != 25 {
		t.Errorf("a value that came back is not written, got %d", got)
	}
	if !reflect.DeepEqual(layout.values, []int{6, 25, 31, 46}) || !reflect.DeepEqual(f.objects("chain"), []string{"postrouting", "v25", "v31", "v46", "v6"}) {
		t.Errorf("chains %v, recorded %v", f.objects("chain"), layout.values)
	}
}

func TestSetDSCPNftGlobalOnlyUnchanged(t *testing.T) {
	configs := []config.DSCPConfig{
		{Enabled: true, Value: 7},
		{Enabled: true, Value: 46, Interfaces: []string{"eth0", "wan"}},
		{Enabled: false, Value: 7},
	}
	for _, global := range configs {
		cfg := dscpTestConfig(global.Enabled, global.Value, global.Interfaces...)
		quiet := dscpPlanTestSet("quiet", 31, "10.0.0.0/8")
		quiet.DSCP.Enabled = false
		cfg.Sets = append(cfg.Sets, quiet)
		plan := dscpPlanFor(cfg)
		if !plan.empty() {
			t.Fatalf("%+v: a configuration without a set's DSCP gave a plan with sets", global)
		}
		want := []string(nil)
		if global.Enabled {
			script, _ := dscpNftScript(global.Value, global.Interfaces)
			want = []string{script}
		}

		f := installDSCPNftFake(t)
		if err := applyDSCPFor(cfg, backendNFTables); err != nil {
			t.Fatalf("applyDSCPFor: %v", err)
		}
		if !reflect.DeepEqual(f.scripts, want) {
			t.Errorf("%+v: the nftables emission changed:\n%s\nwant:\n%s", global, strings.Join(f.scripts, "----\n"), strings.Join(want, "----\n"))
		}

		f = installDSCPNftFake(t)
		out, err := dscpNftApplyPlan(plan, nil)
		if err != nil {
			t.Fatalf("dscpNftApplyPlan: %v", err)
		}
		if !reflect.DeepEqual(f.scripts, want) {
			t.Errorf("%+v: an empty plan must load today's script and nothing else:\n%s", global, strings.Join(f.scripts, "----\n"))
		}
		for _, s := range f.scripts {
			for _, marker := range []string{"vmap", "add map", "add set", "flush chain"} {
				if strings.Contains(s, marker) {
					t.Errorf("%+v: an empty plan emitted %q:\n%s", global, marker, s)
				}
			}
		}
		if global.Enabled {
			_, stamps := dscpNftScript(global.Value, global.Interfaces)
			if out.layout == nil || out.layout.perSet() || out.layout.stamps != stamps || out.layout.vmaps != 0 {
				t.Errorf("%+v: an empty plan must be recorded as the global stamp, got %+v", global, out.layout)
			}
		} else if out.layout != nil || !out.gone {
			t.Errorf("%+v: with nothing to stamp nothing may be recorded, got %+v", global, out)
		}
	}
}
