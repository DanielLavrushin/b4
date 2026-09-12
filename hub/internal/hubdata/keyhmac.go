package hubdata

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

const AuthorLabelLength = 16

func KeyHMAC(secret []byte, keyID string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(keyID))
	return hex.EncodeToString(mac.Sum(nil))
}

func AuthorLabel(keyHMAC string) string {
	if len(keyHMAC) <= AuthorLabelLength {
		return keyHMAC
	}
	return keyHMAC[:AuthorLabelLength]
}
