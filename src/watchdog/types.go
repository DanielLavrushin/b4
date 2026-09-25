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
	OwnerSetId          string    `json:"owner_set_id,omitempty"`
	OwnerSetName        string    `json:"owner_set_name,omitempty"`
	WatchedBySetId      string    `json:"watched_by_set_id,omitempty"`
	WatchedBySetName    string    `json:"watched_by_set_name,omitempty"`
}

type CheckResult struct {
	OK        bool
	Speed     float64
	Error     string
	Verdict   netprobe.DomainStatus
	BytesRead int64
	Unusable  bool
}

type WatchdogState struct {
	Enabled bool             `json:"enabled"`
	Domains []*DomainStatus  `json:"domains"`
	Sets    []SetWatchStatus `json:"sets"`
}

type URLWatchStatus struct {
	URL          string    `json:"url"`
	Host         string    `json:"host"`
	Status       string    `json:"status"`
	OwnerSetId   string    `json:"owner_set_id,omitempty"`
	OwnerSetName string    `json:"owner_set_name,omitempty"`
	EscalatedTo  string    `json:"escalated_to,omitempty"`
	StatusCode   int       `json:"status_code,omitempty"`
	BytesRead    int64     `json:"bytes_read,omitempty"`
	Speed        float64   `json:"speed,omitempty"`
	LastError    string    `json:"last_error,omitempty"`
	LastCheck    time.Time `json:"last_check,omitzero"`
}

type SetWatchStatus struct {
	SetId               string           `json:"set_id"`
	SetName             string           `json:"set_name"`
	Status              string           `json:"status"`
	Reason              string           `json:"reason,omitempty"`
	URLs                []URLWatchStatus `json:"urls"`
	ConsecutiveFailures int              `json:"consecutive_failures"`
	HealFailures        int              `json:"heal_failures"`
	Interval            int              `json:"interval_sec"`
	LastCheck           time.Time        `json:"last_check,omitzero"`
	LastHeal            time.Time        `json:"last_heal,omitzero"`
	LastHealPreset      string           `json:"last_heal_preset,omitempty"`
	CooldownUntil       time.Time        `json:"cooldown_until,omitzero"`
	LastError           string           `json:"last_error,omitempty"`
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

const markThroughEngine uint = 0

const (
	StatusHealthy    = "healthy"
	StatusDegraded   = "degraded"
	StatusEscalating = "escalating"
	StatusQueued     = "queued"
)

const (
	SetStatusQueued       = StatusQueued
	SetStatusHealthy      = StatusHealthy
	SetStatusDegraded     = StatusDegraded
	SetStatusHealQueued   = "heal_queued"
	SetStatusHealing      = "healing"
	SetStatusCooldown     = "cooldown"
	SetStatusUnverifiable = "unverifiable"
	SetStatusGaveUp       = "gave_up"
)

const (
	URLStatusQueued    = "queued"
	URLStatusOK        = "ok"
	URLStatusFailed    = "failed"
	URLStatusNotOwned  = "not_owned"
	URLStatusEscalated = "escalated"
	URLStatusUnusable  = "unusable"
)

const (
	ReasonNotOwned     = "not_owned"
	ReasonEscalated    = "escalated"
	ReasonUnusable     = "unusable"
	ReasonCurrentWorks = "current_works"
	ReasonNotNeeded    = "not_needed"
	ReasonPartial      = "partial"
	ReasonNone         = "none"
	ReasonIncomplete   = "incomplete"
	ReasonBudget       = "budget"
	ReasonStartFailed  = "start_failed"
	ReasonVerifyFailed = "verify_failed"
	ReasonEdited       = "edited"
	ReasonBusy         = "busy"
)

const (
	ForceCheckScheduled  = "scheduled"
	ForceCheckNotWatched = "not_watched"
	ForceCheckHealing    = "healing"
	ForceCheckMasterOff  = "master_off"
)
