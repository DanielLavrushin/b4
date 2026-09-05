package discovery

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/daniellavrushin/b4/log"
)

const discoveryLogFile = "discovery_last.log"

func runLogPath(configPath string) string {
	if configPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(configPath), discoveryLogFile)
}

func (ds *DiscoverySuite) saveRunLog() {
	if ds.cfg == nil {
		return
	}
	SaveLastRunLog(ds.cfg.ConfigPath)
}

func SaveLastRunLog(configPath string) {
	path := runLogPath(configPath)
	if path == "" {
		return
	}
	lines := log.GetDiscoveryHub().Snapshot()
	if len(lines) == 0 {
		return
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		log.Errorf("Failed to save the discovery log: %v", err)
	}
}

func LoadLastRunLog(configPath string) ([]byte, error) {
	path := runLogPath(configPath)
	if path == "" {
		return nil, errors.New("no config path")
	}
	return os.ReadFile(path)
}
