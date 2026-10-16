package tables

import (
	"slices"
	"strings"
	"testing"
)

func TestTheFallbackMarkRuleMatchesOnlyTheOriginalDirection(t *testing.T) {
	stubBinaries(t, backendIPTables, backendIP6Tables)
	for _, c := range []struct {
		name string
		be   routeBackend
		want []string
	}{
		{"nftables", &routeNftBackend{}, []string{
			"nft add rule inet b4_route b4r_fb_pre iifname \"br0\" meta mark & 0x27fff == 0x0 ip daddr @b4r_fb_v4 ct direction original meta mark set meta mark & 0xfffd8000 or 0x5179",
			"nft add rule inet b4_route b4r_fb_pre iifname \"br0\" meta mark & 0x27fff == 0x0 ip6 daddr @b4r_fb_v6 ct direction original meta mark set meta mark & 0xfffd8000 or 0x5179",
		}},
		{"iptables", &routeIptBackend{}, []string{
			"iptables -w -t mangle -A b4r_fb_pre -i br0 -m mark --mark 0x0/0x27fff -m set --match-set b4r_fb_v4 dst -m conntrack --ctdir ORIGINAL -j MARK --set-xmark 0x5179/0x27fff",
			"ip6tables -w -t mangle -A b4r_fb_pre -i br0 -m mark --mark 0x0/0x27fff -m set --match-set b4r_fb_v6 dst -m conntrack --ctdir ORIGINAL -j MARK --set-xmark 0x5179/0x27fff",
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			var cmds []string
			prev := runLogged
			t.Cleanup(func() { runLogged = prev })
			runLogged = func(op string, args ...string) bool {
				cmds = append(cmds, strings.Join(args, " "))
				return true
			}
			c.be.addMarkFallbackRule("b4r_fb_pre", false, "b4r_fb_v4", 0x5179, "br0")
			c.be.addMarkFallbackRule("b4r_fb_pre", true, "b4r_fb_v6", 0x5179, "br0")
			if !slices.Equal(cmds, c.want) {
				t.Errorf("the fallback marks every packet to the set's addresses once the connection mark is gone; without the original-direction match it marks the replies too and sends them back out the egress:\ngot:\n%s\nwant:\n%s",
					strings.Join(cmds, "\n"), strings.Join(c.want, "\n"))
			}
		})
	}
}
