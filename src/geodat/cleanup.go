package geodat

import (
	"os"
	"path/filepath"
	"time"

	"github.com/daniellavrushin/b4/log"
)

const staleDownloadAge = time.Hour

var staleDownloadPatterns = []string{
	".download-*.tmp",
	".geodat-upload-*.tmp",
	"*.dat.part",
	"*.dat.new",
	"*.dat.new.part",
}

func RemoveStaleDownloads(paths ...string) {
	seen := map[string]bool{}
	for _, p := range paths {
		if p == "" || !filepath.IsAbs(p) {
			continue
		}
		dir := filepath.Dir(p)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		for _, pattern := range staleDownloadPatterns {
			matches, err := filepath.Glob(filepath.Join(dir, pattern))
			if err != nil {
				continue
			}
			for _, match := range matches {
				info, err := os.Lstat(match)
				if err != nil || !info.Mode().IsRegular() || time.Since(info.ModTime()) < staleDownloadAge {
					continue
				}
				if err := os.Remove(match); err != nil {
					log.Warnf("[GEODAT] could not remove the unfinished download %s: %v", match, err)
					continue
				}
				log.Infof("[GEODAT] removed the unfinished download %s", match)
			}
		}
	}
}
