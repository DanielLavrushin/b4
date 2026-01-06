package capture

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

var (
	instance *Manager
	once     sync.Once
)

func GetManager(cfg *config.Config) *Manager {
	once.Do(func() {
		baseDirPath := filepath.Dir(cfg.ConfigPath)
		outputPath := filepath.Join(baseDirPath, "captures")

		instance = &Manager{
			metadata:        make(map[string]map[string]*PayloadMetadata),
			outputPath:      outputPath,
			metadataFile:    filepath.Join(outputPath, "payloads.json"),
			activeProbes:    make(map[string]time.Time),
			pendingCaptures: make(map[string]*PendingCapture),
			connToDomain:    make(map[string]string),
		}

		os.MkdirAll(instance.outputPath, 0755)

		instance.loadMetadata()
		go instance.cleanupExpiredProbes()
	})
	return instance
}

func (m *Manager) GetOutputPath() string {
	return m.outputPath
}

func (m *Manager) cleanupExpiredProbes() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		m.mu.Lock()
		now := time.Now()

		for key, expiry := range m.activeProbes {
			if now.After(expiry) {
				delete(m.activeProbes, key)
				log.Infof("Probe expired: %s", key)
			}
		}

		for connKey, pending := range m.pendingCaptures {
			if now.Sub(pending.firstSeen) > 5*time.Second {
				delete(m.pendingCaptures, connKey)
				delete(m.connToDomain, connKey)
				log.Tracef("Cleaned stale capture for %s", connKey)
			}
		}

		m.mu.Unlock()
	}
}

func (m *Manager) loadMetadata() {
	data, err := os.ReadFile(m.metadataFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Errorf("Failed to read metadata: %v", err)
		}
		return
	}

	if err := json.Unmarshal(data, &m.metadata); err != nil {
		log.Errorf("Failed to parse metadata: %v", err)
	}
}

func (m *Manager) saveMetadata() error {
	if m.metadata == nil {
		m.metadata = make(map[string]map[string]*PayloadMetadata)
	}
	data, err := json.MarshalIndent(m.metadata, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.OpenFile(m.metadataFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return log.Errorf("failed to create config file: %v", err)
	}
	defer file.Close()

	_, err = file.Write(data)
	if err != nil {
		return log.Errorf("failed to write config file: %v", err)
	}
	return nil
}

func (m *Manager) CapturePayload(connKey, domain, protocol string, payload []byte) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(payload) == 0 {
		return false
	}

	log.Tracef("CapturePayload: connKey=%s, domain=%s, protocol=%s, len=%d",
		connKey, domain, protocol, len(payload))

	if domain != "" {
		domain = strings.ToLower(strings.TrimSpace(domain))
		m.connToDomain[connKey] = domain
		log.Tracef("Mapped connection %s -> %s", connKey, domain)
	}

	pending, exists := m.pendingCaptures[connKey]
	if !exists {
		if protocol == "tls" {
			if len(payload) < 6 || payload[0] != 0x16 || payload[5] != 0x01 {
				return false
			}
		}

		if domain == "" {
			if mappedDomain, ok := m.connToDomain[connKey]; ok {
				domain = mappedDomain
			} else {
				if protocol == "tls" {
					log.Tracef("No domain yet for TLS connection %s, ignoring", connKey)
					return false
				}
			}
		}

		pending = &PendingCapture{
			protocol:  protocol,
			domain:    domain,
			data:      make([]byte, 0, 4096),
			firstSeen: time.Now(),
		}
		m.pendingCaptures[connKey] = pending
	} else {
		if domain != "" && pending.domain == "" {
			pending.domain = domain
			log.Tracef("Updated pending capture domain to %s", domain)
		}
	}

	if pending.domain == "" {
		return false
	}

	probeKey := fmt.Sprintf("%s:%s", protocol, pending.domain)
	if expiry, exists := m.activeProbes[probeKey]; !exists || time.Now().After(expiry) {
		return false
	}

	pending.data = append(pending.data, payload...)
	log.Tracef("Connection %s: accumulated %d bytes (total: %d)",
		connKey, len(payload), len(pending.data))

	var captureData []byte
	switch protocol {
	case "tls":
		if len(pending.data) < 9 {
			return false
		}

		if pending.data[0] != 0x16 {
			log.Warnf("Not a TLS handshake: %02x", pending.data[0])
			delete(m.pendingCaptures, connKey)
			return false
		}

		if pending.data[5] != 0x01 {
			log.Warnf("Not a ClientHello: %02x", pending.data[5])
			delete(m.pendingCaptures, connKey)
			return false
		}

		recordLen := int(pending.data[3])<<8 | int(pending.data[4])
		handshakeLen := int(pending.data[6])<<16 | int(pending.data[7])<<8 | int(pending.data[8])

		totalNeeded := 9 + handshakeLen

		if totalNeeded > 4096 {
			log.Warnf("ClientHello too large: %d bytes", totalNeeded)
			delete(m.pendingCaptures, connKey)
			return false
		}

		log.Tracef("TLS ClientHello: record_len=%d, handshake_len=%d, need=%d, have=%d",
			recordLen, handshakeLen, totalNeeded, len(pending.data))

		if len(pending.data) < totalNeeded {
			// Need more data
			return false
		}

		captureData = pending.data[:totalNeeded]

		// Log size for debugging
		log.Infof("Capturing TLS ClientHello for %s: %d bytes", pending.domain, totalNeeded)

	case "quic":
		// QUIC Initial packet - capture first packet only
		captureData = payload // Use only this packet, not accumulated
		log.Infof("Capturing QUIC Initial for %s: %d bytes", pending.domain, len(captureData))
	default:
		captureData = pending.data
	}

	filename := fmt.Sprintf("%s_%s.bin", protocol, sanitizeDomain(pending.domain))
	filepath := filepath.Join(m.outputPath, filename)

	if err := os.WriteFile(filepath, captureData, 0644); err != nil {
		log.Errorf("Failed to save capture: %v", err)
		return false
	}

	// Update metadata
	if m.metadata[pending.domain] == nil {
		m.metadata[pending.domain] = make(map[string]*PayloadMetadata)
	}

	m.metadata[pending.domain][protocol] = &PayloadMetadata{
		Timestamp: time.Now(),
		Size:      len(captureData),
		Filepath:  filename,
	}

	m.saveMetadata()

	// Clean up
	delete(m.pendingCaptures, connKey)
	delete(m.connToDomain, connKey)

	// Remove probe
	delete(m.activeProbes, probeKey)

	log.Infof("✓ Captured %s payload for %s (%d bytes)",
		protocol, pending.domain, len(captureData))

	return true
}

func (m *Manager) ProbeCapture(domain, protocol string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already captured
	if m.metadata[domain] != nil && m.metadata[domain][protocol] != nil {
		return fmt.Errorf("already captured")
	}

	// Enable capture for 30 seconds to give time to open browser
	if protocol == "both" {
		m.activeProbes[fmt.Sprintf("tls:%s", domain)] = time.Now().Add(30 * time.Second)
		m.activeProbes[fmt.Sprintf("quic:%s", domain)] = time.Now().Add(30 * time.Second)
		log.Infof("Capture enabled for both TLS and QUIC on %s (expires in 30s)", domain)
	} else {
		key := fmt.Sprintf("%s:%s", protocol, domain)
		m.activeProbes[key] = time.Now().Add(30 * time.Second)
		log.Infof("Capture enabled for %s (expires in 30s)", key)
	}

	log.Infof("Open https://%s in your browser NOW to capture real payload", domain)

	return nil
}

// ListCaptures returns all captured payloads for API
func (m *Manager) ListCaptures() []*Capture {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var captures []*Capture

	for domain, protocols := range m.metadata {
		for protocol, meta := range protocols {
			// Read hex data on demand
			filepath := filepath.Join(m.outputPath, meta.Filepath)
			data, err := os.ReadFile(filepath)
			var hexData string
			if err == nil {
				hexData = hex.EncodeToString(data)
			}

			captures = append(captures, &Capture{
				Protocol:  protocol,
				Domain:    domain,
				Timestamp: meta.Timestamp,
				Size:      meta.Size,
				Filepath:  filepath,
				HexData:   hexData,
			})
		}
	}

	return captures
}

// GetCapture returns specific capture
func (m *Manager) GetCapture(protocol, domain string) (*Capture, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.metadata[domain] == nil || m.metadata[domain][protocol] == nil {
		return nil, false
	}

	meta := m.metadata[domain][protocol]
	filepath := filepath.Join(m.outputPath, meta.Filepath)
	data, err := os.ReadFile(filepath)
	var hexData string
	if err == nil {
		hexData = hex.EncodeToString(data)
	}

	capture := &Capture{
		Protocol:  protocol,
		Domain:    domain,
		Timestamp: meta.Timestamp,
		Size:      meta.Size,
		Filepath:  filepath,
		HexData:   hexData,
	}

	return capture, true
}

// DeleteCapture removes a capture
func (m *Manager) DeleteCapture(protocol, domain string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.metadata[domain] == nil || m.metadata[domain][protocol] == nil {
		return fmt.Errorf("capture not found")
	}

	// Delete file
	meta := m.metadata[domain][protocol]
	filepath := filepath.Join(m.outputPath, meta.Filepath)
	os.Remove(filepath)

	// Update metadata
	delete(m.metadata[domain], protocol)
	if len(m.metadata[domain]) == 0 {
		delete(m.metadata, domain)
	}

	// Save updated metadata
	if err := m.saveMetadata(); err != nil {
		log.Errorf("Failed to save metadata: %v", err)
	}

	log.Infof("Deleted capture for %s:%s", protocol, domain)
	return nil
}

// ClearAll removes all captures
func (m *Manager) ClearAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Delete all binary files
	for _, protocols := range m.metadata {
		for _, meta := range protocols {
			filepath := filepath.Join(m.outputPath, meta.Filepath)
			os.Remove(filepath)
		}
	}

	// Clear metadata
	m.metadata = make(map[string]map[string]*PayloadMetadata)

	// Save empty metadata
	if err := m.saveMetadata(); err != nil {
		log.Errorf("Failed to save metadata: %v", err)
	}

	log.Infof("Cleared all captures")
	return nil
}

func sanitizeDomain(domain string) string {
	result := ""
	for _, ch := range domain {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '-' {
			result += string(ch)
		} else if ch == '.' {
			result += "_"
		}
	}
	return result
}

func (m *Manager) SaveUploadedCapture(protocol, domain string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	domain = strings.ToLower(strings.TrimSpace(domain))
	filename := fmt.Sprintf("%s_%s.bin", protocol, sanitizeDomain(domain))
	filePath := filepath.Join(m.outputPath, filename)

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to save file: %v", err)
	}

	if m.metadata[domain] == nil {
		m.metadata[domain] = make(map[string]*PayloadMetadata)
	}

	m.metadata[domain][protocol] = &PayloadMetadata{
		Timestamp: time.Now(),
		Size:      len(data),
		Filepath:  filename,
	}

	if err := m.saveMetadata(); err != nil {
		return fmt.Errorf("failed to save metadata: %v", err)
	}

	log.Infof("✓ Saved uploaded %s payload for %s (%d bytes)", protocol, domain, len(data))
	return nil
}

func (m *Manager) LoadCaptureData(c *Capture) ([]byte, error) {
	if c == nil || c.Filepath == "" {
		return nil, fmt.Errorf("invalid capture")
	}
	return os.ReadFile(c.Filepath)
}
