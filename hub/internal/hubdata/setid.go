package hubdata

import (
	"crypto/rand"
	"encoding/base32"
	"regexp"
)

var crockford = base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding)

var setIDPattern = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

func NewSetID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return crockford.EncodeToString(raw), nil
}

func ValidSetID(id string) bool {
	return setIDPattern.MatchString(id)
}
