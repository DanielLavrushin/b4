package handler

import "github.com/daniellavrushin/b4/detector"

type DetectorRequest struct {
	Sites     []string         `json:"sites"`
	Scopes    []detector.Scope `json:"scopes"`
	IPVersion string           `json:"ip_version,omitempty"`
	Parallel  int              `json:"parallel,omitempty"`
	FetchMode string           `json:"fetch_mode,omitempty"`
	SkipTLS12 bool             `json:"skip_tls12,omitempty"`
	SNISearch bool             `json:"sni_search,omitempty"`
}

type DetectorResponse struct {
	Id      string `json:"id"`
	Total   int    `json:"total"`
	Message string `json:"message"`
}

type DetectorListsResponse struct {
	ListsDate    string   `json:"lists_date"`
	ListsSource  string   `json:"lists_source"`
	EmbeddedDate string   `json:"embedded_date"`
	Custom       bool     `json:"custom"`
	Sites        []string `json:"sites"`
	SiteCount    int      `json:"site_count"`
	DNSServers   int      `json:"dns_servers"`
	TCPTargets   int      `json:"tcp_targets"`
	WhitelistSNI int      `json:"whitelist_sni"`
}
