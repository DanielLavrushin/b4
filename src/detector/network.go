package detector

import "net"

var fakeIPNet *net.IPNet

func init() {
	_, fakeIPNet, _ = net.ParseCIDR("198.18.0.0/15")
}

func isFakeIP(ipStr string) bool {
	if ipStr == "" || fakeIPNet == nil {
		return false
	}
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	ip4 := ip.To4()
	return ip4 != nil && fakeIPNet.Contains(ip4)
}
