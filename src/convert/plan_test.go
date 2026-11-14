package convert

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlan_SaysWhyASetIsDisabled(t *testing.T) {
	for _, c := range []struct {
		name, line string
		want       []string
	}{
		{"a profile with a strategy and no host list", "--filter-tcp=443 --dpi-desync=fake,multisplit", []string{DisabledNeedsTargets}},
		{"a profile with its own domains", "--filter-tcp=443 --hostlist-domains=example.com --dpi-desync=fake", []string{""}},
		{"a host list in a file", "--filter-tcp=443 --hostlist=/opt/zapret/hosts.txt --dpi-desync=fake", []string{DisabledNeedsTargets}},
		{"a call profile without UDP ports", "--filter-l7=discord,stun --dpi-desync=fake --dpi-desync-repeats=6", []string{DisabledNeedsPorts}},
		{"a call profile with UDP ports", "--filter-udp=50000-50100 --filter-l7=discord,stun --dpi-desync=fake", []string{""}},
		{"a profile without a strategy", "--filter-tcp=443 --hostcase", []string{DisabledNoStrategy}},
		{"a profile without a strategy that has a domain", "--filter-tcp=443 --hostlist-domains=example.com --hostcase", []string{DisabledNoStrategy}},
		{"a skipped profile", "--filter-tcp=443 --dpi-desync=fake,multisplit --skip", []string{DisabledSkipped}},
		{"a skipped profile that has a domain", "--filter-tcp=443 --hostlist-domains=example.com --dpi-desync=fake --skip", []string{DisabledSkipped}},
		{"a set shadowed by an earlier one",
			"--filter-tcp=443 --hostlist-domains=example.com --dpi-desync=fake --new --filter-tcp=80 --hostlist-domains=example.com --dpi-desync=multisplit",
			[]string{"", DisabledShadowed}},
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
			if plan.DisabledFor != c.want[i] {
				t.Errorf("%s: set %d DisabledFor = %q, want %q (enabled=%v accepts_targets=%v domains=%v)",
					c.name, i+1, plan.DisabledFor, c.want[i], plan.Enabled, plan.AcceptsTargets, plan.Domains)
			}
			if (plan.DisabledFor == "") != plan.Enabled {
				t.Errorf("%s: set %d enabled=%v with DisabledFor %q: every disabled set, and no enabled one, has a reason", c.name, i+1, plan.Enabled, plan.DisabledFor)
			}
			if text := res.DisabledEnglish(i); (text == "") != plan.Enabled || strings.Contains(text, "{{") {
				t.Errorf("%s: set %d enabled=%v has the reason text %q", c.name, i+1, plan.Enabled, text)
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
	if bare.Plan[0].DisabledFor != DisabledNeedsTargets || bare.Plan[0].Enabled {
		t.Fatalf("precondition: without domains the set is disabled for lack of targets: %+v", bare.Plan[0])
	}
	given, err := Analyze(line, Options{Tool: "zapret", Domains: []string{"example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if !given.Plan[0].Enabled || given.Plan[0].DisabledFor != "" {
		t.Errorf("a set that lacked only targets must come out enabled once it has a domain: %+v", given.Plan[0])
	}
}

func TestPlan_DisabledReasonStaysOutOfTheJSON(t *testing.T) {
	res, err := Analyze("--filter-tcp=443 --dpi-desync=fake,multisplit", Options{Tool: "zapret"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(res.Plan)
	if err != nil {
		t.Fatal(err)
	}
	if lower := strings.ToLower(string(raw)); strings.Contains(lower, "needs") || strings.Contains(lower, "disabled") {
		t.Errorf("the plan in the REST answer must not gain a field: %s", raw)
	}
}

func TestPlan_EveryDisabledSetOfTheSamplesHasAReason(t *testing.T) {
	samples, err := filepath.Glob("testdata/*")
	if err != nil || len(samples) == 0 {
		t.Fatalf("no samples: %v", err)
	}
	disabled := 0
	for _, path := range samples {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		res, err := Analyze(string(raw), Options{})
		if err != nil {
			continue
		}
		for i, plan := range res.Plan {
			text := res.DisabledEnglish(i)
			if plan.Enabled != (plan.DisabledFor == "") || plan.Enabled != (text == "") {
				t.Errorf("%s: set %d enabled=%v, DisabledFor %q, text %q", path, i+1, plan.Enabled, plan.DisabledFor, text)
			}
			if !plan.Enabled {
				disabled++
			}
		}
	}
	if disabled == 0 {
		t.Fatal("the samples hold no disabled set, so this test checks nothing")
	}
}
