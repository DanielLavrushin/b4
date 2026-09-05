package detector

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/daniellavrushin/b4/log"
)

//go:embed targets.json
var targetsJSON []byte

const overrideFile = "detector_targets.json"

type DNSServer struct {
	Name    string `json:"name"`
	Brand   string `json:"brand"`
	Address string `json:"address"`
	Kind    string `json:"kind"`
	Port    int    `json:"port,omitempty"`
}

type TelegramTargets struct {
	DownloadURL  string `json:"download_url"`
	DownloadSize int64  `json:"download_size"`
	UploadIP     string `json:"upload_ip"`
	UploadPort   int    `json:"upload_port"`
	UploadSize   int64  `json:"upload_size"`
}

type TargetLists struct {
	ListsDate          string          `json:"lists_date"`
	ListsSource        string          `json:"lists_source"`
	Sites              []string        `json:"sites"`
	DNSCheckDomains    []string        `json:"dns_check_domains"`
	DNSTrustedDomains  []string        `json:"dns_trusted_domains"`
	DNSServers         []DNSServer     `json:"dns_servers"`
	CymruDoHServers    []string        `json:"cymru_doh_servers"`
	KnownResolverNames []string        `json:"known_resolver_names"`
	IPLookupURLs       []string        `json:"ip_lookup_urls"`
	IP6LookupURLs      []string        `json:"ip6_lookup_urls"`
	CDNRedirectPattern []string        `json:"cdn_redirect_patterns"`
	TCPTargets         []TCPTarget     `json:"tcp_targets"`
	WhitelistSNI       []string        `json:"whitelist_sni"`
	Telegram           TelegramTargets `json:"telegram"`
}

var (
	embeddedLists TargetLists
	currentLists  *TargetLists
	listsMu       sync.RWMutex
)

func init() {
	if err := json.Unmarshal(targetsJSON, &embeddedLists); err != nil {
		log.Errorf("Failed to parse embedded detector targets.json: %v", err)
	}
}

func Lists() TargetLists {
	listsMu.RLock()
	defer listsMu.RUnlock()
	if currentLists != nil {
		return *currentLists
	}
	return embeddedLists
}

func EmbeddedLists() TargetLists {
	return embeddedLists
}

func overridePath(configPath string) string {
	if configPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(configPath), overrideFile)
}

func LoadListOverride(configPath string) {
	path := overridePath(configPath)
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var lists TargetLists
	if err := json.Unmarshal(data, &lists); err != nil {
		log.Errorf("Failed to parse %s: %v", path, err)
		return
	}
	if len(lists.Sites) == 0 || len(lists.TCPTargets) == 0 {
		return
	}
	listsMu.Lock()
	currentLists = &lists
	listsMu.Unlock()
	log.Infof("Detector target lists loaded from %s (dated %s)", path, lists.ListsDate)
}

func saveListOverride(configPath string, lists TargetLists) error {
	path := overridePath(configPath)
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(lists, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return err
	}
	listsMu.Lock()
	currentLists = &lists
	listsMu.Unlock()
	return nil
}

func ResetListOverride(configPath string) {
	if path := overridePath(configPath); path != "" {
		os.Remove(path)
	}
	listsMu.Lock()
	currentLists = nil
	listsMu.Unlock()
}
