package tproxy

import (
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func TestAPassOnDecisionConsultsTheSetsOnce(t *testing.T) {
	cases := []struct {
		name      string
		priority  bool
		nameFirst bool
		nameRuns  bool
		wantName  bool
	}{
		{name: "the set that lists the address holds it", priority: true, nameRuns: true},
		{name: "the set that lists the name takes it", nameFirst: true, nameRuns: true, wantName: true},
		{name: "the set that lists the name is not running", nameFirst: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hello := realClientHello(t, "api.telegram.org")
			byAddress := proxySetFor("by-address", nil, []string{"127.0.0.1/32"})
			byAddress.Targets.IPPriority = tc.priority
			byName := proxySetFor("by-name", []string{"api.telegram.org"}, nil)
			sets := []*config.SetConfig{byAddress, byName}
			if tc.nameFirst {
				sets = []*config.SetConfig{byName, byAddress}
			}
			upAddress := startMockSocks(t, 0, okReply)
			upName := startMockSocks(t, 0, okReply)
			peers := []*Listener{peerListener(t, upAddress.port(), byAddress)}
			if tc.nameRuns {
				peers = append(peers, peerListener(t, upName.port(), byName))
			}
			bridge, names := bridgeListener(t, &passOnBridge{consume: 4}, sets, peers...)

			proxyThrough(t, bridge, hello)
			want, other := upAddress, upName
			if tc.wantName {
				want, other = upName, upAddress
			}
			want.next(t)
			noSocksRequest(t, other, 100*time.Millisecond)
			if n := names.asked.Load(); n != 1 {
				t.Fatalf("one pass-on decision consulted the sets %d times, want once: each consult can see a different save", n)
			}
		})
	}
}
