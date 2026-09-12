package hubwire

import (
	"net"
	"net/url"
	"reflect"
	"sort"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sni"
)

type Stripped struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type Warning struct {
	Code   string                 `json:"code"`
	Params map[string]interface{} `json:"params,omitempty"`
}

type Report struct {
	Stripped []Stripped `json:"stripped"`
	Warnings []Warning  `json:"warnings"`
}

func (r *Report) strip(path, reason string) {
	r.Stripped = append(r.Stripped, Stripped{Path: path, Reason: reason})
}

func (r *Report) warn(code string, params map[string]interface{}) {
	r.Warnings = append(r.Warnings, Warning{Code: code, Params: params})
}

func (r *Report) sorted() *Report {
	sort.SliceStable(r.Stripped, func(i, j int) bool { return r.Stripped[i].Path < r.Stripped[j].Path })
	if r.Stripped == nil {
		r.Stripped = []Stripped{}
	}
	if r.Warnings == nil {
		r.Warnings = []Warning{}
	}
	return r
}

type blockRule struct {
	path   string
	toggle string
	off    interface{}
	keep   []string
}

var disabledBlockRules = []blockRule{
	{path: "faking", toggle: "sni", off: false},
	{path: "tcp.duplicate", toggle: "enabled", off: false},
	{path: "tcp.ip_block_detect", toggle: "enabled", off: false},
	{path: "tcp.rst_protection", toggle: "enabled", off: false},
	{path: "tcp.desync", toggle: "mode", off: "off"},
	{path: "tcp.win", toggle: "mode", off: "off"},
	{path: "tcp.incoming", toggle: "mode", off: "off"},
	{path: "fragmentation", toggle: "strategy", off: "none"},
	{path: "dns", toggle: "enabled", off: false, keep: []string{"pins"}},
	{path: "routing", toggle: "enabled", off: false},
}

func resetDisabledBlocks(m, def map[string]interface{}) {
	for _, rule := range disabledBlockRules {
		nodeRaw, ok := lookupPath(m, rule.path)
		if !ok {
			continue
		}
		node, ok := nodeRaw.(map[string]interface{})
		if !ok {
			continue
		}
		defRaw, ok := lookupPath(def, rule.path)
		if !ok {
			continue
		}
		defNode, ok := defRaw.(map[string]interface{})
		if !ok {
			continue
		}
		if !reflect.DeepEqual(node[rule.toggle], rule.off) {
			continue
		}
		reset := make(map[string]interface{}, len(defNode))
		for k, v := range defNode {
			reset[k] = deepCopy(v)
		}
		reset[rule.toggle] = rule.off
		for _, k := range rule.keep {
			if v, ok := node[k]; ok {
				reset[k] = v
			}
		}
		setPath(m, rule.path, reset)
	}
}

var blockRoutingKeep = map[string]bool{"enabled": true, "mode": true, "block_action": true}

func scrubRouting(m map[string]interface{}, r *Report) {
	routing, ok := m["routing"].(map[string]interface{})
	if !ok {
		return
	}
	enabled, _ := routing["enabled"].(bool)
	mode, _ := routing["mode"].(string)
	if !enabled {
		delete(m, "routing")
		return
	}
	if mode != config.RoutingModeBlock {
		r.strip("routing", "routing_not_shareable")
		delete(m, "routing")
		return
	}
	for k := range routing {
		if !blockRoutingKeep[k] {
			delete(routing, k)
		}
	}
}

var reservedNets = func() []*net.IPNet {
	cidrs := []string{
		"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15",
		"198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "255.255.255.255/32",
		"::/128", "100::/64", "2001:db8::/32", "2001::/23",
	}
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err == nil {
			out = append(out, n)
		}
	}
	return out
}()

func isPublicIP(raw string) bool {
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil {
		return false
	}
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return false
	}
	for _, n := range reservedNets {
		if n.Contains(ip) {
			return false
		}
	}
	return true
}

func isPublicTarget(raw string) bool {
	raw = strings.TrimSpace(raw)
	if ip, _, err := net.ParseCIDR(raw); err == nil {
		return isPublicIP(ip.String())
	}
	if strings.Contains(raw, "-") {
		parts := strings.SplitN(raw, "-", 2)
		return isPublicIP(parts[0]) && isPublicIP(parts[1])
	}
	return isPublicIP(raw)
}

var knownDoHHosts = map[string]bool{
	"1.1.1.1": true, "8.8.8.8": true, "8.8.4.4": true, "9.9.9.9": true, "1.0.0.1": true,
	"cloudflare-dns.com": true, "mozilla.cloudflare-dns.com": true, "security.cloudflare-dns.com": true,
	"family.cloudflare-dns.com": true, "dns.google": true, "dns.quad9.net": true, "dns10.quad9.net": true,
	"dns11.quad9.net": true, "dns12.quad9.net": true, "dns.adguard.com": true, "dns.adguard-dns.com": true,
	"family.adguard-dns.com": true, "unfiltered.adguard-dns.com": true, "dns.opendns.com": true,
	"doh.opendns.com": true, "dns.nextdns.io": true, "dns.mullvad.net": true, "adblock.dns.mullvad.net": true,
	"dns.sb": true, "doh.dns.sb": true, "dns.alidns.com": true, "doh.pub": true, "dns.comss.one": true,
	"common.dot.dns.yandex.net": true, "dns.yandex.ru": true, "anycast.uncensoreddns.org": true,
	"unicast.uncensoreddns.org": true, "dns.digitale-gesellschaft.ch": true, "dnsforge.de": true,
	"doh.libredns.gr": true, "doh.dns4all.eu": true, "protective.joindns4.eu": true, "dnspub.restena.lu": true,
	"dns.aquilenet.fr": true, "dns.anon.no": true, "wikimedia-dns.org": true, "xbox-dns.ru": true,
	"v0dka.ru": true, "ibuki.cgnat.net": true, "doh.cleanbrowsing.org": true, "dns.controld.com": true,
	"freedns.controld.com": true, "doh.tiarap.org": true, "doh.tiar.app": true, "dns.switch.ch": true,
	"dns.brahma.world": true, "doh.applied-privacy.net": true, "basic.rethinkdns.com": true,
}

func DoHHost(rawURL string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	return host, knownDoHHosts[host]
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

func stringList(v interface{}) []string {
	items, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		if s, ok := it.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func scrubPins(m map[string]interface{}, r *Report) {
	pinsRaw, ok := lookupPath(m, "dns.pins")
	if !ok {
		return
	}
	pins, ok := pinsRaw.(map[string]interface{})
	if !ok || len(pins) == 0 {
		deletePath(m, "dns.pins")
		return
	}
	targetsRaw, _ := lookupPath(m, "targets.sni_domains")
	targets := stringList(targetsRaw)
	for _, domain := range sortedKeys(pins) {
		if !domainTargeted(domain, targets) {
			r.strip("dns.pins."+domain, "pin_not_targeted")
			delete(pins, domain)
			continue
		}
		for _, addr := range stringList(pins[domain]) {
			if !isPublicIP(addr) {
				r.strip("dns.pins."+domain, "pin_private_address")
				delete(pins, domain)
				break
			}
		}
	}
	if len(pins) == 0 {
		deletePath(m, "dns.pins")
	}
}

func stripNever(m, def map[string]interface{}, r *Report) {
	paths := neverPaths()
	sort.Strings(paths)
	for _, path := range paths {
		val, ok := lookupPath(m, path)
		if !ok {
			continue
		}
		defVal, hasDef := lookupPath(def, path)
		changed := !hasDef || !reflect.DeepEqual(val, defVal)
		if changed && !silentStrips[path] && !isEmptyValue(val) {
			r.strip(path, "private")
		}
		deletePath(m, path)
	}
}

func isEmptyValue(v interface{}) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	case bool:
		return !t
	case float64:
		return t == 0
	case []interface{}:
		return len(t) == 0
	case map[string]interface{}:
		return len(t) == 0
	}
	return false
}

var privateSuffixes = []string{".local", ".lan", ".home", ".internal", ".localdomain", ".corp", ".intranet", ".arpa"}

func looksPrivate(entry string) bool {
	value, isRegex := sni.ParseDomainEntry(entry)
	if isRegex || value == "" {
		return false
	}
	if net.ParseIP(value) != nil {
		return true
	}
	if !strings.Contains(value, ".") {
		return true
	}
	for _, suffix := range privateSuffixes {
		if strings.HasSuffix(value, suffix) {
			return true
		}
	}
	return false
}

func addWarnings(sparse map[string]interface{}, r *Report) {
	domainsRaw, _ := lookupPath(sparse, "targets.sni_domains")
	domains := stringList(domainsRaw)
	ipsRaw, _ := lookupPath(sparse, "targets.ip")
	ips := stringList(ipsRaw)
	geositeRaw, _ := lookupPath(sparse, "targets.geosite_categories")
	geoipRaw, _ := lookupPath(sparse, "targets.geoip_categories")

	if len(domains) == 0 && len(ips) == 0 && len(stringList(geositeRaw)) == 0 && len(stringList(geoipRaw)) == 0 {
		r.warn("no_targets", nil)
	}

	private := make([]string, 0)
	for _, d := range domains {
		if looksPrivate(d) {
			private = append(private, d)
		}
	}
	if len(private) > 0 {
		r.warn("private_domains", map[string]interface{}{"domains": private})
	}
	privateAddrs := make([]string, 0)
	for _, ip := range ips {
		if !isPublicTarget(ip) {
			privateAddrs = append(privateAddrs, ip)
		}
	}
	if len(privateAddrs) > 0 {
		r.warn("private_addresses", map[string]interface{}{"addresses": privateAddrs})
	}
	if len(domains) > MaxSNIDomains {
		r.warn("too_many_domains", map[string]interface{}{"count": len(domains), "max": MaxSNIDomains})
	}
	if len(ips) > MaxIPs {
		r.warn("too_many_ips", map[string]interface{}{"count": len(ips), "max": MaxIPs})
	}
	if custom, ok := lookupPath(sparse, "faking.custom_payload"); ok {
		if s, ok := custom.(string); ok && len(s) > MaxCustomPayloadBytes {
			r.warn("custom_payload_large", map[string]interface{}{"size": len(s), "max": MaxCustomPayloadBytes})
		}
	}
	if pinsRaw, ok := lookupPath(sparse, "dns.pins"); ok {
		if pins, ok := pinsRaw.(map[string]interface{}); ok && len(pins) > 0 {
			r.warn("pins", map[string]interface{}{"domains": sortedKeys(pins)})
		}
	}
	if enabled, _ := lookupPath(sparse, "dns.enabled"); enabled == true {
		if dohRaw, ok := lookupPath(sparse, "dns.doh_url"); ok {
			if doh, _ := dohRaw.(string); doh != "" {
				if host, known := DoHHost(doh); !known {
					r.warn("doh_unknown", map[string]interface{}{"host": host, "url": doh})
				}
			}
		}
	}
	if mode, _ := lookupPath(sparse, "routing.mode"); mode == config.RoutingModeBlock {
		for _, d := range domains {
			if _, isRegex := sni.ParseDomainEntry(d); isRegex {
				r.warn("block_regexp", map[string]interface{}{"entry": d})
				break
			}
		}
	}
}

func dropUnknownDoH(m map[string]interface{}, warn func(code string, params map[string]interface{})) {
	dns, ok := m["dns"].(map[string]interface{})
	if !ok {
		return
	}
	if enabled, _ := dns["enabled"].(bool); !enabled {
		return
	}
	doh, _ := dns["doh_url"].(string)
	if doh == "" {
		return
	}
	host, known := DoHHost(doh)
	if known {
		return
	}
	warn("dns_dropped", map[string]interface{}{"host": host, "url": doh})
	delete(m, "dns")
}

func Scrub(set *config.SetConfig) (map[string]interface{}, *Report, error) {
	r := &Report{}
	clone := *set
	config.ApplySetDefaults(&clone)
	m, err := config.SetToMap(&clone)
	if err != nil {
		return nil, nil, err
	}
	defSet := config.NewSetConfig()
	def, err := config.SetToMap(&defSet)
	if err != nil {
		return nil, nil, err
	}
	resetDisabledBlocks(m, def)
	scrubRouting(m, r)
	scrubPins(m, r)
	stripNever(m, def, r)
	sparse, err := config.SparsifySetMap(m)
	if err != nil {
		return nil, nil, err
	}
	addWarnings(sparse, r)
	return sparse, r.sorted(), nil
}

func stripForeign(m map[string]interface{}) {
	if routing, ok := m["routing"].(map[string]interface{}); ok {
		enabled, _ := routing["enabled"].(bool)
		mode, _ := routing["mode"].(string)
		if !enabled || mode != config.RoutingModeBlock {
			delete(m, "routing")
		} else {
			for k := range routing {
				if !blockRoutingKeep[k] {
					delete(routing, k)
				}
			}
		}
	}
	for _, path := range neverPaths() {
		deletePath(m, path)
	}
}
