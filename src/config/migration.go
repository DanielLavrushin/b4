package config

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/daniellavrushin/b4/log"
	"github.com/google/uuid"
)

type MigrationFunc func(*Config, map[string]interface{}) error

const (
	CurrentConfigVersion = 51
	MinSupportedVersion  = 0
)

var migrationRegistry = map[int]MigrationFunc{
	14: migrateV14to15,
	24: migrateV24to25,
	33: migrateV33to34,
	45: migrateV45to46,
	46: migrateV46to47,
	48: migrateV48to49,
	49: migrateV49to50,
}

func migrateV49to50(c *Config, raw map[string]interface{}) error {
	log.Tracef("Migration v49->v50: Moving single MTProto secret into secrets list")
	m := &c.System.MTProto
	if len(m.Secrets) > 0 {
		return nil
	}
	system, _ := raw["system"].(map[string]interface{})
	mt, _ := system["mtproto"].(map[string]interface{})
	legacy, _ := mt["secret"].(string)
	if strings.TrimSpace(legacy) != "" {
		m.Secrets = []MTProtoSecret{{
			ID:      uuid.NewString(),
			Name:    "default",
			Secret:  strings.TrimSpace(legacy),
			Enabled: true,
		}}
	}
	return nil
}

func migrateV48to49(c *Config, raw map[string]interface{}) error {
	log.Tracef("Migration v48->v49: Converting masquerade to nested config with multiple interfaces")
	system, _ := raw["system"].(map[string]interface{})
	tables, _ := system["tables"].(map[string]interface{})
	if iface, ok := tables["masquerade_interface"].(string); ok && iface != "" {
		c.System.Tables.Masquerade.Interfaces = []string{iface}
	}
	return nil
}

func migrateV46to47(c *Config, _ map[string]interface{}) error {
	log.Tracef("Migration v46->v47: Dropping hardcoded legacy MTProto WS endpoint host (empty falls back to the same edge)")
	if c.System.MTProto.WSEndpointHost == TGWSEndpointHost {
		c.System.MTProto.WSEndpointHost = ""
	}
	return nil
}

func migrateV45to46(c *Config, raw map[string]interface{}) error {
	log.Tracef("Migration v45->v46: Replacing logging.error_file with logging.directory")

	system, _ := raw["system"].(map[string]interface{})
	logging, _ := system["logging"].(map[string]interface{})
	old, present := logging["error_file"]
	if !present {
		// Nothing to convert — keep the default directory already in place.
		return nil
	}

	oldPath, _ := old.(string)
	if oldPath == "" {
		// Empty error_file meant "file logging disabled" — preserve that.
		c.System.Logging.Directory = ""
	} else {
		c.System.Logging.Directory = filepath.Dir(oldPath)
	}
	return nil
}

func migrateV33to34(c *Config, raw map[string]interface{}) error {
	log.Tracef("Migration v33->v34: Unifying device model")

	deviceMap := make(map[string]*Device)

	queue, _ := raw["queue"].(map[string]interface{})
	devicesRaw, _ := queue["devices"].(map[string]interface{})

	if macs, ok := devicesRaw["mac"].([]interface{}); ok {
		for _, m := range macs {
			if mac, ok := m.(string); ok {
				mac = strings.ToUpper(strings.TrimSpace(mac))
				if mac == "" {
					continue
				}
				d := deviceMap[mac]
				if d == nil {
					d = &Device{MAC: mac}
					deviceMap[mac] = d
				}
				d.Selected = true
			}
		}
	}

	if clamps, ok := devicesRaw["mss_clamps"].([]interface{}); ok {
		for _, item := range clamps {
			m, _ := item.(map[string]interface{})
			mac, _ := m["mac"].(string)
			size, _ := m["size"].(float64)
			mac = strings.ToUpper(strings.TrimSpace(mac))
			if mac == "" || int(size) <= 0 {
				continue
			}
			d := deviceMap[mac]
			if d == nil {
				d = &Device{MAC: mac}
				deviceMap[mac] = d
			}
			d.MSSClamp = int(size)
		}
	}

	if manuals, ok := devicesRaw["manual_devices"].([]interface{}); ok {
		for _, item := range manuals {
			m, _ := item.(map[string]interface{})
			ip, _ := m["ip"].(string)
			mac, _ := m["mac"].(string)
			name, _ := m["name"].(string)
			if ip == "" {
				continue
			}
			mac = strings.ToUpper(strings.TrimSpace(mac))
			if mac == "" {
				mac = generateSyntheticMAC(ip)
			}
			if mac == "" {
				continue
			}
			d := deviceMap[mac]
			if d == nil {
				d = &Device{MAC: mac}
				deviceMap[mac] = d
			}
			d.IP = ip
			d.IsManual = true
			if name != "" {
				d.Name = name
			}
		}
	}

	aliases := loadAliasesForMigration(c.ConfigPath)
	for mac, name := range aliases {
		mac = strings.ToUpper(strings.TrimSpace(mac))
		d := deviceMap[mac]
		if d == nil {
			d = &Device{MAC: mac}
			deviceMap[mac] = d
		}
		if d.Name == "" {
			d.Name = name
		}
	}

	devices := make([]Device, 0, len(deviceMap))
	for _, d := range deviceMap {
		devices = append(devices, *d)
	}
	c.Queue.Devices.Devices = devices

	return nil
}

func generateSyntheticMAC(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ""
	}
	v4 := parsed.To4()
	if v4 != nil {
		return fmt.Sprintf("02:B4:%02X:%02X:%02X:%02X", v4[0], v4[1], v4[2], v4[3])
	}
	v6 := parsed.To16()
	return fmt.Sprintf("02:B4:%02X:%02X:%02X:%02X", v6[12], v6[13], v6[14], v6[15])
}

func loadAliasesForMigration(configPath string) map[string]string {
	if configPath == "" {
		return nil
	}
	path := filepath.Join(filepath.Dir(configPath), "mac_aliases.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var aliases map[string]string
	if err := json.Unmarshal(data, &aliases); err != nil {
		return nil
	}
	return aliases
}

// Migration: v24 -> v25 (remove main set, move ConnBytesLimit to queue config)
func migrateV24to25(c *Config, _ map[string]interface{}) error {
	log.Tracef("Migration v24->v25: Removing main set, moving ConnBytesLimit to queue config")

	oldMainSetID := "11111111-1111-1111-1111-111111111111"

	// Move ConnBytesLimit from main set to queue config, and give it a new ID
	for _, set := range c.Sets {
		if set.Id == oldMainSetID {
			c.Queue.TCPConnBytesLimit = set.TCP.ConnBytesLimit
			c.Queue.UDPConnBytesLimit = set.UDP.ConnBytesLimit
			set.Id = uuid.New().String()
			break
		}
	}

	// Ensure defaults if main set wasn't found
	if c.Queue.TCPConnBytesLimit == 0 {
		c.Queue.TCPConnBytesLimit = DefaultConfig.Queue.TCPConnBytesLimit
	}
	if c.Queue.UDPConnBytesLimit == 0 {
		c.Queue.UDPConnBytesLimit = DefaultConfig.Queue.UDPConnBytesLimit
	}

	return nil
}

func migrateV14to15(c *Config, raw map[string]interface{}) error {
	sets, _ := raw["sets"].([]interface{})

	for i, setRaw := range sets {
		if i >= len(c.Sets) {
			break
		}
		setMap, _ := setRaw.(map[string]interface{})
		tcpMap, _ := setMap["tcp"].(map[string]interface{})
		if tcpMap == nil {
			continue
		}

		// Extract old flat fields
		if mode, ok := tcpMap["desync_mode"].(string); ok {
			c.Sets[i].TCP.Desync.Mode = mode
		}
		if ttl, ok := tcpMap["desync_ttl"].(float64); ok {
			c.Sets[i].TCP.Desync.TTL = uint8(ttl)
		}
		if count, ok := tcpMap["desync_count"].(float64); ok {
			c.Sets[i].TCP.Desync.Count = int(count)
		}
		if post, ok := tcpMap["post_desync"].(bool); ok {
			c.Sets[i].TCP.Desync.PostDesync = post
		}

		if mode, ok := tcpMap["win_mode"].(string); ok {
			c.Sets[i].TCP.Win.Mode = mode
		}
		if values, ok := tcpMap["win_values"].([]interface{}); ok {
			c.Sets[i].TCP.Win.Values = make([]int, len(values))
			for j, v := range values {
				if f, ok := v.(float64); ok {
					c.Sets[i].TCP.Win.Values[j] = int(f)
				}
			}
		}

	}
	return nil
}

// discoverConfigPath checks well-known locations for a config file.
func discoverConfigPath() string {
	candidates := []string{
		"/etc/b4/b4.json",
		"/etc/b4/config.json",
		"/opt/etc/b4/b4.json",
		"/opt/etc/b4/config.json",
	}
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	if info, err := os.Stat("/etc/b4"); err == nil && info.IsDir() {
		return "/etc/b4/b4.json"
	}
	if info, err := os.Stat("/opt/etc/b4"); err == nil && info.IsDir() {
		return "/opt/etc/b4/b4.json"
	}
	return "/etc/b4/b4.json"
}

func (c *Config) LoadWithMigration(path string) (bool, error) {
	if path == "" {
		path = discoverConfigPath()
		log.Infof("Using config path: %s", path)
	}
	c.ConfigPath = path

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			log.Infof("Config file does not exist yet, using defaults: %s", path)
			return true, nil
		}
		return false, log.Errorf("failed to stat config file: %v", err)
	}
	if info.IsDir() {
		return false, log.Errorf("config path is a directory, not a file: %s", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return false, log.Errorf("failed to read config file: %v", err)
	}

	var rawJSON map[string]interface{}
	if err := json.Unmarshal(data, &rawJSON); err != nil {
		return false, log.Errorf("failed to parse config file: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return false, log.Errorf("failed to parse config file: %v", err)
	}

	rawSets := raw["sets"]
	delete(raw, "sets")

	withoutSets, _ := json.Marshal(raw)
	if err := json.Unmarshal(withoutSets, c); err != nil {
		return false, log.Errorf("failed to parse config file: %v", err)
	}

	if rawSets != nil {
		var setArray []json.RawMessage
		if err := json.Unmarshal(rawSets, &setArray); err != nil {
			return false, log.Errorf("failed to parse sets: %v", err)
		}
		c.Sets = make([]*SetConfig, 0, len(setArray))
		for _, rs := range setArray {
			set := NewSetConfig()
			if err := json.Unmarshal(rs, &set); err != nil {
				return false, log.Errorf("failed to parse set: %v", err)
			}
			c.Sets = append(c.Sets, &set)
		}
	}

	migrated := false
	if c.Version < CurrentConfigVersion {
		migrated = true
		log.Infof("Config version %d is older than current version %d, migrating",
			c.Version, CurrentConfigVersion)
		backupConfigBeforeMigration(path, data, c.Version)
		if err := c.applyMigrations(c.Version, rawJSON); err != nil {
			return false, err
		}
	} else if c.Version > CurrentConfigVersion {
		log.Warnf("Config version %d is newer than the version %d supported by this binary; unknown settings will be dropped on the next save",
			c.Version, CurrentConfigVersion)
	}

	c.System.Geo.SanitizePaths(filepath.Dir(c.ConfigPath))

	if c.migratePasswordHash() {
		migrated = true
	}

	return migrated, nil
}

func backupConfigBeforeMigration(path string, data []byte, fromVersion int) {
	backupPath := fmt.Sprintf("%s.v%d.bak", path, fromVersion)
	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		log.Warnf("Failed to back up config to %s before migration: %v", backupPath, err)
		return
	}
	log.Infof("Backed up pre-migration config to %s", backupPath)
}

func (c *Config) migratePasswordHash() bool {
	p := c.System.WebServer.Password
	if p == "" || IsHashedPassword(p) {
		return false
	}
	h, err := HashPassword(p)
	if err != nil {
		log.Errorf("failed to hash web server password during migration: %v", err)
		return false
	}
	c.System.WebServer.Password = h
	log.Infof("Migrated plaintext web server password to bcrypt hash")
	return true
}

func (c *Config) applyMigrations(startVersion int, rawJSON map[string]interface{}) error {
	for v := startVersion; v < CurrentConfigVersion; v++ {
		migrationFunc, exists := migrationRegistry[v]
		if !exists {
			c.Version = v + 1
			continue
		}

		log.Infof("Applying migration: v%d -> v%d", v, v+1)
		if err := migrationFunc(c, rawJSON); err != nil {
			return fmt.Errorf("migration from v%d to v%d failed: %w", v, v+1, err)
		}
		c.Version = v + 1
	}
	return nil
}
