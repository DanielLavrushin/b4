package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4/sni"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	FlagNeedsPayload = "needs_payload"
	FlagBlock        = "block"
	FlagHasPins      = "has_pins"
	FlagBlanket      = "blanket"
	FlagCatchAll     = "catch_all"

	FamilyPlain = "plain"
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

func TargetsKey(projection map[string]interface{}) string {
	canon := map[string][]string{
		"sni_domains": canonicalTargets(store.TargetList(projection, "sni_domains"), sni.CanonicalDomainEntry),
		"ip":          canonicalTargets(store.TargetList(projection, "ip"), lowerTrimmed),
		"geosite":     canonicalTargets(store.TargetList(projection, "geosite_categories"), lowerTrimmed),
		"geoip":       canonicalTargets(store.TargetList(projection, "geoip_categories"), lowerTrimmed),
	}
	data, _ := json.Marshal(canon)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TargetSet(projection map[string]interface{}) map[string]struct{} {
	out := make(map[string]struct{})
	add := func(prefix string, items []string, normalise func(string) string) {
		for _, item := range canonicalTargets(items, normalise) {
			out[prefix+item] = struct{}{}
		}
	}
	add("sni:", store.TargetList(projection, "sni_domains"), sni.CanonicalDomainEntry)
	add("ip:", store.TargetList(projection, "ip"), lowerTrimmed)
	add("geosite:", store.TargetList(projection, "geosite_categories"), lowerTrimmed)
	add("geoip:", store.TargetList(projection, "geoip_categories"), lowerTrimmed)
	return out
}

func targetsOverlap(a, b map[string]struct{}) bool {
	for k := range a {
		if _, ok := b[k]; ok {
			return true
		}
	}
	return false
}

func sameTitle(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func Flags(projection map[string]interface{}, payloads []hubwire.BlobRef) []string {
	flags := make([]string, 0, 5)
	if len(payloads) > 0 {
		flags = append(flags, FlagNeedsPayload)
	}
	if routing, ok := projection["routing"].(map[string]interface{}); ok {
		if mode, _ := routing["mode"].(string); mode == config.RoutingModeBlock {
			flags = append(flags, FlagBlock)
		}
	}
	if dns, ok := projection["dns"].(map[string]interface{}); ok {
		if pins, ok := dns["pins"].(map[string]interface{}); ok && len(pins) > 0 {
			flags = append(flags, FlagHasPins)
		}
	}
	domains := store.TargetList(projection, "sni_domains")
	ips := store.TargetList(projection, "ip")
	categories := store.TargetList(projection, "geosite_categories")
	if len(domains) == 0 && len(ips) == 0 && len(categories) > 0 {
		flags = append(flags, FlagBlanket)
	}
	for _, entry := range domains {
		if _, isRegex := sni.ParseDomainEntry(entry); isRegex {
			flags = append(flags, FlagCatchAll)
			break
		}
	}
	return flags
}

func Family(set *config.SetConfig) string {
	parts := make([]string, 0, 8)
	if set.Faking.SNI {
		parts = append(parts, "fake")
	}
	if strategy := strings.ToLower(set.Fragmentation.Strategy); strategy != "" && strategy != "none" {
		parts = append(parts, "frag-"+strategy)
	}
	if mode := strings.ToLower(set.TCP.Desync.Mode); mode != "" && mode != "off" {
		parts = append(parts, "desync")
	}
	if set.TCP.SynFake {
		parts = append(parts, "synfake")
	}
	if mode := strings.ToLower(set.TCP.Incoming.Mode); mode != "" && mode != "off" {
		parts = append(parts, "incoming")
	}
	if mode := strings.ToLower(set.UDP.Mode); mode != "" && mode != "none" && mode != strings.ToLower(config.DefaultSetConfig.UDP.Mode) {
		parts = append(parts, "udp-"+mode)
	}
	if set.DNS.Enabled {
		parts = append(parts, "dns")
	}
	if set.Routing.Enabled && set.Routing.Mode == config.RoutingModeBlock {
		parts = append(parts, "block")
	}
	if set.MSSClamp.Enabled {
		parts = append(parts, "mss")
	}
	if len(parts) == 0 {
		return FamilyPlain
	}
	return strings.Join(parts, "+")
}

func domainTargeted(domain string, entries []string) bool {
	for _, entry := range entries {
		rel, _ := sni.MatchDomainEntry(entry, domain)
		if rel == sni.RelationExact || rel == sni.RelationCovered || rel == sni.RelationRegexp {
			return true
		}
	}
	return false
}

func clip(s string, max int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return strings.TrimSpace(string(runes[:max]))
}
