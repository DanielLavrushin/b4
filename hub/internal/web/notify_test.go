package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4hub/internal/notify"
)

const testBotToken = "123456789:AAHfakeTokenForTestsOnly_abcdefghij"

type fakeTelegram struct {
	mu    sync.Mutex
	texts []string
	fail  bool
}

func (tg *fakeTelegram) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var msg struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(body, &msg)
	tg.mu.Lock()
	defer tg.mu.Unlock()
	if tg.fail {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":401,"description":"Unauthorized"}`))
		return
	}
	tg.texts = append(tg.texts, msg.Text)
	_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
}

func withNotify(f *fixture, env map[string]string) *fakeTelegram {
	tg := &fakeTelegram{}
	srv := httptest.NewServer(http.HandlerFunc(tg.handler))
	f.t.Cleanup(srv.Close)
	f.web.Notify = &notify.Service{
		Store:        f.store,
		Secret:       f.web.Secret,
		Env:          func(name string) string { return env[name] },
		PublicURL:    "https://hub.example",
		TelegramBase: srv.URL,
		Now:          func() time.Time { return f.clock },
	}
	return tg
}

func TestNotifySettingsNeverEchoSecrets(t *testing.T) {
	f := newFixture(t, password)
	withNotify(f, nil)

	var view NotifyView
	f.admin(http.MethodGet, PathAPI+"/notify", nil).decode(t, &view)
	if view.Telegram.Enabled || view.Telegram.TokenSet || len(view.Events) != 2 || view.DigestSeconds != notify.DefaultDigest || view.Language != notify.LanguageEN {
		t.Fatalf("defaults before any save: %+v", view)
	}

	token := testBotToken
	hook := "https://hooks.example/path/with/secret"
	secret := "shared-secret"
	req := NotifyRequest{
		Telegram:      NotifyTelegramRequest{Enabled: true, ChatID: "-100123", Token: &token},
		Webhook:       NotifyWebhookRequest{Enabled: true, URL: &hook, Secret: &secret},
		Events:        []string{notify.EventShare, notify.EventBuildFailed},
		DigestSeconds: 120,
		Language:      notify.LanguageRU,
	}
	resp := f.admin(http.MethodPut, PathAPI+"/notify", req)
	if resp.status != http.StatusOK {
		t.Fatalf("save: %d %s", resp.status, resp.body)
	}
	for _, leak := range []string{token, "AAHfake", "/path/with/secret", secret} {
		if strings.Contains(resp.body, leak) {
			t.Fatalf("the response echoes a secret %q: %s", leak, resp.body)
		}
	}
	resp.decode(t, &view)
	if !view.Telegram.TokenSet || view.Telegram.TokenHint != "bot 123456789" || view.Webhook.URLHint != "https://hooks.example" || !view.Webhook.SecretSet {
		t.Fatalf("set flags and hints: %+v", view)
	}
	if view.DigestSeconds != 120 || view.Language != notify.LanguageRU || len(view.Events) != 2 {
		t.Fatalf("saved values: %+v", view)
	}

	audit := f.auditLog("?target_kind=notify")
	if len(audit.Items) != 1 || audit.Items[0].Action != "notify.save" {
		t.Fatalf("the save is audited: %+v", audit.Items)
	}
	raw, _ := json.Marshal(audit.Items)
	if strings.Contains(string(raw), "AAHfake") || strings.Contains(string(raw), secret) || strings.Contains(string(raw), "/path/with/secret") {
		t.Fatalf("the audit entry carries a secret: %s", raw)
	}

	req.Telegram.Token = nil
	req.Webhook.URL = nil
	req.Webhook.Secret = nil
	req.Webhook.Enabled = false
	f.admin(http.MethodPut, PathAPI+"/notify", req).decode(t, &view)
	if !view.Telegram.TokenSet || !view.Webhook.URLSet {
		t.Fatalf("an omitted secret keeps the stored one: %+v", view)
	}

	req.Webhook.ClearURL = true
	f.admin(http.MethodPut, PathAPI+"/notify", req).decode(t, &view)
	if view.Webhook.URLSet {
		t.Fatalf("clear_url removes the stored URL: %+v", view)
	}
}

func TestNotifySaveRefusesBadInput(t *testing.T) {
	f := newFixture(t, password)
	withNotify(f, nil)
	bad := "not a token"
	cases := []struct {
		req  NotifyRequest
		code string
	}{
		{NotifyRequest{Telegram: NotifyTelegramRequest{ChatID: "chat!"}}, notify.CodeBadChatID},
		{NotifyRequest{Telegram: NotifyTelegramRequest{Token: &bad}}, notify.CodeBadToken},
		{NotifyRequest{Telegram: NotifyTelegramRequest{Enabled: true}}, notify.CodeIncomplete},
		{NotifyRequest{DigestSeconds: 5}, notify.CodeBadDigest},
		{NotifyRequest{Events: []string{"everything"}}, notify.CodeBadEvent},
		{NotifyRequest{Language: "de"}, notify.CodeBadLanguage},
	}
	for _, c := range cases {
		f.expectError(f.admin(http.MethodPut, PathAPI+"/notify", c.req), http.StatusBadRequest, c.code)
	}
	plain := "http://hooks.example/x"
	f.expectError(f.admin(http.MethodPut, PathAPI+"/notify", NotifyRequest{Webhook: NotifyWebhookRequest{URL: &plain}}), http.StatusBadRequest, notify.CodeBadURL)
}

func TestNotifyEnvironmentWins(t *testing.T) {
	f := newFixture(t, password)
	withNotify(f, map[string]string{notify.EnvTelegramToken: testBotToken, notify.EnvTelegramChat: "@hubops"})
	other := "987654321:BBHotherTokenForTestsOnly_abcdefghij"
	var view NotifyView
	f.admin(http.MethodPut, PathAPI+"/notify", NotifyRequest{Telegram: NotifyTelegramRequest{Enabled: true, ChatID: "-1", Token: &other}}).decode(t, &view)
	if !view.Telegram.TokenFromEnv || !view.Telegram.ChatFromEnv || view.Telegram.ChatID != "@hubops" || view.Telegram.TokenHint != "bot 123456789" || !view.Telegram.Enabled {
		t.Fatalf("values from the environment override the console: %+v", view)
	}
}

func TestNotifyTestMessage(t *testing.T) {
	f := newFixture(t, password)
	tg := withNotify(f, nil)
	f.expectError(f.admin(http.MethodPost, PathAPI+"/notify/test", NotifyTestRequest{Channel: notify.ChannelTelegram}), http.StatusBadRequest, notify.CodeIncomplete)
	f.expectError(f.admin(http.MethodPost, PathAPI+"/notify/test", NotifyTestRequest{Channel: "pigeon"}), http.StatusBadRequest, notify.CodeBadChannel)

	token := testBotToken
	f.expectOK(f.admin(http.MethodPut, PathAPI+"/notify", NotifyRequest{Telegram: NotifyTelegramRequest{ChatID: "-100123", Token: &token}}))
	var view NotifyView
	resp := f.admin(http.MethodPost, PathAPI+"/notify/test", NotifyTestRequest{Channel: notify.ChannelTelegram})
	if resp.status != http.StatusOK {
		t.Fatalf("test message: %d %s", resp.status, resp.body)
	}
	resp.decode(t, &view)
	if len(tg.texts) != 1 || view.Status[notify.ChannelTelegram].SentTotal != 1 || view.Status[notify.ChannelTelegram].LastOKAt == nil {
		t.Fatalf("a disabled but configured channel can be tested: %d %+v", len(tg.texts), view.Status)
	}

	tg.mu.Lock()
	tg.fail = true
	tg.mu.Unlock()
	resp = f.admin(http.MethodPost, PathAPI+"/notify/test", NotifyTestRequest{Channel: notify.ChannelTelegram})
	body := f.expectError(resp, http.StatusBadGateway, codeNotifyFailed)
	if strings.Contains(body.Error, "AAHfake") || strings.Contains(resp.body, "AAHfake") {
		t.Fatalf("a delivery error carries the token: %s", resp.body)
	}
	if audit := f.auditLog("?action=notify.test"); len(audit.Items) != 2 {
		t.Fatalf("both test sends are audited: %+v", audit.Items)
	}
}
