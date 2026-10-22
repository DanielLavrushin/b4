package sock

import "testing"

func TestUDPDecoysTreatAZeroTTLAsTheTCPFakeDefault(t *testing.T) {
	for _, c := range []struct {
		configured uint8
		want       uint8
	}{{0, 5}, {1, 1}, {7, 7}, {64, 64}, {255, 255}} {
		v4, ok := BuildFakeUDPFromOriginalV4(buildMinimalIPv4UDPPacket(20), 50, c.configured, nil)
		if !ok {
			t.Fatal("IPv4 decoy not built")
		}
		if v4[8] != c.want {
			t.Errorf("faking.ttl %d: IPv4 decoy TTL %d, want %d", c.configured, v4[8], c.want)
		}
		v6, ok := BuildFakeUDPFromOriginalV6(buildMinimalIPv6UDPPacket(20), 50, c.configured, nil)
		if !ok {
			t.Fatal("IPv6 decoy not built")
		}
		if v6[7] != c.want {
			t.Errorf("faking.ttl %d: IPv6 decoy hop limit %d, want %d", c.configured, v6[7], c.want)
		}
	}
}
