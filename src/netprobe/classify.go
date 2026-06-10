package netprobe

import "strings"

type DomainStatus string

const (
	DomainOk       DomainStatus = "OK"
	DomainTLSDPI   DomainStatus = "TLS_DPI"
	DomainTLSMITM  DomainStatus = "TLS_MITM"
	DomainTLSSpoof DomainStatus = "TLS_SPOOF"
	DomainTLSAlert DomainStatus = "TLS_ALERT"
	DomainTLSReset DomainStatus = "TLS_RST"
	DomainTLSDrop  DomainStatus = "TLS_DROP"
	DomainSYNDrop  DomainStatus = "SYN_DROP"
	DomainTCP16    DomainStatus = "TCP16"
	DomainISPPage  DomainStatus = "ISP_PAGE"
	DomainBlocked  DomainStatus = "BLOCKED"
	DomainDNSFake  DomainStatus = "DNS_FAKE"
	DomainTimeout  DomainStatus = "TIMEOUT"
	DomainError    DomainStatus = "ERROR"
)

type TLSStage int

const (
	StageConnect TLSStage = iota
	StageHandshake
	StageRead
)

const (
	TCP16MinBytes = 12 * 1024
	TCP16MaxBytes = 69 * 1024
)

func ClassifyTLSError(err error) (DomainStatus, string) {
	return ClassifyTLSErrorStaged(err, StageHandshake, 0)
}

func ClassifyTLSErrorStaged(err error, stage TLSStage, bytesRead int) (DomainStatus, string) {
	if err == nil {
		return DomainOk, ""
	}

	msg := strings.ToLower(err.Error())

	isTimeout := strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "timed out")
	isEOF := strings.Contains(msg, "eof")
	isReset := strings.Contains(msg, "connection reset") || strings.Contains(msg, "reset by peer")

	if stage == StageRead && isTimeout && bytesRead >= TCP16MinBytes && bytesRead <= TCP16MaxBytes {
		return DomainTCP16, "Read stalled after TSPU fat-flow window (12-69KB)"
	}

	if isTimeout {
		switch stage {
		case StageConnect:
			return DomainSYNDrop, "TCP SYN dropped (no handshake)"
		case StageHandshake:
			return DomainTLSDrop, "TLS handshake timed out (drop)"
		default:
			return DomainTimeout, "Connection timed out"
		}
	}

	if strings.Contains(msg, "wrong version number") {
		return DomainTLSSpoof, "Non-TLS response received (DPI replacement)"
	}
	for _, p := range []string{"record overflow", "oversized", "record layer failure", "decode error", "decoding error", "illegal parameter", "bad record mac", "decryption failed"} {
		if strings.Contains(msg, p) {
			return DomainTLSSpoof, "Garbage TLS response (DPI injection)"
		}
	}

	if strings.Contains(msg, "alert") || strings.Contains(msg, "unrecognized name") || strings.Contains(msg, "handshake failure") || strings.Contains(msg, "protocol version") {
		switch {
		case strings.Contains(msg, "unrecognized name"):
			return DomainTLSAlert, "SNI blocked (unrecognized name)"
		case strings.Contains(msg, "protocol version"):
			return DomainTLSAlert, "TLS protocol version alert"
		default:
			return DomainTLSAlert, "TLS alert (DPI disruption)"
		}
	}

	if isReset {
		if stage == StageHandshake || stage == StageConnect {
			return DomainTLSReset, "TCP RST during handshake (active reset)"
		}
		return DomainTLSReset, "TCP RST during transfer"
	}

	if isEOF {
		if stage == StageHandshake || bytesRead == 0 {
			return DomainTLSReset, "Connection terminated (EOF injection)"
		}
		return DomainTLSReset, "Connection dropped during transfer (EOF)"
	}

	for _, p := range []string{"self-signed", "self signed", "unknown authority", "certificate has expired", "certificate is not valid", "hostname mismatch", "name mismatch", "x509", "certificate"} {
		if strings.Contains(msg, p) {
			return DomainTLSMITM, "Certificate substitution (possible MITM)"
		}
	}

	if strings.Contains(msg, "no shared cipher") || strings.Contains(msg, "cipher") {
		return DomainTLSMITM, "Cipher mismatch (possible MITM)"
	}

	if strings.Contains(msg, "refused") {
		return DomainBlocked, "Connection refused"
	}
	if strings.Contains(msg, "no route to host") {
		return DomainError, "No route to host (IP unreachable)"
	}
	if strings.Contains(msg, "network is unreachable") {
		return DomainError, "Network unreachable"
	}
	if strings.Contains(msg, "no such host") || strings.Contains(msg, "no address") {
		return DomainError, "DNS resolution failed"
	}
	if strings.Contains(msg, "internal error") {
		return DomainError, "TLS internal error"
	}

	return DomainError, err.Error()
}

func ClassifyHTTPResponse(statusCode int, location, body string) (DomainStatus, string) {
	if statusCode == 451 {
		return DomainISPPage, "HTTP 451 Unavailable For Legal Reasons"
	}

	if IsBlockPageRedirect(location) {
		return DomainISPPage, "Redirect to ISP block page: " + location
	}

	if DetectBlockPageBody([]byte(body)) != "" {
		return DomainISPPage, "ISP block page detected in response body"
	}

	return DomainOk, ""
}
