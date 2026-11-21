package sni

import (
	"net"
	"strings"
)

const maxHostNameLength = 253

func ValidHostName(host string) bool {
	if host == "" || len(host) > maxHostNameLength || !IsValidSNI([]byte(host)) || net.ParseIP(host) != nil {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > maxLabelLength {
			return false
		}
	}
	return true
}
