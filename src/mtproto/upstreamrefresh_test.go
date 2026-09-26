package mtproto

import (
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

type upstreamRefreshCalls struct {
	dcs []string
	cf  []string
}

func upstreamRefreshTestEnv(t *testing.T) *upstreamRefreshCalls {
	t.Helper()
	prevDCs, prevCF := upstreamRefreshDCs, upstreamRefreshCF
	t.Cleanup(func() { upstreamRefreshDCs, upstreamRefreshCF = prevDCs, prevCF })
	calls := &upstreamRefreshCalls{}
	upstreamRefreshDCs = func(_ bool, url string) error {
		calls.dcs = append(calls.dcs, url)
		return nil
	}
	upstreamRefreshCF = func(url string) (int, error) {
		calls.cf = append(calls.cf, url)
		return 5, nil
	}
	return calls
}

func TestUpstreamRefreshStaysOfflineWhileTelegramIsUnused(t *testing.T) {
	calls := upstreamRefreshTestEnv(t)
	cfg := config.NewConfig()
	var st upstreamRefreshState
	now := time.Unix(1_800_000_000, 0)

	if next := st.step(&cfg, now); next != 0 || len(calls.dcs)+len(calls.cf) != 0 {
		t.Fatalf("with every Telegram feature off nothing may be fetched, got dcs=%v cf=%v next=%v", calls.dcs, calls.cf, next)
	}

	set := config.NewSetConfig()
	set.Id, set.Enabled = "legacy", true
	set.Routing.Enabled, set.Routing.Mode = true, config.RoutingModeMTProtoWS
	cfg.Sets = append(cfg.Sets, &set)
	if !cfg.TelegramInUse() {
		t.Fatal("an enabled mtproto-ws set uses the bridge")
	}
	set.Enabled = false
	if cfg.TelegramInUse() {
		t.Error("a disabled mtproto-ws set must not count")
	}
}

func TestUpstreamRefreshFetchesOnceWhenTelegramIsTurnedOn(t *testing.T) {
	calls := upstreamRefreshTestEnv(t)
	cfg := config.NewConfig()
	var st upstreamRefreshState
	now := time.Unix(1_800_000_000, 0)
	st.step(&cfg, now)

	cfg.System.MTProto.Bridge.Enabled = true
	next := st.step(&cfg, now)
	if len(calls.dcs) != 1 || len(calls.cf) != 1 {
		t.Fatalf("turning the bridge on must fetch both lists once, got dcs=%v cf=%v", calls.dcs, calls.cf)
	}
	if next != cfProxyRefreshInt {
		t.Errorf("next wake %v, want the hourly refresh", next)
	}

	if st.step(&cfg, now.Add(time.Minute)); len(calls.dcs) != 1 || len(calls.cf) != 1 {
		t.Errorf("an unrelated config push must not fetch again, got dcs=%v cf=%v", calls.dcs, calls.cf)
	}

	st.step(&cfg, now.Add(cfProxyRefreshInt))
	if len(calls.dcs) != 1 || len(calls.cf) != 2 {
		t.Errorf("after an hour only the CF list is refreshed, got dcs=%v cf=%v", calls.dcs, calls.cf)
	}

	cfg.System.MTProto.DCFallbackURL = "https://mirror.example/getProxyConfig"
	cfg.System.MTProto.CFProxyURL = "https://mirror.example/cf.txt"
	st.step(&cfg, now.Add(cfProxyRefreshInt+time.Minute))
	if len(calls.dcs) != 2 || len(calls.cf) != 3 || calls.cf[2] != "https://mirror.example/cf.txt" {
		t.Errorf("a new source URL must be fetched at once, got dcs=%v cf=%v", calls.dcs, calls.cf)
	}

	cfg.System.MTProto.CFProxyEnabled = false
	if next := st.step(&cfg, now.Add(2*cfProxyRefreshInt)); next != 0 || len(calls.cf) != 3 {
		t.Errorf("with the CF fallback off the list is not refreshed, got cf=%v next=%v", calls.cf, next)
	}

	cfg.System.MTProto.Bridge.Enabled = false
	st.step(&cfg, now.Add(3*cfProxyRefreshInt))
	cfg.System.MTProto.Enabled = true
	cfg.System.MTProto.CFProxyEnabled = true
	st.step(&cfg, now.Add(3*cfProxyRefreshInt))
	if len(calls.dcs) != 3 || len(calls.cf) != 4 {
		t.Errorf("turning Telegram off and on again must fetch again, got dcs=%v cf=%v", calls.dcs, calls.cf)
	}
}
