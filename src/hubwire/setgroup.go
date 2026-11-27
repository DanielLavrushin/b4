package hubwire

import "github.com/daniellavrushin/b4/config"

const (
	RecordGroup = "group"

	MaxGroupMembers = 10
)

type GroupBody struct {
	Title       string        `json:"title"`
	Description string        `json:"description,omitempty"`
	Members     []GroupMember `json:"members"`
	DerivedFrom *Origin       `json:"derived_from,omitempty"`
	ASNHint     string        `json:"asn_hint,omitempty"`
	CountryHint string        `json:"country_hint,omitempty"`
	Engine      string        `json:"engine,omitempty"`
	B4Version   string        `json:"b4_version,omitempty"`
}

type GroupMember struct {
	Envelope Envelope         `json:"envelope"`
	Escalate *GroupEscalation `json:"escalate,omitempty"`
}

type GroupEscalation struct {
	To             int `json:"to"`
	RstThreshold   int `json:"rst_threshold,omitempty"`
	RstWindowSec   int `json:"rst_window_sec,omitempty"`
	TtlSec         int `json:"ttl_sec,omitempty"`
	StallThreshold int `json:"stall_threshold,omitempty"`
	StallTimeoutMs int `json:"stall_timeout_ms,omitempty"`
	DNSThreshold   int `json:"dns_threshold,omitempty"`
}

type GroupResult struct {
	GroupID string              `json:"group_id"`
	Version int                 `json:"version"`
	Status  string              `json:"status"`
	Members []GroupMemberResult `json:"members"`
}

type GroupMemberResult struct {
	SetID   string `json:"set_id"`
	Version int    `json:"version"`
	Status  string `json:"status"`
	Linked  bool   `json:"linked,omitempty"`
}

type CatalogueGroup struct {
	ID          string                 `json:"id"`
	Version     int                    `json:"version"`
	Title       string                 `json:"title"`
	Description string                 `json:"description,omitempty"`
	Author      string                 `json:"author"`
	CreatedAt   string                 `json:"created_at"`
	UpdatedAt   string                 `json:"updated_at"`
	Members     []CatalogueGroupMember `json:"members"`
}

type CatalogueGroupMember struct {
	SetID    string           `json:"set_id"`
	Escalate *GroupEscalation `json:"escalate,omitempty"`
}

func EscalationTo(e config.EscalateConfig, to int) *GroupEscalation {
	g := &GroupEscalation{
		To:             to,
		RstThreshold:   e.RstThreshold,
		RstWindowSec:   e.RstWindowSec,
		TtlSec:         e.TtlSec,
		StallThreshold: e.StallThreshold,
		StallTimeoutMs: e.StallTimeoutMs,
		DNSThreshold:   e.DNSThreshold,
	}
	for _, f := range g.thresholds() {
		*f.value = min(max(*f.value, 0), f.limit)
	}
	return g
}

func (g *GroupEscalation) Config(toID string) config.EscalateConfig {
	return config.EscalateConfig{
		To:             toID,
		RstThreshold:   g.RstThreshold,
		RstWindowSec:   g.RstWindowSec,
		TtlSec:         g.TtlSec,
		StallThreshold: g.StallThreshold,
		StallTimeoutMs: g.StallTimeoutMs,
		DNSThreshold:   g.DNSThreshold,
	}
}

func ValidGroupEscalation(g *GroupEscalation, self, members int) bool {
	return g == nil || (g.To >= 0 && g.To < members && g.To != self)
}

type escalationThreshold struct {
	name  string
	value *int
	limit int
}

func (g *GroupEscalation) thresholds() []escalationThreshold {
	return []escalationThreshold{
		{"rst_threshold", &g.RstThreshold, 20},
		{"rst_window_sec", &g.RstWindowSec, 600},
		{"ttl_sec", &g.TtlSec, 86400},
		{"stall_threshold", &g.StallThreshold, 20},
		{"stall_timeout_ms", &g.StallTimeoutMs, 15000},
		{"dns_threshold", &g.DNSThreshold, 10},
	}
}

func EscalationOutOfRange(g *GroupEscalation) (field string, limit int) {
	if g == nil {
		return "", 0
	}
	for _, f := range g.thresholds() {
		if *f.value < 0 || *f.value > f.limit {
			return f.name, f.limit
		}
	}
	return "", 0
}
