package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4/sni"
)

func canonicalTargets(items []string, normalise func(string) string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		value := normalise(item)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func lowerTrimmed(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func canonicalASN(s string) string {
	if id, ok := config.NormalizeASN(s); ok {
		return id
	}
	return lowerTrimmed(s)
}

func udpPorts(projection map[string]interface{}) []string {
	udp, _ := projection["udp"].(map[string]interface{})
	ports, _ := udp["dport_filter"].(string)
	return strings.Split(ports, ",")
}

func TargetsKey(projection map[string]interface{}) string {
	canon := map[string][]string{
		"sni_domains": canonicalTargets(TargetList(projection, "sni_domains"), sni.CanonicalDomainEntry),
		"ip":          canonicalTargets(TargetList(projection, "ip"), lowerTrimmed),
		"geosite":     canonicalTargets(TargetList(projection, "geosite_categories"), lowerTrimmed),
		"geoip":       canonicalTargets(TargetList(projection, "geoip_categories"), lowerTrimmed),
	}
	if asns := canonicalTargets(TargetList(projection, "asns"), canonicalASN); len(asns) > 0 {
		canon["asns"] = asns
	}
	if classes := canonicalTargets(hubwire.UDPL7(projection), lowerTrimmed); len(classes) > 0 {
		canon["udp_l7"] = classes
		canon["udp_ports"] = canonicalTargets(udpPorts(projection), lowerTrimmed)
	}
	data, _ := json.Marshal(canon)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TargetEntries(projection map[string]interface{}) map[string]struct{} {
	out := make(map[string]struct{})
	add := func(prefix string, items []string, normalise func(string) string) {
		for _, item := range canonicalTargets(items, normalise) {
			out[prefix+item] = struct{}{}
		}
	}
	add("sni:", TargetList(projection, "sni_domains"), sni.CanonicalDomainEntry)
	add("ip:", TargetList(projection, "ip"), lowerTrimmed)
	add("geosite:", TargetList(projection, "geosite_categories"), lowerTrimmed)
	add("geoip:", TargetList(projection, "geoip_categories"), lowerTrimmed)
	add("asn:", TargetList(projection, "asns"), canonicalASN)
	add("udp_l7:", hubwire.UDPL7(projection), lowerTrimmed)
	return out
}
