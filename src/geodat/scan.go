package geodat

import (
	"context"
	"fmt"
	"net/netip"
	"os"

	"github.com/daniellavrushin/b4/log"
)

func fileStamp(path string) (string, bool) {
	if path == "" {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", false
	}
	return fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano()), true
}

var ErrStopScan = errStopScan

func (gm *GeodataManager) ScanGeositeEntries(fn func(tag, entry string) error) error {
	path := gm.GetGeositePath()
	if !geositeReadable(path) {
		return log.Errorf("geosite database is not available at %q", path)
	}
	return streamAllGeoSite(path, func(tag string, kind uint64, value string) error {
		entry := formatDomainEntry(kind, value)
		if entry == "" {
			return nil
		}
		return fn(tag, entry)
	})
}

func (gm *GeodataManager) ScanGeoipPrefixes(fn func(tag string, prefix netip.Prefix) error) error {
	path := gm.GetGeoipPath()
	if !geoipReadable(path) {
		return log.Errorf("geoip database is not available at %q", path)
	}
	return streamAllGeoIP(path, fn)
}

func (gm *GeodataManager) PreviewGeoipCategory(ctx context.Context, category string, limit int) ([]string, int, error) {
	gm.mu.RLock()
	ips, cached := gm.categoryIps[category]
	path := gm.geoipPath
	gm.mu.RUnlock()

	if cached {
		preview := ips
		if len(preview) > limit {
			preview = preview[:limit]
		}
		return preview, len(ips), nil
	}

	if !geoipReadable(path) {
		return nil, 0, log.Errorf("geoip database is not available at %q", path)
	}

	preview := []string{}
	total := 0
	err := streamGeoIP(path, []string{category}, func(_ string, prefix netip.Prefix) error {
		if ctx.Err() != nil {
			return ErrStopScan
		}
		total++
		if len(preview) < limit {
			preview = append(preview, prefix.String())
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return preview, total, nil
}
