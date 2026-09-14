package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/asn"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/ratelimit"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	MaxBodyBytes = 96 << 10

	CodeBadSignature      = "bad_signature"
	CodeBadRecord         = "bad_record"
	CodeInvalidSet        = "invalid_set"
	CodePayloadInvalid    = "payload_invalid"
	CodeUnknownSet        = "unknown_set"
	CodeNotActive         = "not_active"
	CodeFingerprint       = "fp_mismatch"
	CodeDuplicateStrategy = "duplicate_strategy"
	CodeBadMirrorURL      = "bad_mirror_url"
	CodeBanned            = "banned"
	CodeRateLimited       = "rate_limited"
	CodeTooLarge          = "too_large"
	CodeInternal          = "internal"
)

type Service struct {
	Store   *store.Store
	Blobs   hubdata.Blobs
	Secret  []byte
	Limiter *ratelimit.Limiter
	ASN     *asn.Resolver
	Now     func() time.Time
}

type Response struct {
	Status int
	Body   map[string]interface{}
}

func fail(status int, code, message string) Response {
	return Response{Status: status, Body: map[string]interface{}{"code": code, "error": message}}
}

var limitedActions = map[string]string{
	ratelimit.ScopeShare:   "shares",
	ratelimit.ScopeVote:    "votes",
	ratelimit.ScopeReport:  "reports",
	ratelimit.ScopeMirror:  "mirror announcements",
	ratelimit.ScopeNewKey:  "new keys from one network",
	ratelimit.ScopeRequest: "requests from one network",
}

func rateLimited(scope string, limit int, window, retryAfter time.Duration) Response {
	seconds := int(math.Ceil(retryAfter.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	per := "day"
	if window == ratelimit.Hour {
		per = "hour"
	}
	message := "the hub accepts at most " + strconv.Itoa(limit) + " " + limitedActions[scope] + " per " + per + "; try again in " + humanDuration(time.Duration(seconds)*time.Second)
	return Response{Status: http.StatusTooManyRequests, Body: map[string]interface{}{
		"code":        CodeRateLimited,
		"error":       message,
		"retry_after": seconds,
		"scope":       scope,
		"limit":       limit,
		"window":      per,
	}}
}

func humanDuration(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	switch {
	case h > 0 && m > 0:
		return strconv.Itoa(h) + "h " + strconv.Itoa(m) + "m"
	case h > 0:
		return strconv.Itoa(h) + "h"
	case m > 0:
		return strconv.Itoa(m) + "m"
	}
	return strconv.Itoa(int(d.Seconds())) + "s"
}

func internalError(err error) Response {
	return fail(http.StatusInternalServerError, CodeInternal, err.Error())
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

type origin struct {
	ASN     string
	Country string
}

func (o origin) verified() bool {
	return o.ASN != ""
}

func (s *Service) observe(ctx context.Context, peer net.IP) origin {
	if s.ASN == nil || peer == nil {
		return origin{}
	}
	info := s.ASN.Lookup(ctx, peer)
	return origin{ASN: info.ASN, Country: info.Country}
}

func (s *Service) Handle(ctx context.Context, raw []byte, peer net.IP) Response {
	now := s.now()
	if len(raw) > MaxBodyBytes {
		return fail(http.StatusRequestEntityTooLarge, CodeTooLarge, "record exceeds the message limit")
	}
	limits, err := s.Store.Settings(ctx)
	if err != nil {
		return internalError(err)
	}
	addressKey := ""
	if peer != nil {
		addressKey = ratelimit.AddressKey(s.Secret, peer, now)
		if ok, retry := s.Limiter.Allow(ratelimit.ScopeRequest, addressKey, limits.RequestsPerHour, ratelimit.Hour); !ok {
			return rateLimited(ratelimit.ScopeRequest, limits.RequestsPerHour, ratelimit.Hour, retry)
		}
	}
	var rec hubwire.Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		return fail(http.StatusBadRequest, CodeBadRecord, "record does not decode: "+err.Error())
	}
	if _, err := hubwire.VerifyRecord(&rec); err != nil {
		if errors.Is(err, hubwire.ErrBadSignature) || errors.Is(err, hubwire.ErrBadKey) {
			return fail(http.StatusBadRequest, CodeBadSignature, err.Error())
		}
		return fail(http.StatusBadRequest, CodeBadRecord, err.Error())
	}
	keyHMAC := hubdata.KeyHMAC(s.Secret, rec.Key)
	key, err := s.Store.GetKey(ctx, keyHMAC)
	switch {
	case errors.Is(err, store.ErrNotFound):
		if addressKey != "" {
			if ok, retry := s.Limiter.Allow(ratelimit.ScopeNewKey, addressKey, limits.NewKeysPerDay, ratelimit.Day); !ok {
				return rateLimited(ratelimit.ScopeNewKey, limits.NewKeysPerDay, ratelimit.Day, retry)
			}
		}
		key, _, err = s.Store.TouchKey(ctx, keyHMAC, now)
		if err != nil {
			return internalError(err)
		}
	case err != nil:
		return internalError(err)
	}
	if key.Banned {
		return fail(http.StatusForbidden, CodeBanned, "this key is banned")
	}
	recordID := rec.ID()
	if seen, err := s.Store.GetRecord(ctx, recordID); err == nil {
		body := map[string]interface{}{"id": recordID, "duplicate": true}
		if seen.SetID != "" {
			body["set_id"] = seen.SetID
			body["version"] = seen.Version
		}
		return Response{Status: http.StatusOK, Body: body}
	} else if !errors.Is(err, store.ErrNotFound) {
		return internalError(err)
	}
	scope, limit := ratelimit.ScopeShare, limits.SharesPerDay
	switch rec.Kind {
	case hubwire.RecordVote:
		scope, limit = ratelimit.ScopeVote, limits.VotesPerDay
	case hubwire.RecordReport:
		scope, limit = ratelimit.ScopeReport, limits.ReportsPerDay
	case hubwire.RecordMirror:
		scope, limit = ratelimit.ScopeMirror, limits.MirrorsPerDay
	}
	if !key.Trusted {
		if ok, retry := s.Limiter.Allow(scope, keyHMAC, limit, ratelimit.Day); !ok {
			return rateLimited(scope, limit, ratelimit.Day, retry)
		}
	}
	observed := s.observe(ctx, peer)
	entry := record{rec: &rec, id: recordID, keyHMAC: keyHMAC, origin: observed, now: now}
	resp := s.dispatch(ctx, entry)
	if !key.Trusted && resp.Status >= http.StatusBadRequest {
		s.Limiter.Refund(scope, keyHMAC, ratelimit.Day)
	}
	return resp
}

func (s *Service) dispatch(ctx context.Context, entry record) Response {
	switch entry.rec.Kind {
	case hubwire.RecordShare:
		return s.share(ctx, entry)
	case hubwire.RecordVote:
		return s.vote(ctx, entry)
	case hubwire.RecordReport:
		return s.report(ctx, entry)
	case hubwire.RecordMirror:
		return s.mirror(ctx, entry)
	}
	return fail(http.StatusBadRequest, CodeBadRecord, "unknown record kind")
}

type record struct {
	rec     *hubwire.Record
	id      string
	keyHMAC string
	origin  origin
	now     time.Time
}

func (s *Service) remember(ctx context.Context, entry record, setID string, version int) error {
	return s.Store.InsertRecord(ctx, store.Record{
		ID:         entry.id,
		Kind:       entry.rec.Kind,
		KeyHMAC:    entry.keyHMAC,
		SetID:      setID,
		Version:    version,
		ReceivedAt: entry.now,
	})
}
