package hubdata

import (
	"crypto/rand"
	"encoding/base32"
	"regexp"
	"time"
)

var crockford = base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding)

var setIDPattern = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

func NewSetID(now time.Time) (string, error) {
	raw := make([]byte, 16)
	ms := uint64(now.UnixMilli())
	for i := 5; i >= 0; i-- {
		raw[i] = byte(ms)
		ms >>= 8
	}
	if _, err := rand.Read(raw[6:]); err != nil {
		return "", err
	}
	return crockford.EncodeToString(raw), nil
}

func ValidSetID(id string) bool {
	return setIDPattern.MatchString(id)
}
