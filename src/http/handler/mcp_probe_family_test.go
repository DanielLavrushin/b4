package handler

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hub/hubtest"
)

func onlyIPv6Names(t *testing.T) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			end := 12
			for end < n && buf[end] != 0 {
				end += int(buf[end]) + 1
			}
			if end+5 > n {
				continue
			}
			reply := append([]byte(nil), buf[:end+5]...)
			reply[2], reply[3] = 0x81, 0x80
			copy(reply[6:12], make([]byte, 6))
			if reply[end+2] == 28 {
				reply[7] = 1
				reply = append(reply, 0xC0, 0x0C, 0, 28, 0, 1, 0, 0, 0, 30, 0, 16)
				reply = append(reply, net.ParseIP("2001:db8::9")...)
			}
			_, _ = pc.WriteTo(reply, from)
		}
	}()
	previous := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "udp", pc.LocalAddr().String())
		},
	}
	t.Cleanup(func() { net.DefaultResolver = previous })
}

func TestBothArmsOfTheProbeStayInTheFamilyB4Handles(t *testing.T) {
	onlyIPv6Names(t)
	t.Cleanup(func() { config.SetRunningEngineTUN(false) })
	for _, tc := range []struct {
		name string
		ipv6 bool
		tun  bool
	}{
		{"IPv6 support off", false, false},
		{"TUN with IPv6 support on", true, true},
	} {
		config.SetRunningEngineTUN(tc.tun)
		cfg := config.NewConfig()
		cfg.Queue.IPv4Enabled, cfg.Queue.IPv6Enabled = true, tc.ipv6

		through, baseline := probeDomainBothWays(context.Background(), &cfg, "six.example", 3*time.Second)
		for _, r := range []mcpProbeResult{through, baseline} {
			if !mcpProbeUnfetched(&r) || !strings.Contains(r.Error, "six.example has only IPv6 addresses") {
				t.Errorf("%s, %s: nothing is fetched over a family b4 does not handle, got %+v", tc.name, r.Mode, r)
			}
		}
		if note := mcpProbeVerdictNote("six.example", &through, &baseline); !strings.Contains(note, "was not fetched") || strings.Contains(note, "censored") {
			t.Errorf("%s: the note draws no conclusion about the site: %q", tc.name, note)
		}
	}
}

func TestTheHubTestSaysWhyNothingWasFetched(t *testing.T) {
	onlyIPv6Names(t)
	env := newHubEnv(t)
	set := hubStrategySet("Over IPv6", "six.example")
	cs, _ := hubtest.CatalogueSet(t, "six-1", 1, &set, nil)
	env.publish(t, cs)

	rec := postJSON(t, env.mux, "/api/hub/sets/six-1/test", HubTestRequest{})

	expectCode(t, rec, http.StatusBadRequest, "not_fetched")
	if !strings.Contains(rec.Body.String(), "six.example has only IPv6 addresses") {
		t.Errorf("the reason replaces a verdict about the site: %s", rec.Body.String())
	}
	var body APIError
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	expectTranslatedField(t, body.Fields, "unhandled_family_ipv6", map[string]any{"host": "six.example"})
}
