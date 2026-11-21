package capture

import (
	"sync"
	"time"
)

type Manager struct {
	mu           sync.RWMutex
	metadata     map[string]map[string]*PayloadMetadata
	outputPath   string
	metadataFile string
}

type PayloadMetadata struct {
	Timestamp time.Time `json:"timestamp"`
	Size      int       `json:"size"`
	Filepath  string    `json:"filepath"`
}

// API response structure
type Capture struct {
	Protocol  string    `json:"protocol"`
	Domain    string    `json:"domain"`
	Timestamp time.Time `json:"timestamp"`
	Size      int       `json:"size"`
	Filepath  string    `json:"filepath"`
	HexData   string    `json:"hex_data"`
	UsedBy    []string  `json:"used_by,omitempty"`
}
