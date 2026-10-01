package mirror

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/ingest"
	"github.com/daniellavrushin/b4hub/internal/ratelimit"
)

const (
	RelayDir              = "relay"
	RelayTimeout          = 30 * time.Second
	LiveTimeout           = 15 * time.Second
	QueueMaxAge           = 30 * 24 * time.Hour
	DefaultQueueLimit     = 5000
	MaxSmallRecord        = 8 << 10
	ClientRequestsPerHour = 600
	QueuedPerClientPerDay = 200
	QueuedPerKeyPerDay    = 40
	DrainBudget           = 500
	MaxHops               = 8
	answerLimit           = 64 << 10
	queueFileSuffix       = ".json"
	deferDefault          = time.Hour

	HeaderVia  = "B4hub-Via"
	HeaderNode = "B4hub-Node"

	CodeHubUnreachable = "hub_unreachable"
	CodeRelayLoop      = "relay_loop"
	CodeQueueFull      = "queue_full"

	scopeClient      = "relay-client"
	scopeQueueClient = "queue-client"
	scopeQueueKey    = "queue-key"
)

type Answer struct {
	Status      int
	ContentType string
	RetryAfter  string
	Body        []byte
}

func jsonAnswer(status int, body map[string]interface{}) *Answer {
	raw, _ := json.Marshal(body)
	return &Answer{Status: status, ContentType: "application/json", Body: raw}
}

func failAnswer(status int, code, message string) *Answer {
	return jsonAnswer(status, map[string]interface{}{"code": code, "error": message})
}

func retryIn(a *Answer, wait time.Duration) *Answer {
	seconds := int(math.Ceil(wait.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	a.RetryAfter = strconv.Itoa(seconds)
	return a
}

type Relay struct {
	Upstream    string
	Dir         string
	Client      *http.Client
	Now         func() time.Time
	Timeout     time.Duration
	LiveTimeout time.Duration
	MaxAge      time.Duration
	QueueLimit  int
	Node        string

	once     sync.Once
	limiter  *ratelimit.Limiter
	secret   []byte
	breaker  breaker
	warnOnce sync.Once

	mu          sync.Mutex
	deferred    map[string]time.Time
	pausedUntil time.Time
}

func (r *Relay) init() {
	r.once.Do(func() {
		r.limiter = ratelimit.New(r.now)
		r.secret = make([]byte, 32)
		_, _ = rand.Read(r.secret)
		r.deferred = make(map[string]time.Time)
	})
}

func (r *Relay) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Relay) client() *http.Client {
	if r.Client != nil {
		return r.Client
	}
	return http.DefaultClient
}

func (r *Relay) timeout() time.Duration {
	if r.Timeout <= 0 {
		return RelayTimeout
	}
	return r.Timeout
}

func (r *Relay) liveTimeout() time.Duration {
	if r.LiveTimeout <= 0 {
		return LiveTimeout
	}
	return r.LiveTimeout
}

func (r *Relay) maxAge() time.Duration {
	if r.MaxAge <= 0 {
		return QueueMaxAge
	}
	return r.MaxAge
}

func (r *Relay) queueLimit() int {
	switch {
	case r.QueueLimit < 0:
		return 0
	case r.QueueLimit == 0:
		return DefaultQueueLimit
	}
	return r.QueueLimit
}

func (r *Relay) Forward(ctx context.Context, raw []byte) (*Answer, error) {
	return r.forward(ctx, raw, "", r.timeout())
}

func (r *Relay) forward(ctx context.Context, raw []byte, via string, timeout time.Duration) (*Answer, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.Upstream+hubwire.PathMessage, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", ingest.RelayAgent)
	if hops := joinVia(via, r.Node); hops != "" {
		req.Header.Set(HeaderVia, hops)
	}
	resp, err := r.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, answerLimit))
	if err != nil {
		return nil, err
	}
	return &Answer{Status: resp.StatusCode, ContentType: resp.Header.Get("Content-Type"), RetryAfter: resp.Header.Get("Retry-After"), Body: body}, nil
}

func joinVia(via, node string) string {
	via = strings.TrimSpace(via)
	switch {
	case node == "":
		return via
	case via == "":
		return node
	}
	return via + "," + node
}

func (r *Relay) looped(via string) bool {
	hops := 0
	for _, hop := range strings.Split(via, ",") {
		hop = strings.TrimSpace(hop)
		if hop == "" {
			continue
		}
		hops++
		if r.Node != "" && hop == r.Node {
			return true
		}
	}
	return hops >= MaxHops
}

func unreachable(a *Answer, err error) (string, bool) {
	if err != nil {
		return err.Error(), true
	}
	switch a.Status {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return fmt.Sprintf("upstream answered %d", a.Status), true
	}
	return "", false
}

func queueable(kind string) bool {
	return kind == hubwire.RecordVote || kind == hubwire.RecordReport
}

func publicClient(ip net.IP) bool {
	return ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate()
}

func (r *Relay) limitClient(client net.IP, now time.Time) *Answer {
	if !publicClient(client) {
		if client != nil {
			r.warnOnce.Do(func() {
				log.Printf("relay: records arrive from %s, a private address; per-client limits apply only when --trusted-proxies names the reverse proxy in front of this mirror", client)
			})
		}
		return nil
	}
	ok, wait := r.limiter.Allow(scopeClient, ratelimit.AddressKey(r.secret, client, now), ClientRequestsPerHour, ratelimit.Hour)
	if ok {
		return nil
	}
	seconds := int(math.Ceil(wait.Seconds()))
	return retryIn(jsonAnswer(http.StatusTooManyRequests, map[string]interface{}{
		"code":        ingest.CodeRateLimited,
		"error":       "this mirror accepts at most " + strconv.Itoa(ClientRequestsPerHour) + " records per hour from one network; try again in " + strconv.Itoa(seconds) + "s",
		"retry_after": seconds,
		"scope":       ratelimit.ScopeRequest,
		"limit":       ClientRequestsPerHour,
		"window":      "hour",
	}), wait)
}

func (r *Relay) Handle(ctx context.Context, raw []byte, client net.IP, via string) *Answer {
	r.init()
	if r.looped(via) {
		return failAnswer(http.StatusLoopDetected, CodeRelayLoop, "the record has already passed through this mirror")
	}
	now := r.now()
	if limited := r.limitClient(client, now); limited != nil {
		return limited
	}
	var rec hubwire.Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		return failAnswer(http.StatusBadRequest, ingest.CodeBadRecord, "record does not decode: "+err.Error())
	}
	known := true
	if _, err := hubwire.VerifyRecord(&rec); err != nil {
		switch {
		case errors.Is(err, hubwire.ErrUnknownKind), errors.Is(err, hubwire.ErrWireVersion):
			known = false
		case errors.Is(err, hubwire.ErrBadSignature), errors.Is(err, hubwire.ErrBadKey):
			return failAnswer(http.StatusBadRequest, ingest.CodeBadSignature, err.Error())
		default:
			return failAnswer(http.StatusBadRequest, ingest.CodeBadRecord, err.Error())
		}
	}
	if known && !ingest.CanonicalEncoding(&rec) {
		return failAnswer(http.StatusBadRequest, ingest.CodeBadSignature, "the key or the signature is not in its canonical encoding")
	}
	if known && rec.Kind != hubwire.RecordShare && len(raw) > MaxSmallRecord {
		return failAnswer(http.StatusRequestEntityTooLarge, ingest.CodeTooLarge, "a "+rec.Kind+" record is never larger than "+strconv.Itoa(MaxSmallRecord)+" bytes")
	}
	hold := known && queueable(rec.Kind)
	if !r.breaker.allow(now) {
		return r.hold(&rec, raw, hold, client, now, "the hub has not been answering since "+r.breaker.since().UTC().Format(time.RFC3339))
	}
	answer, err := r.forward(context.WithoutCancel(ctx), raw, via, r.liveTimeout())
	if reason, down := unreachable(answer, err); down {
		r.breaker.failure(r.now())
		return r.hold(&rec, raw, hold, client, now, reason)
	}
	r.breaker.success()
	return answer
}

func (r *Relay) hold(rec *hubwire.Record, raw []byte, queue bool, client net.IP, now time.Time, reason string) *Answer {
	if queue {
		if answer := r.queue(rec, raw, client, now, reason); answer != nil {
			return answer
		}
	}
	return retryIn(failAnswer(http.StatusBadGateway, CodeHubUnreachable, "the hub cannot be reached: "+reason), breakerMinBackoff)
}

func (r *Relay) queue(rec *hubwire.Record, raw []byte, client net.IP, now time.Time, reason string) *Answer {
	limit := r.queueLimit()
	if limit == 0 {
		return nil
	}
	id := rec.ID()
	accepted := jsonAnswer(http.StatusAccepted, map[string]interface{}{"id": id, "kind": rec.Kind, "queued": true})
	if _, err := os.Stat(r.queuePath(id)); err == nil {
		return accepted
	}
	if r.Queued() >= limit {
		return retryIn(failAnswer(http.StatusServiceUnavailable, CodeQueueFull, "the hub cannot be reached and this mirror's queue is full"), breakerMaxBackoff)
	}
	if publicClient(client) {
		if ok, _ := r.limiter.Allow(scopeQueueClient, ratelimit.AddressKey(r.secret, client, now), QueuedPerClientPerDay, ratelimit.Day); !ok {
			return nil
		}
	}
	if ok, _ := r.limiter.Allow(scopeQueueKey, rec.Key, QueuedPerKeyPerDay, ratelimit.Day); !ok {
		return nil
	}
	if err := r.enqueue(id, raw); err != nil {
		return failAnswer(http.StatusInternalServerError, ingest.CodeInternal, err.Error())
	}
	log.Printf("relay: hub unreachable (%s), queued %s %s", reason, rec.Kind, id[:12])
	return accepted
}

func (r *Relay) queuePath(id string) string {
	return filepath.Join(r.Dir, id+queueFileSuffix)
}

func (r *Relay) enqueue(id string, raw []byte) error {
	if err := os.MkdirAll(r.Dir, 0o700); err != nil {
		return err
	}
	path := r.queuePath(id)
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return hubdata.WriteFileAtomic(path, raw, 0o600)
}

type queuedFile struct {
	name    string
	modTime time.Time
}

func (r *Relay) queuedFiles() ([]queuedFile, error) {
	entries, err := os.ReadDir(r.Dir)
	if err != nil {
		return nil, err
	}
	files := make([]queuedFile, 0, len(entries))
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), queueFileSuffix) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, queuedFile{name: e.Name(), modTime: info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool {
		if !files[i].modTime.Equal(files[j].modTime) {
			return files[i].modTime.Before(files[j].modTime)
		}
		return files[i].name < files[j].name
	})
	return files, nil
}

func (r *Relay) Queued() int {
	entries, err := os.ReadDir(r.Dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), queueFileSuffix) {
			n++
		}
	}
	return n
}

func limitOf(a *Answer) (string, time.Duration) {
	var body struct {
		Scope      string `json:"scope"`
		RetryAfter int    `json:"retry_after"`
	}
	_ = json.Unmarshal(a.Body, &body)
	wait := time.Duration(body.RetryAfter) * time.Second
	if wait <= 0 {
		if seconds, err := strconv.Atoi(strings.TrimSpace(a.RetryAfter)); err == nil && seconds > 0 {
			wait = time.Duration(seconds) * time.Second
		}
	}
	if wait <= 0 {
		wait = deferDefault
	}
	return body.Scope, wait
}

func (r *Relay) deferredUntil(name string) time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.deferred[name]
}

func (r *Relay) setDeferred(name string, until time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if until.IsZero() {
		delete(r.deferred, name)
		return
	}
	r.deferred[name] = until
}

func (r *Relay) Retry(ctx context.Context) error {
	r.init()
	now := r.now()
	r.mu.Lock()
	paused := r.pausedUntil
	r.mu.Unlock()
	if now.Before(paused) {
		return fmt.Errorf("delivery paused by the hub's request limit until %s, %d records queued", paused.UTC().Format(time.RFC3339), r.Queued())
	}
	if r.breaker.open(now) {
		return fmt.Errorf("delivery paused, the hub has not been answering since %s, %d records queued", r.breaker.since().UTC().Format(time.RFC3339), r.Queued())
	}
	files, err := r.queuedFiles()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	sent := 0
	for i, f := range files {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		path := filepath.Join(r.Dir, f.name)
		if now.Sub(f.modTime) > r.maxAge() {
			_ = os.Remove(path)
			r.setDeferred(f.name, time.Time{})
			log.Printf("relay: dropped %s, queued for more than %s", f.name, r.maxAge())
			continue
		}
		if now.Before(r.deferredUntil(f.name)) {
			continue
		}
		if sent >= DrainBudget {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		answer, err := r.forward(ctx, raw, "", r.timeout())
		sent++
		if reason, down := unreachable(answer, err); down {
			r.breaker.failure(r.now())
			return fmt.Errorf("delivery paused (%s), %d records still queued", reason, len(files)-i)
		}
		r.breaker.success()
		if answer.Status == http.StatusTooManyRequests {
			scope, wait := limitOf(answer)
			if scope == "" || scope == ratelimit.ScopeRequest {
				r.mu.Lock()
				r.pausedUntil = r.now().Add(wait)
				r.mu.Unlock()
				return fmt.Errorf("delivery paused by the hub's request limit for %s, %d records still queued", wait, len(files)-i)
			}
			r.setDeferred(f.name, r.now().Add(wait))
			continue
		}
		if answer.Status >= http.StatusInternalServerError {
			return fmt.Errorf("delivery paused (upstream answered %d), %d records still queued", answer.Status, len(files)-i)
		}
		_ = os.Remove(path)
		r.setDeferred(f.name, time.Time{})
		log.Printf("relay: delivered queued %s: %d %s", f.name, answer.Status, strings.TrimSpace(string(answer.Body)))
	}
	return nil
}
