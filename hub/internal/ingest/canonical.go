package ingest

import (
	"encoding/base64"

	"github.com/daniellavrushin/b4/hubwire"
)

const RelayAgent = "b4hub-mirror"

func CanonicalEncoding(rec *hubwire.Record) bool {
	pub, err := hubwire.DecodeKey(rec.Key)
	if err != nil || hubwire.EncodeKey(pub) != rec.Key {
		return false
	}
	sig, err := base64.RawURLEncoding.DecodeString(rec.Sig)
	return err == nil && base64.RawURLEncoding.EncodeToString(sig) == rec.Sig
}
