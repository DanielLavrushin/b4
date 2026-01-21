package handler

type DiscoveryRequest struct {
	CheckURL        string   `json:"check_url,omitempty"`
	SkipDNS         bool     `json:"skip_dns,omitempty"`
	PayloadFiles    []string `json:"payload_files,omitempty"`
	ValidationTries int      `json:"validation_tries,omitempty"`
}

type DiscoveryResponse struct {
	Id             string `json:"id"`
	Domain         string `json:"domain"`
	CheckURL       string `json:"check_url"`
	EstimatedTests int    `json:"estimated_tests"`
	Message        string `json:"message"`
}
