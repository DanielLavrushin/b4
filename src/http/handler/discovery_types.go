package handler

import (
	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
)

type HistoryEntryView struct {
	discovery.HistoryEntry
	SizeBytes int `json:"size_bytes"`
}

type DiscoveryReplaceRequest struct {
	SetId        string              `json:"set_id"`
	Set          config.SetConfig    `json:"set"`
	Domains      []string            `json:"domains"`
	Pins         map[string][]string `json:"pins,omitempty"`
	StrategyOnly bool                `json:"strategy_only,omitempty"`
	KeepTargets  bool                `json:"keep_targets,omitempty"`
	ProbeURLs    []string            `json:"probe_urls,omitempty"`
}

type HistoryAppliedRequest struct {
	Domains []string `json:"domains"`
	Preset  string   `json:"preset"`
	SetId   string   `json:"set_id,omitempty"`
}

type DiscoveryRequest struct {
	CheckURL        string   `json:"check_url,omitempty"`
	CheckURLs       []string `json:"check_urls,omitempty"`
	SkipDNS         bool     `json:"skip_dns,omitempty"`
	SkipCache       bool     `json:"skip_cache,omitempty"`
	SkipCommunity   bool     `json:"skip_community,omitempty"`
	PayloadFiles    []string `json:"payload_files,omitempty"`
	ValidationTries int      `json:"validation_tries,omitempty"`
	TLSVersion      string   `json:"tls_version,omitempty"` // "auto", "tls12", "tls13"
	IPVersion       string   `json:"ip_version,omitempty"`  // "auto", "ipv4", "ipv6"
	SetId           string   `json:"set_id,omitempty"`
	StopWhenCovered bool     `json:"stop_when_covered,omitempty"`
}

type DiscoveryResponse struct {
	Id             string   `json:"id"`
	Domain         string   `json:"domain"`
	Domains        []string `json:"domains,omitempty"`
	CheckURL       string   `json:"check_url"`
	EstimatedTests int      `json:"estimated_tests"`
	Message        string   `json:"message"`
	SetId          string   `json:"set_id,omitempty"`
}

type DiscoverySuggestion struct {
	URL          string `json:"url"`
	Host         string `json:"host"`
	Source       string `json:"source"`
	OwnerSetId   string `json:"owner_set_id,omitempty"`
	OwnerSetName string `json:"owner_set_name,omitempty"`
}

type DiscoverySuggestResponse struct {
	SetId string                `json:"set_id"`
	URLs  []DiscoverySuggestion `json:"urls"`
}
