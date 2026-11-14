package convert

import "testing"

func TestWarnings_ADisabledSetWithDomainsIsNotCalledTargetlessOrShadowed(t *testing.T) {
	const (
		skipped  = "--filter-tcp=443 --hostlist-domains=example.com --dpi-desync=fake --skip"
		shadowed = "--filter-tcp=443 --hostlist-domains=example.com --dpi-desync=fake --new --filter-tcp=80 --hostlist-domains=example.com --dpi-desync=multisplit"
	)
	for _, c := range []struct {
		name, line string
		want, not  []string
	}{
		{"a skipped profile that has a domain", skipped, nil, []string{"needsTargets", "shadowedSets", "catchAllProfile"}},
		{"a profile without a strategy that has a domain", "--filter-tcp=443 --hostlist-domains=example.com --hostcase",
			[]string{"nothingRecognized", "setsWithoutStrategy"}, []string{"needsTargets", "shadowedSets"}},
		{"a skipped profile with a domain, then a profile without a host list", skipped + " --new --filter-tcp=80 --dpi-desync=multisplit",
			[]string{"catchAllProfile"}, []string{"needsTargets", "shadowedSets"}},
		{"no host list anywhere", "--filter-tcp=443 --dpi-desync=fake", []string{"needsTargets"}, []string{"catchAllProfile", "shadowedSets"}},
		{"a set shadowed by an earlier one", shadowed, []string{"shadowedSets"}, []string{"needsTargets"}},
	} {
		res, err := Analyze(c.line, Options{Tool: "zapret"})
		if err != nil {
			t.Errorf("%s: Analyze(%q): %v", c.name, c.line, err)
			continue
		}
		for _, code := range c.want {
			if !hasWarning(res, code) {
				t.Errorf("%s: the warning %s is missing: %+v", c.name, code, res.Warnings)
			}
		}
		for _, code := range c.not {
			if hasWarning(res, code) {
				t.Errorf("%s: the warning %s does not describe this configuration: %+v", c.name, code, res.Warnings)
			}
		}
	}

	res, err := Analyze(shadowed+" --new --filter-tcp=8443 --hostlist-domains=example.net --dpi-desync=fake --skip", Options{Tool: "zapret"})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range res.Warnings {
		if w.Code == "shadowedSets" && w.Params["count"] != 1 {
			t.Errorf("one set is shadowed and one is skipped, but shadowedSets counts %v", w.Params["count"])
		}
	}
}
