package mtproto

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestMTProtoMaxConnections(t *testing.T) {
	cases := []struct {
		name string
		set  int
		want int
	}{
		{"legacy config omits the field (zero) -> default 2048", 0, defaultMaxConnections},
		{"explicit value is honored", 5000, 5000},
		{"explicit low value is honored", 64, 64},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.System.MTProto.MaxConnections = tc.set
			if got := mtprotoMaxConnections(cfg); got != tc.want {
				t.Fatalf("mtprotoMaxConnections(%d) = %d, want %d", tc.set, got, tc.want)
			}
		})
	}
}
