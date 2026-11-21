package capture

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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
	started  atomic.Pointer[Manager]
)

func GetManager(cfg *config.Config) *Manager {
	once.Do(func() {
		instance = newManager(filepath.Join(filepath.Dir(cfg.ConfigPath), "captures"))
		os.MkdirAll(instance.outputPath, 0755)
		instance.loadMetadata()
		started.Store(instance)
	})
	return instance
}

func Reload() {
	if m := started.Load(); m != nil {
		m.Reload()
	}
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

func (m *Manager) Reload() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loadMetadata()
}

func (m *Manager) loadMetadata() {
	data, err := os.ReadFile(m.metadataFile)
	if os.IsNotExist(err) {
		m.metadata = make(map[string]map[string]*PayloadMetadata)
		return
	}
	if err != nil {
		log.Errorf("Failed to read metadata: %v", err)
		return
	}

	var loaded map[string]map[string]*PayloadMetadata
	if err := json.Unmarshal(data, &loaded); err != nil {
		log.Errorf("Failed to parse metadata: %v", err)
		return
	}
	m.metadata = usableMetadata(loaded)
	if m.completeLegacyEntries() {
		m.saveMetadata()
	}
}

func (m *Manager) completeLegacyEntries() bool {
	changed := false
	for _, protocols := range m.metadata {
		for protocol, meta := range protocols {
			if meta.Facts != nil && meta.Origin != "" {
				continue
			}
			data, err := m.readPayloadFile(meta.Filepath)
			if err != nil {
				continue
			}
			if meta.Facts == nil {
				meta.Facts = FactsOf(protocol, data)
				changed = true
			}
			if meta.Origin == "" && protocol == "tls" && madeByTheGenerator(data, meta.Facts.SNI) {
				meta.Origin = OriginGenerated
				changed = true
			}
		}
	}
	return changed
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

func (m *Manager) payloadFileSize(name string) (int64, bool) {
	path, err := m.payloadPath(name)
	if err != nil {
		return 0, false
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return 0, false
	}
	return info.Size(), true
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

	if err := m.storeLocked(protocol, domain, payload, OriginGenerated); err != nil {
		return err
	}

	log.Infof("✓ Generated a %d-byte TLS ClientHello for %s", len(payload), domain)

	return nil
}

var (
	ErrPayloadNotFound = errors.New("payload not found")
	ErrNotGenerated    = errors.New("not a generated payload")
	ErrPayloadExists   = errors.New("a payload of this name and protocol exists")
)

func (m *Manager) RegenerateCapture(domain string) (*Capture, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	meta := m.metadata[domain]["tls"]
	if meta == nil {
		return nil, ErrPayloadNotFound
	}
	if meta.Origin != OriginGenerated {
		return nil, ErrNotGenerated
	}
	host := domain
	if meta.Facts != nil && meta.Facts.SNI != "" {
		host = meta.Facts.SNI
	}
	payload, err := tlsgen.GenerateTLSClientHello(host)
	if err != nil {
		return nil, fmt.Errorf("failed to generate payload: %v", err)
	}
	if err := m.storeLocked("tls", domain, payload, OriginGenerated); err != nil {
		return nil, err
	}
	log.Infof("Regenerated the %d-byte TLS ClientHello of %s for %s", len(payload), domain, host)
	return captureOf("tls", domain, m.metadata[domain]["tls"]), nil
}

func (m *Manager) fileNameFor(protocol, name string, claimUnlisted bool) (string, error) {
	if previous := m.metadata[name][protocol]; previous != nil {
		return previous.Filepath, nil
	}
	sum := sha256.Sum256([]byte(protocol + "/" + name))
	tagged := sanitizeDomain(name) + "-" + hex.EncodeToString(sum[:4])
	for i := range 64 {
		filename := plainFileName(protocol, name)
		switch {
		case i == 1:
			filename = fmt.Sprintf(payloadFilenameFmt, protocol, tagged)
		case i > 1:
			filename = fmt.Sprintf(payloadFilenameFmt, protocol, fmt.Sprintf("%s-%d", tagged, i))
		}
		if m.fileNameFree(filename, claimUnlisted && i == 0) {
			return filename, nil
		}
	}
	return "", fmt.Errorf("no free file name for the %s payload %q", protocol, name)
}

func plainFileName(protocol, name string) string {
	return fmt.Sprintf(payloadFilenameFmt, protocol, sanitizeDomain(name))
}

func (m *Manager) fileOwned(filename string) bool {
	for _, protocols := range m.metadata {
		for _, meta := range protocols {
			if meta.Filepath == filename {
				return true
			}
		}
	}
	return false
}

func (m *Manager) fileNameFree(filename string, claimUnlisted bool) bool {
	if m.fileOwned(filename) {
		return false
	}
	path, err := m.payloadPath(filename)
	if err != nil {
		return false
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return true
	}
	return claimUnlisted && err == nil && info.Mode().IsRegular()
}

func (m *Manager) storeLocked(protocol, name string, data []byte, origin string) error {
	filename, err := m.fileNameFor(protocol, name, true)
	if err != nil {
		return err
	}
	path, err := m.payloadPath(filename)
	if err != nil {
		return err
	}
	previous := m.metadata[name][protocol]
	previousData, readErr := os.ReadFile(path)
	hadFile := readErr == nil

	if err := utils.WriteFileAtomic(path, data, 0644); err != nil {
		return fmt.Errorf("failed to save file: %v", err)
	}

	m.setEntry(name, protocol, &PayloadMetadata{
		Timestamp: time.Now(),
		Size:      len(data),
		Filepath:  filename,
		Origin:    origin,
		Facts:     FactsOf(protocol, data),
	})
	if err := m.saveMetadata(); err != nil {
		if previous == nil {
			m.removeEntry(name, protocol)
		} else {
			m.setEntry(name, protocol, previous)
		}
		if hadFile {
			utils.WriteFileAtomic(path, previousData, 0644)
		} else {
			os.Remove(path)
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
	var captures []*Capture
	var files []string
	for domain, protocols := range m.metadata {
		for protocol, meta := range protocols {
			captures = append(captures, captureOf(protocol, domain, meta))
			files = append(files, meta.Filepath)
		}
	}
	m.mu.RUnlock()

	for i, c := range captures {
		size, present := m.payloadFileSize(files[i])
		c.Missing = !present
		c.Warnings = warningsOf(c.Protocol, c.Facts, !present, size == 0)
	}
	sort.Slice(captures, func(i, j int) bool {
		if captures[i].Domain != captures[j].Domain {
			return captures[i].Domain < captures[j].Domain
		}
		return captures[i].Protocol < captures[j].Protocol
	})
	return captures
}

func (m *Manager) GetCapture(protocol, domain string) (*Capture, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.metadata[domain] == nil || m.metadata[domain][protocol] == nil {
		return nil, false
	}

	return captureOf(protocol, domain, m.metadata[domain][protocol]), true
}

func captureOf(protocol, domain string, meta *PayloadMetadata) *Capture {
	return &Capture{
		Protocol:  protocol,
		Domain:    domain,
		Timestamp: meta.Timestamp,
		Size:      meta.Size,
		Filepath:  filepath.Join("captures", meta.Filepath),
		Origin:    meta.Origin,
		Facts:     meta.Facts,
		Warnings:  []string{},
	}
}

func warningsOf(protocol string, facts *Facts, missing, empty bool) []string {
	switch {
	case missing:
		return []string{"missing"}
	case empty:
		return []string{"empty"}
	}
	warnings := []string{}
	if facts == nil {
		return warnings
	}
	if !facts.FitsOnePacket {
		warnings = append(warnings, "too_large")
	}
	switch {
	case facts.Kind == KindOther:
		warnings = append(warnings, "unrecognized")
	case SuggestedProtocol(facts.Kind) != protocol:
		warnings = append(warnings, "kind_mismatch")
	}
	if len(facts.Faults) > 0 {
		warnings = append(warnings, "malformed")
	}
	return warnings
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

func (m *Manager) SaveUploadedCapture(protocol, domain string, data []byte, replace bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	domain = strings.ToLower(strings.TrimSpace(domain))
	if !replace && m.metadata[domain][protocol] != nil {
		return ErrPayloadExists
	}
	if err := m.storeLocked(protocol, domain, data, OriginUploaded); err != nil {
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
		if listed := m.metadata[name][protocol]; listed != nil {
			existing, err := m.readPayloadFile(listed.Filepath)
			switch {
			case err == nil && bytes.Equal(existing, data):
				return filepath.Join("captures", listed.Filepath), nil
			case err == nil:
				continue
			case !os.IsNotExist(err):
				return "", fmt.Errorf("failed to inspect %s: %v", listed.Filepath, err)
			}
			return m.writeImportedPayload(name, protocol, listed.Filepath, data)
		}
		if plain := plainFileName(protocol, name); m.unlistedFileHolds(plain, data) {
			at := time.Now()
			if path, err := m.payloadPath(plain); err == nil {
				if info, err := os.Stat(path); err == nil {
					at = info.ModTime()
				}
			}
			if err := m.registerImportedPayload(name, protocol, plain, data, at); err != nil {
				return "", err
			}
			log.Infof("Registered existing shared %s payload for %s (%d bytes)", protocol, name, len(data))
			return filepath.Join("captures", plain), nil
		}
		filename, err := m.fileNameFor(protocol, name, false)
		if err != nil {
			return "", err
		}
		return m.writeImportedPayload(name, protocol, filename, data)
	}
	return "", fmt.Errorf("a different payload already exists under every name derived from %s", domain)
}

func (m *Manager) unlistedFileHolds(filename string, data []byte) bool {
	if m.fileOwned(filename) {
		return false
	}
	existing, err := m.readPayloadFile(filename)
	return err == nil && bytes.Equal(existing, data)
}

func (m *Manager) writeImportedPayload(name, protocol, filename string, data []byte) (string, error) {
	path, err := m.payloadPath(filename)
	if err != nil {
		return "", err
	}
	if err := utils.WriteFileAtomic(path, data, 0644); err != nil {
		return "", fmt.Errorf("failed to save file: %v", err)
	}
	if err := m.registerImportedPayload(name, protocol, filename, data, time.Now()); err != nil {
		return "", err
	}
	log.Infof("Saved shared %s payload for %s (%d bytes)", protocol, name, len(data))
	return filepath.Join("captures", filename), nil
}

func (m *Manager) registerImportedPayload(name, protocol, filename string, data []byte, at time.Time) error {
	previous := m.metadata[name][protocol]
	m.setEntry(name, protocol, &PayloadMetadata{
		Timestamp: at,
		Size:      len(data),
		Filepath:  filename,
		Origin:    OriginImported,
		Facts:     FactsOf(protocol, data),
	})
	if err := m.saveMetadata(); err != nil {
		if previous != nil {
			m.setEntry(name, protocol, previous)
		} else {
			m.removeEntry(name, protocol)
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
