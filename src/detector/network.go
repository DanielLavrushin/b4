package detector

import "net"

var (
	fakeIPNet *net.IPNet
	cgnatNet  *net.IPNet
	ispIPNets []*net.IPNet
	localNets []*net.IPNet
)

func init() {
	_, fakeIPNet, _ = net.ParseCIDR("198.18.0.0/15")
	_, cgnatNet, _ = net.ParseCIDR("100.64.0.0/10")
	for _, cidr := range []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"0.0.0.0/8",
	} {
		if _, n, err := net.ParseCIDR(cidr); err == nil {
			localNets = append(localNets, n)
		}
	}
}

func setISPRanges(cidrs []string) {
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		if _, n, err := net.ParseCIDR(cidr); err == nil {
			nets = append(nets, n)
		}
	}
	ispIPNets = nets
}

func getFakeIPType(ipStr string) string {
	if ipStr == "" {
		return ""
	}
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return ""
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return ""
	}
	if fakeIPNet != nil && fakeIPNet.Contains(ip4) {
		return "fakeip"
	}
	if cgnatNet != nil && cgnatNet.Contains(ip4) {
		return "isp"
	}
	for _, n := range ispIPNets {
		if n.Contains(ip4) {
			return "isp"
		}
	}
	for _, n := range localNets {
		if n.Contains(ip4) {
			return "local"
		}
	}
	return ""
}
