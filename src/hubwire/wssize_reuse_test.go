package hubwire

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestWSReuseNeverCrossesHub(t *testing.T) {
	set := sampleSet()
	set.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 1, Scale: 1}
	plain, _, err := Build(&set, BuildOptions{B4Version: "1.91.0"})
	if err != nil {
		t.Fatal(err)
	}
	set.TCP.WSSizeReuse = config.WSSizeReuseConfig{Enabled: true, MaxAgeSeconds: 45}
	env, _, err := Build(&set, BuildOptions{B4Version: "1.91.0"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := lookupPath(env.Set, "tcp.wssize_reuse"); ok {
		t.Fatal("local policy published")
	}
	if env.Fingerprint != plain.Fingerprint {
		t.Fatal("local policy changed shared fingerprint")
	}
	tcp := env.Set["tcp"].(map[string]interface{})
	tcp["wssize_reuse"] = map[string]interface{}{"enabled": true, "max_age_seconds": 45}
	imported, err := Open(env, OpenOptions{B4Version: "1.91.0"})
	if err != nil {
		t.Fatal(err)
	}
	if imported.Set.TCP.WSSizeReuse.Enabled || imported.Set.TCP.WSSizeReuse.MaxAgeSeconds != config.DefaultWSReuseMaxAgeSeconds {
		t.Fatal("crafted import enabled local reuse")
	}
	if !imported.Set.TCP.WSSize.Enabled {
		t.Fatal("ordinary WSSIZE was removed")
	}
}
