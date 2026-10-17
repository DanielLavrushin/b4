package utils

import "net"

func FilterUniqueStrings(input []string) []string {
	seen := make(map[string]bool)
	result := []string{}

	for _, d := range input {
		if !seen[d] {
			seen[d] = true
			result = append(result, d)
		}
	}

	return result
}

func IsPrivateIP(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		return ip4[0] == 10 ||
			(ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31) ||
			(ip4[0] == 192 && ip4[1] == 168) ||
			ip4[0] == 127
	}
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsPrivate()
}

var privateNets = func() []*net.IPNet {
	var nets []*net.IPNet
	for _, cidr := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8", "::1/128", "fe80::/10", "fc00::/7"} {
		_, n, _ := net.ParseCIDR(cidr)
		nets = append(nets, n)
	}
	return nets
}()

func IsPrivateNet(n *net.IPNet) bool {
	if n == nil {
		return false
	}
	ones, bits := n.Mask.Size()
	for _, p := range privateNets {
		pOnes, pBits := p.Mask.Size()
		if bits == pBits && ones >= pOnes && p.Contains(n.IP) {
			return true
		}
	}
	return false
}
