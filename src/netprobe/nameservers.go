package netprobe

import (
	"bufio"
	"io"
	"net/netip"
	"os"
	"strings"
)

const maxNameservers = 3

var defaultNameservers = []string{"127.0.0.1", "::1"}

func SystemNameservers() []string {
	f, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return defaultNameservers
	}
	defer f.Close()
	if servers := parseNameservers(f); len(servers) > 0 {
		return servers
	}
	return defaultNameservers
}

func parseNameservers(r io.Reader) []string {
	var out []string
	seen := make(map[string]bool)
	used := 0
	sc := bufio.NewScanner(r)
	for used < maxNameservers && sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || fields[0] != "nameserver" {
			continue
		}
		addr, err := netip.ParseAddr(fields[1])
		if err != nil {
			continue
		}
		used++
		ip := addr.String()
		if seen[ip] {
			continue
		}
		seen[ip] = true
		out = append(out, ip)
	}
	return out
}
