package hubwire

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRecordSignAndVerify(t *testing.T) {
	id, err := NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 12, 15, 0, 0, 0, time.UTC)
	rec, err := SignRecord(id, RecordVote, VoteBody{SetID: "abc", Version: 1, FP: "ff", Kind: VoteWorks}, now)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Key != id.KeyID() || rec.TS != now.Unix() || len(rec.Nonce) != 32 {
		t.Errorf("unexpected header %+v", rec)
	}
	raw, _ := json.Marshal(rec)
	var decoded Record
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	pub, err := VerifyRecord(&decoded)
	if err != nil {
		t.Fatalf("round trip must verify: %v", err)
	}
	if EncodeKey(pub) != id.KeyID() {
		t.Errorf("verified key differs from signer")
	}
	if decoded.ID() != rec.ID() || decoded.ID() == "" {
		t.Errorf("record id must be stable across the wire")
	}

	tampered := decoded
	tampered.Body = json.RawMessage(`{"set_id":"abc","version":1,"fp":"ff","kind":"broken"}`)
	if _, err := VerifyRecord(&tampered); err != ErrBadSignature {
		t.Errorf("a changed body must fail verification, got %v", err)
	}
	other, _ := NewIdentity()
	swapped := decoded
	swapped.Key = other.KeyID()
	if _, err := VerifyRecord(&swapped); err != ErrBadSignature {
		t.Errorf("a swapped key must fail verification, got %v", err)
	}
	wrongKind := decoded
	wrongKind.Kind = "bogus"
	if _, err := VerifyRecord(&wrongKind); err != ErrUnknownKind {
		t.Errorf("unknown kind must be refused, got %v", err)
	}
	if _, err := SignRecord(id, "bogus", VoteBody{}, now); err != ErrUnknownKind {
		t.Errorf("signing an unknown kind must be refused")
	}
}

func TestRecoveryCodeRoundTrip(t *testing.T) {
	id, _ := NewIdentity()
	code := id.RecoveryCode()
	if len(strings.ReplaceAll(code, "-", "")) != 52 {
		t.Errorf("recovery code must be 52 base32 characters, got %q", code)
	}
	restored, err := IdentityFromRecoveryCode(strings.ToLower(code) + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if restored.KeyID() != id.KeyID() {
		t.Errorf("restored identity differs")
	}
	if _, err := IdentityFromRecoveryCode("not-a-code"); err != ErrRecoveryCode {
		t.Errorf("garbage must be refused, got %v", err)
	}
	seed, err := DecodeSeed(EncodeSeed(id.Seed()))
	if err != nil {
		t.Fatal(err)
	}
	again, _ := IdentityFromSeed(seed)
	if again.KeyID() != id.KeyID() {
		t.Errorf("seed encoding must round trip")
	}
}

func TestManifestSignAndVerify(t *testing.T) {
	hub, _ := NewIdentity()
	spare, _ := NewIdentity()
	m := &Manifest{
		Epoch:       1,
		Seq:         7,
		GeneratedAt: "2026-09-12T15:00:00Z",
		ExpiresAt:   "2026-09-26T15:00:00Z",
		Catalogue:   FileRef{File: CatalogueFileName(1, 7), SHA256: strings.Repeat("a", 64), Size: 1234},
		Mirrors:     []string{"https://hub.b4core.app"},
	}
	if err := SignManifest(m, hub); err != nil {
		t.Fatal(err)
	}
	trusted := []string{spare.KeyID(), hub.KeyID()}
	raw, _ := json.Marshal(m)
	var decoded Manifest
	_ = json.Unmarshal(raw, &decoded)
	if err := VerifyManifest(&decoded, trusted); err != nil {
		t.Fatalf("signed manifest must verify: %v", err)
	}
	if err := VerifyManifest(&decoded, []string{spare.KeyID()}); err != ErrManifestSigner {
		t.Errorf("an untrusted signer must be refused, got %v", err)
	}
	decoded.Seq = 8
	if err := VerifyManifest(&decoded, trusted); err != ErrBadSignature {
		t.Errorf("a changed manifest must fail, got %v", err)
	}
	if !m.Expired(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) || m.Expired(time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("expiry check is wrong")
	}
	older := &Manifest{Epoch: 1, Seq: 6}
	newerEpoch := &Manifest{Epoch: 2, Seq: 1}
	if !m.Newer(older) || !newerEpoch.Newer(m) || older.Newer(m) || !m.Newer(nil) {
		t.Errorf("ordering by (epoch, seq) is wrong")
	}
}

func TestPickScore(t *testing.T) {
	scores := Scores{
		Global: Score{Score: 0.6, N: 10, Devices: 5},
		ASN:    map[string]Score{"AS1": {Score: 0.9, N: 2, Devices: 2}, "AS2": {Score: 0.1, N: 1, Devices: 1}},
		CC:     map[string]Score{"RU": {Score: 0.7, N: 4, Devices: 3}},
	}
	if d := PickScore(scores, "AS1", "RU"); d.Bucket != BucketASN || d.Score != 0.9 {
		t.Errorf("usable ASN cell must win: %+v", d)
	}
	if d := PickScore(scores, "AS2", "ru"); d.Bucket != BucketCountry || d.Score != 0.7 {
		t.Errorf("a single-device ASN cell falls through to the country: %+v", d)
	}
	if d := PickScore(scores, "AS9", "DE"); d.Bucket != BucketGlobal {
		t.Errorf("unknown ASN and country fall through to global: %+v", d)
	}
	if d := PickScore(Scores{Global: Score{Score: 0.5, N: 0.5, Devices: 1}}, "", ""); d.Bucket != BucketNone {
		t.Errorf("thin evidence reads as no reports: %+v", d)
	}
}

func TestCanonicalIsOrderIndependent(t *testing.T) {
	a, _ := Canonical(map[string]interface{}{"b": 1, "a": map[string]interface{}{"z": true, "y": "x"}})
	b, _ := Canonical(struct {
		A map[string]interface{} `json:"a"`
		B int                    `json:"b"`
	}{A: map[string]interface{}{"y": "x", "z": true}, B: 1})
	if string(a) != string(b) {
		t.Errorf("canonical form must not depend on source ordering: %s vs %s", a, b)
	}
}

func TestCatalogueSetToEnvelopeCarriesOrigin(t *testing.T) {
	cs := CatalogueSet{ID: "01H", Version: 3, FP: "ff", Title: "T", B4Min: "1.82.0", Set: map[string]interface{}{"targets": map[string]interface{}{"sni_domains": []interface{}{"example.com"}}}}
	env := cs.ToEnvelope()
	if env.DerivedFrom == nil || env.DerivedFrom.ID != "01H" || env.DerivedFrom.Version != 3 || env.Format != Format {
		t.Errorf("envelope must point back at the catalogue entry: %+v", env)
	}
	imp, err := Open(env, OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if imp.Set.Hub == nil || imp.Set.Hub.ID != "01H" || imp.Set.Hub.Version != 3 {
		t.Errorf("applying a catalogue set must stamp its hub id: %+v", imp.Set.Hub)
	}
}
