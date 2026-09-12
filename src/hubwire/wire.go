package hubwire

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	WireVersion = 1

	RecordShare  = "share"
	RecordVote   = "vote"
	RecordReport = "report"
	RecordMirror = "mirror"

	VoteWorks  = "works"
	VoteBroken = "broken"

	SetStatusPending  = "pending"
	SetStatusActive   = "active"
	SetStatusHidden   = "hidden"
	SetStatusRejected = "rejected"

	signingDomain = "b4hub/1\n"

	PathHealth   = "/b4/health"
	PathManifest = "/b4/hub/manifest.json"
	PathBlob     = "/b4/hub/blob/"
	PathMessage  = "/b4/hub/v1/msg"
	PathFiles    = "/b4/hub/"

	ManifestTTL = 14 * 24 * time.Hour
)

var (
	ErrBadSignature   = errors.New("signature does not verify")
	ErrBadKey         = errors.New("malformed key")
	ErrWireVersion    = errors.New("unsupported wire version")
	ErrUnknownKind    = errors.New("unknown record kind")
	ErrManifestSigner = errors.New("manifest is not signed by a trusted key")
	ErrRecoveryCode   = errors.New("malformed recovery code")
)

var BuiltinHubKeys = []string{"rRHQJTsJaBzitKgCwhD2Q2dtRDAbCO3xEzHewM8zS1o"}

func Canonical(v interface{}) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var generic interface{}
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, err
	}
	return json.Marshal(generic)
}

type Identity struct {
	priv ed25519.PrivateKey
}

func NewIdentity() (*Identity, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Identity{priv: priv}, nil
}

func IdentityFromSeed(seed []byte) (*Identity, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, ErrBadKey
	}
	return &Identity{priv: ed25519.NewKeyFromSeed(seed)}, nil
}

func (i *Identity) Seed() []byte {
	return i.priv.Seed()
}

func (i *Identity) Public() ed25519.PublicKey {
	return i.priv.Public().(ed25519.PublicKey)
}

func (i *Identity) KeyID() string {
	return EncodeKey(i.Public())
}

func (i *Identity) Sign(message []byte) []byte {
	return ed25519.Sign(i.priv, message)
}

var recoveryEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

func (i *Identity) RecoveryCode() string {
	raw := recoveryEncoding.EncodeToString(i.Seed())
	var b strings.Builder
	for n, r := range raw {
		if n > 0 && n%4 == 0 {
			b.WriteByte('-')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func IdentityFromRecoveryCode(code string) (*Identity, error) {
	clean := strings.ToUpper(strings.NewReplacer("-", "", " ", "", "\n", "", "\t", "").Replace(code))
	seed, err := recoveryEncoding.DecodeString(clean)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, ErrRecoveryCode
	}
	return IdentityFromSeed(seed)
}

func EncodeKey(pub ed25519.PublicKey) string {
	return base64.RawURLEncoding.EncodeToString(pub)
}

func DecodeKey(encoded string) (ed25519.PublicKey, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, ErrBadKey
	}
	return ed25519.PublicKey(raw), nil
}

func EncodeSeed(seed []byte) string {
	return base64.RawURLEncoding.EncodeToString(seed)
}

func DecodeSeed(encoded string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil || len(raw) != ed25519.SeedSize {
		return nil, ErrBadKey
	}
	return raw, nil
}

type Record struct {
	V     int             `json:"v"`
	Kind  string          `json:"kind"`
	Key   string          `json:"key"`
	TS    int64           `json:"ts"`
	Nonce string          `json:"nonce"`
	Body  json.RawMessage `json:"body"`
	Sig   string          `json:"sig,omitempty"`
}

type ShareBody struct {
	Envelope    Envelope `json:"envelope"`
	ASNHint     string   `json:"asn_hint,omitempty"`
	CountryHint string   `json:"country_hint,omitempty"`
	Engine      string   `json:"engine,omitempty"`
	B4Version   string   `json:"b4_version,omitempty"`
}

type VoteBody struct {
	SetID       string `json:"set_id"`
	Version     int    `json:"version"`
	FP          string `json:"fp"`
	Kind        string `json:"kind"`
	Domain      string `json:"domain,omitempty"`
	ASNHint     string `json:"asn_hint,omitempty"`
	CountryHint string `json:"country_hint,omitempty"`
	Engine      string `json:"engine,omitempty"`
	B4Version   string `json:"b4_version,omitempty"`
}

type ReportBody struct {
	SetID   string `json:"set_id"`
	Version int    `json:"version"`
	Reason  string `json:"reason"`
}

type MirrorBody struct {
	URL     string `json:"url"`
	Version string `json:"version,omitempty"`
}

func recordDigest(r *Record) ([]byte, error) {
	unsigned := *r
	unsigned.Sig = ""
	canon, err := Canonical(&unsigned)
	if err != nil {
		return nil, err
	}
	return append([]byte(signingDomain), canon...), nil
}

func SignRecord(id *Identity, kind string, body interface{}, now time.Time) (*Record, error) {
	switch kind {
	case RecordShare, RecordVote, RecordReport, RecordMirror:
	default:
		return nil, ErrUnknownKind
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	r := &Record{
		V:     WireVersion,
		Kind:  kind,
		Key:   id.KeyID(),
		TS:    now.Unix(),
		Nonce: hex.EncodeToString(nonce),
		Body:  raw,
	}
	digest, err := recordDigest(r)
	if err != nil {
		return nil, err
	}
	r.Sig = base64.RawURLEncoding.EncodeToString(id.Sign(digest))
	return r, nil
}

func VerifyRecord(r *Record) (ed25519.PublicKey, error) {
	if r == nil || r.V != WireVersion {
		return nil, ErrWireVersion
	}
	switch r.Kind {
	case RecordShare, RecordVote, RecordReport, RecordMirror:
	default:
		return nil, ErrUnknownKind
	}
	pub, err := DecodeKey(r.Key)
	if err != nil {
		return nil, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(r.Sig)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, ErrBadSignature
	}
	digest, err := recordDigest(r)
	if err != nil {
		return nil, err
	}
	if !ed25519.Verify(pub, digest, sig) {
		return nil, ErrBadSignature
	}
	return pub, nil
}

func (r *Record) ID() string {
	canon, err := Canonical(r)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:])
}

type FileRef struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type Manifest struct {
	V            int         `json:"v"`
	KeyID        string      `json:"key_id"`
	Epoch        int64       `json:"epoch"`
	Seq          int64       `json:"seq"`
	GeneratedAt  string      `json:"generated_at"`
	ExpiresAt    string      `json:"expires_at"`
	Catalogue    FileRef     `json:"catalogue"`
	Mirrors      []string    `json:"mirrors,omitempty"`
	GeoSources   []GeoSource `json:"geo_sources,omitempty"`
	DoHAllowlist []string    `json:"doh_allowlist,omitempty"`
	RevokedKeys  []string    `json:"revoked_keys,omitempty"`
	Sig          string      `json:"sig,omitempty"`
}

func manifestDigest(m *Manifest) ([]byte, error) {
	unsigned := *m
	unsigned.Sig = ""
	canon, err := Canonical(&unsigned)
	if err != nil {
		return nil, err
	}
	return append([]byte(signingDomain), canon...), nil
}

func SignManifest(m *Manifest, id *Identity) error {
	m.V = WireVersion
	m.KeyID = id.KeyID()
	digest, err := manifestDigest(m)
	if err != nil {
		return err
	}
	m.Sig = base64.RawURLEncoding.EncodeToString(id.Sign(digest))
	return nil
}

func VerifyManifest(m *Manifest, trusted []string) error {
	if m == nil || m.V != WireVersion {
		return ErrWireVersion
	}
	sig, err := base64.RawURLEncoding.DecodeString(m.Sig)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return ErrBadSignature
	}
	digest, err := manifestDigest(m)
	if err != nil {
		return err
	}
	for _, encoded := range trusted {
		pub, err := DecodeKey(encoded)
		if err != nil {
			continue
		}
		if EncodeKey(pub) != m.KeyID {
			continue
		}
		if ed25519.Verify(pub, digest, sig) {
			return nil
		}
		return ErrBadSignature
	}
	return ErrManifestSigner
}

func (m *Manifest) Expired(now time.Time) bool {
	t, err := time.Parse(time.RFC3339, m.ExpiresAt)
	if err != nil {
		return true
	}
	return now.After(t)
}

func (m *Manifest) Newer(than *Manifest) bool {
	if than == nil {
		return true
	}
	if m.Epoch != than.Epoch {
		return m.Epoch > than.Epoch
	}
	if m.Seq != than.Seq {
		return m.Seq > than.Seq
	}
	return m.GeneratedAt > than.GeneratedAt
}

type BlobRef struct {
	SHA256   string `json:"sha256"`
	Protocol string `json:"protocol"`
	Domain   string `json:"domain,omitempty"`
	Size     int    `json:"size"`
}

type Score struct {
	Score   float64 `json:"score"`
	N       float64 `json:"n"`
	Devices int     `json:"devices"`
	Newest  string  `json:"newest,omitempty"`
}

type Scores struct {
	Global Score            `json:"global"`
	ASN    map[string]Score `json:"asn,omitempty"`
	CC     map[string]Score `json:"cc,omitempty"`
}

type CatalogueSet struct {
	ID          string                 `json:"id"`
	Version     int                    `json:"version"`
	FP          string                 `json:"fp"`
	Title       string                 `json:"title"`
	Description string                 `json:"description,omitempty"`
	Author      string                 `json:"author"`
	B4Min       string                 `json:"b4_min"`
	B4Version   string                 `json:"b4_version,omitempty"`
	Engine      string                 `json:"engine,omitempty"`
	Family      string                 `json:"family,omitempty"`
	Flags       []string               `json:"flags,omitempty"`
	Status      string                 `json:"status"`
	CreatedAt   string                 `json:"created_at"`
	UpdatedAt   string                 `json:"updated_at"`
	Geo         *GeoSource             `json:"geo,omitempty"`
	Set         map[string]interface{} `json:"set"`
	Payloads    []BlobRef              `json:"payloads,omitempty"`
	Scores      Scores                 `json:"scores"`
	DerivedFrom *Origin                `json:"derived_from,omitempty"`
}

type Catalogue struct {
	Epoch       int64             `json:"epoch"`
	Seq         int64             `json:"seq"`
	GeneratedAt string            `json:"generated_at"`
	Sets        []CatalogueSet    `json:"sets"`
	Blobs       []BlobRef         `json:"blobs,omitempty"`
	ASNNames    map[string]string `json:"asn_names,omitempty"`
}

func (c *CatalogueSet) ToEnvelope() *Envelope {
	env := &Envelope{
		Format:      Format,
		B4Version:   c.B4Version,
		MinB4:       c.B4Min,
		Title:       c.Title,
		Description: c.Description,
		Engine:      c.Engine,
		Geo:         c.Geo,
		Set:         c.Set,
		Fingerprint: c.FP,
		DerivedFrom: &Origin{ID: c.ID, Version: c.Version},
	}
	return env
}

const (
	BucketASN     = "asn"
	BucketCountry = "country"
	BucketGlobal  = "global"
	BucketNone    = "none"
)

type Displayed struct {
	Bucket  string  `json:"bucket"`
	Score   float64 `json:"score"`
	N       float64 `json:"n"`
	Devices int     `json:"devices"`
	Newest  string  `json:"newest,omitempty"`
}

func usable(s Score) bool {
	return s.N >= 1.0 && s.Devices >= 2
}

func PickScore(scores Scores, asn, cc string) Displayed {
	if asn != "" {
		if s, ok := scores.ASN[asn]; ok && usable(s) {
			return Displayed{Bucket: BucketASN, Score: s.Score, N: s.N, Devices: s.Devices, Newest: s.Newest}
		}
	}
	if cc != "" {
		if s, ok := scores.CC[strings.ToUpper(cc)]; ok && usable(s) {
			return Displayed{Bucket: BucketCountry, Score: s.Score, N: s.N, Devices: s.Devices, Newest: s.Newest}
		}
	}
	if usable(scores.Global) {
		s := scores.Global
		return Displayed{Bucket: BucketGlobal, Score: s.Score, N: s.N, Devices: s.Devices, Newest: s.Newest}
	}
	return Displayed{Bucket: BucketNone, Score: scores.Global.Score, N: scores.Global.N, Devices: scores.Global.Devices, Newest: scores.Global.Newest}
}

func BlobHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func CatalogueFileName(epoch, seq int64) string {
	return fmt.Sprintf("catalogue-%d-%d.json.gz", epoch, seq)
}
