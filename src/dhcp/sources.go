package dhcp

import (
	"bufio"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/daniellavrushin/b4/utils"
)

const arpPath = "/proc/net/arp"

var localAddrs = func() (macs map[string]struct{}, ips map[string]struct{}) {
	macs = make(map[string]struct{})
	ips = make(map[string]struct{})

	ifaces, err := net.Interfaces()
	if err != nil {
		return macs, ips
	}

	for _, ifi := range ifaces {
		if hw := ifi.HardwareAddr.String(); hw != "" {
			macs[strings.ToUpper(hw)] = struct{}{}
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip != nil {
				ips[ip.String()] = struct{}{}
			}
		}
	}

	return macs, ips
}

func LocalRouterIPs() []string {
	_, ips := localAddrs()
	out := make([]string, 0, len(ips))
	for ip := range ips {
		parsed := net.ParseIP(ip)
		if parsed == nil || parsed.IsLoopback() || parsed.IsLinkLocalUnicast() || parsed.IsUnspecified() {
			continue
		}
		if !utils.IsPrivateIP(parsed) {
			continue
		}
		out = append(out, ip)
	}
	return out
}

// parseARP reads /proc/net/arp and returns complete entries with private IPs only.
func parseARP() ([]ARPEntry, error) {
	return parseARPFile(arpPath)
}

func parseARPFile(path string) ([]ARPEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	localMACs, localIPs := localAddrs()

	var entries []ARPEntry
	scanner := bufio.NewScanner(file)

	// Skip header line
	scanner.Scan()

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 6 {
			continue
		}

		ip := fields[0]
		flags := fields[2]
		mac := strings.ToUpper(fields[3])
		device := fields[5]

		// Only keep complete entries (ATF_COM = 0x2)
		flagVal, err := strconv.ParseUint(flags, 0, 16)
		if err != nil || flagVal&0x2 == 0 {
			continue
		}

		if mac == "00:00:00:00:00:00" || mac == "FF:FF:FF:FF:FF:FF" {
			continue
		}

		if _, ok := localMACs[mac]; ok {
			continue
		}
		if _, ok := localIPs[ip]; ok {
			continue
		}

		// Only keep LAN devices (private IPs)
		parsedIP := net.ParseIP(ip)
		if parsedIP == nil || !utils.IsPrivateIP(parsedIP) {
			continue
		}

		entries = append(entries, ARPEntry{
			IP:     ip,
			MAC:    mac,
			Device: device,
		})
	}

	return entries, scanner.Err()
}

// enrichHostnames tries known DHCP lease file paths to extract MAC->hostname mappings.
// Returns an empty map if no lease files are found. Best-effort only.
func enrichHostnames() map[string]string {
	dnsmasqPaths := []string{
		"/var/lib/misc/dnsmasq.leases",
		"/tmp/dhcp.leases",
		"/var/lib/dnsmasq/dnsmasq.leases",
		"/tmp/dnsmasq.leases",
	}
	for _, p := range dnsmasqPaths {
		if h := parseDnsmasqHostnames(p); len(h) > 0 {
			return h
		}
	}

	iscPaths := []string{
		"/var/lib/dhcp/dhcpd.leases",
		"/var/lib/dhcpd/dhcpd.leases",
	}
	for _, p := range iscPaths {
		if h := parseISCHostnames(p); len(h) > 0 {
			return h
		}
	}

	return make(map[string]string)
}

func parseDnsmasqHostnames(path string) map[string]string {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	result := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 4 && fields[3] != "*" {
			mac := strings.ToUpper(fields[1])
			result[mac] = fields[3]
		}
	}
	return result
}

func parseISCHostnames(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	result := make(map[string]string)
	var mac, hostname string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "hardware ethernet ") {
			mac = strings.ToUpper(strings.TrimSuffix(strings.TrimPrefix(line, "hardware ethernet "), ";"))
		} else if strings.HasPrefix(line, "client-hostname ") {
			hostname = strings.Trim(strings.TrimSuffix(strings.TrimPrefix(line, "client-hostname "), ";"), "\"")
		} else if line == "}" {
			if mac != "" && hostname != "" {
				result[mac] = hostname
			}
			mac, hostname = "", ""
		}
	}
	return result
}
