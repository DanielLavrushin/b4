package hub

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/netprobe"
)

const (
	DefaultBaseURL = "https://hub.b4core.app"
	DirName        = ".hub"
	RequestTimeout = 60 * time.Second

	baseScheme  = "https://"
	plainScheme = "http://"
)

var DefaultBases = []string{DefaultBaseURL}

type Options struct {
	Version      string
	HTTPClient   *http.Client
	BuiltinBases []string
	Now          func() time.Time
}

type Service struct {
	getCfg  func() *config.Config
	version string
	http    *http.Client
	builtin []string
	now     func() time.Time

	mu            sync.RWMutex
	manifest      *hubwire.Manifest
	catalogue     *hubwire.Catalogue
	byID          map[string]*hubwire.CatalogueSet
	lastSync      time.Time
	lastError     string
	preferredBase string

	identityMu      sync.Mutex
	identity        *hubwire.Identity
	identityCreated time.Time

	networkMu     sync.Mutex
	network       Network
	networkPinned bool
	networkReadAt time.Time

	trustMu sync.RWMutex
	mirrors []string
	revoked []string

	syncMu sync.Mutex

	runMu   sync.Mutex
	stop    chan struct{}
	stopped chan struct{}
	kick    chan struct{}
	cancel  context.CancelFunc
}

func New(getCfg func() *config.Config, opts Options) *Service {
	s := &Service{
		getCfg:  getCfg,
		version: opts.Version,
		http:    opts.HTTPClient,
		builtin: opts.BuiltinBases,
		now:     opts.Now,
	}
	if s.http == nil {
		s.http = netprobe.HTTPClient(int(config.SelfDialMark), RequestTimeout)
	}
	if s.builtin == nil {
		s.builtin = DefaultBases
	}
	if s.now == nil {
		s.now = time.Now
	}
	s.loadTrust()
	s.loadStored()
	return s
}

func (s *Service) Version() string {
	return s.version
}

func (s *Service) Dir() string {
	return filepath.Join(filepath.Dir(s.getCfg().ConfigPath), DirName)
}

func (s *Service) ensureDir() error {
	return os.MkdirAll(s.Dir(), 0700)
}

func (s *Service) Enabled() bool {
	return s.getCfg().System.Hub.Enabled
}

func (s *Service) TrustedKeys() []string {
	if key := strings.TrimSpace(s.getCfg().System.Hub.PublicKey); key != "" {
		return s.withoutRevoked([]string{key})
	}
	return s.withoutRevoked(append([]string(nil), hubwire.BuiltinHubKeys...))
}

func (s *Service) Configured() bool {
	for _, key := range s.TrustedKeys() {
		if _, err := hubwire.DecodeKey(key); err == nil {
			return true
		}
	}
	return false
}

func (s *Service) ready() bool {
	return s.Enabled() && s.Configured()
}

func NormalizeBaseURL(raw string) string {
	base := strings.TrimSpace(raw)
	if base == "" {
		return ""
	}
	base = strings.TrimRight(base, "/")
	plain := strings.HasPrefix(base, plainScheme)
	if !strings.HasPrefix(base, baseScheme) && !plain {
		return ""
	}
	for _, r := range base {
		if r < 0x20 || r == 0x7f || unicode.IsSpace(r) {
			return ""
		}
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	if plain && !localHost(u.Hostname()) {
		return ""
	}
	return base
}

func localHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

func (s *Service) BaseURLs() []string {
	seen := map[string]bool{}
	hubCfg := s.getCfg().System.Hub
	configured := hubCfg.URLs
	mirrors := s.KnownMirrors()
	lists := [][]string{configured, mirrors, s.builtin}
	if strings.TrimSpace(hubCfg.PublicKey) != "" && len(configured) > 0 {
		lists = [][]string{configured, mirrors}
	}
	out := make([]string, 0, len(configured)+len(mirrors)+len(s.builtin))
	for _, list := range lists {
		for _, raw := range list {
			base := NormalizeBaseURL(raw)
			if base == "" || seen[base] {
				continue
			}
			seen[base] = true
			out = append(out, base)
		}
	}
	return out
}

func (s *Service) orderedBases() []string {
	bases := s.BaseURLs()
	s.mu.RLock()
	preferred := s.preferredBase
	s.mu.RUnlock()
	if preferred == "" {
		return bases
	}
	out := make([]string, 0, len(bases))
	for _, base := range bases {
		if base == preferred {
			out = append(out, base)
		}
	}
	for _, base := range bases {
		if base != preferred {
			out = append(out, base)
		}
	}
	return out
}

func (s *Service) setPreferredBase(base string) {
	s.mu.Lock()
	s.preferredBase = base
	s.mu.Unlock()
}

func (s *Service) install(m *hubwire.Manifest, cat *hubwire.Catalogue) {
	byID := make(map[string]*hubwire.CatalogueSet, len(cat.Sets))
	for i := range cat.Sets {
		byID[cat.Sets[i].ID] = &cat.Sets[i]
	}
	s.mu.Lock()
	s.manifest = m
	s.catalogue = cat
	s.byID = byID
	s.mu.Unlock()
}

func (s *Service) loadStored() {
	st := s.store()
	m, err := st.loadManifest()
	if err != nil {
		if !os.IsNotExist(err) {
			log.Warnf("hub: stored manifest ignored: %v", err)
		}
		return
	}
	if err := hubwire.VerifyManifest(m, s.TrustedKeys()); err != nil {
		log.Warnf("hub: stored manifest ignored: %v", err)
		return
	}
	cat, err := st.loadCatalogue(m.Catalogue)
	if err != nil {
		log.Warnf("hub: stored catalogue ignored: %v", err)
		return
	}
	s.install(m, cat)
	log.Infof("hub: loaded catalogue %d-%d with %d sets", cat.Epoch, cat.Seq, len(cat.Sets))
}

func (s *Service) Manifest() *hubwire.Manifest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.manifest
}

func (s *Service) catalogueLoaded() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.catalogue != nil
}

func (s *Service) markSynced() {
	s.mu.Lock()
	s.lastSync = s.now()
	s.lastError = ""
	s.mu.Unlock()
}

func (s *Service) markFailed(err error) {
	s.mu.Lock()
	s.lastError = err.Error()
	s.mu.Unlock()
}
