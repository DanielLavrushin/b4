package hub

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4/log"
)

const (
	healthTimeout    = 6 * time.Second
	manifestTimeout  = 20 * time.Second
	catalogueTimeout = RequestTimeout
	blobTimeout      = 15 * time.Second
	messageTimeout   = 20 * time.Second

	manifestLimit = 256 << 10
	blobLimit     = 64 << 10
	messageLimit  = 96 << 10
	responseLimit = 64 << 10
)

var (
	ErrUnreachable   = errors.New("no hub answered")
	ErrNotConfigured = errors.New("no trusted hub key is configured")
	ErrBlobNotFound  = errors.New("the hub does not have this payload")
	ErrBlobRef       = errors.New("malformed payload reference")
	ErrRecordTooBig  = errors.New("record exceeds the hub message limit")
)

type HubError struct {
	Status     int
	Code       string
	Message    string
	SetID      string
	Version    int
	RetryAfter int
	Scope      string
	Limit      int
	Window     string
}

func (e *HubError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("hub answered %d %s", e.Status, e.Code)
}

func (e *HubError) Retryable() bool {
	return e.Status == http.StatusTooManyRequests
}

func Retryable(err error) bool {
	var he *HubError
	if errors.As(err, &he) {
		return he.Retryable()
	}
	return errors.Is(err, ErrUnreachable)
}

type transportError struct{ err error }

func (e transportError) Error() string { return e.err.Error() }

func (e transportError) Unwrap() error { return e.err }

type MessageResponse struct {
	HTTPStatus int    `json:"-"`
	ID         string `json:"id"`
	Kind       string `json:"kind,omitempty"`
	SetID      string `json:"set_id,omitempty"`
	Version    int    `json:"version,omitempty"`
	Status     string `json:"status,omitempty"`
	Duplicate  bool   `json:"duplicate,omitempty"`
	Queued     bool   `json:"queued,omitempty"`
	Code       string `json:"code,omitempty"`
	Error      string `json:"error,omitempty"`
	RetryAfter int    `json:"retry_after,omitempty"`
	Scope      string `json:"scope,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	Window     string `json:"window,omitempty"`
}

func (s *Service) userAgent() string {
	if s.version == "" {
		return "b4"
	}
	return "b4/" + s.version
}

func (s *Service) do(ctx context.Context, req *http.Request, limit int64, timeout time.Duration) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		ConnectDone: func(_, addr string, err error) {
			if err == nil {
				s.noteAddress(addr)
			}
		},
	})
	req = req.WithContext(ctx)
	req.Header.Set("User-Agent", s.userAgent())
	resp, err := s.client().Do(req)
	if err != nil {
		return nil, 0, transportError{err}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, resp.StatusCode, transportError{err}
	}
	if int64(len(body)) > limit {
		return nil, resp.StatusCode, fmt.Errorf("%s returned more than the %d byte limit", req.URL, limit)
	}
	return body, resp.StatusCode, nil
}

func (s *Service) get(ctx context.Context, url string, limit int64, timeout time.Duration) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	return s.do(ctx, req, limit, timeout)
}

func (s *Service) healthy(ctx context.Context, base string) error {
	body, status, err := s.get(ctx, base+hubwire.PathHealth, 64, healthTimeout)
	if err != nil {
		var te transportError
		var urlErr *url.Error
		if errors.As(err, &te) && errors.As(te.err, &urlErr) {
			return transportError{urlErr.Err}
		}
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("health check answered %d", status)
	}
	if answer := strings.TrimSpace(string(body)); answer != "ok" {
		return fmt.Errorf("health check answered %q", answer)
	}
	return nil
}

func (s *Service) fetchManifest(ctx context.Context, base string) (*hubwire.Manifest, error) {
	body, status, err := s.get(ctx, base+hubwire.PathManifest, manifestLimit, manifestTimeout)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", base, err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("%s: manifest returned %d", base, status)
	}
	var m hubwire.Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("%s: manifest does not decode: %w", base, err)
	}
	return &m, nil
}

func (s *Service) fetchCatalogueFile(ctx context.Context, base string, ref hubwire.FileRef) ([]byte, error) {
	if !catalogueFilePattern.MatchString(ref.File) {
		return nil, ErrCatalogueName
	}
	if ref.Size > catalogueLimit {
		return nil, fmt.Errorf("catalogue is %d bytes, the limit is %d", ref.Size, catalogueLimit)
	}
	body, status, err := s.get(ctx, base+hubwire.PathFiles+ref.File, catalogueLimit, catalogueTimeout)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", base, err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("%s: %s returned %d", base, ref.File, status)
	}
	if err := verifyFileRef(body, ref); err != nil {
		return nil, fmt.Errorf("%s: %w", base, err)
	}
	return body, nil
}

func validBlobHash(hash string) bool {
	if len(hash) != 64 {
		return false
	}
	_, err := hex.DecodeString(hash)
	return err == nil
}

func (s *Service) FetchBlob(ctx context.Context, ref hubwire.BlobRef) ([]byte, error) {
	hash := strings.ToLower(strings.TrimSpace(ref.SHA256))
	if !validBlobHash(hash) {
		return nil, ErrBlobRef
	}
	if ref.Size > blobLimit {
		return nil, fmt.Errorf("payload is %d bytes, the limit is %d", ref.Size, blobLimit)
	}
	missing := false
	var lastErr error
	for _, base := range s.orderedBases() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		body, status, err := s.get(ctx, base+hubwire.PathBlob+hash, blobLimit, blobTimeout)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", base, err)
			continue
		}
		switch {
		case status == http.StatusNotFound:
			missing = true
			continue
		case status != http.StatusOK:
			lastErr = fmt.Errorf("%s: blob returned %d", base, status)
			continue
		}
		if ref.Size > 0 && len(body) != ref.Size {
			return nil, fmt.Errorf("payload %s is %d bytes, the catalogue says %d", hash[:12], len(body), ref.Size)
		}
		if hubwire.BlobHash(body) != hash {
			return nil, fmt.Errorf("payload %s does not match its sha256", hash[:12])
		}
		return body, nil
	}
	if missing {
		return nil, ErrBlobNotFound
	}
	if lastErr == nil {
		return nil, ErrUnreachable
	}
	return nil, fmt.Errorf("%w: %w", ErrUnreachable, lastErr)
}

func (s *Service) Send(ctx context.Context, rec *hubwire.Record) (*MessageResponse, error) {
	raw, err := json.Marshal(rec)
	if err != nil {
		return nil, err
	}
	if len(raw) > messageLimit {
		return nil, ErrRecordTooBig
	}
	var lastErr error
	for _, base := range s.orderedBases() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		req, err := http.NewRequest(http.MethodPost, base+hubwire.PathMessage, bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		body, status, err := s.do(ctx, req, responseLimit, messageTimeout)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", base, err)
			continue
		}
		if status >= http.StatusInternalServerError {
			lastErr = fmt.Errorf("%s: hub returned %d", base, status)
			continue
		}
		var resp MessageResponse
		if len(body) > 0 {
			if err := json.Unmarshal(body, &resp); err != nil {
				lastErr = fmt.Errorf("%s: hub answer does not decode: %w", base, err)
				continue
			}
		}
		resp.HTTPStatus = status
		if status == http.StatusOK || status == http.StatusAccepted {
			if resp.Queued {
				log.Debugf("hub: %s record accepted by relay %s and queued for the upstream hub", rec.Kind, base)
			}
			return &resp, nil
		}
		return nil, &HubError{Status: status, Code: resp.Code, Message: resp.Error, SetID: resp.SetID, Version: resp.Version, RetryAfter: resp.RetryAfter, Scope: resp.Scope, Limit: resp.Limit, Window: resp.Window}
	}
	if lastErr == nil {
		return nil, ErrUnreachable
	}
	log.Debugf("hub: no base accepted the %s record: %v", rec.Kind, lastErr)
	return nil, fmt.Errorf("%w: %w", ErrUnreachable, lastErr)
}

func (s *Service) Sign(kind string, body interface{}) (*hubwire.Record, error) {
	id, _, err := s.Identity()
	if err != nil {
		return nil, err
	}
	return hubwire.SignRecord(id, kind, body, s.now())
}

func (s *Service) SendOrQueue(ctx context.Context, rec *hubwire.Record) (sent bool, queued bool, resp *MessageResponse, err error) {
	resp, err = s.Send(ctx, rec)
	if err == nil {
		return true, false, resp, nil
	}
	if !Retryable(err) {
		return false, false, nil, err
	}
	if qerr := s.enqueue(rec); qerr != nil {
		return false, false, nil, qerr
	}
	log.Infof("hub: %s record queued for retry: %v", rec.Kind, err)
	return false, true, nil, nil
}
