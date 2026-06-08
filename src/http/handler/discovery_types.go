package handler

type DiscoveryRequest struct {
	CheckURL        string   `json:"check_url,omitempty"`
	CheckURLs       []string `json:"check_urls,omitempty"`
	SkipDNS         bool     `json:"skip_dns,omitempty"`
	SkipCache       bool     `json:"skip_cache,omitempty"`
	PayloadFiles    []string `json:"payload_files,omitempty"`
	ValidationTries int      `json:"validation_tries,omitempty"`
	TLSVersion      string   `json:"tls_version,omitempty"` // "auto", "tls12", "tls13"
	IPVersion       string   `json:"ip_version,omitempty"`  // "auto", "ipv4", "ipv6"
}

type DiscoveryResponse struct {
	Id             string   `json:"id"`
	Domain         string   `json:"domain"`
	Domains        []string `json:"domains,omitempty"`
	CheckURL       string   `json:"check_url"`
	EstimatedTests int      `json:"estimated_tests"`
	Message        string   `json:"message"`
}
