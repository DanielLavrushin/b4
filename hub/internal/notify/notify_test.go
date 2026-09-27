package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	testToken  = "123456789:AAAbbbCCCdddEEEfffGGGhhhIIIjjjKKKl"
	testChat   = "-1001234567890"
	hookSecret = "s3cret-value"
)

var hubSecret = []byte("0123456789abcdef0123456789abcdef")

type captured struct {
	path   string
	header http.Header
	body   []byte
}

type endpoint struct {
	mu       sync.Mutex
	srv      *httptest.Server
	requests []captured
	status   int
	reply    string
	header   map[string]string
}

func newEndpoint(t *testing.T, status int, reply string) *endpoint {
	t.Helper()
	e := &endpoint{status: status, reply: reply}
	e.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		e.mu.Lock()
		e.requests = append(e.requests, captured{path: r.URL.Path, header: r.Header.Clone(), body: body})
		status, reply, header := e.status, e.reply, e.header
		e.mu.Unlock()
		for k, v := range header {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(e.srv.Close)
	return e
}

func (e *endpoint) respond(status int, reply string, header map[string]string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.status, e.reply, e.header = status, reply, header
}

func (e *endpoint) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.requests)
}

func (e *endpoint) last(t *testing.T) captured {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.requests) == 0 {
		t.Fatal("no request reached the endpoint")
	}
	return e.requests[len(e.requests)-1]
}

type fixture struct {
	svc   *Service
	store *store.Store
	clock time.Time
	env   map[string]string
	tg    *endpoint
	hook  *endpoint
	seq   int
}

const telegramOK = `{"ok":true,"result":{"message_id":1}}`

func newFixture(t *testing.T) *fixture {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	f := &fixture{store: st, clock: time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC), env: map[string]string{}}
	f.tg = newEndpoint(t, http.StatusOK, telegramOK)
	f.hook = newEndpoint(t, http.StatusNoContent, "")
	f.svc = &Service{
		Store:        st,
		Secret:       hubSecret,
		Env:          func(name string) string { return f.env[name] },
		PublicURL:    "https://hub.example/",
		TelegramBase: f.tg.srv.URL,
		Now:          func() time.Time { return f.clock },
	}
	return f
}

func ptr(s string) *string {
	return &s
}

func (f *fixture) update(events ...string) Update {
	return Update{
		TelegramEnabled: true,
		TelegramChatID:  testChat,
		TelegramToken:   ptr(testToken),
		WebhookEnabled:  true,
		WebhookURL:      ptr(f.hook.srv.URL + "/hook"),
		WebhookSecret:   ptr(hookSecret),
		Events:          events,
		DigestSeconds:   60,
		Language:        LanguageEN,
	}
}

func (f *fixture) save(t *testing.T, u Update) Config {
	t.Helper()
	cfg, err := f.svc.Save(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func (f *fixture) share(t *testing.T, uploader, status string) (string, int64) {
	t.Helper()
	f.seq++
	id := "01ARZ3NDEKTSV4RRFFQ69G5F" + strconv.Itoa(10+f.seq)
	v := &store.Version{
		FP:              "fp" + strconv.Itoa(f.seq),
		TargetsKey:      "tk" + strconv.Itoa(f.seq),
		Title:           "Set " + strconv.Itoa(f.seq),
		Projection:      map[string]interface{}{},
		Status:          status,
		UploaderHMAC:    uploader,
		ASNObserved:     "64500",
		CountryObserved: "RU",
		CreatedAt:       f.clock,
		UpdatedAt:       f.clock,
	}
	if err := f.store.CreateSet(context.Background(), store.Set{ID: id, AuthorHMAC: uploader, CreatedAt: f.clock, UpdatedAt: f.clock}, v); err != nil {
		t.Fatal(err)
	}
	return id, v.RowID
}

func (f *fixture) report(t *testing.T, setID, key string) {
	t.Helper()
	if err := f.store.InsertReport(context.Background(), store.Report{SetID: setID, Version: 1, KeyHMAC: key, Reason: "breaks\nthe site", ReceivedAt: f.clock}); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) tagTest(t *testing.T, key string) {
	t.Helper()
	ctx := context.Background()
	if _, _, err := f.store.TouchKey(ctx, key, f.clock); err != nil {
		t.Fatal(err)
	}
	err := f.store.Update(ctx, func(tx *store.Tx) error {
		_, err := tx.SetKeyProfile(ctx, key, store.KeyProfile{Tag: store.TagTest}, f.clock)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) round() {
	f.svc.round(context.Background())
}

func (f *fixture) cursors(t *testing.T, channel string) map[string]int64 {
	t.Helper()
	got, err := f.store.NotifyCursors(context.Background(), channel)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func (f *fixture) status(t *testing.T) Status {
	t.Helper()
	st, err := f.svc.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func telegramBody(t *testing.T, c captured) telegramMessage {
	t.Helper()
	var m telegramMessage
	if err := json.Unmarshal(c.body, &m); err != nil {
		t.Fatalf("telegram body: %v %s", err, c.body)
	}
	return m
}

func verifySignature(t *testing.T, c captured, secret string) WebhookPayload {
	t.Helper()
	ts := c.header.Get(HeaderTimestamp)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "." + string(c.body)))
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if got := c.header.Get(HeaderSignature); got != want {
		t.Fatalf("signature %q, want %q", got, want)
	}
	if c.header.Get("User-Agent") != "b4hub" {
		t.Errorf("user agent %q", c.header.Get("User-Agent"))
	}
	var p WebhookPayload
	if err := json.Unmarshal(c.body, &p); err != nil {
		t.Fatalf("webhook body: %v %s", err, c.body)
	}
	return p
}

func codeOf(err error) string {
	var v *ValidationError
	if errors.As(err, &v) {
		return v.Code
	}
	return ""
}

func TestSealedConfigKeepsSecretsOutOfStorage(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	hookURL := f.hook.srv.URL + "/hook"
	f.save(t, f.update())
	raw, err := f.store.Meta(ctx, metaConfig)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{testToken, hookURL, hookSecret, "AAAbbbCCC"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("stored config carries %q in plain text: %s", secret, raw)
		}
	}
	if strings.Count(raw, sealPrefix) != 3 || !strings.Contains(raw, testChat) {
		t.Fatalf("token, URL and secret must be sealed, the chat id kept: %s", raw)
	}
	cfg, src, err := f.svc.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Telegram.Token != testToken || cfg.Webhook.URL != hookURL || cfg.Webhook.Secret != hookSecret || src != (Sources{}) {
		t.Fatalf("load must unseal what was saved: %+v %+v", cfg, src)
	}
	view, _ := json.Marshal(cfg)
	if strings.Contains(string(view), testToken) || strings.Contains(string(view), hookSecret) || strings.Contains(string(view), hookURL) {
		t.Errorf("the JSON form of Config must not carry secrets: %s", view)
	}

	other := &Service{Store: f.store, Secret: []byte("another hub secret, another key"), Env: f.svc.Env}
	cfg, _, err = other.Load(ctx)
	if err != nil || cfg.Telegram.Token != "" || cfg.Webhook.Secret != "" || cfg.Telegram.ChatID != testChat {
		t.Fatalf("a foreign secret must leave sealed values unset: %+v %v", cfg, err)
	}

	sealed, err := seal(hubSecret, fieldTelegramToken, "value")
	if err != nil {
		t.Fatal(err)
	}
	if plain, err := unseal(hubSecret, fieldTelegramToken, sealed); err != nil || plain != "value" {
		t.Fatalf("round trip: %q %v", plain, err)
	}
	if _, err := unseal(hubSecret, fieldWebhookSecret, sealed); err == nil {
		t.Error("a value sealed for one field must not open as another")
	}
	again, _ := seal(hubSecret, fieldTelegramToken, "value")
	if again == sealed {
		t.Error("every seal must use a fresh nonce")
	}
	if _, err := seal(nil, fieldTelegramToken, "value"); err == nil {
		t.Error("sealing without the hub secret must fail")
	}
}

func TestEnvironmentOverridesStoredValues(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.save(t, Update{TelegramChatID: "-1001", TelegramToken: ptr(testToken)})

	envToken := "987654321:ZZZyyyXXXwwwVVVuuuTTTsssRRRqqqPPPo"
	f.env[EnvTelegramToken] = envToken
	f.env[EnvTelegramChat] = "@b4hub_alerts"
	cfg, src, err := f.svc.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Telegram.Token != envToken || cfg.Telegram.ChatID != "@b4hub_alerts" {
		t.Fatalf("the environment must win: %+v", cfg.Telegram)
	}
	if !src.TelegramToken || !src.TelegramChat || src.WebhookURL || src.WebhookSecret {
		t.Fatalf("sources must name the environment values: %+v", src)
	}

	f.save(t, Update{TelegramEnabled: true, TelegramChatID: "-2002", TelegramToken: ptr("111:ignoredignoredignoredignored"), ClearTelegramToken: true})
	delete(f.env, EnvTelegramToken)
	delete(f.env, EnvTelegramChat)
	cfg, src, _ = f.svc.Load(ctx)
	if cfg.Telegram.Token != testToken || cfg.Telegram.ChatID != "-1001" || src != (Sources{}) {
		t.Fatalf("console edits of environment values must be ignored: %+v %+v", cfg.Telegram, src)
	}

	f.env[EnvWebhookURL] = "http://hooks.example.com/b4hub"
	if _, err := f.svc.Save(ctx, Update{WebhookEnabled: true}); codeOf(err) != CodeBadURL || !strings.Contains(err.Error(), EnvWebhookURL) {
		t.Fatalf("an unusable URL from the environment must be named, got %v", err)
	}
	f.env[EnvWebhookURL] = "https://hooks.example.com/b4hub"
	f.env[EnvWebhookSecret] = "from-env"
	cfg = f.save(t, Update{WebhookEnabled: true})
	if cfg.Webhook.URL != "https://hooks.example.com/b4hub" || cfg.Webhook.Secret != "from-env" || !cfg.Webhook.Enabled {
		t.Fatalf("a webhook configured only from the environment can be enabled: %+v", cfg.Webhook)
	}
	raw, _ := f.store.Meta(ctx, metaConfig)
	if strings.Contains(raw, "hooks.example.com") || strings.Contains(raw, "from-env") {
		t.Fatalf("environment values must not be copied into storage: %s", raw)
	}
}

func TestSaveValidationCodes(t *testing.T) {
	f := newFixture(t)
	long := "https://hooks.example.com/" + strings.Repeat("a", MaxWebhookURLLength)
	cases := []struct {
		name string
		u    Update
		code string
	}{
		{"chat", Update{TelegramChatID: "chat"}, CodeBadChatID},
		{"short channel", Update{TelegramChatID: "@abc"}, CodeBadChatID},
		{"token", Update{TelegramToken: ptr("12345")}, CodeBadToken},
		{"token path", Update{TelegramToken: ptr("123:abcdefghijklmnopqrstuv/../x")}, CodeBadToken},
		{"ftp", Update{WebhookURL: ptr("ftp://hooks.example.com/x")}, CodeBadURL},
		{"public http", Update{WebhookURL: ptr("http://hooks.example.com/x")}, CodeBadURL},
		{"relative", Update{WebhookURL: ptr("/hook")}, CodeBadURL},
		{"long", Update{WebhookURL: &long}, CodeBadURL},
		{"secret", Update{WebhookSecret: ptr(strings.Repeat("s", MaxWebhookSecretLength+1))}, CodeBadSecret},
		{"digest low", Update{DigestSeconds: 10}, CodeBadDigest},
		{"digest high", Update{DigestSeconds: 4000}, CodeBadDigest},
		{"event", Update{Events: []string{EventShare, "vote"}}, CodeBadEvent},
		{"language", Update{Language: "de"}, CodeBadLanguage},
		{"telegram incomplete", Update{TelegramEnabled: true, TelegramChatID: testChat}, CodeIncomplete},
		{"webhook incomplete", Update{WebhookEnabled: true, WebhookSecret: ptr("x")}, CodeIncomplete},
	}
	for _, c := range cases {
		_, err := f.svc.Save(context.Background(), c.u)
		if codeOf(err) != c.code {
			t.Errorf("%s: expected %s, got %v", c.name, c.code, err)
		}
	}
	if raw, _ := f.store.Meta(context.Background(), metaConfig); raw != "" {
		t.Fatalf("a refused save must store nothing: %s", raw)
	}
	for _, ok := range []string{"http://127.0.0.1:9000/hook", "http://192.168.1.5/hook", "http://[::1]/hook", "http://localhost/hook", "https://hooks.example.com/x"} {
		if _, err := f.svc.Save(context.Background(), Update{WebhookURL: ptr(ok)}); err != nil {
			t.Errorf("%s must be accepted: %v", ok, err)
		}
	}
	cfg := f.save(t, Update{TelegramChatID: "@b4hub_alerts", Events: []string{EventMirror, EventShare, EventShare}, Language: LanguageRU})
	if cfg.DigestSeconds != DefaultDigest || cfg.Language != LanguageRU || strings.Join(cfg.Events, ",") != "share,mirror" {
		t.Fatalf("defaults and event order: %+v", cfg)
	}
}

func TestDefaultsBeforeAnySave(t *testing.T) {
	f := newFixture(t)
	cfg, _, err := f.svc.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Telegram.Enabled || cfg.Webhook.Enabled || strings.Join(cfg.Events, ",") != "share,report" || cfg.DigestSeconds != DefaultDigest || cfg.Language != LanguageEN {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	f.share(t, "author", hubwire.SetStatusPending)
	f.round()
	if f.tg.count()+f.hook.count() != 0 {
		t.Fatal("nothing may be sent while every channel is off")
	}
	cfg = f.save(t, Update{Events: []string{}})
	if len(cfg.Events) != 0 {
		t.Fatalf("an explicit empty list must stick: %+v", cfg.Events)
	}
	cfg = f.save(t, Update{})
	if len(cfg.Events) != 0 {
		t.Fatalf("a nil list keeps the stored one: %+v", cfg.Events)
	}
}

func TestEnablingDoesNotReplayHistory(t *testing.T) {
	f := newFixture(t)
	old, _ := f.share(t, "author", hubwire.SetStatusPending)
	f.report(t, old, "reporter")

	f.save(t, f.update())
	f.round()
	if f.tg.count() != 0 || f.hook.count() != 0 {
		t.Fatalf("rows from before the channel was enabled must not be sent: %d %d", f.tg.count(), f.hook.count())
	}

	_, row := f.share(t, "author", hubwire.SetStatusPending)
	f.round()
	if f.tg.count() != 1 || f.hook.count() != 1 {
		t.Fatalf("one digest per channel expected, got %d %d", f.tg.count(), f.hook.count())
	}
	msg := telegramBody(t, f.tg.last(t))
	if !strings.HasPrefix(msg.Text, "b4hub: 1 new set\n") || strings.Contains(msg.Text, "report") {
		t.Fatalf("unexpected digest: %q", msg.Text)
	}
	if got := f.cursors(t, ChannelTelegram)[EventShare]; got != row {
		t.Fatalf("the share cursor must reach row %d, got %d", row, got)
	}

	f.clock = f.clock.Add(time.Hour)
	f.save(t, Update{TelegramEnabled: false, TelegramChatID: testChat})
	f.share(t, "author", hubwire.SetStatusPending)
	f.save(t, f.update(EventShare, EventReport, EventMirror))
	f.round()
	if f.tg.count() != 1 {
		t.Fatal("a share made while the channel was off must not be sent on re-enable")
	}
}

func TestNewlyEnabledEventDoesNotReplay(t *testing.T) {
	f := newFixture(t)
	f.save(t, f.update(EventShare))
	if _, err := f.store.AnnounceMirror(context.Background(), "https://mirror.example", "k", "1.0", f.clock); err != nil {
		t.Fatal(err)
	}
	f.save(t, f.update(EventShare, EventMirror))
	f.round()
	if f.tg.count() != 0 {
		t.Fatal("a mirror announced before the mirror event was enabled must not be sent")
	}
}

func TestDigestCarriesEveryKind(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.save(t, f.update(Events...))

	var lastShare int64
	for i := 0; i < 12; i++ {
		_, lastShare = f.share(t, "author-hmac-0123456789abcdef", hubwire.SetStatusPending)
	}
	listed, _ := f.share(t, "author", hubwire.SetStatusPending)
	if err := f.store.Approve(ctx, listed, 1, f.clock); err != nil {
		t.Fatal(err)
	}
	f.report(t, listed, "reporter")
	if hidden, err := f.store.AutoHide(ctx, listed, 1, "reports", 3, f.clock); err != nil || !hidden {
		t.Fatalf("auto-hide: %v %v", hidden, err)
	}
	build, _ := f.store.StartBuild(ctx, "auto", f.clock)
	if err := f.store.FinishBuild(ctx, store.BuildRun{ID: build, Error: "signing failed", FinishedAt: f.clock}); err != nil {
		t.Fatal(err)
	}
	mirror, err := f.store.AnnounceMirror(ctx, "https://mirror.example", "announcer", "1.2.3", f.clock)
	if err != nil {
		t.Fatal(err)
	}

	f.round()
	c := f.tg.last(t)
	if c.path != "/bot"+testToken+"/sendMessage" {
		t.Fatalf("telegram path %q", c.path)
	}
	msg := telegramBody(t, c)
	if msg.ChatID != testChat || !msg.DisableWebPagePreview {
		t.Fatalf("telegram envelope: %+v", msg)
	}
	for _, want := range []string{
		"b4hub: 12 new sets, 1 report, 1 set hidden by reports, 1 failed build, 1 new mirror\n",
		"New sets in the queue (12):\n- Set 1 (01ARZ3NDEKTSV4RRFFQ69G5F11 v1), AS64500 RU\n",
		"and 2 more",
		"- Set 13 (" + listed + " v1): breaks the site",
		"- Set 13 (" + listed + " v1), 3 reports",
		"- #" + strconv.FormatInt(build, 10) + ", 2026-09-27 10:00 UTC: signing failed",
		"- https://mirror.example (1.2.3)",
		"\n\nConsole: https://hub.example/admin/queue",
	} {
		if !strings.Contains(msg.Text, want) {
			t.Errorf("telegram text lacks %q:\n%s", want, msg.Text)
		}
	}
	if strings.Contains(msg.Text, "Set 11 ") {
		t.Errorf("at most ten lines per kind:\n%s", msg.Text)
	}

	p := verifySignature(t, f.hook.last(t), hookSecret)
	if p.Hub != "https://hub.example" || p.SentAt != "2026-09-27T10:00:00Z" || p.Console != "https://hub.example/admin/queue" {
		t.Fatalf("webhook envelope: %+v", p)
	}
	wantCounts := map[string]int{EventShare: 12, EventReport: 1, EventAutoHide: 1, EventBuildFailed: 1, EventMirror: 1}
	for kind, n := range wantCounts {
		if p.Counts[kind] != n {
			t.Errorf("count %s = %d, want %d", kind, p.Counts[kind], n)
		}
	}
	if len(p.Events) != 14 || p.More != 2 {
		t.Fatalf("expected 14 events and 2 more, got %d and %d", len(p.Events), p.More)
	}
	first := p.Events[0]
	if first.Kind != EventShare || first.Author != "author-hmac-0123" || first.ASN != "64500" || first.Country != "RU" || first.URL != "https://hub.example/admin/sets/01ARZ3NDEKTSV4RRFFQ69G5F11/v/1" {
		t.Errorf("share event: %+v", first)
	}
	if ts := f.hook.last(t).header.Get(HeaderTimestamp); ts != strconv.FormatInt(f.clock.Unix(), 10) {
		t.Errorf("timestamp header %q", ts)
	}

	for _, channel := range Channels {
		got := f.cursors(t, channel)
		if got[EventShare] != lastShare || got[EventMirror] != mirror.ID || got[EventBuildFailed] != build || got[EventReport] != 1 || got[EventAutoHide] != 1 {
			t.Errorf("%s cursors not advanced: %v", channel, got)
		}
	}
	st := f.status(t)
	if st.SentTotal != 2 || st.Failures != 0 || !st.LastOKAt.Equal(f.clock) || st.Channels[ChannelTelegram].SentTotal != 1 {
		t.Fatalf("status: %+v", st)
	}

	f.clock = f.clock.Add(2 * time.Minute)
	f.round()
	if f.tg.count() != 1 || f.hook.count() != 1 {
		t.Fatal("nothing new must mean nothing sent")
	}
}

func TestFailureKeepsCursorsAndBacksOff(t *testing.T) {
	f := newFixture(t)
	u := f.update()
	u.WebhookEnabled = false
	f.save(t, u)
	f.tg.respond(http.StatusInternalServerError, `{"ok":false,"error_code":500,"description":"Internal Server Error"}`, nil)
	start := f.clock
	_, row := f.share(t, "author", hubwire.SetStatusPending)

	f.round()
	if f.tg.count() != 1 || f.cursors(t, ChannelTelegram)[EventShare] != 0 {
		t.Fatalf("a failed send must keep the cursor: %v", f.cursors(t, ChannelTelegram))
	}
	st := f.status(t)
	if st.Failures != 1 || !strings.Contains(st.LastError, "telegram: HTTP 500") || !st.LastErrorAt.Equal(start) {
		t.Fatalf("failure not recorded: %+v", st)
	}

	f.clock = start.Add(10 * time.Second)
	f.round()
	if f.tg.count() != 1 {
		t.Fatal("the channel must back off after a failure")
	}
	f.clock = start.Add(31 * time.Second)
	f.round()
	if f.tg.count() != 2 {
		t.Fatal("the first retry comes after 30 seconds")
	}
	f.tg.respond(http.StatusOK, telegramOK, nil)
	f.clock = start.Add(90 * time.Second)
	f.round()
	if f.tg.count() != 2 {
		t.Fatal("the backoff must double to 60 seconds")
	}
	f.clock = start.Add(91 * time.Second)
	f.round()
	if f.tg.count() != 3 || f.cursors(t, ChannelTelegram)[EventShare] != row {
		t.Fatalf("the retry must deliver and advance: %d %v", f.tg.count(), f.cursors(t, ChannelTelegram))
	}
	st = f.status(t)
	if st.Failures != 0 || st.SentTotal != 1 || !st.LastOKAt.Equal(f.clock) || st.LastError == "" {
		t.Fatalf("recovery not recorded: %+v", st)
	}
}

func TestBackoffIsCapped(t *testing.T) {
	f := newFixture(t)
	f.svc.init()
	now := f.clock
	for i := 0; i < 12; i++ {
		f.svc.settle(ChannelWebhook, now, errors.New("down"))
	}
	st := f.svc.states[ChannelWebhook]
	if st.backoff != BackoffMax || !st.nextAttempt.Equal(now.Add(BackoffMax)) {
		t.Fatalf("backoff must stop at %s, got %s", BackoffMax, st.backoff)
	}
	f.svc.settle(ChannelWebhook, now, nil)
	if st.backoff != 0 || !st.nextAttempt.IsZero() || !st.lastSent.Equal(now) {
		t.Fatalf("a success must reset the backoff: %+v", st)
	}
}

func TestTelegramRetryAfterIsHonoured(t *testing.T) {
	f := newFixture(t)
	u := f.update()
	u.WebhookEnabled = false
	f.save(t, u)
	f.tg.respond(http.StatusTooManyRequests, `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 300","parameters":{"retry_after":300}}`, nil)
	start := f.clock
	f.share(t, "author", hubwire.SetStatusPending)

	f.round()
	if got := f.svc.states[ChannelTelegram].nextAttempt; !got.Equal(start.Add(300 * time.Second)) {
		t.Fatalf("retry_after must set the next attempt, got %s", got)
	}
	if st := f.status(t); !strings.Contains(st.LastError, "Too Many Requests") {
		t.Fatalf("status: %+v", st)
	}
	f.clock = start.Add(299 * time.Second)
	f.round()
	if f.tg.count() != 1 {
		t.Fatal("nothing may be sent before retry_after elapses")
	}
	f.clock = start.Add(300 * time.Second)
	f.round()
	if f.tg.count() != 2 {
		t.Fatal("the send must be retried once retry_after elapsed")
	}
}

func TestWebhookRetryAfterHeader(t *testing.T) {
	f := newFixture(t)
	u := f.update()
	u.TelegramEnabled = false
	f.save(t, u)
	f.hook.respond(http.StatusServiceUnavailable, "", map[string]string{"Retry-After": "120"})
	f.share(t, "author", hubwire.SetStatusPending)
	f.round()
	if got := f.svc.states[ChannelWebhook].nextAttempt; !got.Equal(f.clock.Add(120 * time.Second)) {
		t.Fatalf("Retry-After must set the next attempt, got %s", got)
	}
	if f.cursors(t, ChannelWebhook)[EventShare] != 0 {
		t.Fatal("a failed webhook must keep the cursor")
	}
	if st := f.status(t); !strings.Contains(st.LastError, "webhook: HTTP 503") {
		t.Fatalf("status: %+v", st)
	}
}

func TestDigestWindowThrottles(t *testing.T) {
	f := newFixture(t)
	u := f.update()
	u.WebhookEnabled = false
	u.DigestSeconds = 120
	f.save(t, u)
	start := f.clock
	f.share(t, "author", hubwire.SetStatusPending)
	f.round()
	if f.tg.count() != 1 {
		t.Fatal("the first event goes out at once")
	}
	f.clock = start.Add(30 * time.Second)
	f.share(t, "author", hubwire.SetStatusPending)
	f.share(t, "author", hubwire.SetStatusPending)
	f.round()
	f.clock = start.Add(119 * time.Second)
	f.round()
	if f.tg.count() != 1 {
		t.Fatal("at most one digest per window")
	}
	f.clock = start.Add(120 * time.Second)
	f.round()
	if f.tg.count() != 2 || !strings.HasPrefix(telegramBody(t, f.tg.last(t)).Text, "b4hub: 2 new sets\n") {
		t.Fatalf("the window must end with one digest of both shares: %d", f.tg.count())
	}
}

func TestTestKeysAreSkipped(t *testing.T) {
	f := newFixture(t)
	f.save(t, f.update())
	f.tagTest(t, "tester")
	listed, _ := f.share(t, "tester", hubwire.SetStatusPending)
	f.report(t, listed, "tester")
	f.round()
	if f.tg.count() != 0 || f.hook.count() != 0 {
		t.Fatal("uploads and reports from test keys must not notify")
	}
	f.report(t, listed, "reporter")
	f.round()
	if f.tg.count() != 1 || !strings.HasPrefix(telegramBody(t, f.tg.last(t)).Text, "b4hub: 1 report\n") {
		t.Fatal("a real report must still notify")
	}
	if !strings.Contains(telegramBody(t, f.tg.last(t)).Text, "Console: https://hub.example/admin/feedback?tab=reports") {
		t.Errorf("reports link to the reports tab: %s", telegramBody(t, f.tg.last(t)).Text)
	}
}

func TestTestMessage(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.save(t, Update{TelegramChatID: testChat, TelegramToken: ptr(testToken), Language: LanguageRU})
	if err := f.svc.Test(ctx, ChannelTelegram); err != nil {
		t.Fatal(err)
	}
	if msg := telegramBody(t, f.tg.last(t)); msg.Text != testText(LanguageRU) || msg.ChatID != testChat {
		t.Fatalf("test message: %+v", msg)
	}
	if err := f.svc.Test(ctx, ChannelWebhook); codeOf(err) != CodeIncomplete {
		t.Fatalf("an unconfigured channel must be refused, got %v", err)
	}
	if err := f.svc.Test(ctx, "sms"); codeOf(err) != CodeBadChannel {
		t.Fatalf("an unknown channel must be refused, got %v", err)
	}
	f.save(t, Update{TelegramChatID: testChat, WebhookURL: ptr(f.hook.srv.URL + "/hook"), WebhookSecret: ptr(hookSecret)})
	if err := f.svc.Test(ctx, ChannelWebhook); err != nil {
		t.Fatal(err)
	}
	p := verifySignature(t, f.hook.last(t), hookSecret)
	if !p.Test || p.Message == "" || len(p.Events) != 0 {
		t.Fatalf("webhook test payload: %+v", p)
	}
	f.save(t, Update{TelegramChatID: testChat, ClearWebhookSecret: true})
	if err := f.svc.Test(ctx, ChannelWebhook); err != nil {
		t.Fatal(err)
	}
	if c := f.hook.last(t); c.header.Get(HeaderSignature) != "" || c.header.Get(HeaderTimestamp) == "" {
		t.Fatalf("without a secret there is a timestamp and no signature: %v", c.header)
	}
	f.hook.respond(http.StatusFound, "", map[string]string{"Location": "https://elsewhere.example/"})
	if err := f.svc.Test(ctx, ChannelWebhook); err == nil || !strings.Contains(err.Error(), "HTTP 302") {
		t.Fatalf("redirects must not be followed, got %v", err)
	}
	st := f.status(t)
	if st.SentTotal != 3 || st.Failures != 1 || st.Channels[ChannelWebhook].SentTotal != 2 {
		t.Fatalf("test sends must be recorded: %+v", st)
	}
}

func TestErrorsNeverCarryTheToken(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.save(t, Update{TelegramChatID: testChat, TelegramToken: ptr(testToken)})
	f.tg.respond(http.StatusUnauthorized, `{"ok":false,"error_code":401,"description":"Unauthorized: token `+testToken+` rejected"}`, nil)
	err := f.svc.Test(ctx, ChannelTelegram)
	if err == nil || strings.Contains(err.Error(), testToken) || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("error must be scrubbed: %v", err)
	}
	f.tg.srv.Close()
	err = f.svc.Test(ctx, ChannelTelegram)
	if err == nil || strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "/bot") {
		t.Fatalf("transport errors must not carry the request URL: %v", err)
	}
	if st := f.status(t); strings.Contains(st.LastError, testToken) {
		t.Fatalf("status must not carry the token: %+v", st)
	}
}

func TestSignature(t *testing.T) {
	body := []byte(`{"hub":"https://hub.example"}`)
	mac := hmac.New(sha256.New, []byte("key"))
	mac.Write([]byte("1790000000." + string(body)))
	if got, want := Signature("key", "1790000000", body), "sha256="+hex.EncodeToString(mac.Sum(nil)); got != want {
		t.Fatalf("signature %s, want %s", got, want)
	}
	if Signature("key", "1790000001", body) == Signature("key", "1790000000", body) {
		t.Error("the timestamp must be signed")
	}
}

func TestTelegramTextStaysUnderTheLimit(t *testing.T) {
	long := strings.Repeat("Ж", 300)
	var d digest
	for _, kind := range Events {
		b := store.NotifyBatch{Kind: kind, Count: 50, MaxID: 50}
		for i := 0; i < LinesPerKind; i++ {
			b.Events = append(b.Events, store.NotifyEvent{Kind: kind, ID: int64(i + 1), SetID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Version: 1, Title: long, Reason: long, URL: "https://" + long + ".example", At: time.Now()})
		}
		d.batches = append(d.batches, b)
	}
	for _, lang := range []string{LanguageEN, LanguageRU} {
		text := telegramText(lang, d, "https://hub.example/admin/queue")
		if n := textLen(text); n > telegramLimit {
			t.Fatalf("%s: %d characters exceed the Telegram limit", lang, n)
		}
		if !strings.HasSuffix(text, "https://hub.example/admin/queue") {
			t.Errorf("%s: the console link must survive trimming", lang)
		}
		if strings.Contains(text, strings.Repeat("Ж", fieldRunes)) {
			t.Errorf("%s: titles must be clipped to %d runes", lang, fieldRunes)
		}
	}
}

func TestRussianDigest(t *testing.T) {
	d := digest{batches: []store.NotifyBatch{
		{Kind: EventShare, Count: 2},
		{Kind: EventReport, Count: 5},
		{Kind: EventMirror, Count: 21},
		{Kind: EventBuildFailed, Count: 11},
	}}
	text := telegramText(LanguageRU, d, "")
	if !strings.HasPrefix(text, "b4hub: 2 новых сета, 5 жалоб, 21 новое зеркало, 11 неудачных сборок\n") {
		t.Fatalf("russian header: %q", text)
	}
	if !strings.Contains(text, "и ещё 2") || strings.Contains(text, "Консоль") {
		t.Fatalf("russian body: %q", text)
	}
	for n, want := range map[int]int{1: 0, 2: 1, 4: 1, 5: 2, 11: 2, 12: 2, 21: 0, 22: 1, 25: 2, 111: 2, 101: 0} {
		if got := russianRule(n); got != want {
			t.Errorf("russianRule(%d) = %d, want %d", n, got, want)
		}
	}
	for _, c := range catalogs {
		for _, s := range append([]string{c.more, c.console, c.test}, c.sections[EventShare], c.counts[EventShare].many) {
			if strings.ContainsRune(s, 0x2014) {
				t.Errorf("em dash in %q", s)
			}
		}
	}
}

func TestCleanText(t *testing.T) {
	if got := cleanText(" a\x00b\n\tc\u202e d\u200b ", 0); got != "ab c d" {
		t.Fatalf("control and bidi characters must go: %q", got)
	}
	if got := cleanText(strings.Repeat("x", 100), 10); got != "xxxxxxx..." {
		t.Fatalf("clip: %q", got)
	}
}

func TestRunStopsOnCancelAndNudgeNeverBlocks(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 5; i++ {
		f.svc.Nudge()
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		f.svc.Run(ctx)
		close(done)
	}()
	f.svc.Nudge()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run must return once the context is cancelled")
	}
}
