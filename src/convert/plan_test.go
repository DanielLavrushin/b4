package convert

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPlan_NeedsTargetsOnlyWhenTargetsAreAllASetLacks(t *testing.T) {
	for _, c := range []struct {
		name, line string
		want       []bool
	}{
		{"a profile with a strategy and no host list", "--filter-tcp=443 --dpi-desync=fake,multisplit", []bool{true}},
		{"a profile with its own domains", "--filter-tcp=443 --hostlist-domains=example.com --dpi-desync=fake", []bool{false}},
		{"a host list in a file", "--filter-tcp=443 --hostlist=/opt/zapret/hosts.txt --dpi-desync=fake", []bool{true}},
		{"a call profile without UDP ports", "--filter-l7=discord,stun --dpi-desync=fake --dpi-desync-repeats=6", []bool{false}},
		{"a call profile with UDP ports", "--filter-udp=50000-50100 --filter-l7=discord,stun --dpi-desync=fake", []bool{false}},
		{"a profile without a strategy", "--filter-tcp=443 --hostcase", []bool{false}},
		{"a skipped profile", "--filter-tcp=443 --dpi-desync=fake,multisplit --skip", []bool{false}},
		{"a set shadowed by an earlier one",
			"--filter-tcp=443 --hostlist-domains=example.com --dpi-desync=fake --new --filter-tcp=80 --hostlist-domains=example.com --dpi-desync=multisplit",
			[]bool{false, false}},
	} {
		res, err := Analyze(c.line, Options{Tool: "zapret"})
		if err != nil {
			t.Errorf("%s: Analyze(%q): %v", c.name, c.line, err)
			continue
		}
		if len(res.Plan) != len(c.want) {
			t.Errorf("%s: %d sets, want %d", c.name, len(res.Plan), len(c.want))
			continue
		}
		for i, plan := range res.Plan {
			if plan.NeedsTargets != c.want[i] {
				t.Errorf("%s: set %d NeedsTargets = %v, want %v (enabled=%v accepts_targets=%v domains=%v)",
					c.name, i+1, plan.NeedsTargets, c.want[i], plan.Enabled, plan.AcceptsTargets, plan.Domains)
			}
			if plan.NeedsTargets && (plan.Enabled || !plan.AcceptsTargets) {
				t.Errorf("%s: set %d is marked as lacking targets while enabled=%v accepts_targets=%v", c.name, i+1, plan.Enabled, plan.AcceptsTargets)
			}
		}
	}
}

func TestPlan_GivenDomainsEnableASetThatNeededTargets(t *testing.T) {
	const line = "--filter-tcp=443 --dpi-desync=fake,multisplit"
	bare, err := Analyze(line, Options{Tool: "zapret"})
	if err != nil {
		t.Fatal(err)
	}
	if !bare.Plan[0].NeedsTargets || bare.Plan[0].Enabled {
		t.Fatalf("precondition: without domains the set is disabled for lack of targets: %+v", bare.Plan[0])
	}
	given, err := Analyze(line, Options{Tool: "zapret", Domains: []string{"example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if !given.Plan[0].Enabled || given.Plan[0].NeedsTargets {
		t.Errorf("a set marked NeedsTargets must come out enabled once it has a domain: %+v", given.Plan[0])
	}
}

func TestPlan_NeedsTargetsStaysOutOfTheJSON(t *testing.T) {
	res, err := Analyze("--filter-tcp=443 --dpi-desync=fake,multisplit", Options{Tool: "zapret"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(res.Plan)
	if err != nil {
		t.Fatal(err)
	}
	if lower := strings.ToLower(string(raw)); strings.Contains(lower, "needs") {
		t.Errorf("the plan in the REST answer must not gain a field: %s", raw)
	}
}
