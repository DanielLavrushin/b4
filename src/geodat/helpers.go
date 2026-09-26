package geodat

import (
	"context"
	"fmt"
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

func LoadDomainsByCategory(geodataPath string, categories []string) (map[string][]string, error) {
	return loadByCategory(geodataPath, categories, geositeReadable, siteTag, func(rec []byte, out []string) ([]string, error) {
		kind, value, err := parseDomain(rec)
		if err != nil {
			return out, err
		}
		if domain := formatDomainEntry(kind, value); domain != "" {
			out = append(out, domain)
		}
		return out, nil
	})
}

func LoadIpsByCategory(geodataPath string, categories []string) (map[string][]string, error) {
	return loadByCategory(geodataPath, categories, geoipReadable, strings.ToLower, func(rec []byte, out []string) ([]string, error) {
		ip, bits, err := parseCIDR(rec)
		if err != nil {
			return out, err
		}
		if prefix, ok := toPrefix(ip, bits); ok {
			out = append(out, prefix.String())
		}
		return out, nil
	})
}

func siteTag(category string) string {
	tag, _ := splitAttrs(category)
	return strings.ToLower(tag)
}

func loadByCategory(path string, categories []string, readable func(string) bool, key func(string) string, parse func(rec []byte, out []string) ([]string, error)) (map[string][]string, error) {
	found := make(map[string][]string, len(categories))
	if len(categories) == 0 || !readable(path) {
		return found, nil
	}

	want := make(map[string]struct{}, len(categories))
	for _, category := range categories {
		want[key(category)] = struct{}{}
	}

	byTag := make(map[string][]string, len(want))
	var scratch []byte
	var recordErr error
	before, stamped := fileStamp(path)
	err := scanEntries(path, func(tag string, body *entryBody) error {
		if _, ok := want[tag]; !ok {
			return nil
		}
		var entries []string
		err := scanRecords(body, &scratch, func(rec []byte) error {
			var parseErr error
			entries, parseErr = parse(rec, entries)
			return parseErr
		})
		if err != nil {
			if recordErr == nil {
				recordErr = &DamageError{Path: path, Err: fmt.Errorf("category %s: %w", tag, err)}
			}
			delete(want, tag)
			delete(byTag, tag)
		} else if existing, ok := byTag[tag]; ok {
			byTag[tag] = append(existing, entries...)
		} else {
			byTag[tag] = entries
		}
		if len(byTag) == len(want) {
			return errStopScan
		}
		return nil
	})
	if err == nil {
		err = recordErr
	}

	for _, category := range categories {
		if list, ok := byTag[key(category)]; ok {
			found[category] = list
		}
	}
	return found, track(path, before, stamped, err)
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
