package mtproto

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

const secretTagFakeTLS = 0xee

type Secret struct {
	Key      [16]byte
	Host     string
	RawBytes []byte
	ID       string
	Name     string
}

func (s *Secret) Label() string {
	if s == nil {
		return ""
	}
	if l := sanitizeLabel(s.Name); l != "" {
		return l
	}
	if l := sanitizeLabel(s.ID); l != "" {
		return l
	}
	return "unnamed"
}

func sanitizeLabel(v string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, v)
	return strings.TrimSpace(cleaned)
}

func ParseSecret(s string) (*Secret, error) {
	s = strings.TrimSpace(s)
	raw, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid hex: %w", err)
	}
	if len(raw) < 17 {
		return nil, fmt.Errorf("secret too short: need at least 17 bytes, got %d", len(raw))
	}
	if raw[0] != secretTagFakeTLS {
		return nil, fmt.Errorf("unsupported secret type 0x%02x, only fake-TLS (0xee) is supported", raw[0])
	}

	sec := &Secret{
		Host:     string(raw[17:]),
		RawBytes: raw,
	}
	copy(sec.Key[:], raw[1:17])

	if sec.Host == "" {
		return nil, fmt.Errorf("secret missing hostname")
	}
	return sec, nil
}

func GenerateSecret(host string) (*Secret, error) {
	if host == "" {
		return nil, fmt.Errorf("hostname required")
	}

	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	raw := make([]byte, 0, 1+16+len(host))
	raw = append(raw, secretTagFakeTLS)
	raw = append(raw, key[:]...)
	raw = append(raw, []byte(host)...)

	return &Secret{
		Key:      key,
		Host:     host,
		RawBytes: raw,
	}, nil
}

func (s *Secret) Hex() string {
	return hex.EncodeToString(s.RawBytes)
}
