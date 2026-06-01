package detector

import (
	_ "embed"
	"encoding/json"

	"github.com/daniellavrushin/b4/log"
)

//go:embed targets.json
var targetsJSON []byte

var (
	DNSCheckDomains     []string
	CheckDomains        []string
	UDPDNSServers       []string
	DoHServers          []doHServer
	BlockMarkers        []string
	BodyBlockMarkers    []string
	CDNRedirectPatterns []string
	TCPTargets          []TCPTarget
	WhitelistSNI        []string
	DNSAvailServers     []dnsAvailServer
	DNSAvailDomains     []string
	TelegramConfig      telegramTargets
)

type doHServer struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type dnsAvailServer struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Kind    string `json:"kind"`
}

type telegramTargets struct {
	DownloadURL  string `json:"download_url"`
	DownloadSize int64  `json:"download_size"`
}

type targetsData struct {
	DNSCheckDomains     []string         `json:"dns_check_domains"`
	CheckDomains        []string         `json:"check_domains"`
	UDPDNSServers       []string         `json:"udp_dns_servers"`
	DoHServers          []doHServer      `json:"doh_servers"`
	BlockMarkers        []string         `json:"block_markers"`
	BodyBlockMarkers    []string         `json:"body_block_markers"`
	CDNRedirectPatterns []string         `json:"cdn_redirect_patterns"`
	TCPTargets          []TCPTarget      `json:"tcp_targets"`
	WhitelistSNI        []string         `json:"whitelist_sni"`
	DNSAvailServers     []dnsAvailServer `json:"dns_avail_servers"`
	DNSAvailDomains     []string         `json:"dns_avail_domains"`
	Telegram            telegramTargets  `json:"telegram"`
}

func init() {
	var data targetsData
	if err := json.Unmarshal(targetsJSON, &data); err != nil {
		log.Errorf("Failed to parse embedded targets.json: %v", err)
		return
	}
	DNSCheckDomains = data.DNSCheckDomains
	CheckDomains = data.CheckDomains
	UDPDNSServers = data.UDPDNSServers
	DoHServers = data.DoHServers
	BlockMarkers = data.BlockMarkers
	BodyBlockMarkers = data.BodyBlockMarkers
	CDNRedirectPatterns = data.CDNRedirectPatterns
	TCPTargets = data.TCPTargets
	WhitelistSNI = data.WhitelistSNI
	DNSAvailServers = data.DNSAvailServers
	DNSAvailDomains = data.DNSAvailDomains
	TelegramConfig = data.Telegram
}
