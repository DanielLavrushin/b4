package watchdog

import (
	"net/url"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/netprobe"
)

type DomainStatus struct {
	Domain              string    `json:"domain"`
	Status              string    `json:"status"`
	LastCheck           time.Time `json:"last_check"`
	LastFailure         time.Time `json:"last_failure,omitempty"`
	LastHeal            time.Time `json:"last_heal,omitempty"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	Interval            int       `json:"interval_sec"`
	CooldownUntil       time.Time `json:"cooldown_until,omitempty"`
	LastError           string    `json:"last_error,omitempty"`
	LastSpeed           float64   `json:"last_speed,omitempty"`
	MatchedSet          string    `json:"matched_set,omitempty"`
	MatchedSetId        string    `json:"matched_set_id,omitempty"`
	DisplayDomain       string    `json:"display_domain,omitempty"`
}

type CheckResult struct {
	OK        bool
	Speed     float64
	Error     string
	Verdict   netprobe.DomainStatus
	BytesRead int64
}

type WatchdogState struct {
	Enabled bool            `json:"enabled"`
	Domains []*DomainStatus `json:"domains"`
}

func ExtractDomain(input string) string {
	input = strings.TrimSpace(input)
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		if u, err := url.Parse(input); err == nil && u.Host != "" {
			return u.Hostname()
		}
	}
	if i := strings.IndexAny(input, "/:?"); i >= 0 {
		return input[:i]
	}
	return input
}

const (
	StatusHealthy    = "healthy"
	StatusDegraded   = "degraded"
	StatusEscalating = "escalating"
	StatusQueued     = "queued"
)
