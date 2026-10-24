package discovery

import (
	"fmt"

	"github.com/daniellavrushin/b4/config"
)

type DSCPPathChange struct {
	Set, Family string
	Tested, Now *int
}

func (c *DSCPPathChange) Error() string {
	over, traffic := "", fmt.Sprintf("the traffic of set %q", c.Set)
	if c.Family != "" {
		over, traffic = " over "+c.Family, fmt.Sprintf("the %s traffic of set %q", c.Family, c.Set)
	}
	switch {
	case c.Tested == nil:
		return fmt.Sprintf("the strategy was tested without a DSCP value of the set%s, but %s now carries DSCP %d, so it takes another network path", over, traffic, *c.Now)
	case c.Now == nil:
		return fmt.Sprintf("the strategy was tested with DSCP %d%s, but %s no longer carries a DSCP value of its own, so it takes another network path", *c.Tested, over, traffic)
	}
	return fmt.Sprintf("the strategy was tested with DSCP %d%s, but %s now carries DSCP %d, so it takes another network path", *c.Tested, over, traffic, *c.Now)
}

type dscpFamilyPath struct {
	family      string
	tested, now *int
}

func sameDSCPValue(a, b *int) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func DSCPPathChanged(cfg *config.Config, set, tested *config.SetConfig, testedOnly string, stampNow func(*config.Config, *config.DSCPProbe) (*config.DSCPProbe, error)) (*DSCPPathChange, error) {
	probe := cfg.ProbeDSCP(set)
	if probe != nil {
		probe.IPv4 = probe.IPv4 && tested.MatchesIPVersion(4)
		probe.IPv6 = probe.IPv6 && tested.MatchesIPVersion(6)
		if !probe.IPv4 && !probe.IPv6 {
			probe = nil
		}
	}
	now, err := stampNow(cfg, probe)
	if err != nil {
		return nil, err
	}
	var paths []dscpFamilyPath
	for _, f := range []struct {
		family, only string
		version      uint8
	}{{"IPv4", "4", 4}, {"IPv6", "6", 6}} {
		if !set.MatchesIPVersion(f.version) || !tested.MatchesIPVersion(f.version) {
			continue
		}
		path := dscpFamilyPath{family: f.family}
		if tested.DSCP.Enabled && (testedOnly == "" || testedOnly == f.only) {
			value := tested.DSCP.Value
			path.tested = &value
		}
		if now != nil && ((f.version == 4 && now.IPv4) || (f.version == 6 && now.IPv6)) {
			value := now.Value
			path.now = &value
		}
		paths = append(paths, path)
	}
	for _, path := range paths {
		if sameDSCPValue(path.tested, path.now) {
			continue
		}
		change := &DSCPPathChange{Set: set.Name, Tested: path.tested, Now: path.now}
		if len(paths) == 2 && !(sameDSCPValue(paths[0].tested, paths[1].tested) && sameDSCPValue(paths[0].now, paths[1].now)) {
			change.Family = path.family
		}
		return change, nil
	}
	return nil, nil
}
