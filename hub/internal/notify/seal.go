package notify

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

const (
	sealPrefix  = "v1:"
	sealContext = "b4hub-notify"
)

var errNoSecret = errors.New("the hub secret is missing")

func sealKey(secret []byte) ([]byte, error) {
	if len(secret) == 0 {
		return nil, errNoSecret
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(sealContext))
	return mac.Sum(nil), nil
}

func newAEAD(secret []byte) (cipher.AEAD, error) {
	key, err := sealKey(secret)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func seal(secret []byte, field, plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	aead, err := newAEAD(secret)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := aead.Seal(nonce, nonce, []byte(plain), []byte(field))
	return sealPrefix + base64.StdEncoding.EncodeToString(out), nil
}

func unseal(secret []byte, field, sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	if !strings.HasPrefix(sealed, sealPrefix) {
		return "", errors.New(field + " is not sealed")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(sealed, sealPrefix))
	if err != nil {
		return "", errors.New(field + " is not valid base64")
	}
	aead, err := newAEAD(secret)
	if err != nil {
		return "", err
	}
	if len(raw) < aead.NonceSize() {
		return "", errors.New(field + " is truncated")
	}
	plain, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], []byte(field))
	if err != nil {
		return "", errors.New(field + " does not unseal with this hub secret")
	}
	return string(plain), nil
}
