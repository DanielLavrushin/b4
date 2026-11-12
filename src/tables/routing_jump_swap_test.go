package tables

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

type jumpSwapChains struct {
	rules      map[string][]string
	writes     []string
	positional []string
	unreadable bool
	intruder   bool
	intrusions int
	watch      string
	gaps       []string
}

const jumpSwapHeld = "~held"

func jumpSwapTarget(rule string) string {
	return strings.SplitN(rule, "~", 2)[0]
}

func jumpSwapRule(args []string) string {
	rule := args[len(args)-1]
	for _, a := range args {
		if a == "-m" {
			return rule + jumpSwapHeld
		}
	}
	return rule
}

func (c *jumpSwapChains) key(cmd, chain string) string { return cmd + " " + chain }

func (c *jumpSwapChains) note(cmd, chain string, args []string) {
	c.writes = append(c.writes, strings.Join(args, " "))
	for _, rule := range c.rules[c.key(cmd, chain)] {
		if jumpSwapTarget(rule) == c.watch {
			return
		}
	}
	c.gaps = append(c.gaps, strings.Join(args, " "))
}

func (c *jumpSwapChains) apply(args []string) (string, error) {
	out, err := c.applyOne(args)
	if c.intruder {
		for key, rules := range c.rules {
			c.intrusions++
			c.rules[key] = append([]string{fmt.Sprintf("XRAY%d", c.intrusions)}, rules...)
		}
	}
	return out, err
}

func (c *jumpSwapChains) applyOne(args []string) (string, error) {
	cmd := args[0]
	for i, a := range args {
		if i+1 >= len(args) {
			break
		}
		chain := args[i+1]
		key := c.key(cmd, chain)
		switch a {
		case "-L":
			if c.unreadable {
				return "", errors.New("cannot list")
			}
			var b strings.Builder
			b.WriteString("Chain " + chain + " (policy ACCEPT)\nnum  target  prot opt source destination\n")
			for n, rule := range c.rules[key] {
				b.WriteString(fmt.Sprintf("%d    %s  all  --  0.0.0.0/0  0.0.0.0/0\n", n+1, jumpSwapTarget(rule)))
			}
			return b.String(), nil
		case "-A":
			c.rules[key] = append(c.rules[key], jumpSwapRule(args))
			c.note(cmd, chain, args)
			return "", nil
		case "-I":
			at, _ := strconv.Atoi(args[i+2])
			rules := c.rules[key]
			c.rules[key] = append(append(append([]string{}, rules[:at-1]...), jumpSwapRule(args)), rules[at-1:]...)
			c.note(cmd, chain, args)
			return "", nil
		case "-D":
			rules := c.rules[key]
			at := -1
			if n, err := strconv.Atoi(args[i+2]); err == nil {
				at = n - 1
				c.positional = append(c.positional, strings.Join(args, " "))
			} else {
				for n, rule := range rules {
					if rule == jumpSwapRule(args) {
						at = n
						break
					}
				}
			}
			if at < 0 || at >= len(rules) {
				return "", errors.New("no such rule")
			}
			c.rules[key] = append(append([]string{}, rules[:at]...), rules[at+1:]...)
			c.note(cmd, chain, args)
			return "", nil
		}
	}
	return "", nil
}

func jumpSwapIpt(t *testing.T, chain string, initial []string, watch string) *jumpSwapChains {
	t.Helper()
	stubBinaries(t, backendIPTables, backendIP6Tables)
	origRun, origLogged := run, runLogged
	t.Cleanup(func() {
		run = origRun
		runLogged = origLogged
	})
	c := &jumpSwapChains{rules: map[string][]string{}, watch: watch}
	for _, cmd := range []string{backendIPTables, backendIP6Tables} {
		c.rules[c.key(cmd, chain)] = append([]string{}, initial...)
	}
	run = func(args ...string) (string, error) { return c.apply(args) }
	runLogged = func(op string, args ...string) bool {
		_, err := c.apply(args)
		return err == nil
	}
	return c
}

func TestAJumpIsReplacedWithoutEverLeavingItsChain(t *testing.T) {
	const jump = "b4r_set_out"

	for _, tc := range []struct {
		name    string
		base    string
		mangle  bool
		atTop   bool
		initial []string
		want    string
	}{
		{"the router's own traffic, lifted to the top", "OUTPUT", true, true,
			[]string{"b4r_other_out", jump, "ACCEPT", "B4"}, jump + ",b4r_other_out,ACCEPT,B4"},
		{"a duplicate left by an earlier run", "OUTPUT", true, true,
			[]string{jump, "ACCEPT", jump}, jump + ",ACCEPT"},
		{"the source rewrite, appended", "POSTROUTING", false, false,
			[]string{"MASQ", jump, "OTHER"}, "MASQ,OTHER," + jump},
		{"the source rewrite, ahead of the firmware's masquerade", "POSTROUTING", false, true,
			[]string{"MASQ", jump}, jump + ",MASQ"},
		{"a first install", "OUTPUT", true, true,
			[]string{"ACCEPT"}, jump + ",ACCEPT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			standing := false
			for _, target := range tc.initial {
				standing = standing || target == jump
			}
			c := jumpSwapIpt(t, tc.base, tc.initial, jump)

			(&routeIptBackend{}).ensureJumpRule(tc.base, jump, tc.mangle, tc.atTop)

			for _, cmd := range []string{backendIPTables, backendIP6Tables} {
				if got := strings.Join(c.rules[c.key(cmd, tc.base)], ","); got != tc.want {
					t.Errorf("%s %s is %s, want %s", cmd, tc.base, got, tc.want)
				}
			}
			if standing && len(c.gaps) > 0 {
				t.Errorf("the jump was standing and %d command(s) left %s without it: %v. Every rebuild of a set took its jump out before putting it back, and whatever the router sent in between skipped the set: its own connection to an address the set routes left by the ordinary uplink, and a connection that should have had its source rewritten kept the wrong one for life",
					len(c.gaps), tc.base, c.gaps)
			}
			if len(c.positional) > 0 {
				t.Errorf("the swap deleted by line number: %v. A line number is only good until another program writes to the chain, so a jump b4 can name by its own rule is deleted by that rule", c.positional)
			}
		})
	}
}

func TestAJumpSwapLeavesRulesInsertedMeanwhileAlone(t *testing.T) {
	const jump = "b4r_set_pre"

	for _, tc := range []struct {
		name  string
		atTop bool
	}{
		{"lifted to the top", true},
		{"appended", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := jumpSwapIpt(t, "PREROUTING", []string{"FIRMWARE", jump, "B4"}, jump)
			c.intruder = true

			(&routeIptBackend{}).ensureJumpRule("PREROUTING", jump, true, tc.atTop)

			for _, cmd := range []string{backendIPTables, backendIP6Tables} {
				rules := c.rules[c.key(cmd, "PREROUTING")]
				counts := map[string]int{}
				for _, rule := range rules {
					counts[rule]++
				}
				if counts["FIRMWARE"] != 1 || counts["B4"] != 1 {
					t.Errorf("%s PREROUTING is %v after the swap: a rule that was there before it is gone. Another program put a rule at the head of the chain after every command b4 ran, which moves every line number b4 had read, and a delete by line number then removes whatever sits there now: the firmware's own rule, with nothing in the log",
						cmd, rules)
				}
				if counts[jump] != 1 || counts[jump+jumpSwapHeld] != 0 {
					t.Errorf("%s PREROUTING is %v after the swap, want the set's jump once and nothing held over", cmd, rules)
				}
			}
			if len(c.positional) > 0 {
				t.Errorf("the swap deleted by line number while another program was writing to the chain: %v", c.positional)
			}
			if len(c.gaps) > 0 {
				t.Errorf("%d command(s) left PREROUTING without the set's jump: %v", len(c.gaps), c.gaps)
			}
		})
	}
}

func TestAJumpHeldOverFromAnInterruptedSwapIsRemoved(t *testing.T) {
	const jump = "b4r_set_out"
	c := jumpSwapIpt(t, "OUTPUT", []string{jump + jumpSwapHeld, "ACCEPT", jump}, jump)

	(&routeIptBackend{}).ensureJumpRule("OUTPUT", jump, true, true)

	for _, cmd := range []string{backendIPTables, backendIP6Tables} {
		if got := strings.Join(c.rules[c.key(cmd, "OUTPUT")], ","); got != jump+",ACCEPT" {
			t.Errorf("%s OUTPUT is %s, want %s: b4 stopped between two steps of an earlier swap and left its holding jump in the chain, and the next swap has to take it out with its own", cmd, got, jump+",ACCEPT")
		}
	}
	if len(c.positional) > 0 || len(c.gaps) > 0 {
		t.Errorf("removing a held jump took %d delete(s) by line number and left %d gap(s), want neither: %v %v", len(c.positional), len(c.gaps), c.positional, c.gaps)
	}
}

func TestAJumpOfAnotherFormIsSweptAfterTheSwap(t *testing.T) {
	const jump = "b4r_set_pre"
	c := jumpSwapIpt(t, "PREROUTING", []string{jump + "~gated", "ACCEPT", jump}, jump)

	(&routeIptBackend{}).ensureJumpRule("PREROUTING", jump, true, true)

	for _, cmd := range []string{backendIPTables, backendIP6Tables} {
		if got := strings.Join(c.rules[c.key(cmd, "PREROUTING")], ","); got != jump+",ACCEPT" {
			t.Errorf("%s PREROUTING is %s, want %s: a jump to the set's chain that carries another match, such as the per-device one a set had before its devices were cleared, is not the rule b4 deletes by name, and it still has to go", cmd, got, jump+",ACCEPT")
		}
	}
}

func TestAJumpThatCannotBeListedIsStillReplaced(t *testing.T) {
	const jump = "b4r_set_out"
	c := jumpSwapIpt(t, "OUTPUT", []string{"ACCEPT", jump}, jump)
	c.unreadable = true

	(&routeIptBackend{}).ensureJumpRule("OUTPUT", jump, true, true)

	for _, cmd := range []string{backendIPTables, backendIP6Tables} {
		if got := strings.Join(c.rules[c.key(cmd, "OUTPUT")], ","); got != jump+",ACCEPT" {
			t.Errorf("%s could not list mangle OUTPUT and left it as %s, want %s; without a listing b4 cannot tell which jumps were standing, so it has to delete by rule and add again rather than stack one more on every rebuild",
				cmd, got, jump+",ACCEPT")
		}
	}
}

type jumpSwapNft struct {
	chains map[string][]jumpSwapNftRule
	next   int
	watch  string
	base   string
	gaps   []string
}

type jumpSwapNftRule struct {
	handle int
	text   string
}

func (n *jumpSwapNft) note(args []string) {
	for _, r := range n.chains[n.base] {
		if r.text == "jump "+n.watch {
			return
		}
	}
	n.gaps = append(n.gaps, strings.Join(args, " "))
}

func (n *jumpSwapNft) apply(args []string) (string, error) {
	if len(args) < 2 || args[0] != "nft" {
		return "", nil
	}
	switch {
	case args[1] == "-a" && len(args) == 7:
		chain := args[6]
		var b strings.Builder
		b.WriteString("table inet " + routeNftTable + " {\n\tchain " + chain + " {\n")
		for _, r := range n.chains[chain] {
			b.WriteString(fmt.Sprintf("\t\t%s # handle %d\n", r.text, r.handle))
		}
		b.WriteString("\t}\n}\n")
		return b.String(), nil
	case args[1] == "add" && args[2] == "rule":
		chain := args[5]
		n.next++
		n.chains[chain] = append(n.chains[chain], jumpSwapNftRule{handle: n.next, text: strings.Join(args[6:], " ")})
		n.note(args)
		return "", nil
	case args[1] == "delete" && args[2] == "rule":
		chain := args[5]
		handle, _ := strconv.Atoi(args[7])
		rules := n.chains[chain]
		for i, r := range rules {
			if r.handle == handle {
				n.chains[chain] = append(append([]jumpSwapNftRule{}, rules[:i]...), rules[i+1:]...)
				n.note(args)
				return "", nil
			}
		}
		return "", errors.New("no such rule")
	}
	return "", nil
}

func (n *jumpSwapNft) texts(chain string) string {
	var out []string
	for _, r := range n.chains[chain] {
		out = append(out, r.text)
	}
	return strings.Join(out, ",")
}

func TestAnNftJumpIsReplacedWithoutEverLeavingItsChain(t *testing.T) {
	const jump = "b4r_set_out"
	origRun, origLogged := run, runLogged
	t.Cleanup(func() {
		run = origRun
		runLogged = origLogged
	})

	n := &jumpSwapNft{
		chains: map[string][]jumpSwapNftRule{
			routeNftOutput: {{handle: 4, text: "jump b4r_other_out"}, {handle: 7, text: "jump " + jump}, {handle: 9, text: "jump " + jump}},
		},
		next:  20,
		watch: jump,
		base:  routeNftOutput,
	}
	run = func(args ...string) (string, error) { return n.apply(args) }
	runLogged = func(op string, args ...string) bool {
		_, err := n.apply(args)
		return err == nil
	}

	(&routeNftBackend{}).ensureJumpRule("OUTPUT", jump, true, true)

	if got, want := n.texts(routeNftOutput), "jump b4r_other_out,jump "+jump; got != want {
		t.Errorf("the output chain is %s, want %s", got, want)
	}
	if len(n.gaps) > 0 {
		t.Errorf("%d command(s) left the output chain without the set's jump: %v. Every rebuild of a set took its jump out before putting it back, and the router's own connection to an address the set routes left by the ordinary uplink in between",
			len(n.gaps), n.gaps)
	}
}
