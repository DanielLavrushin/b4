package mirror

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
)

const (
	RelayDir        = "relay"
	RelayTimeout    = 30 * time.Second
	QueueMaxAge     = 30 * 24 * time.Hour
	answerLimit     = 64 << 10
	queueFileSuffix = ".json"

	CodeHubUnreachable = "hub_unreachable"
)

type Answer struct {
	Status      int
	ContentType string
	Body        []byte
}

func jsonAnswer(status int, body map[string]interface{}) *Answer {
	raw, _ := json.Marshal(body)
	return &Answer{Status: status, ContentType: "application/json", Body: raw}
}

type Relay struct {
	Upstream string
	Dir      string
	Client   *http.Client
	Now      func() time.Time
	Timeout  time.Duration
	MaxAge   time.Duration
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

func (r *Relay) maxAge() time.Duration {
	if r.MaxAge <= 0 {
		return QueueMaxAge
	}
	return r.MaxAge
}

func (r *Relay) Forward(ctx context.Context, raw []byte) (*Answer, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.Upstream+hubwire.PathMessage, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "b4hub-mirror")
	resp, err := r.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, answerLimit))
	if err != nil {
		return nil, err
	}
	return &Answer{Status: resp.StatusCode, ContentType: resp.Header.Get("Content-Type"), Body: body}, nil
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

func retained(a *Answer, err error) (string, bool) {
	if reason, down := unreachable(a, err); down {
		return reason, true
	}
	if a.Status == http.StatusTooManyRequests || a.Status >= http.StatusInternalServerError {
		return fmt.Sprintf("upstream answered %d", a.Status), true
	}
	return "", false
}

func queueable(kind string) bool {
	return kind == hubwire.RecordVote || kind == hubwire.RecordReport
}

func (r *Relay) Handle(ctx context.Context, raw []byte) *Answer {
	answer, err := r.Forward(ctx, raw)
	reason, down := unreachable(answer, err)
	if !down {
		return answer
	}
	var rec hubwire.Record
	if json.Unmarshal(raw, &rec) == nil && queueable(rec.Kind) {
		if _, verr := hubwire.VerifyRecord(&rec); verr == nil {
			id := rec.ID()
			if qerr := r.enqueue(id, raw); qerr != nil {
				return jsonAnswer(http.StatusInternalServerError, map[string]interface{}{"code": "internal", "error": qerr.Error()})
			}
			log.Printf("relay: hub unreachable (%s), queued %s %s", reason, rec.Kind, id[:12])
			return jsonAnswer(http.StatusAccepted, map[string]interface{}{"id": id, "kind": rec.Kind, "queued": true})
		}
	}
	return jsonAnswer(http.StatusBadGateway, map[string]interface{}{"code": CodeHubUnreachable, "error": "the hub cannot be reached: " + reason})
}

func (r *Relay) queuePath(id string) string {
	return filepath.Join(r.Dir, id+queueFileSuffix)
}

func (r *Relay) enqueue(id string, raw []byte) error {
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return err
	}
	path := r.queuePath(id)
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return hubdata.WriteFileAtomic(path, raw, 0o644)
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

func (r *Relay) Retry(ctx context.Context) error {
	entries, err := os.ReadDir(r.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	now := r.now()
	for i, e := range entries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !strings.HasSuffix(e.Name(), queueFileSuffix) {
			continue
		}
		path := filepath.Join(r.Dir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > r.maxAge() {
			_ = os.Remove(path)
			log.Printf("relay: dropped %s, queued for more than %s", e.Name(), r.maxAge())
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		answer, err := r.Forward(ctx, raw)
		if reason, keep := retained(answer, err); keep {
			return fmt.Errorf("delivery paused (%s), %d records still queued", reason, len(entries)-i)
		}
		_ = os.Remove(path)
		log.Printf("relay: delivered queued %s: %d %s", e.Name(), answer.Status, strings.TrimSpace(string(answer.Body)))
	}
	return nil
}
