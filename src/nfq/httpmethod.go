package nfq

import (
	"bytes"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

var httpMethods = [][]byte{
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
	methodEOL      = []byte("\r\n")
	userAgentLabel = []byte("user-agent:")
)

const httpIPv6HdrLen = 40

func looksLikeHTTPRequest(payload []byte) bool {
	for _, m := range httpMethods {
		if bytes.HasPrefix(payload, m) {
			return true
		}
	}
	return false
}

func payloadStartOf(packet []byte) int {
	if len(packet) < 1 {
		return -1
	}
	switch packet[0] >> 4 {
	case 4:
		ipHdrLen := int((packet[0] & 0x0F) * 4)
		if len(packet) < ipHdrLen+20 {
			return -1
		}
		start := ipHdrLen + int((packet[ipHdrLen+12]>>4)*4)
		if start > len(packet) {
			return -1
		}
		return start
	case 6:
		if len(packet) < httpIPv6HdrLen+20 {
			return -1
		}
		start := httpIPv6HdrLen + int((packet[httpIPv6HdrLen+12]>>4)*4)
		if start > len(packet) {
			return -1
		}
		return start
	}
	return -1
}

// locateUserAgentValue returns the bounds of the User-Agent header value within an HTTP
// request, excluding the trailing CRLF.
func locateUserAgentValue(payload []byte) (start, end int) {
	pos := 0
	for pos < len(payload) {
		rel := bytes.Index(payload[pos:], methodEOL)
		if rel < 0 {
			return -1, -1
		}
		lineStart, lineEnd := pos, pos+rel
		if lineEnd == lineStart {
			return -1, -1
		}
		line := payload[lineStart:lineEnd]
		if len(line) > len(userAgentLabel) && bytes.EqualFold(line[:len(userAgentLabel)], userAgentLabel) {
			valueStart := lineStart + len(userAgentLabel)
			for valueStart < lineEnd && (payload[valueStart] == ' ' || payload[valueStart] == '\t') {
				valueStart++
			}
			return valueStart, lineEnd
		}
		pos = lineEnd + len(methodEOL)
	}
	return -1, -1
}

// applyHTTPMethodEOL prepends an empty line to an HTTP request line and pays for it by
// dropping the last two characters of the User-Agent value, so the payload keeps its exact
// length and the client's sequence numbers stay correct. RFC 9112 section 2.2 tells a server
// to ignore an empty line before the request-line; inspection that expects the method at
// byte zero stops finding the Host header. Origins other than nginx may reject the result.
func (w *Worker) applyHTTPMethodEOL(cfg *config.SetConfig, packet []byte) []byte {
	if cfg == nil || !cfg.TCP.HTTPMethodEOL {
		return packet
	}
	start := payloadStartOf(packet)
	if start < 0 || start >= len(packet) {
		return packet
	}
	payload := packet[start:]
	if !looksLikeHTTPRequest(payload) {
		return packet
	}
	valueStart, valueEnd := locateUserAgentValue(payload)
	if valueEnd < 0 || valueEnd-valueStart < len(methodEOL) {
		log.Tracef("HTTP method EOL skipped: no User-Agent value to trim")
		return packet
	}

	out := make([]byte, 0, len(packet))
	out = append(out, packet[:start]...)
	out = append(out, methodEOL...)
	out = append(out, payload[:valueEnd-len(methodEOL)]...)
	out = append(out, payload[valueEnd:]...)
	w.updatePacketLengths(out)
	log.Tracef("Prepended an empty line to the HTTP request line, trimmed 2 bytes of User-Agent (%d bytes unchanged)", len(out))
	return out
}
