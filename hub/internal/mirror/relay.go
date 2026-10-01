package mirror

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/daniellavrushin/b4hub/internal/catalogue"
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
	MaxQueuedRecord       = 8 << 10
	ClientRequestsPerHour = 600
	QueuedPerClientPerDay = 200
	QueuedPerKeyPerDay    = 40
	DrainBudget           = 100
	MaxHops               = 8
	knownKeysLimit        = 100000
	answerLimit           = 64 << 10
	queueFileSuffix       = ".json"
	deferDefault          = time.Hour

	HeaderVia  = "B4hub-Via"
	HeaderNode = "B4hub-Node"

	CodeHubUnreachable = "hub_unreachable"
	CodeHubBusy        = "hub_busy"
	CodeRelayLoop      = "relay_loop"
	CodeQueueFull      = "queue_full"

	scopeClient      = "relay-client"
	scopeClientNote  = "relay-client-note"
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
	Identity    *hubwire.Identity

	once     sync.Once
	limiter  *ratelimit.Limiter
	secret   []byte
	breaker  breaker
	warnOnce sync.Once

	mu          sync.Mutex
	deferred    map[string]time.Time
	inflight    map[string]int
	known       map[string]struct{}
	pausedUntil time.Time
	newKeyWall  time.Time
}

func (r *Relay) init() {
	r.once.Do(func() {
		r.limiter = ratelimit.New(r.now)
		r.secret = make([]byte, 32)
		_, _ = rand.Read(r.secret)
		r.deferred = make(map[string]time.Time)
		r.inflight = make(map[string]int)
		r.known = make(map[string]struct{})
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

func (r *Relay) Prepare() {
	if err := os.Chmod(r.Dir, 0o700); err != nil {
		return
	}
	entries, err := os.ReadDir(r.Dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), queueFileSuffix) {
			_ = os.Chmod(filepath.Join(r.Dir, e.Name()), 0o600)
		}
	}
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
	if r.Identity != nil {
		req.Header.Set(hubdata.HeaderRelay, hubdata.SignRelay(r.Identity, r.now(), raw))
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

func bodyHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (r *Relay) enter(hash string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.inflight[hash] > 0 {
		return false
	}
	r.inflight[hash]++
	return true
}

func (r *Relay) leave(hash string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.inflight[hash] <= 1 {
		delete(r.inflight, hash)
		return
	}
	r.inflight[hash]--
}

func (r *Relay) learn(key string) {
	if key == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.known) >= knownKeysLimit {
		r.known = make(map[string]struct{})
	}
	r.known[key] = struct{}{}
}

func (r *Relay) knows(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.known[key]
	return ok
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
	return ip != nil && ip.IsGlobalUnicast() && catalogue.RoutableIP(ip)
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

func addressScope(scope string) bool {
	return scope == "" || scope == ratelimit.ScopeRequest
}

func (r *Relay) pauseDrain(until time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if until.After(r.pausedUntil) {
		r.pausedUntil = until
	}
}

func (r *Relay) wallNewKeys(until time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if until.After(r.newKeyWall) {
		r.newKeyWall = until
	}
}

func (r *Relay) limitClient(client net.IP, now time.Time) *Answer {
	if !publicClient(client) {
		if client != nil {
			r.warnOnce.Do(func() {
				log.Printf("relay: records arrive from %s, which is not a public address; per-client limits apply only when --trusted-proxies names the reverse proxy in front of this mirror", client)
			})
		}
		return nil
	}
	key := ratelimit.AddressKey(r.secret, client, now)
	ok, wait := r.limiter.Allow(scopeClient, key, ClientRequestsPerHour, ratelimit.Hour)
	if ok {
		return nil
	}
	if first, _ := r.limiter.Allow(scopeClientNote, key, 1, ratelimit.Hour); first {
		log.Printf("relay: %s sent more than %d records this hour and is refused for %s", ratelimit.Prefix(client), ClientRequestsPerHour, wait.Round(time.Second))
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

type inspected struct {
	rec   hubwire.Record
	known bool
}

func inspect(raw []byte) (*inspected, *Answer) {
	var head struct {
		V json.RawMessage `json:"v"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, failAnswer(http.StatusBadRequest, ingest.CodeBadRecord, "record does not decode: "+err.Error())
	}
	version := strings.TrimSpace(string(head.V))
	if version == "" || version == "null" {
		return nil, failAnswer(http.StatusBadRequest, ingest.CodeBadRecord, "record carries no wire version")
	}
	if version != strconv.Itoa(hubwire.WireVersion) {
		return &inspected{}, nil
	}
	out := &inspected{known: true}
	if err := json.Unmarshal(raw, &out.rec); err != nil {
		return nil, failAnswer(http.StatusBadRequest, ingest.CodeBadRecord, "record does not decode: "+err.Error())
	}
	if _, err := hubwire.VerifyRecord(&out.rec); err != nil {
		switch {
		case errors.Is(err, hubwire.ErrUnknownKind):
			out.known = false
			return out, nil
		case errors.Is(err, hubwire.ErrBadSignature), errors.Is(err, hubwire.ErrBadKey):
			return nil, failAnswer(http.StatusBadRequest, ingest.CodeBadSignature, err.Error())
		default:
			return nil, failAnswer(http.StatusBadRequest, ingest.CodeBadRecord, err.Error())
		}
	}
	if !ingest.CanonicalEncoding(&out.rec) {
		return nil, failAnswer(http.StatusBadRequest, ingest.CodeBadSignature, "the key or the signature is not in its canonical encoding")
	}
	return out, nil
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
	seen, refused := inspect(raw)
	if refused != nil {
		return refused
	}
	hash := bodyHash(raw)
	if !r.enter(hash) {
		return failAnswer(http.StatusLoopDetected, CodeRelayLoop, "the same record is already on its way through this mirror")
	}
	defer r.leave(hash)
	hold := seen.known && queueable(seen.rec.Kind)
	ok, probe := r.breaker.allow(now)
	if !ok {
		return r.hold(seen, raw, hold, client, now, "the hub has not been answering since "+r.breaker.since().UTC().Format(time.RFC3339))
	}
	answer, err := r.forward(context.WithoutCancel(ctx), raw, via, r.liveTimeout())
	if reason, down := unreachable(answer, err); down {
		r.breaker.failure(now, r.now(), probe)
		return r.hold(seen, raw, hold, client, now, reason)
	}
	r.breaker.success()
	if answer.Status == http.StatusTooManyRequests {
		scope, wait := limitOf(answer)
		switch {
		case addressScope(scope):
			r.pauseDrain(r.now().Add(wait))
			return r.busy(seen, raw, hold, client, now, wait)
		case scope == ratelimit.ScopeNewKey:
			r.wallNewKeys(r.now().Add(wait))
		}
	}
	if seen.known && answer.Status >= 200 && answer.Status < 300 {
		r.learn(seen.rec.Key)
	}
	return answer
}

func (r *Relay) hold(seen *inspected, raw []byte, queue bool, client net.IP, now time.Time, reason string) *Answer {
	if queue {
		if answer := r.queue(&seen.rec, raw, client, now, reason); answer != nil {
			return answer
		}
	}
	return retryIn(failAnswer(http.StatusBadGateway, CodeHubUnreachable, "the hub cannot be reached: "+reason), breakerMinBackoff)
}

func (r *Relay) busy(seen *inspected, raw []byte, queue bool, client net.IP, now time.Time, wait time.Duration) *Answer {
	if queue {
		if answer := r.queue(&seen.rec, raw, client, now, "the hub limits this mirror for "+wait.Round(time.Second).String()); answer != nil {
			return answer
		}
	}
	return retryIn(failAnswer(http.StatusServiceUnavailable, CodeHubBusy, "the hub accepts no more records from this mirror for now; another address may still take them"), wait)
}

func (r *Relay) queue(rec *hubwire.Record, raw []byte, client net.IP, now time.Time, reason string) *Answer {
	limit := r.queueLimit()
	if limit == 0 || len(raw) > MaxQueuedRecord {
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
	log.Printf("relay: %s, queued %s %s", reason, rec.Kind, id[:12])
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

func (r *Relay) walls() (time.Time, time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pausedUntil, r.newKeyWall
}

func recordKey(raw []byte) string {
	var head struct {
		Key string `json:"key"`
	}
	_ = json.Unmarshal(raw, &head)
	return head.Key
}

func (r *Relay) Retry(ctx context.Context) error {
	r.init()
	now := r.now()
	paused, wall := r.walls()
	if now.Before(paused) {
		return fmt.Errorf("delivery paused by the hub's limit for this mirror until %s, %d records queued", paused.UTC().Format(time.RFC3339), r.Queued())
	}
	files, err := r.queuedFiles()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	sent := 0
	probe := false
	took := false
	defer func() {
		if took && probe && sent == 0 {
			r.breaker.release()
		}
	}()
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
		key := recordKey(raw)
		if now.Before(wall) && !r.knows(key) {
			continue
		}
		if !took {
			ok, p := r.breaker.allow(now)
			if !ok {
				return fmt.Errorf("delivery paused, the hub has not been answering since %s, %d records queued", r.breaker.since().UTC().Format(time.RFC3339), len(files)-i)
			}
			took, probe = true, p
		}
		hash := bodyHash(raw)
		if !r.enter(hash) {
			continue
		}
		start := r.now()
		answer, err := r.forward(ctx, raw, "", r.timeout())
		r.leave(hash)
		sent++
		if reason, down := unreachable(answer, err); down {
			r.breaker.failure(start, r.now(), probe && sent == 1)
			return fmt.Errorf("delivery paused (%s), %d records still queued", reason, len(files)-i)
		}
		r.breaker.success()
		if answer.Status == http.StatusTooManyRequests {
			scope, wait := limitOf(answer)
			switch {
			case addressScope(scope):
				r.pauseDrain(r.now().Add(wait))
				return fmt.Errorf("delivery paused by the hub's limit for this mirror for %s, %d records still queued", wait.Round(time.Second), len(files)-i)
			case scope == ratelimit.ScopeNewKey:
				wall = r.now().Add(wait)
				r.wallNewKeys(wall)
			}
			r.setDeferred(f.name, r.now().Add(wait))
			continue
		}
		if answer.Status >= http.StatusInternalServerError {
			return fmt.Errorf("delivery paused (upstream answered %d), %d records still queued", answer.Status, len(files)-i)
		}
		if answer.Status >= 200 && answer.Status < 300 {
			r.learn(key)
		}
		_ = os.Remove(path)
		r.setDeferred(f.name, time.Time{})
		log.Printf("relay: delivered queued %s: %d %s", f.name, answer.Status, strings.TrimSpace(string(answer.Body)))
	}
	return nil
}
