package discovery

import (
	"fmt"
	"slices"
	"strings"

	"github.com/daniellavrushin/b4/config"
)

type DSCPPathChange struct {
	Set, Family      string
	Tested, Now      *int
	Interfaces       bool
	TestedInterfaces []string
	NowInterfaces    []string
}

func dscpScopeLabel(ifaces []string) string {
	if len(ifaces) == 0 {
		return "every interface"
	}
	return strings.Join(ifaces, ", ")
}

func (c *DSCPPathChange) Error() string {
	if c.Interfaces {
		return fmt.Sprintf("the strategy was tested while b4 wrote DSCP values on %s, but it writes them on %s now, so the traffic of set %q takes another network path",
			dscpScopeLabel(c.TestedInterfaces), dscpScopeLabel(c.NowInterfaces), c.Set)
	}
	over, traffic := "", fmt.Sprintf("the traffic of set %q", c.Set)
	if c.Family != "" {
		over, traffic = " over "+c.Family, fmt.Sprintf("the %s traffic of set %q", c.Family, c.Set)
	}
	switch {
	case c.Tested == nil:
		return fmt.Sprintf("the strategy was tested without a DSCP value%s, but %s now carries DSCP %d, so it takes another network path", over, traffic, *c.Now)
	case c.Now == nil:
		return fmt.Sprintf("the strategy was tested with DSCP %d%s, but %s no longer carries a DSCP value, so it takes another network path", *c.Tested, over, traffic)
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

func sameInterfaces(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

func DSCPPathChanged(cfg *config.Config, set, tested *config.SetConfig, path *config.DSCPPath, stampNow func(*config.Config, *config.DSCPProbe) (*config.DSCPProbe, *config.DSCPPath, error)) (*DSCPPathChange, error) {
	probe := cfg.ProbeDSCP(set)
	if probe != nil {
		probe.IPv4 = probe.IPv4 && tested.MatchesIPVersion(4)
		probe.IPv6 = probe.IPv6 && tested.MatchesIPVersion(6)
		if !probe.IPv4 && !probe.IPv6 {
			probe = nil
		}
	}
	stamped, now, err := stampNow(cfg, probe)
	if err != nil {
		return nil, err
	}
	var paths []dscpFamilyPath
	carried := false
	for _, f := range []struct {
		family  string
		version uint8
		v6      bool
	}{{"IPv4", 4, false}, {"IPv6", 6, true}} {
		if !set.MatchesIPVersion(f.version) || !tested.MatchesIPVersion(f.version) {
			continue
		}
		p := dscpFamilyPath{family: f.family}
		if path != nil {
			p.tested, p.now = path.Value(f.v6), now.Value(f.v6)
		} else {
			if tested.DSCP.Enabled {
				value := tested.DSCP.Value
				p.tested = &value
			}
			if stamped != nil && ((f.v6 && stamped.IPv6) || (!f.v6 && stamped.IPv4)) {
				value := stamped.Value
				p.now = &value
			}
		}
		carried = carried || p.tested != nil || p.now != nil
		paths = append(paths, p)
	}
	for _, p := range paths {
		if sameDSCPValue(p.tested, p.now) {
			continue
		}
		change := &DSCPPathChange{Set: set.Name, Tested: p.tested, Now: p.now}
		if len(paths) == 2 && !(sameDSCPValue(paths[0].tested, paths[1].tested) && sameDSCPValue(paths[0].now, paths[1].now)) {
			change.Family = p.family
		}
		return change, nil
	}
	if path == nil || !carried {
		return nil, nil
	}
	var nowInterfaces []string
	if now != nil {
		nowInterfaces = now.Interfaces
	}
	if !sameInterfaces(path.Interfaces, nowInterfaces) {
		return &DSCPPathChange{Set: set.Name, Interfaces: true, TestedInterfaces: path.Interfaces, NowInterfaces: nowInterfaces}, nil
	}
	return nil, nil
}
