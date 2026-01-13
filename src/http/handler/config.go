// src/http/handler/config.go
package handler

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/metrics"
)

func (api *API) RegisterConfigApi() {

	api.mux.HandleFunc("/api/config", api.handleConfig)
	api.mux.HandleFunc("/api/config/reset", api.resetConfig)
}

func (a *API) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.getConfig(w)
	case http.MethodPut:
		a.updateConfig(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *API) getConfig(w http.ResponseWriter) {
	setJsonHeader(w)

	// Calculate statistics for each set
	setsWithStats := make([]SetWithStats, len(a.cfg.Sets))
	totalDomains := 0
	totalIPs := 0

	for i, set := range a.cfg.Sets {
		// Count manual domains and IPs
		manualDomains := len(set.Targets.SNIDomains)
		manualIPs := len(set.Targets.IPs)

		// Get geosite category counts
		geositeCounts := make(map[string]int)
		geositeTotalDomains := 0
		if len(set.Targets.GeoSiteCategories) > 0 && a.geodataManager.IsGeositeConfigured() {
			counts, err := a.geodataManager.GetGeositeCategoryCounts(set.Targets.GeoSiteCategories)
			if err == nil {
				geositeCounts = counts
				for _, count := range counts {
					geositeTotalDomains += count
				}
			}
		}

		// Get geoip category counts
		geoipCounts := make(map[string]int)
		geoipTotalIPs := 0
		if len(set.Targets.GeoIpCategories) > 0 && a.geodataManager.IsGeoipConfigured() {
			counts, err := a.geodataManager.GetGeoipCategoryCounts(set.Targets.GeoIpCategories)
			if err == nil {
				geoipCounts = counts
				for _, count := range counts {
					geoipTotalIPs += count
				}
			}
		}

		setTotalDomains := manualDomains + geositeTotalDomains
		setTotalIPs := manualIPs + geoipTotalIPs

		totalDomains += setTotalDomains
		totalIPs += setTotalIPs

		setsWithStats[i] = SetWithStats{
			SetConfig: set,
			Stats: SetStatistics{
				ManualDomains:            manualDomains,
				ManualIPs:                manualIPs,
				GeositeDomains:           geositeTotalDomains,
				GeoipIPs:                 geoipTotalIPs,
				TotalDomains:             setTotalDomains,
				TotalIPs:                 setTotalIPs,
				GeositeCategoryBreakdown: geositeCounts,
				GeoipCategoryBreakdown:   geoipCounts,
			},
		}
	}

	//get list of interfaces from system
	ifaces, err := getSystemInterfaces()
	if err != nil {
		log.Errorf("Failed to get system interfaces: %v", err)
		http.Error(w, "Failed to get system interfaces", http.StatusInternalServerError)
		return
	}
	sort.Strings(ifaces)

	response := ConfigResponse{
		Config:              a.cfg,
		Sets:                setsWithStats,
		AvailableInterfaces: ifaces,
		Success:             true,
		Message:             "Configuration retrieved successfully",
	}
	enc := json.NewEncoder(w)
	_ = enc.Encode(response)
}

func getSystemInterfaces() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	// Known virtual/internal interface prefixes to exclude
	excludePrefixes := []string{
		"lo", "dummy", "gre", "erspan", "ifb", "imq",
		"ip6_vti", "ip6gre", "ip6tnl", "ip_vti", "sit",
		"spu_", "bcmsw", "blog", "veth", "docker", "virbr",
	}

	var ifaceNames []string
	for _, iface := range ifaces {
		// Skip loopback
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		// Skip interfaces that are down
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		// Skip known virtual prefixes
		skip := false
		for _, prefix := range excludePrefixes {
			if strings.HasPrefix(iface.Name, prefix) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		// Keep if it has addresses OR is a bridge/wireless (br*, wl*, tun*, tap*)
		addrs, _ := iface.Addrs()
		isBridgeOrWireless := strings.HasPrefix(iface.Name, "br") ||
			strings.HasPrefix(iface.Name, "wl") ||
			strings.HasPrefix(iface.Name, "tun") ||
			strings.HasPrefix(iface.Name, "tap")

		if len(addrs) > 0 || isBridgeOrWireless {
			ifaceNames = append(ifaceNames, iface.Name)
		}
	}

	return ifaceNames, nil
}

func (a *API) updateConfig(w http.ResponseWriter, r *http.Request) {
	var newConfig config.Config

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&newConfig); err != nil {
		log.Errorf("Failed to decode config update: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	oldConfig := a.cfg.Clone()
	newConfig.ConfigPath = a.cfg.ConfigPath

	// update logging level if changed
	if newConfig.System.Logging.Level != log.Level(log.CurLevel.Load()) {
		log.SetLevel(log.Level(newConfig.System.Logging.Level))
		log.Infof("Log level changed to %s", newConfig.System.Logging.Level)
	}

	a.geodataManager.UpdatePaths(newConfig.System.Geo.GeoSitePath, newConfig.System.Geo.GeoIpPath)

	// Calculate statistics for response
	setsWithStats := make([]SetWithStats, len(newConfig.Sets))
	allDomainsCount := 0
	allIpsCount := 0

	for i, set := range newConfig.Sets {
		a.loadTargetsForSetCached(set)

		manualDomains := len(set.Targets.SNIDomains)
		manualIPs := len(set.Targets.IPs)

		// Get geosite counts
		geositeCounts := make(map[string]int)
		geositeTotalDomains := 0
		if len(set.Targets.GeoSiteCategories) > 0 {
			counts, err := a.geodataManager.GetGeositeCategoryCounts(set.Targets.GeoSiteCategories)
			if err == nil {
				geositeCounts = counts
				for _, count := range counts {
					geositeTotalDomains += count
				}
			}
		}

		// Get geoip counts
		geoipCounts := make(map[string]int)
		geoipTotalIPs := 0
		if len(set.Targets.GeoIpCategories) > 0 {
			counts, err := a.geodataManager.GetGeoipCategoryCounts(set.Targets.GeoIpCategories)
			if err == nil {
				geoipCounts = counts
				for _, count := range counts {
					geoipTotalIPs += count
				}
			}
		}

		setTotalDomains := manualDomains + geositeTotalDomains
		setTotalIPs := manualIPs + geoipTotalIPs

		allDomainsCount += setTotalDomains
		allIpsCount += setTotalIPs

		setsWithStats[i] = SetWithStats{
			SetConfig: set,
			Stats: SetStatistics{
				ManualDomains:            manualDomains,
				ManualIPs:                manualIPs,
				GeositeDomains:           geositeTotalDomains,
				TotalDomains:             setTotalDomains,
				TotalIPs:                 setTotalIPs,
				GeositeCategoryBreakdown: geositeCounts,
				GeoipCategoryBreakdown:   geoipCounts,
			},
		}
	}

	if err := a.saveAndPushConfig(&newConfig); err != nil {
		log.Errorf("Failed to update config: %v", err)
		http.Error(w, "Failed to update config", http.StatusInternalServerError)
		return
	}

	if a.PerformSoftRestart(&newConfig, oldConfig) {
		log.Infof("Soft restart completed successfully")
	}

	m := metrics.GetMetricsCollector()
	m.RecordEvent("info", fmt.Sprintf("Loaded %d domains and %d IPs across %d sets", allDomainsCount, allIpsCount, len(newConfig.Sets)))
	log.Infof("Loaded %d domains and %d IPs across %d sets", allDomainsCount, allIpsCount, len(newConfig.Sets))

	response := ConfigResponse{
		Success: true,
		Message: "Configuration updated successfully",
		Config:  &newConfig,
		Sets:    setsWithStats,
	}

	setJsonHeader(w)
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	_ = enc.Encode(response)
}

func (a *API) resetConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	log.Infof("Config reset requested")
	oldConfig := a.cfg.Clone()

	defaultCfg := config.NewConfig()
	defaultCfg.System.Checker = a.cfg.System.Checker
	defaultCfg.ConfigPath = a.cfg.ConfigPath
	defaultCfg.System.WebServer.IsEnabled = a.cfg.System.WebServer.IsEnabled

	for _, set := range a.cfg.Sets {
		set.ResetToDefaults()
		a.loadTargetsForSetCached(set)
		defaultCfg.Sets = append(defaultCfg.Sets, set)
	}

	if err := a.saveAndPushConfig(&defaultCfg); err != nil {
		log.Errorf("Failed to reset config: %v", err)
		http.Error(w, "Failed to reset config", http.StatusInternalServerError)
		return
	}

	if a.PerformSoftRestart(&defaultCfg, oldConfig) {
		log.Infof("Soft restart completed successfully")
	}

	setJsonHeader(w)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Configuration reset to defaults (domains and checker preserved)",
	})
}

func (a *API) saveAndPushConfig(newCfg *config.Config) error {

	if err := newCfg.Validate(); err != nil {
		return log.Errorf("Invalid configuration: %v", err)
	}

	if globalPool != nil {
		err := globalPool.UpdateConfig(newCfg)
		if err != nil {
			return fmt.Errorf("failed to update global pool config: %v", err)
		}
	}

	err := newCfg.SaveToFile(newCfg.ConfigPath)
	if err != nil {
		return fmt.Errorf("failed to save config to file: %v", err)
	}

	*a.cfg = *newCfg

	return nil
}

func (a *API) PerformSoftRestart(newCfg *config.Config, oldCfg *config.Config) bool {

	oldPorts := strings.Join(oldCfg.CollectUDPPorts(), ",")
	newPorts := strings.Join(newCfg.CollectUDPPorts(), ",")
	shouldUpdate := false
	if oldCfg.System.Tables.SkipSetup != newCfg.System.Tables.SkipSetup {

		shouldUpdate = true
	}

	if !newCfg.System.Tables.SkipSetup && oldPorts != newPorts {
		shouldUpdate = true
	}

	if oldCfg.MainSet.TCP.ConnBytesLimit != newCfg.MainSet.TCP.ConnBytesLimit {
		shouldUpdate = true
	}

	if oldCfg.MainSet.UDP.ConnBytesLimit != newCfg.MainSet.UDP.ConnBytesLimit {
		shouldUpdate = true
	}

	if oldCfg.Queue.Mark != newCfg.Queue.Mark {
		shouldUpdate = true
	}

	if oldCfg.Queue.IPv4Enabled != newCfg.Queue.IPv4Enabled {
		shouldUpdate = true
	}
	if oldCfg.Queue.IPv6Enabled != newCfg.Queue.IPv6Enabled {
		shouldUpdate = true
	}

	if shouldUpdate {
		log.Infof("Core settings changed, performing soft system restart")
		if oldPorts != newPorts {
			log.Infof("UDP ports changed (%s -> %s), refreshing firewall rules", oldPorts, newPorts)
		}
		if err := tablesRefreshFunc(); err != nil {
			log.Errorf("Failed to refresh tables: %v", err)
		}
	}

	return shouldUpdate
}
