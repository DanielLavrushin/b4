package hubwire

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func TestEverySetFieldIsClassified(t *testing.T) {
	for _, path := range LeafPaths() {
		if _, ok := classOf(path); !ok {
			t.Errorf("SetConfig field %q is not classified in hubwire.Fields; add it as Crosses (with Since) or Never", path)
		}
	}
	for path, f := range Fields {
		known, ok := setPaths[path]
		if !ok {
			t.Errorf("hubwire.Fields names %q, which SetConfig no longer has", path)
			continue
		}
		if f.Class == Crosses && !known.leaf {
			t.Errorf("hubwire.Fields marks %q as Crosses, but it is a struct; only leaves may cross", path)
		}
	}
}

func sampleSet() config.SetConfig {
	set := config.NewSetConfig()
	set.Id = "abc"
	set.Name = "YouTube"
	set.Targets.SNIDomains = []string{"youtube.com", "googlevideo.com"}
	set.Targets.GeoSiteCategories = []string{"youtube"}
	set.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:ff"}
	set.Fragmentation.Strategy = "tls"
	set.Faking.SNI = true
	set.Faking.TTL = 7
	set.Faking.Strategy = "ttl"
	set.TCP.DPortFilter = "443"
	set.Routing.Enabled = true
	set.Routing.Mode = config.RoutingModeProxy
	set.Routing.Upstream.Host = "10.0.0.5"
	set.Routing.Upstream.Port = 1080
	set.Routing.Upstream.Username = "user"
	set.Routing.Upstream.Password = "secret"
	set.Escalate.To = "other-set"
	set.DNS.Enabled = true
	set.DNS.TargetDNS = "192.168.1.1:53"
	set.DNS.DoHURL = "https://dns.google/dns-query"
	set.DNS.Pins = map[string][]string{
		"youtube.com":  {"142.250.1.1"},
		"private.home": {"10.1.1.1"},
		"example.org":  {"93.184.216.34"},
	}
	return set
}

func TestScrubStripsPrivateFields(t *testing.T) {
	set := sampleSet()
	projection, report, err := Scrub(&set)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(projection)
	text := string(raw)
	for _, forbidden := range []string{"secret", "10.0.0.5", "aa:bb:cc", "other-set", "192.168.1.1", "\"routing\"", "\"escalate\"", "\"id\"", "private.home", "example.org"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("projection still carries %q: %s", forbidden, text)
		}
	}
	if !strings.Contains(text, "142.250.1.1") {
		t.Errorf("pin for a targeted domain with a public address must survive: %s", text)
	}
	if !strings.Contains(text, "dns.google") {
		t.Errorf("doh_url must cross: %s", text)
	}
	stripped := map[string]string{}
	for _, s := range report.Stripped {
		stripped[s.Path] = s.Reason
	}
	for path, reason := range map[string]string{
		"routing":                "routing_not_shareable",
		"escalate":               "private",
		"targets.source_devices": "private",
		"dns.target_dns":         "private",
		"dns.pins.private.home":  "pin_not_targeted",
		"dns.pins.example.org":   "pin_not_targeted",
	} {
		if stripped[path] != reason {
			t.Errorf("expected %q stripped as %q, got %q (all: %+v)", path, reason, stripped[path], report.Stripped)
		}
	}
	for _, s := range report.Stripped {
		if s.Path == "id" || s.Path == "enabled" {
			t.Errorf("%q must be stripped silently", s.Path)
		}
	}
}

func TestScrubPinPrivateAddress(t *testing.T) {
	set := config.NewSetConfig()
	set.Name = "x"
	set.Targets.SNIDomains = []string{"example.com"}
	set.DNS.Pins = map[string][]string{"example.com": {"192.168.0.10"}}
	projection, report, err := Scrub(&set)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := lookupPath(projection, "dns.pins"); ok {
		t.Errorf("pin with a private address must be dropped")
	}
	if len(report.Stripped) != 1 || report.Stripped[0].Reason != "pin_private_address" {
		t.Errorf("unexpected report %+v", report.Stripped)
	}
}

func TestScrubKeepsBlockRouting(t *testing.T) {
	set := config.NewSetConfig()
	set.Name = "ads"
	set.Targets.GeoSiteCategories = []string{"category-ads-all"}
	set.Routing.Enabled = true
	set.Routing.Mode = config.RoutingModeBlock
	set.Routing.BlockAction = config.BlockActionDrop
	set.Routing.EgressInterface = "wg0"
	projection, report, err := Scrub(&set)
	if err != nil {
		t.Fatal(err)
	}
	routing, ok := projection["routing"].(map[string]interface{})
	if !ok {
		t.Fatalf("block routing must cross: %v", projection)
	}
	if routing["mode"] != "block" || routing["block_action"] != "drop" || routing["enabled"] != true {
		t.Errorf("unexpected routing projection %v", routing)
	}
	if _, ok := routing["egress_interface"]; ok {
		t.Errorf("egress_interface must not cross")
	}
	for _, s := range report.Stripped {
		if s.Path == "routing" {
			t.Errorf("a block set must not report routing as stripped")
		}
	}
}

func TestScrubResetsDisabledBlocks(t *testing.T) {
	set := config.NewSetConfig()
	set.Name = "x"
	set.Targets.SNIDomains = []string{"example.com"}
	set.Faking.SNI = false
	set.Faking.TTL = 9
	set.Faking.PayloadDomain = "leftover.example"
	projection, _, err := Scrub(&set)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := lookupPath(projection, "faking.ttl"); ok {
		t.Errorf("a disabled faking block must not carry its tuning: %v", projection["faking"])
	}
}

func TestScrubWarnings(t *testing.T) {
	set := config.NewSetConfig()
	set.Name = "x"
	set.Targets.SNIDomains = []string{"router.lan", "nas", "10.0.0.1", "ok.example.com"}
	_, report, err := Scrub(&set)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, w := range report.Warnings {
		if w.Code == "private_domains" {
			found = true
			domains, _ := w.Params["domains"].([]string)
			if len(domains) != 3 {
				t.Errorf("expected 3 private-looking domains, got %v", domains)
			}
		}
	}
	if !found {
		t.Errorf("expected a private_domains warning, got %+v", report.Warnings)
	}

	empty := config.NewSetConfig()
	empty.Name = "empty"
	_, report, _ = Scrub(&empty)
	found = false
	for _, w := range report.Warnings {
		if w.Code == "no_targets" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a no_targets warning, got %+v", report.Warnings)
	}
}

func TestFingerprintIgnoresTargetsAndPorts(t *testing.T) {
	a := sampleSet()
	b := sampleSet()
	b.Name = "other"
	b.Targets.SNIDomains = []string{"instagram.com"}
	b.TCP.DPortFilter = "443,8443"
	b.DNS.Pins = nil
	pa, _, _ := Scrub(&a)
	pb, _, _ := Scrub(&b)
	if Fingerprint(pa) != Fingerprint(pb) {
		t.Errorf("fingerprint must not depend on targets, name, ports or pins")
	}
	b.Faking.TTL = 8
	pb, _, _ = Scrub(&b)
	if Fingerprint(pa) == Fingerprint(pb) {
		t.Errorf("fingerprint must change with the strategy")
	}
}

func TestBuildOpenRoundTripWithPayload(t *testing.T) {
	set := sampleSet()
	set.Faking.SNIType = config.FakePayloadCapture
	set.Faking.PayloadFile = "captures/tls_www_google_com.bin"
	set.UDP.Mode = config.UDPModeFake
	set.UDP.FakePayloadFile = "captures/quic_example.bin"
	read := func(name string) ([]byte, error) {
		switch name {
		case "captures/tls_www_google_com.bin":
			return config.FakeSNI1, nil
		case "captures/quic_example.bin":
			return config.FakeQUIC1, nil
		}
		return nil, errNotFound
	}
	env, report, err := Build(&set, BuildOptions{B4Version: "1.83.0", Engine: "nfqueue", GeoSiteURL: "https://example/geosite.dat", ReadPayload: read})
	if err != nil {
		t.Fatal(err)
	}
	if len(env.Payloads) != 2 {
		t.Fatalf("expected two payload attachments, got %d (warnings %+v)", len(env.Payloads), report.Warnings)
	}
	if env.Payloads[0].Domain != "www.google.com" || env.Payloads[0].Protocol != ProtocolTLS {
		t.Errorf("unexpected tls payload %+v", env.Payloads[0])
	}
	if env.Payloads[1].Protocol != ProtocolQUIC || env.Payloads[1].Domain == "" {
		t.Errorf("unexpected quic payload %+v", env.Payloads[1])
	}
	ref, _ := lookupPath(env.Set, "faking.payload_file")
	if s, _ := ref.(string); !strings.HasPrefix(s, RefPrefix) {
		t.Errorf("payload_file must be a content reference, got %v", ref)
	}
	if env.MinB4 != BaselineVersion {
		t.Errorf("expected baseline min version, got %s", env.MinB4)
	}

	raw, _ := json.Marshal(env)
	var decoded Envelope
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	imp, err := Open(&decoded, OpenOptions{B4Version: "1.83.0", Now: func() time.Time { return fixed }})
	if err != nil {
		t.Fatal(err)
	}
	if imp.Fingerprint != env.Fingerprint {
		t.Errorf("fingerprint drifted across the wire: %s vs %s", imp.Fingerprint, env.Fingerprint)
	}
	if len(imp.Payloads) != 2 {
		t.Errorf("expected two payloads on import, got %d", len(imp.Payloads))
	}
	if imp.Set.Faking.PayloadFile != RefPrefix+env.Payloads[0].SHA256 {
		t.Errorf("payload reference must survive until the caller installs the file, got %q", imp.Set.Faking.PayloadFile)
	}
	if imp.Set.Hub == nil || imp.Set.Hub.Hash != env.Fingerprint || imp.Set.Hub.AppliedAt != "2026-09-12T12:00:00Z" {
		t.Errorf("provenance not stamped: %+v", imp.Set.Hub)
	}
	if imp.Set.Routing.Enabled || imp.Set.Routing.Upstream.Password != "" || imp.Set.Escalate.To != "" || len(imp.Set.Targets.SourceDevices) != 0 {
		t.Errorf("private fields leaked through import: %+v", imp.Set)
	}
	if imp.Set.Name != "YouTube" || imp.Set.Id != "" || !imp.Set.Enabled {
		t.Errorf("unexpected identity on the imported set: %q %q %v", imp.Set.Name, imp.Set.Id, imp.Set.Enabled)
	}
	if imp.Set.Faking.TTL != 7 || imp.Set.Fragmentation.Strategy != "tls" {
		t.Errorf("strategy lost on import: %+v", imp.Set.Faking)
	}
	for _, w := range imp.Warnings {
		if w.Code == "values_changed" || w.Code == "fingerprint_mismatch" || w.Code == "unknown_fields" {
			t.Errorf("unexpected warning on a clean round trip: %+v", w)
		}
	}
}

type notFoundError struct{}

func (notFoundError) Error() string { return "not found" }

var errNotFound = notFoundError{}

func TestBuildWarnsOnMissingPayload(t *testing.T) {
	set := sampleSet()
	set.Faking.SNIType = config.FakePayloadCapture
	set.Faking.PayloadFile = "captures/gone.bin"
	env, report, err := Build(&set, BuildOptions{ReadPayload: func(string) ([]byte, error) { return nil, errNotFound }})
	if err != nil {
		t.Fatal(err)
	}
	if len(env.Payloads) != 0 {
		t.Errorf("no payload should be attached")
	}
	if _, ok := lookupPath(env.Set, "faking.payload_file"); ok {
		t.Errorf("a missing payload reference must not cross")
	}
	found := false
	for _, w := range report.Warnings {
		if w.Code == "payload_missing" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected payload_missing warning, got %+v", report.Warnings)
	}
}

func TestOpenRefusesForeignAndUnknownFields(t *testing.T) {
	env := &Envelope{
		Format: Format,
		Title:  "crafted",
		Set: map[string]interface{}{
			"targets": map[string]interface{}{"sni_domains": []interface{}{"example.com"}},
			"routing": map[string]interface{}{
				"enabled":  true,
				"mode":     "proxy",
				"upstream": map[string]interface{}{"host": "evil.example", "port": float64(1080)},
			},
			"escalate": map[string]interface{}{"to": "x"},
			"dns":      map[string]interface{}{"target_dns": "1.2.3.4:53", "enabled": true},
			"faking":   map[string]interface{}{"sni": true, "future_knob": float64(3)},
			"nonsense": "yes",
		},
	}
	imp, err := Open(env, OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if imp.Set.Routing.Enabled || imp.Set.Routing.Upstream.Host != "" || imp.Set.Escalate.To != "" || imp.Set.DNS.TargetDNS != "" {
		t.Errorf("foreign fields must be dropped: %+v", imp.Set.Routing)
	}
	var unknown []string
	for _, w := range imp.Warnings {
		if w.Code == "unknown_fields" {
			unknown, _ = w.Params["paths"].([]string)
		}
	}
	if len(unknown) != 2 || unknown[0] != "faking.future_knob" || unknown[1] != "nonsense" {
		t.Errorf("expected the two unknown paths, got %v", unknown)
	}
}

func TestOpenWarnsOnOldVersionAndChangedValues(t *testing.T) {
	env := &Envelope{
		Format: Format,
		MinB4:  "9.9.0",
		Title:  "future",
		Set: map[string]interface{}{
			"targets": map[string]interface{}{"sni_domains": []interface{}{"example.com"}},
			"udp":     map[string]interface{}{"mode": "teleport"},
		},
	}
	imp, err := Open(env, OpenOptions{B4Version: "1.83.0"})
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	for _, w := range imp.Warnings {
		codes[w.Code] = true
	}
	if !codes["version_too_old"] {
		t.Errorf("expected version_too_old, got %+v", imp.Warnings)
	}
	if !codes["values_changed"] {
		t.Errorf("expected values_changed for the unknown udp mode, got %+v", imp.Warnings)
	}
	if imp.Set.UDP.Mode == "teleport" {
		t.Errorf("unknown enum value must be normalised")
	}
}

func TestOpenRejectsTamperedPayload(t *testing.T) {
	env := &Envelope{
		Format: Format,
		Title:  "x",
		Set:    map[string]interface{}{"targets": map[string]interface{}{"sni_domains": []interface{}{"example.com"}}},
		Payloads: []Payload{{
			SHA256:   strings.Repeat("0", 64),
			Protocol: ProtocolTLS,
			Data:     config.FakeSNI1,
		}},
	}
	if _, err := Open(env, OpenOptions{}); err == nil {
		t.Errorf("a payload whose hash does not match its data must be refused")
	}
	env.Payloads[0].SHA256 = ""
	env.Payloads[0].Data = []byte("not a client hello")
	if _, err := Open(env, OpenOptions{}); err == nil {
		t.Errorf("a payload that is not a ClientHello must be refused")
	}
}

func TestOpenRejectsOtherFormats(t *testing.T) {
	if _, err := Open(&Envelope{Format: 2, Set: map[string]interface{}{}}, OpenOptions{}); err != ErrUnsupportedFormat {
		t.Errorf("expected ErrUnsupportedFormat, got %v", err)
	}
	if _, err := Open(&Envelope{Format: Format}, OpenOptions{}); err != ErrNoSet {
		t.Errorf("expected ErrNoSet, got %v", err)
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.83.0", "1.83.0", 0},
		{"1.82.0", "1.83.0", -1},
		{"1.90.1", "1.83.0", 1},
		{"v1.83.0", "1.83.0", 0},
		{"1.70.0rc7", "1.70.0", 0},
		{"dev", "1.83.0", 1},
		{"1.83.0", "dev", -1},
		{"2.0", "1.99.99", 1},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestMinVersionFollowsSinceTable(t *testing.T) {
	orig := Fields["faking.fake_len_mode"]
	Fields["faking.fake_len_mode"] = Field{Class: Crosses, Since: "1.90.0"}
	defer func() { Fields["faking.fake_len_mode"] = orig }()

	set := config.NewSetConfig()
	set.Name = "x"
	set.Targets.SNIDomains = []string{"example.com"}
	set.Faking.SNI = true
	projection, _, _ := Scrub(&set)
	if got := MinVersion(projection); got != BaselineVersion {
		t.Errorf("a set that does not use the new field stays at the baseline, got %s", got)
	}
	set.Faking.FakeLenMode = "match"
	projection, _, _ = Scrub(&set)
	if got := MinVersion(projection); got != "1.90.0" {
		t.Errorf("a set that uses the new field needs 1.90.0, got %s", got)
	}
}

func TestIsEnvelope(t *testing.T) {
	if IsEnvelope(map[string]interface{}{"name": "x", "tcp": map[string]interface{}{}}) {
		t.Errorf("a plain export is not an envelope")
	}
	if !IsEnvelope(map[string]interface{}{"format": float64(1), "set": map[string]interface{}{}}) {
		t.Errorf("format plus set is an envelope")
	}
}

func TestOpenAppliesPinPolicy(t *testing.T) {
	env := &Envelope{
		Format: Format,
		Title:  "pins",
		Set: map[string]interface{}{
			"targets": map[string]interface{}{"sni_domains": []interface{}{"example.com"}},
			"dns": map[string]interface{}{
				"enabled": true,
				"doh_url": "https://dns.google/dns-query",
				"pins": map[string]interface{}{
					"bank.com":        []interface{}{"93.184.216.34"},
					"example.com":     []interface{}{"127.0.0.1"},
					"www.example.com": []interface{}{"93.184.216.34"},
				},
			},
		},
	}
	imp, err := Open(env, OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(imp.Set.DNS.Pins) != 1 || len(imp.Set.DNS.Pins["www.example.com"]) != 1 {
		t.Fatalf("only the targeted public pin may survive, got %v", imp.Set.DNS.Pins)
	}
	codes := map[string]string{}
	for _, w := range imp.Warnings {
		if d, ok := w.Params["domain"].(string); ok {
			codes[d] = w.Code
		}
	}
	if codes["bank.com"] != "pin_not_targeted" || codes["example.com"] != "pin_private_address" {
		t.Errorf("expected pin warnings per domain, got %+v", imp.Warnings)
	}
	if imp.Set.Hub == nil || imp.Set.Hub.Hash != imp.Fingerprint {
		t.Fatalf("hub hash must be the imported fingerprint")
	}
	live, err := LiveFingerprint(&imp.Set, nil)
	if err != nil || live != imp.Set.Hub.Hash {
		t.Errorf("a freshly imported set must read as unmodified: live %s stamped %s (%v)", live, imp.Set.Hub.Hash, err)
	}
}

func TestOpenDropsUnknownDoH(t *testing.T) {
	env := &Envelope{
		Format: Format,
		Title:  "doh",
		Set: map[string]interface{}{
			"targets": map[string]interface{}{"sni_domains": []interface{}{"example.com"}},
			"dns": map[string]interface{}{
				"enabled": true,
				"doh_url": "https://attacker.example/dns-query",
				"pins":    map[string]interface{}{"example.com": []interface{}{"93.184.216.34"}},
			},
		},
	}
	imp, err := Open(env, OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if imp.Set.DNS.Enabled || imp.Set.DNS.DoHURL != "" || len(imp.Set.DNS.Pins) != 0 {
		t.Errorf("an unknown DoH host must drop the whole dns block, got %+v", imp.Set.DNS)
	}
	found := false
	for _, w := range imp.Warnings {
		if w.Code == "dns_dropped" && w.Params["host"] == "attacker.example" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected dns_dropped warning, got %+v", imp.Warnings)
	}

	set := config.NewSetConfig()
	set.Name = "share"
	set.Targets.SNIDomains = []string{"example.com"}
	set.DNS.Enabled = true
	set.DNS.DoHURL = "https://attacker.example/dns-query"
	_, report, _ := Scrub(&set)
	found = false
	for _, w := range report.Warnings {
		if w.Code == "doh_unknown" {
			found = true
		}
	}
	if !found {
		t.Errorf("sharing a set with an unknown DoH host must warn, got %+v", report.Warnings)
	}
}

func TestBuildDropsPayloadPathsThatCannotBeUsed(t *testing.T) {
	set := config.NewSetConfig()
	set.Name = "x"
	set.Targets.SNIDomains = []string{"example.com"}
	set.Faking.SNI = true
	set.Faking.SNIType = config.FakePayloadDomain
	set.Faking.PayloadFile = "captures/tls_leftover.bin"
	set.UDP.Mode = config.UDPModeDrop
	set.UDP.FakePayloadFile = "captures/quic_leftover.bin"
	reads := 0
	env, _, err := Build(&set, BuildOptions{ReadPayload: func(string) ([]byte, error) { reads++; return config.FakeSNI1, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if reads != 0 || len(env.Payloads) != 0 {
		t.Errorf("a payload path the strategy does not use must not be read or attached")
	}
	for _, path := range []string{"faking.payload_file", "udp.fake_payload_file"} {
		if _, ok := lookupPath(env.Set, path); ok {
			t.Errorf("%s must not cross when the strategy does not use it", path)
		}
	}

	off := config.NewSetConfig()
	off.Name = "off"
	off.Targets.SNIDomains = []string{"example.com"}
	off.Faking.SNI = false
	off.Faking.SNIType = config.FakePayloadCapture
	off.Faking.PayloadFile = "captures/tls_www_google_com.bin"
	env, _, err = Build(&off, BuildOptions{ReadPayload: func(string) ([]byte, error) { return config.FakeSNI1, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if len(env.Payloads) != 0 {
		t.Errorf("a disabled faking block must not attach its payload")
	}
}

func TestOpenReportsPrivateAddressesAndBlockRegexp(t *testing.T) {
	env := &Envelope{
		Format: Format,
		Title:  "block",
		Set: map[string]interface{}{
			"targets": map[string]interface{}{
				"sni_domains": []interface{}{"regexp:.*"},
				"ip":          []interface{}{"10.0.0.0/8", "93.184.216.34"},
			},
			"routing": map[string]interface{}{"enabled": true, "mode": "block"},
		},
	}
	imp, err := Open(env, OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	for _, w := range imp.Warnings {
		codes[w.Code] = true
	}
	if !codes["private_addresses"] || !codes["block_regexp"] {
		t.Errorf("expected private_addresses and block_regexp, got %+v", imp.Warnings)
	}
	if !imp.Set.Routing.Enabled || imp.Set.Routing.Mode != config.RoutingModeBlock {
		t.Errorf("block routing must survive import: %+v", imp.Set.Routing)
	}
}
