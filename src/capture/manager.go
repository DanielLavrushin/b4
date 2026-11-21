package capture

import (
	"bytes"
	"crypto/sha256"
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
	"github.com/daniellavrushin/b4/tlsgen"
	"github.com/daniellavrushin/b4/utils"
)

const payloadFilenameFmt = "%s_%s.bin"

var (
	instance *Manager
	once     sync.Once
)

func GetManager(cfg *config.Config) *Manager {
	once.Do(func() {
		instance = newManager(filepath.Join(filepath.Dir(cfg.ConfigPath), "captures"))
		os.MkdirAll(instance.outputPath, 0755)
		instance.loadMetadata()
	})
	return instance
}

func newManager(outputPath string) *Manager {
	return &Manager{
		metadata:     make(map[string]map[string]*PayloadMetadata),
		outputPath:   outputPath,
		metadataFile: filepath.Join(outputPath, "payloads.json"),
	}
}

func (m *Manager) GetOutputPath() string {
	return m.outputPath
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
	if err := utils.WriteFileAtomic(m.metadataFile, data, 0644); err != nil {
		return log.Errorf("failed to write capture metadata: %v", err)
	}
	return nil
}

func (m *Manager) GenerateCapture(domain, protocol string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.metadata[domain] != nil && m.metadata[domain][protocol] != nil {
		return fmt.Errorf("already captured")
	}

	if protocol != "tls" {
		return fmt.Errorf("generation only supported for TLS")
	}

	payload, err := tlsgen.GenerateTLSClientHello(domain)
	if err != nil {
		return fmt.Errorf("failed to generate payload: %v", err)
	}

	filename := fmt.Sprintf(payloadFilenameFmt, protocol, sanitizeDomain(domain))
	filepath := filepath.Join(m.outputPath, filename)

	if err := os.WriteFile(filepath, payload, 0644); err != nil {
		return fmt.Errorf("failed to save payload: %v", err)
	}

	if m.metadata[domain] == nil {
		m.metadata[domain] = make(map[string]*PayloadMetadata)
	}

	m.metadata[domain][protocol] = &PayloadMetadata{
		Timestamp: time.Now(),
		Size:      len(payload),
		Filepath:  filename,
	}

	if err := m.saveMetadata(); err != nil {
		log.Errorf("Failed to save metadata: %v", err)
	}

	log.Infof("✓ Generated a %d-byte TLS ClientHello for %s", len(payload), domain)

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
			fullPath := filepath.Join(m.outputPath, meta.Filepath)
			data, err := os.ReadFile(fullPath)
			var hexData string
			if err == nil {
				hexData = hex.EncodeToString(data)
			}

			// Return relative path in the format "captures/filename.bin"
			// This is what the config system expects when loading payloads
			relativePath := filepath.Join("captures", meta.Filepath)

			captures = append(captures, &Capture{
				Protocol:  protocol,
				Domain:    domain,
				Timestamp: meta.Timestamp,
				Size:      meta.Size,
				Filepath:  relativePath,
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
	fullPath := filepath.Join(m.outputPath, meta.Filepath)
	data, err := os.ReadFile(fullPath)
	var hexData string
	if err == nil {
		hexData = hex.EncodeToString(data)
	}

	// Return relative path in the format "captures/filename.bin"
	// This is what the config system expects when loading payloads
	relativePath := filepath.Join("captures", meta.Filepath)

	capture := &Capture{
		Protocol:  protocol,
		Domain:    domain,
		Timestamp: meta.Timestamp,
		Size:      meta.Size,
		Filepath:  relativePath,
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
	filename := fmt.Sprintf(payloadFilenameFmt, protocol, sanitizeDomain(domain))
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

func (m *Manager) SaveImportedPayload(protocol, domain string, data []byte) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		domain = "shared"
	}
	sum := sha256.Sum256(data)
	candidates := []string{domain, domain + "-" + hex.EncodeToString(sum[:4])}
	for _, name := range candidates {
		filename := fmt.Sprintf(payloadFilenameFmt, protocol, sanitizeDomain(name))
		filePath := filepath.Join(m.outputPath, filename)
		existing, err := os.ReadFile(filePath)
		if err == nil {
			if !bytes.Equal(existing, data) {
				continue
			}
			if m.metadata[name] == nil || m.metadata[name][protocol] == nil {
				at := time.Now()
				if info, err := os.Stat(filePath); err == nil {
					at = info.ModTime()
				}
				if err := m.registerImportedPayload(name, protocol, filename, len(data), at); err != nil {
					return "", err
				}
				log.Infof("Registered existing shared %s payload for %s (%d bytes)", protocol, name, len(data))
			}
			return filepath.Join("captures", filename), nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("failed to inspect %s: %v", filename, err)
		}
		if err := os.WriteFile(filePath, data, 0644); err != nil {
			return "", fmt.Errorf("failed to save file: %v", err)
		}
		if err := m.registerImportedPayload(name, protocol, filename, len(data), time.Now()); err != nil {
			return "", err
		}
		log.Infof("Saved shared %s payload for %s (%d bytes)", protocol, name, len(data))
		return filepath.Join("captures", filename), nil
	}
	return "", fmt.Errorf("a different payload already exists under every name derived from %s", domain)
}

func (m *Manager) registerImportedPayload(name, protocol, filename string, size int, at time.Time) error {
	if m.metadata[name] == nil {
		m.metadata[name] = make(map[string]*PayloadMetadata)
	}
	m.metadata[name][protocol] = &PayloadMetadata{
		Timestamp: at,
		Size:      size,
		Filepath:  filename,
	}
	if err := m.saveMetadata(); err != nil {
		delete(m.metadata[name], protocol)
		if len(m.metadata[name]) == 0 {
			delete(m.metadata, name)
		}
		return fmt.Errorf("failed to save metadata: %v", err)
	}
	return nil
}

func (m *Manager) LoadCaptureData(c *Capture) ([]byte, error) {
	if c == nil || c.Filepath == "" {
		return nil, fmt.Errorf("invalid capture")
	}

	filename := filepath.Base(c.Filepath)

	if !strings.HasSuffix(filename, ".bin") {
		return nil, fmt.Errorf("invalid file extension: %s", filename)
	}

	fullPath := filepath.Clean(filepath.Join(m.outputPath, filename))
	cleanOutputPath := filepath.Clean(m.outputPath)

	if !strings.HasPrefix(fullPath, cleanOutputPath+string(filepath.Separator)) &&
		fullPath != cleanOutputPath {
		return nil, fmt.Errorf("access denied: path outside captures directory")
	}

	return os.ReadFile(fullPath)
}
