package detector

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const upstreamRaw = "https://raw.githubusercontent.com/Runnin4ik/dpi-detector/main/"

var yamlServerLine = regexp.MustCompile(`^\s*-\s*\[(.*)\]`)
var yamlTopKey = regexp.MustCompile(`^[A-Z_0-9]+:`)

func fetchUpstream(ctx context.Context, mark uint, name string) (string, error) {
	data, err := fetchUpstreamFull(ctx, mark, name)
	if err != nil {
		return "", err
	}
	body := strings.TrimSpace(string(data))
	if body == "" {
		return "", fmt.Errorf("could not download %s", name)
	}
	return body, nil
}

func fetchUpstreamFull(ctx context.Context, mark uint, name string) ([]byte, error) {
	data, err := fetchBytes(ctx, mark, upstreamRaw+name, 20*time.Second, 2*1024*1024)
	if err != nil {
		return nil, fmt.Errorf("could not download %s: %w", name, err)
	}
	return data, nil
}

func parseLines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

func parseUpstreamDNSServers(configYAML string) []DNSServer {
	var out []DNSServer
	in := false
	seen := make(map[string]bool)
	for _, line := range strings.Split(configYAML, "\n") {
		if strings.HasPrefix(line, "DNS_AVAILABILITY_SERVERS") {
			in = true
			continue
		}
		if in && yamlTopKey.MatchString(line) {
			break
		}
		if !in {
			continue
		}
		if idx := strings.Index(line, "#"); idx >= 0 && !strings.Contains(line[:idx], "[") {
			continue
		}
		m := yamlServerLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		var parts []string
		for _, p := range strings.Split(m[1], ",") {
			parts = append(parts, strings.Trim(strings.TrimSpace(p), `"'`))
		}
		if len(parts) < 3 {
			continue
		}
		kind := map[string]string{"udp": "udp", "doh_wire": "doh", "dot": "dot"}[parts[2]]
		if kind == "" {
			continue
		}
		srv := DNSServer{Name: parts[1], Brand: brandOf(parts[1]), Address: parts[0], Kind: kind}
		if len(parts) > 3 {
			srv.Port, _ = strconv.Atoi(parts[3])
		}
		key := srv.Brand + "/" + kind
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, srv)
	}
	return out
}

func brandOf(name string) string {
	for _, suffix := range []string{" IP 2", " IP", " one", " dot", " dns"} {
		name = strings.Replace(name, suffix, "", 1)
	}
	return name
}

type upstreamTarget struct {
	ID       string `json:"id"`
	ASN      string `json:"asn"`
	Provider string `json:"provider"`
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	SNI      string `json:"sni"`
}

func UpdateListsFromUpstream(ctx context.Context, mark uint, configPath string) (TargetLists, error) {
	lists := Lists()

	domains, err := fetchUpstream(ctx, mark, "domains.txt")
	if err != nil {
		return lists, err
	}
	whitelist, err := fetchUpstream(ctx, mark, "whitelist_sni.txt")
	if err != nil {
		return lists, err
	}
	tcpRaw, err := fetchUpstreamFull(ctx, mark, "tcp16.json")
	if err != nil {
		return lists, err
	}
	configYAML, err := fetchUpstreamFull(ctx, mark, "config.yml")
	if err != nil {
		return lists, err
	}

	var raw []upstreamTarget
	if err := json.Unmarshal(tcpRaw, &raw); err != nil {
		return lists, fmt.Errorf("tcp16.json: %w", err)
	}
	sniByIP := make(map[string]string)
	for _, t := range lists.TCPTargets {
		if t.SNI != "" {
			sniByIP[t.IP] = t.SNI
		}
	}
	var targets []TCPTarget
	for _, t := range raw {
		if t.IP == "" || t.Port == 0 {
			continue
		}
		tt := TCPTarget{
			ID: t.ID, IP: t.IP, Port: t.Port, Provider: t.Provider,
			ASN:       strings.TrimSuffix(t.ASN, "☆"),
			Reference: strings.HasSuffix(t.ASN, "☆"),
			SNI:       t.SNI,
		}
		if tt.SNI == "" {
			tt.SNI = sniByIP[tt.IP]
		}
		targets = append(targets, tt)
	}
	sites := parseLines(domains)
	wl := parseLines(whitelist)
	servers := parseUpstreamDNSServers(string(configYAML))
	if len(sites) < 5 || len(targets) < 10 || len(wl) < 10 {
		return lists, fmt.Errorf("upstream lists look truncated: %d sites, %d targets, %d SNI", len(sites), len(targets), len(wl))
	}

	lists.Sites = sites
	lists.WhitelistSNI = wl
	lists.TCPTargets = targets
	if len(servers) >= 10 {
		lists.DNSServers = servers
	}
	lists.ListsDate = time.Now().UTC().Format("2006-01-02")
	lists.ListsSource = "https://github.com/Runnin4ik/dpi-detector"

	if err := saveListOverride(configPath, lists); err != nil {
		return lists, err
	}
	return lists, nil
}
