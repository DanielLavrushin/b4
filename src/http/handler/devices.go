package handler

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/log"
)

const (
	ouiURL    = "https://standards-oui.ieee.org/oui/oui.txt"
	ouiMaxAge = 30 * 24 * time.Hour // refresh monthly
)

type VendorInfo struct {
	Company string `json:"company"`
}

type OUIDatabase struct {
	data      map[string]string // OUI -> Company
	mu        sync.RWMutex
	cachePath string
	loading   bool
	loadingMu sync.Mutex
}

type DeviceInfo struct {
	MAC       string `json:"mac"`
	IP        string `json:"ip"`
	Hostname  string `json:"hostname"`
	Vendor    string `json:"vendor"`
	IsPrivate bool   `json:"is_private"`
	Alias     string `json:"alias,omitempty"`
	IsManual  bool   `json:"is_manual,omitempty"`
	IsOnline  bool   `json:"is_online"`
	MSSClamp  int    `json:"mss_clamp,omitempty"`
}

type DevicesResponse struct {
	Available bool         `json:"available"`
	Source    string       `json:"source,omitempty"`
	Devices   []DeviceInfo `json:"devices"`
	RouterIPs []string     `json:"router_ips,omitempty"`
}

var (
	ouiDB   *OUIDatabase
	ouiOnce sync.Once
)

func initOUIDatabase(configPath string) *OUIDatabase {
	ouiOnce.Do(func() {
		cachePath := "/tmp/b4_oui.txt" // fallback
		if configPath != "" {
			cachePath = filepath.Join(filepath.Dir(configPath), "oui.txt")
		}

		ouiDB = &OUIDatabase{
			data:      make(map[string]string),
			cachePath: cachePath,
		}
	})
	return ouiDB
}

func (db *OUIDatabase) Cleanup() {
	db.mu.Lock()
	db.data = make(map[string]string)
	db.mu.Unlock()

	if db.cachePath == "" || filepath.Base(db.cachePath) != "oui.txt" {
		return
	}

	if err := os.Remove(db.cachePath); err != nil && !os.IsNotExist(err) {
		log.Errorf("Failed to remove OUI cache file: %v", err)
	} else if err == nil {
		log.Infof("OUI cache file removed: %s", db.cachePath)
	}
}

func (db *OUIDatabase) ensureLoaded() {
	db.loadingMu.Lock()
	if db.loading {
		db.loadingMu.Unlock()
		return
	}
	db.loading = true
	db.loadingMu.Unlock()

	defer func() {
		db.loadingMu.Lock()
		db.loading = false
		db.loadingMu.Unlock()
	}()

	// Check if cache exists and is fresh
	if info, err := os.Stat(db.cachePath); err == nil {
		if time.Since(info.ModTime()) < ouiMaxAge {
			if err := db.loadFromFile(); err == nil {
				log.Infof("OUI database loaded from cache: %d entries", len(db.data))
				return
			}
		}
	}

	// Download fresh copy
	if err := db.download(); err != nil {
		log.Errorf("Failed to download OUI database: %v", err)
		// Try loading stale cache as fallback
		if err := db.loadFromFile(); err == nil {
			log.Infof("OUI database loaded from stale cache: %d entries", len(db.data))
		}
		return
	}

	if err := db.loadFromFile(); err != nil {
		log.Errorf("Failed to load OUI database: %v", err)
		return
	}

	log.Infof("OUI database refreshed: %d entries", len(db.data))
}

func (db *OUIDatabase) download() error {
	client := &http.Client{Timeout: 60 * time.Second}

	req, err := http.NewRequest("GET", ouiURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	// IEEE blocks requests without a browser-like User-Agent
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download status: %d", resp.StatusCode)
	}

	tmpPath := db.cachePath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		f.WriteString(scanner.Text() + "\n")
	}
	f.Close()

	if err := scanner.Err(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("read response: %w", err)
	}

	if err := os.Rename(tmpPath, db.cachePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}

	return nil
}

func (db *OUIDatabase) loadFromFile() error {
	f, err := os.Open(db.cachePath)
	if err != nil {
		return err
	}
	defer f.Close()

	newData := make(map[string]string)
	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		line := scanner.Text()
		// Format: "XX-XX-XX   (hex)		Company Name"
		if !strings.Contains(line, "(hex)") {
			continue
		}

		parts := strings.SplitN(line, "(hex)", 2)
		if len(parts) != 2 {
			continue
		}

		oui := normalizeMAC(strings.TrimSpace(parts[0]))
		if len(oui) != 6 {
			continue
		}

		company := strings.TrimSpace(parts[1])
		if company != "" {
			newData[oui] = company
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	db.mu.Lock()
	db.data = newData
	db.mu.Unlock()

	return nil
}

func (db *OUIDatabase) Lookup(mac string) string {
	normalized := normalizeMAC(mac)
	if len(normalized) < 6 {
		return ""
	}

	// Trigger lazy load on first use
	db.mu.RLock()
	empty := len(db.data) == 0
	db.mu.RUnlock()
	if empty {
		go db.ensureLoaded()
		return ""
	}

	db.mu.RLock()
	company := db.data[normalized[:6]]
	db.mu.RUnlock()

	return company
}

func (api *API) RegisterDevicesApi() {
	initOUIDatabase(api.getCfg().ConfigPath)

	api.mux.HandleFunc("/api/devices", api.handleDevices)
	api.mux.HandleFunc("/api/devices/{mac}/vendor", api.handleDeviceVendor)
}

// @Summary Get device vendor info
// @Tags Devices
// @Produce json
// @Param mac path string true "MAC address"
// @Success 200 {object} VendorInfo
// @Security BearerAuth
// @Router /devices/{mac}/vendor [get]
func (api *API) handleDeviceVendor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	mac := r.PathValue("mac")
	if mac == "" {
		http.Error(w, "MAC address required", http.StatusBadRequest)
		return
	}

	var company string
	if api.getCfg().Queue.Devices.VendorLookup {
		company = ouiDB.Lookup(mac)
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(VendorInfo{Company: company})
}

// @Summary Get all devices
// @Tags Devices
// @Produce json
// @Success 200 {object} DevicesResponse
// @Security BearerAuth
// @Router /devices [get]
func (api *API) handleDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	setJsonHeader(w)

	if globalPool == nil || globalPool.Dhcp == nil || !globalPool.Dhcp.IsAvailable() {
		json.NewEncoder(w).Encode(DevicesResponse{
			Available: false,
			Devices:   []DeviceInfo{},
		})
		return
	}

	cfg := api.getCfg()
	sourceName, _ := globalPool.Dhcp.SourceInfo()
	mappings := globalPool.Dhcp.GetAllMappings()
	hostnames := globalPool.Dhcp.GetAllHostnames()
	hostnamesNorm := make(map[string]string, len(hostnames))
	for mac, hn := range hostnames {
		hostnamesNorm[normalizeMAC(mac)] = hn
	}
	devices := make([]DeviceInfo, 0, len(mappings))
	seen := make(map[string]struct{}, len(mappings))

	for ip, macAddr := range mappings {
		var vendor string
		var isPrivate bool

		if isPrivateMAC(macAddr) {
			vendor = "Private"
			isPrivate = true
		} else if cfg.Queue.Devices.VendorLookup {
			vendor = ouiDB.Lookup(macAddr)
		}

		var alias string
		var isManual bool
		var mssClamp int
		if d := cfg.Queue.Devices.FindByMAC(macAddr); d != nil {
			alias = d.Name
			isManual = d.IsManual
			mssClamp = d.MSSClamp
		}

		seen[normalizeMAC(macAddr)] = struct{}{}
		devices = append(devices, DeviceInfo{
			MAC:       macAddr,
			IP:        ip,
			Hostname:  hostnamesNorm[normalizeMAC(macAddr)],
			Vendor:    vendor,
			IsPrivate: isPrivate,
			Alias:     alias,
			IsManual:  isManual,
			IsOnline:  true,
			MSSClamp:  mssClamp,
		})
	}

	for i := range cfg.Queue.Devices.Devices {
		d := &cfg.Queue.Devices.Devices[i]
		if d.Name == "" || d.MAC == "" {
			continue
		}
		mac := normalizeMAC(d.MAC)
		if _, ok := seen[mac]; ok {
			continue
		}
		seen[mac] = struct{}{}

		var vendor string
		isPrivate := isPrivateMAC(mac)
		if isPrivate {
			vendor = "Private"
		} else if cfg.Queue.Devices.VendorLookup {
			vendor = ouiDB.Lookup(mac)
		}

		devices = append(devices, DeviceInfo{
			MAC:       d.MAC,
			IP:        d.IP,
			Hostname:  hostnamesNorm[mac],
			Vendor:    vendor,
			IsPrivate: isPrivate,
			Alias:     d.Name,
			IsManual:  d.IsManual,
			IsOnline:  false,
			MSSClamp:  d.MSSClamp,
		})
	}

	json.NewEncoder(w).Encode(DevicesResponse{
		Available: true,
		Source:    sourceName,
		Devices:   devices,
		RouterIPs: globalPool.Dhcp.RouterIPs(),
	})
}

func normalizeMAC(mac string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(mac, ":", ""), "-", ""))
}

func isPrivateMAC(mac string) bool {
	normalized := normalizeMAC(mac)
	if len(normalized) < 2 {
		return false
	}
	secondChar := normalized[1]
	return secondChar == '2' || secondChar == '6' || secondChar == 'A' || secondChar == 'E'
}
