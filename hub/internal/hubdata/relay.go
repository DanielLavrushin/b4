package hubdata

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
)

const (
	HeaderRelay = "B4hub-Relay"
	RelayWindow = 10 * time.Minute

	relayVersion = "1"
	relayDomain  = "b4hub-relay/1\n"
)

func relayMessage(keyID string, ts int64, body []byte) []byte {
	sum := sha256.Sum256(body)
	return []byte(relayDomain + keyID + "\n" + strconv.FormatInt(ts, 10) + "\n" + hex.EncodeToString(sum[:]))
}

func SignRelay(id *hubwire.Identity, now time.Time, body []byte) string {
	keyID := id.KeyID()
	ts := now.Unix()
	sig := id.Sign(relayMessage(keyID, ts, body))
	return relayVersion + " " + keyID + " " + strconv.FormatInt(ts, 10) + " " + base64.RawURLEncoding.EncodeToString(sig)
}

func RelayKeyID(header string) string {
	parts := strings.Fields(header)
	if len(parts) != 4 || parts[0] != relayVersion {
		return ""
	}
	return parts[1]
}

func VerifyRelay(header string, body []byte, now time.Time) (string, bool) {
	parts := strings.Fields(header)
	if len(parts) != 4 || parts[0] != relayVersion {
		return "", false
	}
	keyID := parts[1]
	pub, err := hubwire.DecodeKey(keyID)
	if err != nil || hubwire.EncodeKey(pub) != keyID {
		return "", false
	}
	ts, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return "", false
	}
	if skew := now.Sub(time.Unix(ts, 0)); skew > RelayWindow || skew < -RelayWindow {
		return "", false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(sig) != ed25519.SignatureSize {
		return "", false
	}
	if !ed25519.Verify(pub, relayMessage(keyID, ts, body), sig) {
		return "", false
	}
	return keyID, true
}
