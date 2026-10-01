package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	TickInterval   = 30 * time.Second
	BackoffMin     = 30 * time.Second
	BackoffMax     = 30 * time.Minute
	RequestTimeout = 10 * time.Second

	LinesPerKind = 10

	retryAfterMax  = 24 * time.Hour
	replyReadLimit = 64 << 10
	userAgent      = "b4hub"

	HeaderTimestamp = "X-B4hub-Timestamp"
	HeaderSignature = "X-B4hub-Signature"
)

var defaultClient = &http.Client{
	Timeout: RequestTimeout,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

type channelState struct {
	lastSent    time.Time
	nextAttempt time.Time
	backoff     time.Duration
}

type sendError struct {
	message    string
	retryAfter time.Duration
}

func (e *sendError) Error() string {
	return e.message
}

type digest struct {
	batches []store.NotifyBatch
}

func (d digest) total() int {
	n := 0
	for _, b := range d.batches {
		n += b.Count
	}
	return n
}

func (d digest) maxIDs() map[string]int64 {
	out := make(map[string]int64, len(d.batches))
	for _, b := range d.batches {
		out[b.Kind] = b.MaxID
	}
	return out
}

func (s *Service) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return defaultClient
}

func (s *Service) telegramBase() string {
	base := strings.TrimSpace(s.TelegramBase)
	if base == "" {
		base = DefaultTelegramBase
	}
	return strings.TrimRight(base, "/")
}

func (s *Service) publicURL() string {
	return strings.TrimRight(strings.TrimSpace(s.PublicURL), "/")
}

func (s *Service) consoleURL(path string) string {
	base := s.publicURL()
	if base == "" {
		return ""
	}
	return base + "/admin" + path
}

var consolePaths = map[string]string{
	EventShare:       "/queue",
	EventReport:      "/feedback?tab=reports",
	EventAutoHide:    "/feedback?tab=reports",
	EventBuildFailed: "/catalogue",
	EventMirror:      "/mirrors",
}

func (s *Service) digestConsole(d digest) string {
	if len(d.batches) == 0 {
		return ""
	}
	return s.consoleURL(consolePaths[d.batches[0].Kind])
}

func (s *Service) Run(ctx context.Context) {
	s.init()
	ticker := time.NewTicker(TickInterval)
	defer ticker.Stop()
	for {
		s.round(ctx)
		select {
		case <-ctx.Done():
			return
		case <-s.kick:
		case <-ticker.C:
		}
	}
}

func (s *Service) round(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	cfg, _, err := s.Load(ctx)
	if err != nil {
		log.Printf("notify: %v", err)
		return
	}
	for _, channel := range Channels {
		if !cfg.ChannelEnabled(channel) || !cfg.Configured(channel) {
			continue
		}
		if err := s.deliver(ctx, cfg, channel); err != nil && ctx.Err() == nil {
			log.Printf("notify: %v", err)
		}
	}
}

func (s *Service) state(channel string) *channelState {
	st, ok := s.states[channel]
	if !ok {
		st = &channelState{}
		s.states[channel] = st
	}
	return st
}

func (s *Service) due(channel string, now time.Time, window time.Duration) bool {
	s.init()
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state(channel)
	if now.Before(st.nextAttempt) {
		return false
	}
	return st.lastSent.IsZero() || !now.Before(st.lastSent.Add(window))
}

func (s *Service) settle(channel string, now time.Time, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state(channel)
	if err == nil {
		st.lastSent = now
		st.backoff = 0
		st.nextAttempt = time.Time{}
		return
	}
	switch {
	case st.backoff == 0:
		st.backoff = BackoffMin
	case st.backoff*2 > BackoffMax:
		st.backoff = BackoffMax
	default:
		st.backoff *= 2
	}
	wait := st.backoff
	var se *sendError
	if errors.As(err, &se) && se.retryAfter > wait {
		wait = se.retryAfter
		if wait > retryAfterMax {
			wait = retryAfterMax
		}
	}
	st.nextAttempt = now.Add(wait)
}

func (s *Service) cursors(ctx context.Context, channel string) (map[string]int64, error) {
	cursors, err := s.Store.NotifyCursors(ctx, channel)
	if err != nil {
		return nil, err
	}
	missing := map[string]int64{}
	var heads map[string]int64
	for _, kind := range store.NotifySources {
		if _, ok := cursors[kind]; ok {
			continue
		}
		if heads == nil {
			if heads, err = s.Store.NotifyMaxIDs(ctx); err != nil {
				return nil, err
			}
		}
		missing[kind] = heads[kind]
		cursors[kind] = heads[kind]
	}
	if err := s.Store.AdvanceNotifyCursors(ctx, channel, missing); err != nil {
		return nil, err
	}
	return cursors, nil
}

func (s *Service) collect(ctx context.Context, cfg Config, cursors map[string]int64) (digest, error) {
	var d digest
	for _, kind := range Events {
		if !cfg.EventEnabled(kind) {
			continue
		}
		b, err := s.Store.NotifyEvents(ctx, kind, cursors[kind], LinesPerKind)
		if err != nil {
			return digest{}, err
		}
		if b.Count > 0 {
			d.batches = append(d.batches, b)
		}
	}
	return d, nil
}

func (s *Service) deliver(ctx context.Context, cfg Config, channel string) error {
	now := s.now()
	if !s.due(channel, now, time.Duration(cfg.DigestSeconds)*time.Second) {
		return nil
	}
	cursors, err := s.cursors(ctx, channel)
	if err != nil {
		return err
	}
	d, err := s.collect(ctx, cfg, cursors)
	if err != nil {
		return err
	}
	if d.total() == 0 {
		return nil
	}
	sendErr := s.send(ctx, cfg, channel, d, now)
	if sendErr != nil && ctx.Err() != nil {
		return nil
	}
	s.settle(channel, now, sendErr)
	if sendErr == nil {
		if err := s.Store.AdvanceNotifyCursors(ctx, channel, d.maxIDs()); err != nil {
			return err
		}
	}
	s.record(ctx, channel, now, sendErr)
	return sendErr
}

func (s *Service) send(ctx context.Context, cfg Config, channel string, d digest, now time.Time) error {
	switch channel {
	case ChannelTelegram:
		return s.sendTelegram(ctx, cfg.Telegram, telegramText(cfg.Language, d, s.digestConsole(d)))
	case ChannelWebhook:
		body, err := json.Marshal(s.webhookDigest(cfg, d, now))
		if err != nil {
			return err
		}
		return s.postWebhook(ctx, cfg.Webhook, body, now)
	}
	return errors.New("unknown channel " + channel)
}

func (s *Service) sendTest(ctx context.Context, cfg Config, channel string, now time.Time) error {
	text := testText(cfg.Language)
	switch channel {
	case ChannelTelegram:
		return s.sendTelegram(ctx, cfg.Telegram, text)
	case ChannelWebhook:
		body, err := json.Marshal(WebhookPayload{
			Hub:     s.publicURL(),
			SentAt:  now.UTC().Format(time.RFC3339),
			Test:    true,
			Message: text,
			Counts:  map[string]int{},
			Events:  []WebhookEvent{},
		})
		if err != nil {
			return err
		}
		return s.postWebhook(ctx, cfg.Webhook, body, now)
	}
	return invalid(CodeBadChannel, "unknown notification channel "+quote(channel))
}

func scrub(err error, secrets ...string) string {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		err = uerr.Err
	}
	msg := err.Error()
	for _, secret := range secrets {
		if secret != "" {
			msg = strings.ReplaceAll(msg, secret, "***")
		}
	}
	return msg
}

type telegramMessage struct {
	ChatID                string `json:"chat_id"`
	Text                  string `json:"text"`
	DisableWebPagePreview bool   `json:"disable_web_page_preview"`
}

type telegramReply struct {
	OK          bool   `json:"ok"`
	ErrorCode   int    `json:"error_code"`
	Description string `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

func (s *Service) sendTelegram(ctx context.Context, t TelegramConfig, text string) error {
	body, err := json.Marshal(telegramMessage{ChatID: t.ChatID, Text: text, DisableWebPagePreview: true})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.telegramBase()+"/bot"+t.Token+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return &sendError{message: "telegram: " + scrub(err, t.Token)}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := s.client().Do(req)
	if err != nil {
		return &sendError{message: "telegram: " + scrub(err, t.Token)}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, replyReadLimit))
	var reply telegramReply
	_ = json.Unmarshal(raw, &reply)
	if resp.StatusCode == http.StatusOK && reply.OK {
		return nil
	}
	msg := "telegram: HTTP " + strconv.Itoa(resp.StatusCode)
	if desc := cleanText(reply.Description, 200); desc != "" {
		msg += ": " + desc
	}
	se := &sendError{message: strings.ReplaceAll(msg, t.Token, "***")}
	if reply.Parameters.RetryAfter > 0 {
		se.retryAfter = time.Duration(reply.Parameters.RetryAfter) * time.Second
	}
	return se
}

type WebhookEvent struct {
	Kind    string `json:"kind"`
	At      string `json:"at"`
	SetID   string `json:"set_id,omitempty"`
	Version int    `json:"version,omitempty"`
	Title   string `json:"title,omitempty"`
	Author  string `json:"author,omitempty"`
	ASN     string `json:"asn,omitempty"`
	Country string `json:"country,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Detail  string `json:"detail,omitempty"`
	Reports int    `json:"reports,omitempty"`
	URL     string `json:"url,omitempty"`
}

type WebhookPayload struct {
	Hub     string         `json:"hub"`
	SentAt  string         `json:"sent_at"`
	Test    bool           `json:"test,omitempty"`
	Message string         `json:"message,omitempty"`
	Counts  map[string]int `json:"counts"`
	Events  []WebhookEvent `json:"events"`
	More    int            `json:"more"`
	Console string         `json:"console,omitempty"`
}

func (s *Service) eventURL(e store.NotifyEvent) string {
	switch e.Kind {
	case EventMirror:
		return e.URL
	case EventBuildFailed:
		return s.consoleURL(consolePaths[EventBuildFailed])
	}
	if e.SetID == "" {
		return ""
	}
	return s.consoleURL("/sets/" + url.PathEscape(e.SetID) + "/v/" + strconv.Itoa(e.Version))
}

func (s *Service) webhookDigest(cfg Config, d digest, now time.Time) WebhookPayload {
	p := WebhookPayload{
		Hub:     s.publicURL(),
		SentAt:  now.UTC().Format(time.RFC3339),
		Counts:  map[string]int{},
		Events:  []WebhookEvent{},
		Console: s.digestConsole(d),
	}
	for _, kind := range cfg.Events {
		p.Counts[kind] = 0
	}
	for _, b := range d.batches {
		p.Counts[b.Kind] = b.Count
		for _, e := range b.Events {
			ev := WebhookEvent{
				Kind:    e.Kind,
				SetID:   e.SetID,
				Version: e.Version,
				Title:   cleanText(e.Title, 200),
				ASN:     cleanText(e.ASN, 20),
				Country: cleanText(e.Country, 4),
				Reason:  cleanText(e.Reason, 500),
				Detail:  cleanText(e.Detail, 100),
				Reports: e.Reports,
				URL:     s.eventURL(e),
			}
			if !e.At.IsZero() {
				ev.At = e.At.UTC().Format(time.RFC3339)
			}
			if e.KeyHMAC != "" {
				ev.Author = hubdata.AuthorLabel(e.KeyHMAC)
			}
			p.Events = append(p.Events, ev)
		}
		p.More += b.Count - len(b.Events)
	}
	return p
}

func Signature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte{'.'})
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func retryAfterHeader(raw string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func (s *Service) postWebhook(ctx context.Context, w WebhookConfig, body []byte, now time.Time) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
	if err != nil {
		return &sendError{message: "webhook: " + scrub(err, w.URL, w.Secret)}
	}
	timestamp := strconv.FormatInt(now.Unix(), 10)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set(HeaderTimestamp, timestamp)
	if w.Secret != "" {
		req.Header.Set(HeaderSignature, Signature(w.Secret, timestamp, body))
	}
	resp, err := s.client().Do(req)
	if err != nil {
		return &sendError{message: "webhook: " + scrub(err, w.URL, w.Secret)}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, replyReadLimit))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	se := &sendError{message: "webhook: HTTP " + strconv.Itoa(resp.StatusCode)}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
		se.retryAfter = retryAfterHeader(resp.Header.Get("Retry-After"))
	}
	return se
}
