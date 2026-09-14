package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/daniellavrushin/b4/detector"
	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/utils"
)

const (
	networkReadInterval = time.Minute
	networkFileName     = "network.json"
	networkTimeout      = 8 * time.Second
	networkLimit        = 4 << 10

	NetworkSourceHub      = "hub"
	NetworkSourceDetector = "detector"
)

type Network struct {
	ASN    string `json:"asn"`
	CC     string `json:"cc"`
	Name   string `json:"name,omitempty"`
	Source string `json:"source,omitempty"`
}

type learnedNetwork struct {
	Network
	LearnedAt string `json:"learned_at"`
}

func (s *Service) SetNetwork(asn, cc string) {
	s.networkMu.Lock()
	defer s.networkMu.Unlock()
	s.network = Network{ASN: asn, CC: cc}
	s.networkPinned = true
}

func (s *Service) Network() Network {
	s.networkMu.Lock()
	defer s.networkMu.Unlock()
	if s.networkPinned {
		return s.network
	}
	if s.hubNetwork.ASN != "" || s.hubNetwork.CC != "" {
		return s.hubNetwork
	}
	now := s.now()
	if !s.networkReadAt.IsZero() && now.Sub(s.networkReadAt) < networkReadInterval {
		return s.network
	}
	s.networkReadAt = now
	s.network = detectedNetwork(s.getCfg().ConfigPath)
	return s.network
}

func detectedNetwork(configPath string) Network {
	for _, entry := range detector.LoadHistory(configPath).Entries {
		if entry == nil || entry.Network == nil {
			continue
		}
		if entry.Network.ASN == "" && entry.Network.Country == "" {
			continue
		}
		return Network{ASN: entry.Network.ASN, CC: entry.Network.Country, Source: NetworkSourceDetector}
	}
	return Network{}
}

func (s *Service) networkPath() string {
	return filepath.Join(s.Dir(), networkFileName)
}

func (s *Service) loadNetwork() {
	raw, err := os.ReadFile(s.networkPath())
	if err != nil {
		if !os.IsNotExist(err) {
			log.Warnf("hub: %s ignored: %v", networkFileName, err)
		}
		return
	}
	var learned learnedNetwork
	if err := json.Unmarshal(raw, &learned); err != nil {
		log.Warnf("hub: %s ignored: %v", networkFileName, err)
		return
	}
	learned.Source = NetworkSourceHub
	s.networkMu.Lock()
	s.hubNetwork = learned.Network
	s.networkMu.Unlock()
}

func (s *Service) learnNetwork(ctx context.Context, base string) {
	body, status, err := s.get(ctx, base+hubwire.PathNetwork, networkLimit, networkTimeout)
	if err != nil || status != http.StatusOK {
		return
	}
	var info hubwire.NetworkInfo
	if err := json.Unmarshal(body, &info); err != nil || (info.ASN == "" && info.Country == "") {
		return
	}
	learned := learnedNetwork{
		Network:   Network{ASN: info.ASN, CC: info.Country, Name: info.Name, Source: NetworkSourceHub},
		LearnedAt: s.now().UTC().Format(time.RFC3339),
	}
	s.networkMu.Lock()
	changed := s.hubNetwork != learned.Network
	s.hubNetwork = learned.Network
	s.networkMu.Unlock()
	if !changed {
		return
	}
	log.Infof("hub: this router is on AS%s %s (%s) according to %s", info.ASN, info.Country, info.Name, base)
	if err := s.ensureDir(); err != nil {
		return
	}
	if raw, err := json.Marshal(learned); err == nil {
		if err := utils.WriteFileAtomic(s.networkPath(), raw, 0600); err != nil {
			log.Warnf("hub: could not store %s: %v", networkFileName, err)
		}
	}
}
