package hub

import (
	"time"

	"github.com/daniellavrushin/b4/detector"
)

const networkReadInterval = time.Minute

type Network struct {
	ASN string `json:"asn"`
	CC  string `json:"cc"`
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
		return Network{ASN: entry.Network.ASN, CC: entry.Network.Country}
	}
	return Network{}
}
