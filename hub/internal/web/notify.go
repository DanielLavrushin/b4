package web

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/daniellavrushin/b4hub/internal/notify"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const codeNotifyFailed = "notify_failed"

type NotifyTelegramView struct {
	Enabled      bool   `json:"enabled"`
	ChatID       string `json:"chat_id,omitempty"`
	ChatFromEnv  bool   `json:"chat_from_env"`
	TokenSet     bool   `json:"token_set"`
	TokenHint    string `json:"token_hint,omitempty"`
	TokenFromEnv bool   `json:"token_from_env"`
}

type NotifyWebhookView struct {
	Enabled       bool   `json:"enabled"`
	URLSet        bool   `json:"url_set"`
	URLHint       string `json:"url_hint,omitempty"`
	URLFromEnv    bool   `json:"url_from_env"`
	SecretSet     bool   `json:"secret_set"`
	SecretFromEnv bool   `json:"secret_from_env"`
}

type NotifyChannelStatusView struct {
	LastOKAt    *time.Time `json:"last_ok_at,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
	LastErrorAt *time.Time `json:"last_error_at,omitempty"`
	Failures    int        `json:"failures"`
	SentTotal   int        `json:"sent_total"`
}

type NotifyView struct {
	Telegram      NotifyTelegramView                 `json:"telegram"`
	Webhook       NotifyWebhookView                  `json:"webhook"`
	Events        []string                           `json:"events"`
	Available     []string                           `json:"available"`
	DigestSeconds int                                `json:"digest_seconds"`
	DigestMin     int                                `json:"digest_min"`
	DigestMax     int                                `json:"digest_max"`
	Language      string                             `json:"language"`
	Status        map[string]NotifyChannelStatusView `json:"status"`
}

type NotifyTelegramRequest struct {
	Enabled    bool    `json:"enabled"`
	ChatID     string  `json:"chat_id"`
	Token      *string `json:"token,omitempty"`
	ClearToken bool    `json:"clear_token,omitempty"`
}

type NotifyWebhookRequest struct {
	Enabled     bool    `json:"enabled"`
	URL         *string `json:"url,omitempty"`
	ClearURL    bool    `json:"clear_url,omitempty"`
	Secret      *string `json:"secret,omitempty"`
	ClearSecret bool    `json:"clear_secret,omitempty"`
}

type NotifyRequest struct {
	Telegram      NotifyTelegramRequest `json:"telegram"`
	Webhook       NotifyWebhookRequest  `json:"webhook"`
	Events        []string              `json:"events"`
	DigestSeconds int                   `json:"digest_seconds"`
	Language      string                `json:"language"`
}

type NotifyTestRequest struct {
	Channel string `json:"channel"`
}

func tokenHint(token string) string {
	if i := strings.IndexByte(token, ':'); i > 0 {
		return "bot " + token[:i]
	}
	return ""
}

func urlHint(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func channelStatusView(cs notify.ChannelStatus) NotifyChannelStatusView {
	return NotifyChannelStatusView{
		LastOKAt:    optionalTime(cs.LastOKAt),
		LastError:   cs.LastError,
		LastErrorAt: optionalTime(cs.LastErrorAt),
		Failures:    cs.Failures,
		SentTotal:   cs.SentTotal,
	}
}

func notifyView(cfg notify.Config, src notify.Sources, status notify.Status) NotifyView {
	v := NotifyView{
		Telegram: NotifyTelegramView{
			Enabled:      cfg.Telegram.Enabled,
			ChatID:       cfg.Telegram.ChatID,
			ChatFromEnv:  src.TelegramChat,
			TokenSet:     cfg.Telegram.Token != "",
			TokenHint:    tokenHint(cfg.Telegram.Token),
			TokenFromEnv: src.TelegramToken,
		},
		Webhook: NotifyWebhookView{
			Enabled:       cfg.Webhook.Enabled,
			URLSet:        cfg.Webhook.URL != "",
			URLHint:       urlHint(cfg.Webhook.URL),
			URLFromEnv:    src.WebhookURL,
			SecretSet:     cfg.Webhook.Secret != "",
			SecretFromEnv: src.WebhookSecret,
		},
		Events:        append([]string{}, cfg.Events...),
		Available:     append([]string{}, notify.Events...),
		DigestSeconds: cfg.DigestSeconds,
		DigestMin:     notify.MinDigest,
		DigestMax:     notify.MaxDigest,
		Language:      cfg.Language,
		Status:        map[string]NotifyChannelStatusView{},
	}
	for _, channel := range notify.Channels {
		v.Status[channel] = channelStatusView(status.Channels[channel])
	}
	return v
}

func notifyAuditMap(v NotifyView) map[string]interface{} {
	return map[string]interface{}{
		"telegram":         v.Telegram.Enabled,
		"telegram_chat":    v.Telegram.ChatID,
		"telegram_token":   v.Telegram.TokenSet,
		"webhook":          v.Webhook.Enabled,
		"webhook_url":      v.Webhook.URLHint,
		"webhook_secret":   v.Webhook.SecretSet,
		"events":           strings.Join(v.Events, ","),
		"digest_seconds":   v.DigestSeconds,
		"message_language": v.Language,
	}
}

func (s *Server) currentNotify(r *http.Request) (NotifyView, error) {
	ctx := r.Context()
	cfg, src, err := s.Notify.Load(ctx)
	if err != nil {
		return NotifyView{}, err
	}
	status, err := s.Notify.Status(ctx)
	if err != nil {
		return NotifyView{}, err
	}
	return notifyView(cfg, src, status), nil
}

func (s *Server) notifyAvailable(w http.ResponseWriter) bool {
	if s.Notify == nil {
		writeError(w, http.StatusNotFound, codeNotFound, "notifications are not available on this hub")
		return false
	}
	return true
}

func (s *Server) failNotify(w http.ResponseWriter, err error) {
	var invalid *notify.ValidationError
	if errors.As(err, &invalid) {
		writeError(w, http.StatusBadRequest, invalid.Code, invalid.Message)
		return
	}
	s.fail(w, err)
}

func (s *Server) notifySettings(w http.ResponseWriter, r *http.Request) {
	if !s.notifyAvailable(w) {
		return
	}
	view, err := s.currentNotify(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) saveNotify(w http.ResponseWriter, r *http.Request) {
	if !s.notifyAvailable(w) {
		return
	}
	var req NotifyRequest
	if !s.readJSON(w, r, &req) {
		return
	}
	before, err := s.currentNotify(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	events := req.Events
	if events == nil {
		events = []string{}
	}
	_, err = s.Notify.Save(r.Context(), notify.Update{
		TelegramEnabled:    req.Telegram.Enabled,
		TelegramChatID:     req.Telegram.ChatID,
		TelegramToken:      req.Telegram.Token,
		ClearTelegramToken: req.Telegram.ClearToken,
		WebhookEnabled:     req.Webhook.Enabled,
		WebhookURL:         req.Webhook.URL,
		ClearWebhookURL:    req.Webhook.ClearURL,
		WebhookSecret:      req.Webhook.Secret,
		ClearWebhookSecret: req.Webhook.ClearSecret,
		Events:             events,
		DigestSeconds:      req.DigestSeconds,
		Language:           req.Language,
	})
	if err != nil {
		s.failNotify(w, err)
		return
	}
	after, err := s.currentNotify(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	a := s.actor(r)
	s.recordAudit(r, store.AuditEntry{
		At: s.now().UTC(), Actor: a.Kind, ActorRef: a.Ref, ActorIP: a.IP, Action: "notify.save", TargetKind: store.TargetNotify,
		Before: notifyAuditMap(before), After: notifyAuditMap(after),
	})
	s.Notify.Nudge()
	writeJSON(w, http.StatusOK, after)
}

func (s *Server) testNotify(w http.ResponseWriter, r *http.Request) {
	if !s.notifyAvailable(w) {
		return
	}
	var req NotifyTestRequest
	if !s.readJSON(w, r, &req) {
		return
	}
	err := s.Notify.Test(r.Context(), req.Channel)
	a := s.actor(r)
	entry := store.AuditEntry{
		At: s.now().UTC(), Actor: a.Kind, ActorRef: a.Ref, ActorIP: a.IP, Action: "notify.test", TargetKind: store.TargetNotify, TargetID: req.Channel,
		After: map[string]interface{}{"delivered": err == nil},
	}
	var invalid *notify.ValidationError
	if err != nil && errors.As(err, &invalid) {
		s.failNotify(w, err)
		return
	}
	s.recordAudit(r, entry)
	if err != nil {
		writeError(w, http.StatusBadGateway, codeNotifyFailed, err.Error())
		return
	}
	view, err := s.currentNotify(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
