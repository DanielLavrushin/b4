// src/http/handler/config.go
package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/metrics"
	"github.com/daniellavrushin/b4/mtproto"
)

func (api *API) RegisterConfigApi() {

	api.mux.HandleFunc("/api/config", api.handleConfig)
	api.mux.HandleFunc("/api/config/reset", api.handleConfigReset)
}

// @Summary Reset configuration to defaults
// @Description Resets configuration to defaults, preserving sets, web server settings and geo file paths.
// @Tags Config
// @Produce json
// @Success 200 {object} ConfigResponse
// @Security BearerAuth
// @Router /config/reset [post]
func (a *API) handleConfigReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	curCfg := a.getCfg()
	oldConfig := curCfg.Clone()

	newCfg := config.NewConfig()
	newCfg.ConfigPath = curCfg.ConfigPath
	newCfg.Sets = oldConfig.Sets
	newCfg.System.WebServer = curCfg.System.WebServer
	newCfg.System.Geo = curCfg.System.Geo

	a.applyRuntimeChanges(&newCfg, curCfg)

	if err := a.saveAndPushConfig(&newCfg); err != nil {
		log.Errorf("Failed to reset config: %v", err)
		writeAPIError(w, ErrInternal("Failed to reset config"))
		return
	}

	a.PerformSoftRestart(&newCfg, oldConfig)

	setJsonHeader(w)
	_ = json.NewEncoder(w).Encode(ConfigResponse{
		Success: true,
		Message: "Configuration reset to defaults",
		Config:  &newCfg,
	})
}

func (a *API) applyRuntimeChanges(newCfg, oldCfg *config.Config) {
	if newCfg.System.Logging.Level != log.Level(log.CurLevel.Load()) {
		log.SetLevel(log.Level(newCfg.System.Logging.Level))
		log.Infof("Log level changed to %s", newCfg.System.Logging.Level)
	}

	if newCfg.System.Timezone != oldCfg.System.Timezone {
		config.ApplyTimezone(newCfg.System.Timezone)
		log.Infof("Timezone changed to %s", newCfg.System.Timezone)
	}

	a.geodataManager.UpdatePaths(newCfg.System.Geo.GeoSitePath, newCfg.System.Geo.GeoIpPath)
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

// @Summary Get full configuration with statistics
// @Tags Config
// @Produce json
// @Success 200 {object} ConfigResponse
// @Security BearerAuth
// @Router /config [get]
func (a *API) getConfig(w http.ResponseWriter) {
	setJsonHeader(w)

	// Calculate statistics for each set
	cfg := a.getCfg()
	setsWithStats := make([]SetWithStats, len(cfg.Sets))
	totalDomains := 0
	totalIPs := 0

	for i, set := range cfg.Sets {
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
		ifaces = []string{}
	} else if ifaces == nil {
		ifaces = []string{}
	}
	sort.Strings(ifaces)

	response := ConfigResponse{
		Config:              cfg,
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

	inDocker := isDockerEnvironment()

	// Known virtual/internal interface prefixes to exclude
	excludePrefixes := []string{
		"lo", "gre", "erspan", "ifb", "imq",
		"ip6_vti", "ip6gre", "ip6tnl", "ip_vti", "sit",
		"spu_", "bcmsw", "blog",
	}
	// Only exclude container-related interfaces when running on bare metal
	if !inDocker {
		excludePrefixes = append(excludePrefixes, "docker", "virbr")
	}

	var ifaceNames []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

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

		if inDocker {
			// In container mode, keep all UP interfaces (no IP requirement)
			ifaceNames = append(ifaceNames, iface.Name)
		} else {
			// On host, require IP or known useful prefix
			addrs, _ := iface.Addrs()
			allowedWithoutIP := strings.HasPrefix(iface.Name, "br") ||
				strings.HasPrefix(iface.Name, "wl") ||
				strings.HasPrefix(iface.Name, "tun") ||
				strings.HasPrefix(iface.Name, "tap") ||
				strings.HasPrefix(iface.Name, "dummy")

			if len(addrs) > 0 || allowedWithoutIP {
				ifaceNames = append(ifaceNames, iface.Name)
			}
		}
	}

	return ifaceNames, nil
}

// @Summary Update configuration
// @Tags Config
// @Accept json
// @Produce json
// @Param config body config.Config true "Updated configuration"
// @Success 200 {object} ConfigResponse
// @Security BearerAuth
// @Router /config [put]
func (a *API) updateConfig(w http.ResponseWriter, r *http.Request) {
	var newConfig config.Config

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&newConfig); err != nil {
		log.Errorf("Failed to decode config update: %v", err)
		writeAPIError(w, ErrInvalidJSON())
		return
	}

	curCfg := a.getCfg()
	oldConfig := curCfg.Clone()
	newConfig.ConfigPath = curCfg.ConfigPath

	a.applyRuntimeChanges(&newConfig, curCfg)

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
		writeAPIError(w, err)
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

func (a *API) saveAndPushConfig(newCfg *config.Config) error {

	if err := newCfg.Validate(); err != nil {
		var ve *config.ValidationError
		if errors.As(err, &ve) {
			log.Errorf("Invalid configuration: %v", err)
			return fromValidationError(ve)
		}
		return ErrInternal("Invalid configuration: " + err.Error())
	}

	if fields := preflightConfig(newCfg, a.getCfg()); len(fields) > 0 {
		return ErrValidation("Some ports are unavailable", fields...)
	}

	if globalPool != nil {
		err := globalPool.UpdateConfig(newCfg)
		if err != nil {
			return ErrInternal(fmt.Sprintf("failed to update global pool config: %v", err))
		}
	}

	if globalSocks5Server != nil {
		globalSocks5Server.UpdateConfig(newCfg)
	}

	if globalMTProtoServer != nil {
		globalMTProtoServer.UpdateConfig(newCfg)
	}

	if globalMTProtoBridge != nil {
		globalMTProtoBridge.UpdateConfig(newCfg)
	}

	if mtprotoCFRefreshFunc != nil {
		mtprotoCFRefreshFunc(newCfg)
	}

	oldMT := a.getCfg().System.MTProto
	newMT := newCfg.System.MTProto
	if oldMT.DCFallbackEnabled != newMT.DCFallbackEnabled || oldMT.DCFallbackURL != newMT.DCFallbackURL {
		go func() { _ = mtproto.RefreshDCs(newMT.DCFallbackEnabled, newMT.DCFallbackURL) }()
	}

	err := newCfg.SaveToFile(newCfg.ConfigPath)
	if err != nil {
		return fmt.Errorf("failed to save config to file: %v", err)
	}

	if ouiDB != nil {
		oldCfg := a.getCfg()
		if oldCfg.Queue.Devices.VendorLookup && !newCfg.Queue.Devices.VendorLookup {
			go ouiDB.Cleanup()
		} else if !oldCfg.Queue.Devices.VendorLookup && newCfg.Queue.Devices.VendorLookup {
			go ouiDB.ensureLoaded()
		}
	}

	a.cfgPtr.Store(newCfg)
	if routingSyncFunc != nil {
		routingSyncFunc(newCfg)
	}
	if globalAIManager != nil {
		globalAIManager.Update(newCfg.System.AI)
	}

	return nil
}

func (a *API) PerformSoftRestart(newCfg *config.Config, oldCfg *config.Config) bool {

	oldUDPPorts := strings.Join(oldCfg.CollectUDPPorts(), ",")
	newUDPPorts := strings.Join(newCfg.CollectUDPPorts(), ",")
	oldTCPPorts := strings.Join(oldCfg.CollectTCPPorts(), ",")
	newTCPPorts := strings.Join(newCfg.CollectTCPPorts(), ",")
	shouldUpdate := false
	if oldCfg.System.Tables.SkipSetup != newCfg.System.Tables.SkipSetup {

		shouldUpdate = true
	}

	if !newCfg.System.Tables.SkipSetup && oldUDPPorts != newUDPPorts {
		shouldUpdate = true
	}

	if !newCfg.System.Tables.SkipSetup && oldTCPPorts != newTCPPorts {
		shouldUpdate = true
	}

	if oldCfg.Queue.TCPConnBytesLimit != newCfg.Queue.TCPConnBytesLimit {
		shouldUpdate = true
	}

	if oldCfg.Queue.UDPConnBytesLimit != newCfg.Queue.UDPConnBytesLimit {
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

	if oldCfg.System.Tables.Masquerade != newCfg.System.Tables.Masquerade {
		shouldUpdate = true
	}
	if oldCfg.System.Tables.MasqueradeInterface != newCfg.System.Tables.MasqueradeInterface {
		shouldUpdate = true
	}

	if oldCfg.MSSClampFingerprint() != newCfg.MSSClampFingerprint() {
		shouldUpdate = true
		log.Infof("MSS clamp settings changed, refreshing firewall rules")
	}

	if shouldUpdate {
		log.Infof("Core settings changed, performing soft system restart")
		if oldUDPPorts != newUDPPorts {
			log.Infof("UDP ports changed (%s -> %s), refreshing firewall rules", oldUDPPorts, newUDPPorts)
		}
		if oldTCPPorts != newTCPPorts {
			log.Infof("TCP ports changed (%s -> %s), refreshing firewall rules", oldTCPPorts, newTCPPorts)
		}
		if err := tablesRefreshFunc(); err != nil {
			log.Errorf("Failed to refresh tables: %v", err)
		}
	}

	return shouldUpdate
}
