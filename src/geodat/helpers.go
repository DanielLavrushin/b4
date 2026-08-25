package geodat

import (
	"context"
	"net/netip"
	"os"
	"strings"

	"github.com/daniellavrushin/b4/log"
)

func geositeReadable(path string) bool {
	if path == "" {
		return false
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		log.Errorf("Geosite file not found: %s - categories will be ignored", path)
		return false
	}
	return true
}

func geoipReadable(path string) bool {
	if path == "" {
		return false
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		log.Errorf("GeoIP file not found: %s - categories will be ignored", path)
		return false
	}
	return true
}

func LoadDomainsFromCategories(geodataPath string, categories []string) ([]string, error) {
	if len(categories) == 0 || !geositeReadable(geodataPath) {
		return []string{}, nil
	}

	allDomains := []string{}
	err := streamGeoSite(geodataPath, categories, func(_ string, kind uint64, value string) error {
		if domain := formatDomainEntry(kind, value); domain != "" {
			allDomains = append(allDomains, domain)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return allDomains, nil
}

func LoadIpsFromCategories(geodataPath string, categories []string) ([]string, error) {
	if len(categories) == 0 || !geoipReadable(geodataPath) {
		return []string{}, nil
	}

	allIps := []string{}
	err := streamGeoIP(geodataPath, categories, func(_ string, prefix netip.Prefix) error {
		allIps = append(allIps, prefix.String())
		return nil
	})
	if err != nil {
		return nil, err
	}

	return allIps, nil
}

func CountDomainsInCategories(geodataPath string, categories []string) (map[string]int, error) {
	counts := make(map[string]int, len(categories))
	if len(categories) == 0 || !geositeReadable(geodataPath) {
		return counts, nil
	}

	byTag := make(map[string]int, len(categories))
	err := streamGeoSite(geodataPath, categories, func(tag string, kind uint64, value string) error {
		if formatDomainEntry(kind, value) != "" {
			byTag[tag]++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	for _, category := range categories {
		tag, _ := splitAttrs(category)
		counts[category] = byTag[strings.ToLower(tag)]
	}

	return counts, nil
}

func CountIpsInCategories(geodataPath string, categories []string) (map[string]int, error) {
	counts := make(map[string]int, len(categories))
	if len(categories) == 0 || !geoipReadable(geodataPath) {
		return counts, nil
	}

	byTag := make(map[string]int, len(categories))
	err := streamGeoIP(geodataPath, categories, func(tag string, _ netip.Prefix) error {
		byTag[tag]++
		return nil
	})
	if err != nil {
		return nil, err
	}

	for _, category := range categories {
		counts[category] = byTag[strings.ToLower(category)]
	}

	return counts, nil
}

func PreviewDomainsInCategory(ctx context.Context, geodataPath, category string, limit int) ([]string, int, error) {
	preview := []string{}
	total := 0
	if category == "" || !geositeReadable(geodataPath) {
		return preview, 0, nil
	}

	err := streamGeoSite(geodataPath, []string{category}, func(_ string, kind uint64, value string) error {
		if ctx.Err() != nil {
			return errStopScan
		}
		domain := formatDomainEntry(kind, value)
		if domain == "" {
			return nil
		}
		total++
		if len(preview) < limit {
			preview = append(preview, domain)
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}

	return preview, total, nil
}

func formatDomainEntry(kind uint64, value string) string {
	if value == "" {
		return ""
	}
	if kind == domainTypeRegex {
		return "regexp:" + value
	}
	return value
}
