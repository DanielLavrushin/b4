package netprobe

import (
	_ "embed"
	"encoding/json"
	"strings"

	"github.com/daniellavrushin/b4/log"
)

//go:embed markers.json
var markersJSON []byte

var (
	BlockPageRedirectMarkers []string
	BlockPageBodyMarkers     []string
)

type markersData struct {
	RedirectMarkers []string `json:"redirect_markers"`
	BodyMarkers     []string `json:"body_markers"`
}

func init() {
	var data markersData
	if err := json.Unmarshal(markersJSON, &data); err != nil {
		log.Errorf("Failed to parse embedded netprobe markers.json: %v", err)
		return
	}
	BlockPageRedirectMarkers = data.RedirectMarkers
	BlockPageBodyMarkers = data.BodyMarkers
}

func IsBlockPageRedirect(location string) bool {
	if location == "" {
		return false
	}
	lower := strings.ToLower(location)
	for _, marker := range BlockPageRedirectMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func DetectBlockPageBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	lower := strings.ToLower(string(body))
	for _, marker := range BlockPageBodyMarkers {
		if strings.Contains(lower, marker) {
			return "ISP block page detected in response"
		}
	}
	return ""
}
