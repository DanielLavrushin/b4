package discovery

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestDiscoveryNeverWarmsClientTuples(t *testing.T) {
	set := config.NewSetConfig()
	set.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 1, Scale: 1}
	set.TCP.WSSizeReuse = config.WSSizeReuseConfig{Enabled: true, MaxAgeSeconds: 45}
	strategy := SetRunStrategy(&set)
	if strategy.TCP.WSSizeReuse.Enabled || !set.TCP.WSSizeReuse.Enabled {
		t.Fatal("discovery policy escaped or live set changed")
	}
	ds := setRunFixture(t, "a.example")
	ds.Domain = "a.example"
	ds.cfg = &config.Config{ConfigPath: t.TempDir() + "/b4.json"}
	preset := ConfigPreset{Name: "reuse", Config: set}
	for _, c := range []*config.Config{ds.buildTestConfig(preset), ds.buildTestConfigMulti(preset)} {
		if c.Sets[0].TCP.WSSizeReuse.Enabled {
			t.Fatal("probe warms client tuples")
		}
	}
	c := config.NewConfig()
	c.Sets = []*config.SetConfig{&set}
	pool := discoveryPoolConfig(&c, 540, 1, 0x123, 0x124)
	if !pool.Queue.IsDiscovery || pool.Sets[0].TCP.WSSizeReuse.Enabled {
		t.Fatal("discovery pool retained local policy")
	}
}
