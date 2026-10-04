package geodat

import (
	_ "embed"
	"encoding/json"
	"sync"
)

//go:embed sources.json
var sourcesJSON []byte

type Source struct {
	Name       string `json:"name"`
	GeositeURL string `json:"geosite_url"`
	GeoipURL   string `json:"geoip_url"`
}

var (
	sources     []Source
	sourcesErr  error
	sourcesOnce sync.Once
)

func Sources() ([]Source, error) {
	sourcesOnce.Do(func() {
		if sourcesErr = json.Unmarshal(sourcesJSON, &sources); sourcesErr != nil {
			sources = []Source{}
		}
	})
	return sources, sourcesErr
}

func IsSourceURL(raw string) bool {
	if raw == "" {
		return false
	}
	list, _ := Sources()
	for _, s := range list {
		if raw == s.GeositeURL || raw == s.GeoipURL {
			return true
		}
	}
	return false
}
