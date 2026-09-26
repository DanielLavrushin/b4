package handler

import (
	"github.com/daniellavrushin/b4/hub"
	"github.com/daniellavrushin/b4/hubwire"
)

type HubTargets struct {
	Domains []string `json:"domains"`
	GeoSite []string `json:"geosite"`
	GeoIP   []string `json:"geoip"`
	ASNs    []string `json:"asns"`
	IPCount int      `json:"ip_count"`
}

type HubApplied struct {
	SetID    string `json:"set_id"`
	SetName  string `json:"set_name"`
	HubState string `json:"hub_state"`
	Version  int    `json:"version"`
	Vote     string `json:"vote,omitempty"`
	VotedAt  string `json:"voted_at,omitempty"`
}

type HubSet struct {
	ID          string                 `json:"id"`
	Version     int                    `json:"version"`
	FP          string                 `json:"fp"`
	Title       string                 `json:"title"`
	Description string                 `json:"description,omitempty"`
	Author      string                 `json:"author"`
	B4Min       string                 `json:"b4_min"`
	B4Version   string                 `json:"b4_version,omitempty"`
	Engine      string                 `json:"engine,omitempty"`
	Family      string                 `json:"family,omitempty"`
	Flags       []string               `json:"flags"`
	Status      string                 `json:"status"`
	CreatedAt   string                 `json:"created_at"`
	UpdatedAt   string                 `json:"updated_at"`
	Geo         *hubwire.GeoSource     `json:"geo"`
	Set         map[string]interface{} `json:"set"`
	Payloads    []hubwire.BlobRef      `json:"payloads"`
	Targets     HubTargets             `json:"targets"`
	Display     hubwire.Displayed      `json:"display"`
	Match       *hub.Match             `json:"match"`
	Applied     *HubApplied            `json:"applied"`
}

type HubSetsResponse struct {
	Sets  []HubSet `json:"sets"`
	Total int      `json:"total"`
}

type HubApplyRequest struct {
	Replace string `json:"replace,omitempty"`
}

type HubApplyResponse struct {
	ID       string                `json:"id"`
	Name     string                `json:"name"`
	Moved    []DomainReassignment  `json:"moved"`
	Warnings []hubwire.Warning     `json:"warnings"`
	Payloads []HubInstalledPayload `json:"payloads"`
}

type HubVoteRequest struct {
	Kind   string `json:"kind"`
	Domain string `json:"domain,omitempty"`
}

type HubVoteResponse struct {
	Queued bool `json:"queued"`
	Sent   bool `json:"sent"`
}

type HubReportRequest struct {
	Reason string `json:"reason"`
}

type HubReportResponse struct {
	Queued bool `json:"queued"`
	Sent   bool `json:"sent"`
}

type HubShareRequest struct {
	SetID       string `json:"set_id"`
	Description string `json:"description,omitempty"`
}

type HubShareResponse struct {
	HubID   string `json:"hub_id"`
	Version int    `json:"version"`
	Status  string `json:"status"`
}

type HubRemoteError struct {
	Code       string `json:"code"`
	Message    string `json:"error"`
	HubID      string `json:"hub_id,omitempty"`
	Version    int    `json:"version,omitempty"`
	RetryAfter int    `json:"retry_after,omitempty"`
	Scope      string `json:"scope,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	Window     string `json:"window,omitempty"`
}

type HubIdentityResponse struct {
	KeyID     string `json:"key_id"`
	CreatedAt string `json:"created_at"`
}

type HubRecoveryCodeResponse struct {
	Code string `json:"code"`
}

type HubRestoreRequest struct {
	Code string `json:"code"`
}

type HubRestoreResponse struct {
	KeyID string `json:"key_id"`
}

type HubTestRequest struct {
	Domain string `json:"domain,omitempty"`
}

type HubProbeResult struct {
	OK     bool   `json:"ok"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type HubTestResponse struct {
	Domain    string         `json:"domain"`
	ThroughB4 HubProbeResult `json:"through_b4"`
	Bypassed  HubProbeResult `json:"bypassed"`
}
