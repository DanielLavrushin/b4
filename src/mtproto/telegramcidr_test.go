package mtproto

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func telegramCIDRTestEnv(t *testing.T, handlers ...http.HandlerFunc) *config.Config {
	t.Helper()
	prevSources, prevClient, prevOnChange := telegramCIDRSources, telegramCIDRHTTPClient, telegramCIDROnChange
	prevErr, prevAttempt := telegramCIDRLastErr, telegramCIDRLastAttempt
	prevDone, prevGeo := telegramCIDRLocalDone, telegramCIDRLocalGeoIP
	t.Cleanup(func() {
		telegramCIDRSources, telegramCIDRHTTPClient, telegramCIDROnChange = prevSources, prevClient, prevOnChange
		telegramCIDRLastErr, telegramCIDRLastAttempt = prevErr, prevAttempt
		telegramCIDRLocalDone, telegramCIDRLocalGeoIP = prevDone, prevGeo
		config.SetTelegramCIDRs(config.TelegramCIDRSourceBuiltin, time.Time{}, nil)
	})
	config.SetTelegramCIDRs(config.TelegramCIDRSourceBuiltin, time.Time{}, nil)
	telegramCIDRLastErr, telegramCIDRLastAttempt = "", time.Time{}
	telegramCIDRLocalDone, telegramCIDRLocalGeoIP = false, ""
	telegramCIDROnChange = nil
	telegramCIDRHTTPClient = func() *http.Client { return &http.Client{Timeout: 5 * time.Second} }

	names := []string{config.TelegramCIDRSourceTelegram, config.TelegramCIDRSourceMirror}
	telegramCIDRSources = nil
	for i, h := range handlers {
		srv := httptest.NewServer(h)
		t.Cleanup(srv.Close)
		telegramCIDRSources = append(telegramCIDRSources, telegramCIDRSource{url: srv.URL + "/cidr.txt", source: names[i]})
	}

	cfg := config.NewConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")
	return &cfg
}

func serveText(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }
}

func serveStatus(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
}

func TestFetchTelegramCIDRsFallsBackToTheMirrorAndSavesACopy(t *testing.T) {
	cfg := telegramCIDRTestEnv(t, serveStatus(http.StatusServiceUnavailable), serveText("91.108.56.0/22\n# comment\n\n203.0.113.0/24\n0.0.0.0/0\n"))

	changed, err := FetchTelegramCIDRs(context.Background(), cfg)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !changed {
		t.Error("a new range must report a change")
	}
	cur := config.CurrentTelegramCIDRs()
	if cur.Source != config.TelegramCIDRSourceMirror {
		t.Errorf("source %q, want the mirror", cur.Source)
	}
	if strings.Join(cur.Primary, ",") != "91.108.56.0/22,203.0.113.0/24" {
		t.Errorf("primary list %v, the /0 must be refused", cur.Primary)
	}
	st := TelegramCIDRState()
	if st.LastError != "" || st.LastAttempt == "" {
		t.Errorf("status after a success: %+v", st)
	}

	data, err := os.ReadFile(filepath.Join(filepath.Dir(cfg.ConfigPath), TelegramCIDRCacheFile))
	if err != nil {
		t.Fatalf("no saved copy: %v", err)
	}
	if !strings.Contains(string(data), "203.0.113.0/24") {
		t.Errorf("saved copy lacks the downloaded range:\n%s", data)
	}

	config.SetTelegramCIDRs(config.TelegramCIDRSourceBuiltin, time.Time{}, nil)
	LoadLocalTelegramCIDRs(cfg)
	cur = config.CurrentTelegramCIDRs()
	if cur.Source != config.TelegramCIDRSourceCache || strings.Join(cur.Primary, ",") != "91.108.56.0/22,203.0.113.0/24" {
		t.Errorf("reload from the saved copy got %s %v", cur.Source, cur.Primary)
	}
	if cur.UpdatedAt.IsZero() {
		t.Error("the saved copy must keep its download time")
	}
}

func TestFetchTelegramCIDRsKeepsTheListWhenEverySourceFails(t *testing.T) {
	cfg := telegramCIDRTestEnv(t, serveStatus(http.StatusNotFound), serveText("garbage\nnot a prefix\n"))
	before := config.CurrentTelegramCIDRs()

	changed, err := FetchTelegramCIDRs(context.Background(), cfg)
	if err == nil || changed {
		t.Fatalf("expected a failure without a change, got changed=%v err=%v", changed, err)
	}
	if config.CurrentTelegramCIDRs() != before {
		t.Error("a failed download replaced the list in use")
	}
	st := TelegramCIDRState()
	if st.LastError == "" || !strings.Contains(st.LastError, "status 404") {
		t.Errorf("the status must carry the error, got %q", st.LastError)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(cfg.ConfigPath), TelegramCIDRCacheFile)); !os.IsNotExist(err) {
		t.Errorf("a failed download must not write the saved copy: %v", err)
	}
}

func TestLoadLocalTelegramCIDRsFallsBackToTheBuiltinList(t *testing.T) {
	cfg := telegramCIDRTestEnv(t)
	cfg.System.Geo.GeoIpPath = filepath.Join(t.TempDir(), "missing.dat")
	LoadLocalTelegramCIDRs(cfg)
	cur := config.CurrentTelegramCIDRs()
	if cur.Source != config.TelegramCIDRSourceBuiltin || len(cur.All) != len(config.TelegramBuiltinCIDRs) {
		t.Errorf("got %s with %d ranges, want the builtin list", cur.Source, len(cur.All))
	}
}

func TestRefreshTelegramCIDRsNowReportsAChange(t *testing.T) {
	cfg := telegramCIDRTestEnv(t, serveText("198.51.100.0/24\n"))
	calls := 0
	telegramCIDROnChange = func() { calls++ }
	if err := RefreshTelegramCIDRsNow(context.Background(), cfg); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if calls != 1 {
		t.Errorf("onChange ran %d times, want once", calls)
	}
	if err := RefreshTelegramCIDRsNow(context.Background(), cfg); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if calls != 1 {
		t.Errorf("an unchanged list must not resync the firewall, onChange ran %d times", calls)
	}
}

func TestTelegramCIDRRetryDelayBacksOff(t *testing.T) {
	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute}
	for i, w := range want {
		if got := telegramCIDRRetryDelay(i + 1); got != w {
			t.Errorf("failure %d: delay %v, want %v", i+1, got, w)
		}
	}
	if got := telegramCIDRRetryDelay(50); got != telegramCIDRRetryMax {
		t.Errorf("the delay must stop at %v, got %v", telegramCIDRRetryMax, got)
	}
}

func TestRefreshReloadsLocalSourcesOnlyWhenTheyChanged(t *testing.T) {
	cfg := telegramCIDRTestEnv(t, serveStatus(http.StatusNotFound))
	if !telegramCIDRLocalStale(cfg) {
		t.Fatal("nothing loaded yet, the local sources must be read")
	}
	_ = RefreshTelegramCIDRsNow(context.Background(), cfg)
	if telegramCIDRLocalStale(cfg) {
		t.Error("the local sources were just read, a failed download must not re-read them")
	}
	cfg.System.Geo.GeoIpPath = "/elsewhere/geoip.dat"
	if !telegramCIDRLocalStale(cfg) {
		t.Error("a new GeoIP path must re-read the local sources")
	}
}

func TestRelayRoutesSkipDPISetsAndTelegramRoutesDoNot(t *testing.T) {
	base := uint(config.SelfDialMark)
	relay := uint(config.SelfDialRelayMark)
	cases := []struct {
		name string
		plan transportPlan
		want uint
	}{
		{"cloudflare proxied domain", transportPlan{kind: transportWS, cfBase: "example.co.uk"}, relay},
		{"custom websocket domain", transportPlan{kind: transportWS, cfBase: "relay.example.com"}, relay},
		{"worker", transportPlan{kind: transportWS, isWorker: true}, relay},
		{"telegram websocket edge", transportPlan{kind: transportWS, native: true}, base},
		{"fronted telegram edge", transportPlan{kind: transportWS, native: true, frontSNI: "sprinthost.ru"}, base},
		{"direct tcp to a data centre", transportPlan{kind: transportTCP, addr: "149.154.167.51:443"}, base},
	}
	for _, c := range cases {
		if got := planDialMark(c.plan, base); got != c.want {
			t.Errorf("%s: mark 0x%x, want 0x%x", c.name, got, c.want)
		}
	}
	if got := planDialMark(transportPlan{isWorker: true}, 0); got != 0 {
		t.Errorf("a test build without SO_MARK must stay unmarked, got 0x%x", got)
	}
	if config.SelfDialRelayMark&config.SelfDialMark == 0 {
		t.Error("the relay mark must keep the self-dial bit so the routing chains still let it pass")
	}
	if config.SelfDialNoDPIBit&(config.PerSetRouteMarkBits|0x8000|0x10000|config.SelfDialMark) != 0 {
		t.Error("the no-DPI bit overlaps a mark bit already in use")
	}
}
