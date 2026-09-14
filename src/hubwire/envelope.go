package hubwire

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sni"
)

const Format = 1

const RefPrefix = "sha256:"

const (
	ProtocolTLS  = "tls"
	ProtocolQUIC = "quic"
)

var (
	ErrUnsupportedFormat = errors.New("unsupported envelope format")
	ErrNoSet             = errors.New("envelope carries no set")
)

type Origin struct {
	ID      string `json:"id"`
	Version int    `json:"version,omitempty"`
}

type GeoSource struct {
	SiteURL string `json:"site_url,omitempty"`
	IPURL   string `json:"ip_url,omitempty"`
}

type Payload struct {
	SHA256   string `json:"sha256"`
	Protocol string `json:"protocol"`
	Domain   string `json:"domain,omitempty"`
	Size     int    `json:"size"`
	Data     []byte `json:"data"`
}

type Envelope struct {
	Format      int                    `json:"format"`
	B4Version   string                 `json:"b4_version"`
	MinB4       string                 `json:"min_b4_version"`
	Title       string                 `json:"title"`
	Description string                 `json:"description,omitempty"`
	Engine      string                 `json:"engine,omitempty"`
	Geo         *GeoSource             `json:"geo,omitempty"`
	Set         map[string]interface{} `json:"set"`
	Payloads    []Payload              `json:"payloads,omitempty"`
	Fingerprint string                 `json:"fingerprint"`
	DerivedFrom *Origin                `json:"derived_from,omitempty"`
}

type BuildOptions struct {
	B4Version   string
	Engine      string
	GeoSiteURL  string
	GeoIPURL    string
	Description string
	ReadPayload func(name string) ([]byte, error)
}

type PayloadError struct {
	Index  int
	Reason string
}

func (e *PayloadError) Error() string {
	return fmt.Sprintf("payload %d: %s", e.Index, e.Reason)
}

func IsEnvelope(raw map[string]interface{}) bool {
	if raw == nil {
		return false
	}
	_, hasFormat := raw["format"]
	_, hasSet := raw["set"]
	return hasFormat && hasSet
}

type payloadSlot struct {
	path     string
	protocol string
	wanted   func(set *config.SetConfig) bool
}

var payloadSlots = []payloadSlot{
	{path: "faking.payload_file", protocol: ProtocolTLS, wanted: func(set *config.SetConfig) bool {
		return set.Faking.SNIType == config.FakePayloadCapture
	}},
	{path: "udp.fake_payload_file", protocol: ProtocolQUIC, wanted: func(set *config.SetConfig) bool {
		return set.UDP.Mode == config.UDPModeFake
	}},
}

func Build(set *config.SetConfig, opts BuildOptions) (*Envelope, *Report, error) {
	projection, report, err := Scrub(set)
	if err != nil {
		return nil, nil, err
	}
	env := &Envelope{
		Format:      Format,
		B4Version:   opts.B4Version,
		Title:       set.Name,
		Description: opts.Description,
		Engine:      opts.Engine,
		Set:         projection,
	}
	if opts.GeoSiteURL != "" || opts.GeoIPURL != "" {
		env.Geo = &GeoSource{SiteURL: opts.GeoSiteURL, IPURL: opts.GeoIPURL}
	}
	effective := *set
	config.ApplySetDefaults(&effective)
	for _, slot := range payloadSlots {
		raw, ok := lookupPath(projection, slot.path)
		if !ok {
			continue
		}
		file, _ := raw.(string)
		if file == "" || strings.HasPrefix(file, "@") {
			continue
		}
		if !slot.wanted(&effective) {
			deletePath(projection, slot.path)
			continue
		}
		attachPayload(env, projection, report, slot.path, file, slot.protocol, opts.ReadPayload)
	}
	env.Fingerprint = Fingerprint(projection)
	env.MinB4 = MinVersion(projection)
	if set.Hub != nil && set.Hub.ID != "" {
		env.DerivedFrom = &Origin{ID: set.Hub.ID, Version: set.Hub.Version}
	}
	return env, report.sorted(), nil
}

func attachPayload(env *Envelope, projection map[string]interface{}, report *Report, path, file, protocol string, read func(string) ([]byte, error)) {
	if read == nil {
		report.warn("payload_missing", map[string]interface{}{"file": file, "path": path})
		deletePath(projection, path)
		return
	}
	data, err := read(file)
	if err != nil {
		report.warn("payload_missing", map[string]interface{}{"file": file, "path": path})
		deletePath(projection, path)
		return
	}
	p, err := validatePayload(data, protocol)
	if err != nil {
		report.warn("payload_invalid", map[string]interface{}{"file": file, "path": path, "reason": err.Error()})
		deletePath(projection, path)
		return
	}
	found := false
	for _, existing := range env.Payloads {
		if existing.SHA256 == p.SHA256 {
			found = true
			break
		}
	}
	if !found {
		env.Payloads = append(env.Payloads, p)
	}
	setPath(projection, path, RefPrefix+p.SHA256)
}

func validatePayload(data []byte, protocol string) (Payload, error) {
	if len(data) == 0 {
		return Payload{}, errors.New("empty payload")
	}
	if len(data) > MaxPayloadBytes {
		return Payload{}, fmt.Errorf("payload is %d bytes, the limit is %d", len(data), MaxPayloadBytes)
	}
	var domain string
	switch protocol {
	case ProtocolTLS:
		name, _, ok := sni.ParseTLSClientHelloSNI(data)
		if !ok {
			return Payload{}, errors.New("not a TLS ClientHello with a server name")
		}
		domain = name
	case ProtocolQUIC:
		name, ok := sni.ParseQUICClientHelloSNI(data)
		if !ok {
			return Payload{}, errors.New("not a QUIC Initial with a readable ClientHello")
		}
		domain = name
	default:
		return Payload{}, fmt.Errorf("unknown payload protocol %q", protocol)
	}
	sum := sha256.Sum256(data)
	return Payload{
		SHA256:   hex.EncodeToString(sum[:]),
		Protocol: protocol,
		Domain:   domain,
		Size:     len(data),
		Data:     data,
	}, nil
}

type OpenOptions struct {
	B4Version string
	Now       func() time.Time
}

type Imported struct {
	Set         config.SetConfig
	Payloads    []Payload
	Warnings    []Warning
	Fingerprint string
}

const (
	validationGeoPath = "/dev/null"
	validationSetID   = "hub-import"
)

func decodeSet(m map[string]interface{}, title string) (config.SetConfig, error) {
	set := config.NewSetConfig()
	data, err := json.Marshal(m)
	if err != nil {
		return set, err
	}
	if err := json.Unmarshal(data, &set); err != nil {
		return set, fmt.Errorf("set does not decode: %w", err)
	}
	set.Id = ""
	set.Enabled = true
	set.Hub = nil
	if strings.TrimSpace(title) != "" {
		set.Name = strings.TrimSpace(title)
	}
	if set.Name == "" && len(set.Targets.SNIDomains) > 0 {
		set.Name = set.Targets.SNIDomains[0]
	}
	config.ApplySetDefaults(&set)
	return set, nil
}

func normaliseSet(set *config.SetConfig) error {
	cfg := config.NewConfig()
	cfg.System.Geo.GeoSitePath = validationGeoPath
	cfg.System.Geo.GeoIpPath = validationGeoPath
	set.Id = validationSetID
	cfg.Sets = []*config.SetConfig{set}
	err := cfg.Validate()
	set.Id = ""
	if err != nil {
		return fmt.Errorf("set is invalid: %w", err)
	}
	return nil
}

func Open(env *Envelope, opts OpenOptions) (*Imported, error) {
	if env == nil || env.Format != Format {
		return nil, ErrUnsupportedFormat
	}
	if env.Set == nil {
		return nil, ErrNoSet
	}
	m, _ := deepCopy(env.Set).(map[string]interface{})
	imp := &Imported{Warnings: []Warning{}}
	warn := func(code string, params map[string]interface{}) {
		imp.Warnings = append(imp.Warnings, Warning{Code: code, Params: params})
	}

	if unknown := unknownPaths(m); len(unknown) > 0 {
		warn("unknown_fields", map[string]interface{}{"paths": unknown})
		for _, p := range unknown {
			deletePath(m, p)
		}
	}
	stripForeign(m)
	dropUnknownDoH(m, warn)

	byHash := make(map[string]Payload, len(env.Payloads))
	for i, p := range env.Payloads {
		v, err := validatePayload(p.Data, p.Protocol)
		if err != nil {
			return nil, &PayloadError{Index: i, Reason: err.Error()}
		}
		if p.SHA256 != "" && !strings.EqualFold(p.SHA256, v.SHA256) {
			return nil, &PayloadError{Index: i, Reason: "sha256 does not match the data"}
		}
		byHash[v.SHA256] = v
	}

	if env.Fingerprint != "" && !strings.EqualFold(env.Fingerprint, Fingerprint(m)) {
		warn("fingerprint_mismatch", nil)
	}
	if opts.B4Version != "" && env.MinB4 != "" && CompareVersions(opts.B4Version, env.MinB4) < 0 {
		warn("version_too_old", map[string]interface{}{"min": env.MinB4, "running": opts.B4Version})
	}

	set, err := decodeSet(m, env.Title)
	if err != nil {
		return nil, err
	}
	if err := normaliseSet(&set); err != nil {
		return nil, err
	}

	normMap, err := config.SetToMap(&set)
	if err != nil {
		return nil, err
	}
	defSet := config.NewSetConfig()
	defMap, err := config.SetToMap(&defSet)
	if err != nil {
		return nil, err
	}
	changed := make([]map[string]interface{}, 0)
	walkLeaves(m, "", func(path string, v interface{}) {
		nv, ok := lookupPath(normMap, path)
		if !ok {
			nv, _ = lookupPath(defMap, path)
		}
		if !reflect.DeepEqual(v, nv) {
			changed = append(changed, map[string]interface{}{"path": path, "from": v, "to": nv})
		}
	})
	if len(changed) > 0 {
		warn("values_changed", map[string]interface{}{"fields": changed})
	}

	projection, report, err := Scrub(&set)
	if err != nil {
		return nil, err
	}
	for _, s := range report.Stripped {
		if strings.HasPrefix(s.Path, "dns.pins.") {
			warn(s.Reason, map[string]interface{}{"path": s.Path, "domain": strings.TrimPrefix(s.Path, "dns.pins.")})
		}
	}
	imp.Warnings = append(imp.Warnings, report.Warnings...)

	for _, slot := range payloadSlots {
		raw, ok := lookupPath(projection, slot.path)
		if !ok {
			continue
		}
		s, _ := raw.(string)
		if !strings.HasPrefix(s, RefPrefix) {
			continue
		}
		hash := strings.ToLower(strings.TrimPrefix(s, RefPrefix))
		p, ok := byHash[hash]
		if !ok {
			warn("payload_missing", map[string]interface{}{"path": slot.path})
			deletePath(projection, slot.path)
			continue
		}
		imp.Payloads = append(imp.Payloads, p)
	}

	set, err = decodeSet(projection, set.Name)
	if err != nil {
		return nil, err
	}

	imp.Fingerprint = Fingerprint(projection)
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	set.Hub = &config.HubOrigin{Hash: imp.Fingerprint, AppliedAt: now().UTC().Format(time.RFC3339)}
	if env.DerivedFrom != nil && env.DerivedFrom.ID != "" {
		set.Hub.ID = env.DerivedFrom.ID
		set.Hub.Version = env.DerivedFrom.Version
	}
	imp.Set = set
	return imp, nil
}

func LiveFingerprint(set *config.SetConfig, read func(string) ([]byte, error)) (string, error) {
	env, _, err := Build(set, BuildOptions{ReadPayload: read})
	if err != nil {
		return "", err
	}
	return env.Fingerprint, nil
}
