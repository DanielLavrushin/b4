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

	var loaded map[string]map[string]*PayloadMetadata
	if err := json.Unmarshal(data, &loaded); err != nil {
		log.Errorf("Failed to parse metadata: %v", err)
		return
	}
	m.metadata = usableMetadata(loaded)
}

func usableMetadata(loaded map[string]map[string]*PayloadMetadata) map[string]map[string]*PayloadMetadata {
	usable := make(map[string]map[string]*PayloadMetadata, len(loaded))
	for name, protocols := range loaded {
		for protocol, meta := range protocols {
			if meta == nil {
				continue
			}
			if !isPayloadFileName(meta.Filepath) {
				log.Errorf("Dropped the %s payload %q of payloads.json: its file %q is not a .bin file in the captures folder", protocol, name, meta.Filepath)
				continue
			}
			if usable[name] == nil {
				usable[name] = make(map[string]*PayloadMetadata)
			}
			usable[name][protocol] = meta
		}
	}
	return usable
}

func isPayloadFileName(name string) bool {
	return name != "" && filepath.Base(name) == name && strings.HasSuffix(name, ".bin")
}

func (m *Manager) payloadPath(name string) (string, error) {
	if !isPayloadFileName(name) {
		return "", fmt.Errorf("invalid payload file name %q", name)
	}
	fullPath := filepath.Clean(filepath.Join(m.outputPath, name))
	if !strings.HasPrefix(fullPath, filepath.Clean(m.outputPath)+string(filepath.Separator)) {
		return "", fmt.Errorf("access denied: path outside captures directory")
	}
	return fullPath, nil
}

func (m *Manager) readPayloadFile(name string) ([]byte, error) {
	path, err := m.payloadPath(name)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func (m *Manager) removePayloadFile(name string) {
	path, err := m.payloadPath(name)
	if err != nil {
		log.Errorf("Not removing payload file %q: %v", name, err)
		return
	}
	os.Remove(path)
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

	if err := m.storeLocked(protocol, domain, payload); err != nil {
		return err
	}

	log.Infof("✓ Generated a %d-byte TLS ClientHello for %s", len(payload), domain)

	return nil
}

func (m *Manager) storeLocked(protocol, name string, data []byte) error {
	filename := fmt.Sprintf(payloadFilenameFmt, protocol, sanitizeDomain(name))
	path, err := m.payloadPath(filename)
	if err != nil {
		return err
	}
	previous := m.metadata[name][protocol]
	var previousData []byte
	if previous != nil {
		previousData, _ = os.ReadFile(path)
	}

	if err := utils.WriteFileAtomic(path, data, 0644); err != nil {
		return fmt.Errorf("failed to save file: %v", err)
	}

	m.setEntry(name, protocol, &PayloadMetadata{
		Timestamp: time.Now(),
		Size:      len(data),
		Filepath:  filename,
	})
	if err := m.saveMetadata(); err != nil {
		if previous == nil {
			m.removeEntry(name, protocol)
			os.Remove(path)
		} else {
			m.setEntry(name, protocol, previous)
			if previousData != nil {
				utils.WriteFileAtomic(path, previousData, 0644)
			}
		}
		return fmt.Errorf("failed to save metadata: %v", err)
	}
	return nil
}

func (m *Manager) setEntry(name, protocol string, meta *PayloadMetadata) {
	if m.metadata[name] == nil {
		m.metadata[name] = make(map[string]*PayloadMetadata)
	}
	m.metadata[name][protocol] = meta
}

func (m *Manager) removeEntry(name, protocol string) {
	delete(m.metadata[name], protocol)
	if len(m.metadata[name]) == 0 {
		delete(m.metadata, name)
	}
}

func (m *Manager) ListCaptures() []*Capture {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var captures []*Capture

	for domain, protocols := range m.metadata {
		for protocol, meta := range protocols {
			var hexData string
			if data, err := m.readPayloadFile(meta.Filepath); err == nil {
				hexData = hex.EncodeToString(data)
			}

			captures = append(captures, &Capture{
				Protocol:  protocol,
				Domain:    domain,
				Timestamp: meta.Timestamp,
				Size:      meta.Size,
				Filepath:  filepath.Join("captures", meta.Filepath),
				HexData:   hexData,
			})
		}
	}

	return captures
}

func (m *Manager) GetCapture(protocol, domain string) (*Capture, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.metadata[domain] == nil || m.metadata[domain][protocol] == nil {
		return nil, false
	}

	meta := m.metadata[domain][protocol]
	var hexData string
	if data, err := m.readPayloadFile(meta.Filepath); err == nil {
		hexData = hex.EncodeToString(data)
	}

	return &Capture{
		Protocol:  protocol,
		Domain:    domain,
		Timestamp: meta.Timestamp,
		Size:      meta.Size,
		Filepath:  filepath.Join("captures", meta.Filepath),
		HexData:   hexData,
	}, true
}

func (m *Manager) DeleteCapture(protocol, domain string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.metadata[domain] == nil || m.metadata[domain][protocol] == nil {
		return fmt.Errorf("capture not found")
	}

	m.removePayloadFile(m.metadata[domain][protocol].Filepath)
	m.removeEntry(domain, protocol)

	if err := m.saveMetadata(); err != nil {
		log.Errorf("Failed to save metadata: %v", err)
	}

	log.Infof("Deleted capture for %s:%s", protocol, domain)
	return nil
}

func (m *Manager) ClearAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, protocols := range m.metadata {
		for _, meta := range protocols {
			m.removePayloadFile(meta.Filepath)
		}
	}

	m.metadata = make(map[string]map[string]*PayloadMetadata)

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
	if err := m.storeLocked(protocol, domain, data); err != nil {
		return err
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
		if err := utils.WriteFileAtomic(filePath, data, 0644); err != nil {
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
	return m.readPayloadFile(filepath.Base(c.Filepath))
}
