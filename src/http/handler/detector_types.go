package handler

type DetectorRequest struct {
	Tests []string `json:"tests"` // "dns", "dns-availability", "domains", "tcp", "sni", "telegram"
}

type DetectorResponse struct {
	Id             string   `json:"id"`
	Tests          []string `json:"tests"`
	EstimatedTests int      `json:"estimated_tests"`
	Message        string   `json:"message"`
}
