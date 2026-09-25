package sni

import (
	"bytes"
	"net"
)

var httpRequestMethods = [][]byte{
	[]byte("GET "),
	[]byte("POST "),
	[]byte("HEAD "),
	[]byte("PUT "),
	[]byte("DELETE "),
	[]byte("OPTIONS "),
	[]byte("PATCH "),
	[]byte("TRACE "),
	[]byte("CONNECT "),
}

var (
	httpLineEnd    = []byte("\r\n")
	httpHeadersEnd = []byte("\r\n\r\n")
	httpHostLabel  = []byte("host:")
)

func LooksLikeHTTPRequest(b []byte) bool {
	for _, m := range httpRequestMethods {
		if bytes.HasPrefix(b, m) {
			return true
		}
	}
	return false
}

func IsHTTPMethodPrefix(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for _, m := range httpRequestMethods {
		if len(b) < len(m) && bytes.HasPrefix(m, b) {
			return true
		}
	}
	return false
}

func HTTPHeadersComplete(b []byte) bool {
	return bytes.Contains(b, httpHeadersEnd)
}

func ParseHTTPHost(b []byte) (string, bool) {
	if !LooksLikeHTTPRequest(b) {
		return "", false
	}
	first := bytes.Index(b, httpLineEnd)
	if first < 0 {
		return "", false
	}
	pos := first + len(httpLineEnd)
	for pos < len(b) {
		rel := bytes.Index(b[pos:], httpLineEnd)
		if rel <= 0 {
			return "", false
		}
		line := b[pos : pos+rel]
		pos += rel + len(httpLineEnd)
		if len(line) <= len(httpHostLabel) || !bytes.EqualFold(line[:len(httpHostLabel)], httpHostLabel) {
			continue
		}
		value := string(bytes.TrimSpace(line[len(httpHostLabel):]))
		if h, _, err := net.SplitHostPort(value); err == nil {
			value = h
		}
		host := NormalizeDomain(value)
		if !validateSNI(host) {
			return "", false
		}
		return host, true
	}
	return "", false
}
