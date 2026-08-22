package mtproto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"strings"
)

const webBridgeContext = "tdesktop-web-proxy-bridge-v1\n"

const secretTagPadded = 0xdd

func WebBridgeCapability(host string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(webBridgeContext))
	mac.Write([]byte(host))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func WebSecretForms(key [16]byte) (plain string, padded string) {
	plain = hex.EncodeToString(key[:])
	return plain, hex.EncodeToString([]byte{secretTagPadded}) + plain
}

func webSecretBytes(key [16]byte) [][]byte {
	padded := make([]byte, 0, 17)
	padded = append(padded, secretTagPadded)
	padded = append(padded, key[:]...)
	plain := make([]byte, 16)
	copy(plain, key[:])
	return [][]byte{padded, plain}
}

func WebProxyLink(host string, key [16]byte) string {
	_, padded := WebSecretForms(key)
	return "https://t.me/webproxy?server=" + host + "&secret=" + padded
}

func CanonicalWebHost(v string) string {
	h := strings.TrimSpace(v)
	if i := strings.Index(h, "://"); i >= 0 {
		h = h[i+3:]
	}
	h = strings.TrimSuffix(h, "/")
	if i := strings.IndexAny(h, "/?#"); i >= 0 {
		h = h[:i]
	}
	if i := strings.LastIndex(h, ":"); i > 0 && !strings.Contains(h[i:], "]") {
		h = h[:i]
	}
	h = strings.TrimSuffix(strings.ToLower(h), ".")
	return h
}

var errWebHostEmpty = errors.New("relay hostname is required")

func ValidateWebProxyHost(v string) (string, error) {
	raw := strings.TrimSpace(v)
	if raw == "" {
		return "", errWebHostEmpty
	}
	if strings.ContainsAny(raw, "/?#@ \t") || strings.Contains(raw, ":") {
		return "", errors.New("enter a bare hostname, without scheme, port, path or credentials")
	}

	host := CanonicalWebHost(raw)
	if host == "" {
		return "", errWebHostEmpty
	}
	if len(host) > 253 {
		return "", errors.New("hostname is longer than 253 characters")
	}
	for _, r := range host {
		if r > 0x7f {
			return "", errors.New("enter the punycode (xn--) form of an international hostname")
		}
	}
	if net.ParseIP(host) != nil {
		return "", errors.New("an IP address cannot be used, a DNS hostname is required")
	}

	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return "", errors.New("a single-label name cannot be used, a full domain is required")
	}
	for _, label := range labels {
		if label == "" {
			return "", errors.New("hostname has an empty label")
		}
		if len(label) > 63 {
			return "", errors.New("hostname has a label longer than 63 characters")
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("hostname has a label starting or ending with a hyphen")
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
				continue
			}
			return "", errors.New("hostname may only contain letters, digits and hyphens")
		}
	}

	tld := labels[len(labels)-1]
	if len(tld) < 2 {
		return "", errors.New("hostname must end in a domain suffix")
	}
	for i := 0; i < len(tld); i++ {
		if c := tld[i]; c < 'a' || c > 'z' {
			return "", errors.New("hostname must end in a letters-only domain suffix")
		}
	}
	return host, nil
}
