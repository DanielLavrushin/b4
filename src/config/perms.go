package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/daniellavrushin/b4/log"
)

const ConfigFileMode os.FileMode = 0600

func restrictFileMode(path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if info.Mode().Perm() == ConfigFileMode {
		return
	}
	if err := os.Chmod(path, ConfigFileMode); err != nil {
		log.Debugf("failed to restrict permissions on %s: %v", path, err)
		return
	}
	log.Infof("Tightened permissions on %s from %#o to %#o", path, info.Mode().Perm(), ConfigFileMode)
}

func restrictConfigFiles(path string) {
	restrictFileMode(path)

	dir := filepath.Dir(path)
	base := filepath.Base(path)
	if dir == "" || base == "" {
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == base || !strings.HasPrefix(name, base+".") {
			continue
		}
		restrictFileMode(filepath.Join(dir, name))
	}
}
