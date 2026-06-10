package detector

import (
	_ "embed"
	"encoding/json"

	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/netprobe"
)

//go:embed targets.json
var targetsJSON []byte

var (
	DNSCheckDomains     []string
	CheckDomains        []string
	UDPDNSServers       []string
	DoHServers          []netprobe.DoHServer
	CDNRedirectPatterns []string
	TCPTargets          []TCPTarget
	WhitelistSNI        []string
	DNSAvailServers     []dnsAvailServer
	DNSAvailDomains     []string
	TelegramConfig      telegramTargets
)

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
	UDPDNSServers = netprobe.DefaultUDPServers
	DoHServers = netprobe.DefaultDoHServers
	CDNRedirectPatterns = data.CDNRedirectPatterns
	TCPTargets = data.TCPTargets
	WhitelistSNI = data.WhitelistSNI
	DNSAvailServers = data.DNSAvailServers
	DNSAvailDomains = data.DNSAvailDomains
	TelegramConfig = data.Telegram
}
