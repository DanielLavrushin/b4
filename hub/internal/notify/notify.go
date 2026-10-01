package notify

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	EventShare       = store.NotifyShare
	EventReport      = store.NotifyReport
	EventAutoHide    = store.NotifyAutoHide
	EventBuildFailed = store.NotifyBuildFailed
	EventMirror      = store.NotifyMirror

	ChannelTelegram = "telegram"
	ChannelWebhook  = "webhook"

	DefaultDigest = 60
	MinDigest     = 30
	MaxDigest     = 3600

	LanguageEN = "en"
	LanguageRU = "ru"

	EnvTelegramToken = "B4HUB_TELEGRAM_TOKEN"
	EnvTelegramChat  = "B4HUB_TELEGRAM_CHAT"
	EnvWebhookURL    = "B4HUB_WEBHOOK_URL"
	EnvWebhookSecret = "B4HUB_WEBHOOK_SECRET"

	CodeBadChatID   = "bad_chat_id"
	CodeBadToken    = "bad_token"
	CodeBadURL      = "bad_url"
	CodeBadSecret   = "bad_secret"
	CodeBadDigest   = "bad_digest"
	CodeBadEvent    = "bad_event"
	CodeBadLanguage = "bad_language"
	CodeBadChannel  = "bad_channel"
	CodeIncomplete  = "notify_incomplete"

	MaxWebhookURLLength    = 500
	MaxWebhookSecretLength = 256

	DefaultTelegramBase = "https://api.telegram.org"

	metaConfig = "notify.config"
	metaStatus = "notify.status"
)

var (
	Events   = []string{EventShare, EventReport, EventAutoHide, EventBuildFailed, EventMirror}
	Channels = []string{ChannelTelegram, ChannelWebhook}

	defaultEvents = []string{EventShare, EventReport}

	chatIDPattern   = regexp.MustCompile(`^(-?[0-9]{1,20}|@[A-Za-z0-9_]{5,32})$`)
	botTokenPattern = regexp.MustCompile(`^[0-9]{1,20}:[A-Za-z0-9_-]{20,100}$`)
)

type Config struct {
	Telegram      TelegramConfig `json:"telegram"`
	Webhook       WebhookConfig  `json:"webhook"`
	Events        []string       `json:"events"`
	DigestSeconds int            `json:"digest_seconds"`
	Language      string         `json:"language"`
}

type TelegramConfig struct {
	Enabled bool   `json:"enabled"`
	ChatID  string `json:"chat_id"`
	Token   string `json:"-"`
}

type WebhookConfig struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"-"`
	Secret  string `json:"-"`
}

func (c Config) EventEnabled(kind string) bool {
	for _, e := range c.Events {
		if e == kind {
			return true
		}
	}
	return false
}

func (c Config) ChannelEnabled(channel string) bool {
	switch channel {
	case ChannelTelegram:
		return c.Telegram.Enabled
	case ChannelWebhook:
		return c.Webhook.Enabled
	}
	return false
}

func (c Config) Configured(channel string) bool {
	switch channel {
	case ChannelTelegram:
		return c.Telegram.Token != "" && c.Telegram.ChatID != ""
	case ChannelWebhook:
		return c.Webhook.URL != ""
	}
	return false
}

type Sources struct {
	TelegramToken bool `json:"telegram_token"`
	TelegramChat  bool `json:"telegram_chat"`
	WebhookURL    bool `json:"webhook_url"`
	WebhookSecret bool `json:"webhook_secret"`
}

type Update struct {
	TelegramEnabled    bool
	TelegramChatID     string
	TelegramToken      *string
	ClearTelegramToken bool
	WebhookEnabled     bool
	WebhookURL         *string
	ClearWebhookURL    bool
	WebhookSecret      *string
	ClearWebhookSecret bool
	Events             []string
	DigestSeconds      int
	Language           string
}

type ChannelStatus struct {
	LastOKAt    time.Time `json:"last_ok_at"`
	LastError   string    `json:"last_error"`
	LastErrorAt time.Time `json:"last_error_at"`
	Failures    int       `json:"failures"`
	SentTotal   int       `json:"sent_total"`
}

type Status struct {
	LastOKAt    time.Time                `json:"last_ok_at"`
	LastError   string                   `json:"last_error"`
	LastErrorAt time.Time                `json:"last_error_at"`
	Failures    int                      `json:"failures"`
	SentTotal   int                      `json:"sent_total"`
	Channels    map[string]ChannelStatus `json:"channels"`
}

type ValidationError struct {
	Code    string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

func invalid(code, message string) *ValidationError {
	return &ValidationError{Code: code, Message: message}
}

type Service struct {
	Store        *store.Store
	Secret       []byte
	Env          func(string) string
	PublicURL    string
	Client       *http.Client
	TelegramBase string
	Now          func() time.Time

	once   sync.Once
	kick   chan struct{}
	mu     sync.Mutex
	states map[string]*channelState
}

func (s *Service) init() {
	s.once.Do(func() {
		s.kick = make(chan struct{}, 1)
		s.states = map[string]*channelState{}
	})
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) env(name string) string {
	get := s.Env
	if get == nil {
		get = os.Getenv
	}
	return strings.TrimSpace(get(name))
}

func (s *Service) Nudge() {
	s.init()
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

type storedTelegram struct {
	Enabled bool   `json:"enabled,omitempty"`
	ChatID  string `json:"chat_id,omitempty"`
	Token   string `json:"token,omitempty"`
}

type storedWebhook struct {
	Enabled bool   `json:"enabled,omitempty"`
	URL     string `json:"url,omitempty"`
	Secret  string `json:"secret,omitempty"`
}

type storedConfig struct {
	Telegram      storedTelegram `json:"telegram"`
	Webhook       storedWebhook  `json:"webhook"`
	Events        []string       `json:"events"`
	DigestSeconds int            `json:"digest_seconds,omitempty"`
	Language      string         `json:"language,omitempty"`
}

const (
	fieldTelegramToken = "telegram.token"
	fieldWebhookURL    = "webhook.url"
	fieldWebhookSecret = "webhook.secret"
)

func (s *Service) loadStored(ctx context.Context) (storedConfig, error) {
	var st storedConfig
	raw, err := s.Store.Meta(ctx, metaConfig)
	if err != nil || raw == "" {
		return st, err
	}
	err = json.Unmarshal([]byte(raw), &st)
	return st, err
}

func (s *Service) open(field, sealed string) string {
	plain, err := unseal(s.Secret, field, sealed)
	if err != nil {
		log.Printf("notify: %v; treating it as unset", err)
		return ""
	}
	return plain
}

func (s *Service) fromStored(st storedConfig) Config {
	cfg := Config{
		Telegram: TelegramConfig{
			Enabled: st.Telegram.Enabled,
			ChatID:  st.Telegram.ChatID,
			Token:   s.open(fieldTelegramToken, st.Telegram.Token),
		},
		Webhook: WebhookConfig{
			Enabled: st.Webhook.Enabled,
			URL:     s.open(fieldWebhookURL, st.Webhook.URL),
			Secret:  s.open(fieldWebhookSecret, st.Webhook.Secret),
		},
		DigestSeconds: st.DigestSeconds,
		Language:      st.Language,
	}
	if st.Events == nil {
		cfg.Events = append([]string(nil), defaultEvents...)
	} else {
		cfg.Events = knownEvents(st.Events)
	}
	if cfg.DigestSeconds == 0 {
		cfg.DigestSeconds = DefaultDigest
	}
	if cfg.DigestSeconds < MinDigest {
		cfg.DigestSeconds = MinDigest
	}
	if cfg.DigestSeconds > MaxDigest {
		cfg.DigestSeconds = MaxDigest
	}
	if cfg.Language != LanguageRU {
		cfg.Language = LanguageEN
	}
	return cfg
}

func (s *Service) toStored(cfg Config) (storedConfig, error) {
	st := storedConfig{
		Telegram:      storedTelegram{Enabled: cfg.Telegram.Enabled, ChatID: cfg.Telegram.ChatID},
		Webhook:       storedWebhook{Enabled: cfg.Webhook.Enabled},
		Events:        append([]string{}, cfg.Events...),
		DigestSeconds: cfg.DigestSeconds,
		Language:      cfg.Language,
	}
	var err error
	if st.Telegram.Token, err = seal(s.Secret, fieldTelegramToken, cfg.Telegram.Token); err != nil {
		return st, err
	}
	if st.Webhook.URL, err = seal(s.Secret, fieldWebhookURL, cfg.Webhook.URL); err != nil {
		return st, err
	}
	if st.Webhook.Secret, err = seal(s.Secret, fieldWebhookSecret, cfg.Webhook.Secret); err != nil {
		return st, err
	}
	return st, nil
}

func (s *Service) sources() Sources {
	return Sources{
		TelegramToken: s.env(EnvTelegramToken) != "",
		TelegramChat:  s.env(EnvTelegramChat) != "",
		WebhookURL:    s.env(EnvWebhookURL) != "",
		WebhookSecret: s.env(EnvWebhookSecret) != "",
	}
}

func (s *Service) withEnv(cfg Config) Config {
	if v := s.env(EnvTelegramToken); v != "" {
		cfg.Telegram.Token = v
	}
	if v := s.env(EnvTelegramChat); v != "" {
		cfg.Telegram.ChatID = v
	}
	if v := s.env(EnvWebhookURL); v != "" {
		cfg.Webhook.URL = v
	}
	if v := s.env(EnvWebhookSecret); v != "" {
		cfg.Webhook.Secret = v
	}
	return cfg
}

func (s *Service) Load(ctx context.Context) (Config, Sources, error) {
	st, err := s.loadStored(ctx)
	if err != nil {
		return Config{}, Sources{}, err
	}
	return s.withEnv(s.fromStored(st)), s.sources(), nil
}

func knownEvents(list []string) []string {
	out := make([]string, 0, len(Events))
	for _, kind := range Events {
		for _, e := range list {
			if e == kind {
				out = append(out, kind)
				break
			}
		}
	}
	return out
}

func normalizeEvents(list []string) ([]string, error) {
	for _, e := range list {
		found := false
		for _, kind := range Events {
			if e == kind {
				found = true
				break
			}
		}
		if !found {
			return nil, invalid(CodeBadEvent, "unknown notification event "+quote(e))
		}
	}
	return knownEvents(list), nil
}

func quote(s string) string {
	raw, _ := json.Marshal(cleanText(s, 40))
	return string(raw)
}

func ValidChatID(chat string) bool {
	return chatIDPattern.MatchString(chat)
}

func ValidBotToken(token string) bool {
	return botTokenPattern.MatchString(token)
}

func localHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

func ValidateWebhookURL(raw string) error {
	if raw == "" {
		return invalid(CodeBadURL, "the webhook URL is empty")
	}
	if len(raw) > MaxWebhookURLLength {
		return invalid(CodeBadURL, "the webhook URL is longer than 500 characters")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Hostname() == "" || u.Opaque != "" {
		return invalid(CodeBadURL, "the webhook URL does not parse as an absolute http(s) URL")
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
	case "http":
		if !localHost(u.Hostname()) {
			return invalid(CodeBadURL, "the webhook URL must use https unless it points to a loopback or private address")
		}
	default:
		return invalid(CodeBadURL, "the webhook URL must use http or https")
	}
	return nil
}

func (s *Service) Save(ctx context.Context, u Update) (Config, error) {
	s.init()
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.loadStored(ctx)
	if err != nil {
		return Config{}, err
	}
	prev := s.fromStored(st)
	src := s.sources()
	next := prev

	next.Telegram.Enabled = u.TelegramEnabled
	if !src.TelegramChat {
		chat := strings.TrimSpace(u.TelegramChatID)
		if chat != "" && !ValidChatID(chat) {
			return Config{}, invalid(CodeBadChatID, "the chat id must be a number or an @channel name")
		}
		next.Telegram.ChatID = chat
	}
	if !src.TelegramToken {
		switch {
		case u.ClearTelegramToken:
			next.Telegram.Token = ""
		case u.TelegramToken != nil:
			token := strings.TrimSpace(*u.TelegramToken)
			if token != "" && !ValidBotToken(token) {
				return Config{}, invalid(CodeBadToken, "the bot token does not look like a Telegram bot token")
			}
			next.Telegram.Token = token
		}
	}

	next.Webhook.Enabled = u.WebhookEnabled
	if !src.WebhookURL {
		switch {
		case u.ClearWebhookURL:
			next.Webhook.URL = ""
		case u.WebhookURL != nil:
			raw := strings.TrimSpace(*u.WebhookURL)
			if raw != "" {
				if err := ValidateWebhookURL(raw); err != nil {
					return Config{}, err
				}
			}
			next.Webhook.URL = raw
		}
	}
	if !src.WebhookSecret {
		switch {
		case u.ClearWebhookSecret:
			next.Webhook.Secret = ""
		case u.WebhookSecret != nil:
			secret := strings.TrimSpace(*u.WebhookSecret)
			if len(secret) > MaxWebhookSecretLength {
				return Config{}, invalid(CodeBadSecret, "the webhook secret is longer than 256 bytes")
			}
			next.Webhook.Secret = secret
		}
	}

	if u.Events != nil {
		events, err := normalizeEvents(u.Events)
		if err != nil {
			return Config{}, err
		}
		next.Events = events
	}
	digest := u.DigestSeconds
	if digest == 0 {
		digest = DefaultDigest
	}
	if digest < MinDigest || digest > MaxDigest {
		return Config{}, invalid(CodeBadDigest, "the digest interval must be between 30 and 3600 seconds")
	}
	next.DigestSeconds = digest
	switch u.Language {
	case "":
		next.Language = LanguageEN
	case LanguageEN, LanguageRU:
		next.Language = u.Language
	default:
		return Config{}, invalid(CodeBadLanguage, "the message language must be en or ru")
	}

	if err := checkEffective(s.withEnv(next), src); err != nil {
		return Config{}, err
	}

	sealed, err := s.toStored(next)
	if err != nil {
		return Config{}, err
	}
	raw, err := json.Marshal(sealed)
	if err != nil {
		return Config{}, err
	}
	if err := s.jumpCursors(ctx, prev, next); err != nil {
		return Config{}, err
	}
	if err := s.Store.SetMeta(ctx, metaConfig, string(raw)); err != nil {
		return Config{}, err
	}
	for _, channel := range Channels {
		delete(s.states, channel)
	}
	return s.withEnv(s.fromStored(sealed)), nil
}

func checkEffective(cfg Config, src Sources) error {
	if cfg.Telegram.Enabled {
		if cfg.Telegram.Token == "" || cfg.Telegram.ChatID == "" {
			return invalid(CodeIncomplete, "Telegram needs a bot token and a chat id before it can be enabled")
		}
		if !ValidChatID(cfg.Telegram.ChatID) {
			return invalid(CodeBadChatID, "the chat id"+fromEnv(src.TelegramChat, EnvTelegramChat)+" must be a number or an @channel name")
		}
		if !ValidBotToken(cfg.Telegram.Token) {
			return invalid(CodeBadToken, "the bot token"+fromEnv(src.TelegramToken, EnvTelegramToken)+" does not look like a Telegram bot token")
		}
	}
	if cfg.Webhook.Enabled {
		if cfg.Webhook.URL == "" {
			return invalid(CodeIncomplete, "the webhook needs a URL before it can be enabled")
		}
		if err := ValidateWebhookURL(cfg.Webhook.URL); err != nil {
			return invalid(CodeBadURL, err.Error()+fromEnv(src.WebhookURL, EnvWebhookURL))
		}
	}
	return nil
}

func fromEnv(set bool, name string) string {
	if !set {
		return ""
	}
	return " (from " + name + ")"
}

func (s *Service) jumpCursors(ctx context.Context, prev, next Config) error {
	jumps := map[string][]string{}
	for _, channel := range Channels {
		if !next.ChannelEnabled(channel) {
			continue
		}
		if !prev.ChannelEnabled(channel) {
			jumps[channel] = append([]string(nil), store.NotifySources...)
			continue
		}
		for _, kind := range next.Events {
			if !prev.EventEnabled(kind) {
				jumps[channel] = append(jumps[channel], kind)
			}
		}
	}
	if len(jumps) == 0 {
		return nil
	}
	heads, err := s.Store.NotifyMaxIDs(ctx)
	if err != nil {
		return err
	}
	for channel, kinds := range jumps {
		if len(kinds) == 0 {
			continue
		}
		ids := make(map[string]int64, len(kinds))
		for _, kind := range kinds {
			ids[kind] = heads[kind]
		}
		if err := s.Store.AdvanceNotifyCursors(ctx, channel, ids); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) loadStatus(ctx context.Context) (map[string]ChannelStatus, error) {
	out := map[string]ChannelStatus{}
	raw, err := s.Store.Meta(ctx, metaStatus)
	if err != nil || raw == "" {
		return out, err
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return map[string]ChannelStatus{}, nil
	}
	return out, nil
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	channels, err := s.loadStatus(ctx)
	if err != nil {
		return Status{}, err
	}
	out := Status{Channels: channels}
	for _, cs := range channels {
		if cs.LastOKAt.After(out.LastOKAt) {
			out.LastOKAt = cs.LastOKAt
		}
		if cs.LastErrorAt.After(out.LastErrorAt) {
			out.LastErrorAt = cs.LastErrorAt
			out.LastError = cs.LastError
		}
		out.Failures += cs.Failures
		out.SentTotal += cs.SentTotal
	}
	return out, nil
}

func (s *Service) record(ctx context.Context, channel string, at time.Time, sendErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	channels, err := s.loadStatus(ctx)
	if err != nil {
		log.Printf("notify: status: %v", err)
		return
	}
	cs := channels[channel]
	if sendErr == nil {
		cs.LastOKAt = at.UTC()
		cs.SentTotal++
		cs.Failures = 0
	} else {
		cs.LastError = cleanText(sendErr.Error(), 300)
		cs.LastErrorAt = at.UTC()
		cs.Failures++
	}
	channels[channel] = cs
	raw, err := json.Marshal(channels)
	if err != nil {
		return
	}
	if err := s.Store.SetMeta(ctx, metaStatus, string(raw)); err != nil {
		log.Printf("notify: status: %v", err)
	}
}

func (s *Service) Test(ctx context.Context, channel string) error {
	if channel != ChannelTelegram && channel != ChannelWebhook {
		return invalid(CodeBadChannel, "unknown notification channel "+quote(channel))
	}
	cfg, _, err := s.Load(ctx)
	if err != nil {
		return err
	}
	if !cfg.Configured(channel) {
		if channel == ChannelTelegram {
			return invalid(CodeIncomplete, "Telegram needs a bot token and a chat id before a test message can be sent")
		}
		return invalid(CodeIncomplete, "the webhook needs a URL before a test message can be sent")
	}
	now := s.now()
	err = s.sendTest(ctx, cfg, channel, now)
	s.record(ctx, channel, now, err)
	return err
}
