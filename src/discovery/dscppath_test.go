package discovery

import (
	"errors"
	"fmt"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

type fakeFirewall struct {
	setFamilies    string
	global4        *int
	global6        *int
	interfaces     []string
	pending        bool
	asked          *config.DSCPProbe
	askedForNoSets bool
}

func (f *fakeFirewall) stamp(_ *config.Config, dscp *config.DSCPProbe) (*config.DSCPProbe, *config.DSCPPath, error) {
	f.asked, f.askedForNoSets = dscp, dscp == nil
	if f.pending {
		return nil, nil, ErrDSCPRulesPending
	}
	path := &config.DSCPPath{IPv4: f.global4, IPv6: f.global6}
	var stamped *config.DSCPProbe
	if dscp != nil {
		taken := config.DSCPProbe{Value: dscp.Value, IPv4: dscp.IPv4 && f.setFamilies != "6", IPv6: dscp.IPv6 && f.setFamilies != "4"}
		if f.setFamilies == "none" {
			taken.IPv4, taken.IPv6 = false, false
		}
		if taken.IPv4 || taken.IPv6 {
			stamped = &taken
			value := taken.Value
			if taken.IPv4 {
				path.IPv4 = &value
			}
			if taken.IPv6 {
				path.IPv6 = &value
			}
		}
	}
	if path.IPv4 != nil || path.IPv6 != nil {
		path.Interfaces = f.interfaces
	}
	return stamped, path, nil
}

func dscpValue(v int) *int {
	return &v
}

func dscpPathSet(name string, value int, ipVersion string) *config.SetConfig {
	set := config.NewSetConfig()
	set.Name = name
	set.Targets.SNIDomains = []string{"youtube.com"}
	set.Targets.IPVersion = ipVersion
	if value >= 0 {
		set.DSCP = config.SetDSCPConfig{Enabled: true, Value: value}
	}
	return &set
}

func dscpChangeText(c *DSCPPathChange) string {
	if c == nil {
		return "none"
	}
	value := func(v *int) string {
		if v == nil {
			return "-"
		}
		return fmt.Sprint(*v)
	}
	if c.Interfaces {
		return fmt.Sprintf("interfaces %v -> %v", c.TestedInterfaces, c.NowInterfaces)
	}
	return fmt.Sprintf("%s %s -> %s", c.Family, value(c.Tested), value(c.Now))
}

func TestDSCPPathChangedComparesThePathTheRunsProbesTook(t *testing.T) {
	cfg := config.NewConfig()
	for _, tc := range []struct {
		name     string
		setValue int
		firewall fakeFirewall
		path     *config.DSCPPath
		want     string
	}{
		{"the set's value reaches both families, as in the run", 25, fakeFirewall{interfaces: []string{"wan0"}},
			&config.DSCPPath{IPv4: dscpValue(25), IPv6: dscpValue(25), Interfaces: []string{"wan0"}}, "none"},
		{"the set's value changed to 7", 7, fakeFirewall{interfaces: []string{"wan0"}},
			&config.DSCPPath{IPv4: dscpValue(25), IPv6: dscpValue(25), Interfaces: []string{"wan0"}}, " 25 -> 7"},
		{"the run's IPv6 probes carried the global 30, the set's IPv6 traffic now carries its own 25", 25, fakeFirewall{global4: dscpValue(30), global6: dscpValue(30), interfaces: []string{"wan0"}},
			&config.DSCPPath{IPv4: dscpValue(25), IPv6: dscpValue(30), Interfaces: []string{"wan0"}}, "IPv6 30 -> 25"},
		{"the global value moved from 30 to 31 for a set without its own", -1, fakeFirewall{global4: dscpValue(31), global6: dscpValue(31)},
			&config.DSCPPath{IPv4: dscpValue(30), IPv6: dscpValue(30)}, " 30 -> 31"},
		{"the global value was switched on after a run without any value", -1, fakeFirewall{global4: dscpValue(30), global6: dscpValue(30), interfaces: []string{"wan0"}},
			&config.DSCPPath{}, " - -> 30"},
		{"the values match but b4 writes them on another interface now", 25, fakeFirewall{interfaces: []string{"wan1"}},
			&config.DSCPPath{IPv4: dscpValue(25), IPv6: dscpValue(25), Interfaces: []string{"wan0"}}, "interfaces [wan0] -> [wan1]"},
		{"the same interfaces in another order", 25, fakeFirewall{interfaces: []string{"wan1", "wan0"}},
			&config.DSCPPath{IPv4: dscpValue(25), IPv6: dscpValue(25), Interfaces: []string{"wan0", "wan1"}}, "none"},
		{"no value on either side, the interface list does not matter", -1, fakeFirewall{},
			&config.DSCPPath{Interfaces: []string{"wan0"}}, "none"},
		{"a record from before b4 kept the path compares the set's own value only", 25, fakeFirewall{global6: dscpValue(30), interfaces: []string{"wan1"}, setFamilies: "4"},
			nil, "IPv6 25 -> -"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := dscpPathSet("video", tc.setValue, "")
			tested := dscpPathSet("winner", 25, "")
			if tc.path == nil {
				tested.DSCP = config.SetDSCPConfig{Enabled: true, Value: 25}
			}
			change, err := DSCPPathChanged(&cfg, set, tested, tc.path, tc.firewall.stamp)
			if err != nil || dscpChangeText(change) != tc.want {
				t.Errorf("want %q, got %q, %v", tc.want, dscpChangeText(change), err)
			}
		})
	}
}

func TestDSCPPathChangedComparesOnlyTheFamiliesTheRunTested(t *testing.T) {
	cfg := config.NewConfig()
	firewall := fakeFirewall{global6: dscpValue(30)}
	path := &config.DSCPPath{IPv4: dscpValue(25)}
	set := dscpPathSet("video", 25, "")
	tested := dscpPathSet("winner", 25, "4")
	change, err := DSCPPathChanged(&cfg, set, tested, path, firewall.stamp)
	if err != nil || change != nil {
		t.Errorf("the run tested IPv4 only, so the IPv6 traffic's global value does not matter, got %q, %v", dscpChangeText(change), err)
	}
	if firewall.asked == nil || firewall.asked.IPv6 || !firewall.asked.IPv4 {
		t.Errorf("the firewall is asked about the families the run tested, got %+v", firewall.asked)
	}
}

func TestDSCPPathChangedPassesThePendingRulesOn(t *testing.T) {
	cfg := config.NewConfig()
	firewall := fakeFirewall{pending: true}
	if _, err := DSCPPathChanged(&cfg, dscpPathSet("video", -1, ""), dscpPathSet("winner", -1, ""), &config.DSCPPath{}, firewall.stamp); !errors.Is(err, ErrDSCPRulesPending) {
		t.Errorf("while b4 is still applying its DSCP rules the path is unknown, even for a set without its own value, got %v", err)
	}
	if !firewall.askedForNoSets {
		t.Error("a set without its own value is still checked against the rules b4 is applying")
	}
}
