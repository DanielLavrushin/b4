package handler

import "github.com/daniellavrushin/b4/config"

// Response types for API endpoints
type GeositeResponse struct {
	Tags []string `json:"tags"`
}

type SetStatistics struct {
	ManualDomains            int            `json:"manual_domains"`
	ManualIPs                int            `json:"manual_ips"`
	GeositeDomains           int            `json:"geosite_domains"`
	GeoipIPs                 int            `json:"geoip_ips"`
	ASNIPs                   int            `json:"asn_ips"`
	TotalDomains             int            `json:"total_domains"`
	TotalIPs                 int            `json:"total_ips"`
	GeositeCategoryBreakdown map[string]int `json:"geosite_category_breakdown,omitempty"`
	GeoipCategoryBreakdown   map[string]int `json:"geoip_category_breakdown,omitempty"`
	ASNBreakdown             map[string]int `json:"asn_breakdown,omitempty"`
	ASNUnresolved            []string       `json:"asn_unresolved,omitempty"`
}

type SetWithStats struct {
	*config.SetConfig
	Stats    SetStatistics `json:"stats"`
	HubState string        `json:"hub_state,omitempty"`
}

// CategoryPreviewResponse for previewing category contents
type CategoryPreviewResponse struct {
	Category     string   `json:"category"`
	TotalDomains int      `json:"total_domains"`
	PreviewCount int      `json:"preview_count"`
	Preview      []string `json:"preview"`
}

type AddDomainRequest struct {
	Domain  string `json:"domain"`
	SetId   string `json:"set_id"`
	SetName string `json:"set_name,omitempty"`
}

type AddDomainResponse struct {
	Success       bool     `json:"success"`
	Message       string   `json:"message"`
	Domain        string   `json:"domain"`
	TotalDomains  int      `json:"total_domains"`
	ManualDomains []string `json:"manual_domains,omitempty"`
}
