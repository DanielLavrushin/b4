package catalogue

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	DefaultMirrorTimeout = 5 * time.Second
	DefaultMirrorWindow  = 24 * time.Hour
	mirrorHealthLimit    = 64
	mirrorManifestLimit  = 1 << 20
)

type MirrorHealth struct {
	Store   *store.Store
	KeyID   string
	Client  *http.Client
	Now     func() time.Time
	Timeout time.Duration
	Window  time.Duration
}

func (h *MirrorHealth) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func (h *MirrorHealth) timeout() time.Duration {
	if h.Timeout <= 0 {
		return DefaultMirrorTimeout
	}
	return h.Timeout
}

func (h *MirrorHealth) window() time.Duration {
	if h.Window <= 0 {
		return DefaultMirrorWindow
	}
	return h.Window
}

func (h *MirrorHealth) client() *http.Client {
	if h.Client != nil {
		return h.Client
	}
	return http.DefaultClient
}

func (h *MirrorHealth) Healthy(ctx context.Context) ([]string, error) {
	mirrors, err := h.Store.MirrorsByStatus(ctx, store.MirrorApproved)
	if err != nil {
		return nil, err
	}
	now := h.now().UTC()
	results := make([]error, len(mirrors))
	var wg sync.WaitGroup
	for i := range mirrors {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = h.check(ctx, mirrors[i].URL)
		}(i)
	}
	wg.Wait()
	healthy := make([]string, 0, len(mirrors))
	for i, m := range mirrors {
		ok := results[i] == nil
		reason := ""
		if !ok {
			reason = results[i].Error()
		}
		if err := h.Store.RecordMirrorCheck(ctx, m.ID, now, ok, reason); err != nil {
			return nil, err
		}
		lastOK := m.LastOK
		if ok {
			lastOK = now
		}
		if !lastOK.IsZero() && now.Sub(lastOK) <= h.window() {
			healthy = append(healthy, m.URL)
		}
	}
	return healthy, nil
}

func (h *MirrorHealth) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, h.timeout())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "b4hub")
	resp, err := h.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("returned %d", resp.StatusCode)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("exceeds %d bytes", limit)
	}
	return body, nil
}

func (h *MirrorHealth) check(ctx context.Context, base string) error {
	body, err := h.get(ctx, base+hubwire.PathHealth, mirrorHealthLimit)
	if err != nil {
		return fmt.Errorf("health: %w", err)
	}
	if strings.TrimSpace(string(body)) != "ok" {
		return fmt.Errorf("health answered %q", strings.TrimSpace(string(body)))
	}
	body, err = h.get(ctx, base+hubwire.PathManifest, mirrorManifestLimit)
	if err != nil {
		return fmt.Errorf("manifest: %w", err)
	}
	var m hubwire.Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return fmt.Errorf("manifest does not decode: %w", err)
	}
	if m.KeyID != h.KeyID {
		return fmt.Errorf("manifest is signed by key %s, not by this hub", m.KeyID)
	}
	return nil
}
