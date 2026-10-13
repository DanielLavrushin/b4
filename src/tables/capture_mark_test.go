package tables

import (
	"strings"
	"testing"
)

func TestCaptureChainLetsB4sOwnPacketsThroughFirst(t *testing.T) {
	for _, tc := range []struct {
		name string
		mark uint
		want string
	}{
		{"default mark", 0, "-m mark --mark 0x8000/0x8000 -j RETURN"},
		{"configured mark", 0x400000, "-m mark --mark 0x400000/0x400000 -j RETURN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager := dnsQueryTestManager(t, nil)
			manager.cfg.Queue.Mark = tc.mark
			m, err := manager.buildManifest()
			if err != nil {
				t.Fatalf("buildManifest: %v", err)
			}
			for _, ipt := range []string{backendIPTables, backendIP6Tables} {
				returns, firstQueue := -1, -1
				for i, r := range m.Rules {
					if r.IPT != ipt || r.Table != "mangle" || r.Chain != iptChainName {
						continue
					}
					spec := strings.Join(r.Spec, " ")
					if spec == tc.want {
						if returns >= 0 {
							t.Errorf("%s: %s holds the queue-mark RETURN twice", ipt, iptChainName)
						}
						returns = i
						if r.Action != "I" {
							t.Errorf("%s: the queue-mark RETURN uses action %q; it must be inserted, so a restore that adds only this rule still puts it ahead of the queue rules", ipt, r.Action)
						}
					}
					if !strings.Contains(spec, "NFQUEUE") {
						continue
					}
					if firstQueue < 0 {
						firstQueue = i
					}
					if r.Action != "A" {
						t.Errorf("%s: %s inserts the queue rule %q; a restore that adds only this rule would put it above the queue-mark RETURN, and the monitor checks that rules are present, not their order", ipt, iptChainName, spec)
					}
				}
				if returns < 0 {
					t.Fatalf("%s: %s has no %q, so packets b4 injects itself are queued a second time at POSTROUTING and clash in conntrack on kernels before 4.18; rules: %q", ipt, iptChainName, tc.want, manifestSpecs(m, ipt, "mangle", iptChainName))
				}
				if firstQueue >= 0 && returns > firstQueue {
					t.Errorf("%s: the queue-mark RETURN comes after a queue rule in %s", ipt, iptChainName)
				}
			}

			teardown, err := manager.buildTeardownManifest()
			if err != nil {
				t.Fatalf("buildTeardownManifest: %v", err)
			}
			for _, ipt := range []string{backendIPTables, backendIP6Tables} {
				found := false
				for _, s := range manifestSpecs(teardown, ipt, "mangle", iptChainName) {
					found = found || s == tc.want
				}
				if !found {
					t.Errorf("%s: the teardown manifest misses %q, so a clear would leave it behind", ipt, tc.want)
				}
			}
		})
	}
}
