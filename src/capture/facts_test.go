package capture

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/tlsgen"
)

func legacyGeneratedHello(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "legacy-generated-"+name+".bin"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func generatedHello(t *testing.T, name string) []byte {
	t.Helper()
	data, err := tlsgen.GenerateTLSClientHello(name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func factsSummary(f *Facts) string {
	if f == nil {
		return "<nil>"
	}
	out, _ := json.Marshal(f)
	return string(out)
}

func extensionCount(f *Facts) int {
	if f == nil || f.Extensions == nil {
		return -1
	}
	return *f.Extensions
}

func carriesECH(f *Facts) bool {
	return f != nil && f.ECH != nil && *f.ECH
}

var legacyGeneratedNames = []string{"www.google.com", "max.ru", "www.example.com"}

func TestTheOldGeneratorsHellosShowItsThreeMalformedFields(t *testing.T) {
	want := []string{"delegated_credentials", "key_share", "ech"}
	for _, name := range legacyGeneratedNames {
		f := FactsOf("tls", legacyGeneratedHello(t, name))
		if f.Kind != KindTLSClientHello || f.SNI != name || extensionCount(f) != 15 || !carriesECH(f) || !f.FitsOnePacket {
			t.Errorf("%s: facts %s, want a TLS ClientHello for %s with 15 extensions, ECH, fitting one packet", name, factsSummary(f), name)
		}
		if !reflect.DeepEqual(f.Faults, want) {
			t.Errorf("%s: faults %q, want %q", name, f.Faults, want)
		}
	}
}

func TestTheGeneratorsHellosHaveNoMalformedFields(t *testing.T) {
	for _, name := range append([]string{"vk.com", "a-very-long-label-for-the-generator.example.org"}, legacyGeneratedNames...) {
		f := FactsOf("tls", generatedHello(t, name))
		if f.Kind != KindTLSClientHello || f.SNI != name || extensionCount(f) != 15 || !carriesECH(f) || len(f.Faults) != 0 {
			t.Errorf("%s: facts %s, want a well-formed TLS ClientHello for %s with 15 extensions and ECH", name, factsSummary(f), name)
		}
	}
}

func TestFactsOfTheBuiltInPayloads(t *testing.T) {
	cases := []struct {
		name     string
		protocol string
		data     []byte
		kind     string
		version  int
		sni      string
	}{
		{"FakeSNI1", "tls", config.FakeSNI1, KindTLSClientHello, 0, "www.google.com"},
		{"FakeSNI2", "tls", config.FakeSNI2, KindTLSClientHello, 0, "vk.com"},
		{"FakeQUIC1", "quic", config.FakeQUIC1, KindQUICInitial, 1, "www.google.com"},
		{"FakeQUIC2", "quic", config.FakeQUIC2, KindQUICInitial, 1, "dbankcloud.ru"},
		{"FakeSTUN", "tls", config.FakeSTUN, KindSTUN, 0, ""},
	}
	for _, c := range cases {
		f := FactsOf(c.protocol, c.data)
		if f.Kind != c.kind || f.QUICVersion != c.version || f.SNI != c.sni || !f.FitsOnePacket || len(f.Faults) != 0 {
			t.Errorf("%s: facts %s, want kind %s, version %d, SNI %q, fitting one packet, no faults", c.name, factsSummary(f), c.kind, c.version, c.sni)
		}
		if tls := c.kind == KindTLSClientHello; (f.Extensions != nil) != tls || (f.ECH != nil) != tls {
			t.Errorf("%s: extensions and ech belong to a TLS ClientHello only, got %s", c.name, factsSummary(f))
		}
	}
	if f := FactsOf("tls", config.FakeSNI1); extensionCount(f) != 16 || !carriesECH(f) {
		t.Errorf("Preset: Google (classic) carries 16 extensions with ECH, got %s", factsSummary(f))
	}
}

func quicV2InitialHeader() []byte {
	p := bytes.Repeat([]byte{0x3c}, 1200)
	p[0] = 0xc0 | 0x01<<4
	binary.BigEndian.PutUint32(p[1:5], 0x6b3343cf)
	p[5] = 8
	p[14] = 0
	p[15] = 0
	return p
}

func dtlsClientHello(host string) []byte {
	ext := []byte{0x00, 0x00}
	ext = binary.BigEndian.AppendUint16(ext, uint16(len(host)+5))
	ext = binary.BigEndian.AppendUint16(ext, uint16(len(host)+3))
	ext = append(ext, 0x00)
	ext = binary.BigEndian.AppendUint16(ext, uint16(len(host)))
	ext = append(ext, host...)

	body := []byte{0xfe, 0xfd}
	body = append(body, bytes.Repeat([]byte{0x2a}, 32)...)
	body = append(body, 0x00, 0x00)
	body = append(body, 0x00, 0x02, 0xc0, 0x2b)
	body = append(body, 0x01, 0x00)
	body = binary.BigEndian.AppendUint16(body, uint16(len(ext)))
	body = append(body, ext...)

	hs := []byte{0x01, byte(len(body) >> 16), byte(len(body) >> 8), byte(len(body))}
	hs = append(hs, 0x00, 0x00, 0x00, 0x00, 0x00)
	hs = append(hs, byte(len(body)>>16), byte(len(body)>>8), byte(len(body)))
	hs = append(hs, body...)

	rec := []byte{0x16, 0xfe, 0xfd, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	rec = binary.BigEndian.AppendUint16(rec, uint16(len(hs)))
	return append(rec, hs...)
}

var notAnyKnownPayload = bytes.Repeat([]byte{0x5c, 0x2e, 0x91, 0x07, 0xd3, 0x68, 0x0b, 0xae}, 40)

func TestFactsTellTheKindOfAFile(t *testing.T) {
	if f := FactsOf("quic", quicV2InitialHeader()); f.Kind != KindQUICInitial || f.QUICVersion != 2 || f.SNI != "" {
		t.Errorf("a QUIC v2 Initial header: facts %s, want kind quic_initial, version 2, no SNI", factsSummary(f))
	}
	if f := FactsOf("quic", dtlsClientHello("dtls.example")); f.Kind != KindDTLSClientHello || f.SNI != "dtls.example" || f.Extensions != nil {
		t.Errorf("a DTLS ClientHello: facts %s, want kind dtls_client_hello with SNI dtls.example", factsSummary(f))
	}
	if f := FactsOf("tls", notAnyKnownPayload); f.Kind != KindOther || f.SNI != "" || f.Extensions != nil || f.ECH != nil || len(f.Faults) != 0 {
		t.Errorf("bytes of no known kind: facts %s, want kind other and nothing else", factsSummary(f))
	}
	if f := FactsOf("tls", nil); f.Kind != KindOther || !f.FitsOnePacket {
		t.Errorf("an empty file: facts %s, want kind other", factsSummary(f))
	}
}

func TestFactsSayWhetherAPayloadFitsOnePacket(t *testing.T) {
	cases := []struct {
		protocol string
		size     int
		fits     bool
	}{
		{"tls", config.MaxTCPFakeLenInOnePacket, true},
		{"tls", config.MaxTCPFakeLenInOnePacket + 1, false},
		{"quic", config.MaxUDPFakeLen, true},
		{"quic", config.MaxUDPFakeLen + 1, false},
	}
	for _, c := range cases {
		if f := FactsOf(c.protocol, make([]byte, c.size)); f.FitsOnePacket != c.fits {
			t.Errorf("a %d-byte %s payload: fits_one_packet %t, want %t", c.size, c.protocol, f.FitsOnePacket, c.fits)
		}
	}
}

func helloExtension(t *testing.T, hello []byte, typ uint16) int {
	t.Helper()
	p := 9 + 2 + 32
	p += 1 + int(hello[p])
	p += 2 + int(binary.BigEndian.Uint16(hello[p:]))
	p += 1 + int(hello[p])
	end := p + 2 + int(binary.BigEndian.Uint16(hello[p:]))
	p += 2
	for p+4 <= end {
		if binary.BigEndian.Uint16(hello[p:]) == typ {
			return p + 4
		}
		p += 4 + int(binary.BigEndian.Uint16(hello[p+2:]))
	}
	t.Fatalf("the hello carries no extension 0x%04x", typ)
	return 0
}

func bumpUint16(b []byte, by int) {
	binary.BigEndian.PutUint16(b, uint16(int(binary.BigEndian.Uint16(b))+by))
}

func TestEachMalformedFieldHasItsOwnCode(t *testing.T) {
	cases := []struct {
		code       string
		breakField func(t *testing.T, h []byte)
	}{
		{"record", func(t *testing.T, h []byte) { bumpUint16(h[3:], 1) }},
		{"handshake", func(t *testing.T, h []byte) { h[8]++ }},
		{"session_id", func(t *testing.T, h []byte) { h[43] = 33 }},
		{"cipher_suites", func(t *testing.T, h []byte) { bumpUint16(h[44+int(h[43]):], 1) }},
		{"extensions", func(t *testing.T, h []byte) {
			p := 44 + int(h[43])
			p += 2 + int(binary.BigEndian.Uint16(h[p:]))
			p += 1 + int(h[p])
			bumpUint16(h[p:], 1)
		}},
		{"server_name", func(t *testing.T, h []byte) { bumpUint16(h[helloExtension(t, h, 0x0000):], 1) }},
		{"supported_groups", func(t *testing.T, h []byte) { bumpUint16(h[helloExtension(t, h, 0x000a):], -1) }},
		{"key_share", func(t *testing.T, h []byte) {
			p := helloExtension(t, h, 0x0033) + 2
			p += 4 + int(binary.BigEndian.Uint16(h[p+2:]))
			h[p+4] = 0x05
		}},
		{"delegated_credentials", func(t *testing.T, h []byte) { bumpUint16(h[helloExtension(t, h, 0x0022):], -4) }},
		{"ech", func(t *testing.T, h []byte) { h[helloExtension(t, h, 0xfe0d)] = 0xfe }},
		{"supported_versions", func(t *testing.T, h []byte) { h[helloExtension(t, h, 0x002b)]++ }},
		{"signature_algorithms", func(t *testing.T, h []byte) { bumpUint16(h[helloExtension(t, h, 0x000d):], 2) }},
		{"alpn", func(t *testing.T, h []byte) { h[helloExtension(t, h, 0x0010)+2]++ }},
	}
	for _, c := range cases {
		hello := generatedHello(t, "faults.example")
		c.breakField(t, hello)
		if f := FactsOf("tls", hello); !reflect.DeepEqual(f.Faults, []string{c.code}) {
			t.Errorf("a hello with a broken %s field: faults %q, want [%s]", c.code, f.Faults, c.code)
		}
	}
}

func TestAKeyShareOfTheWrongLengthForItsGroupIsMalformed(t *testing.T) {
	hello := generatedHello(t, "keyshare.example")
	p := helloExtension(t, hello, 0x0033) + 2
	binary.BigEndian.PutUint16(hello[p:], 0x0018)
	if f := FactsOf("tls", hello); !reflect.DeepEqual(f.Faults, []string{"key_share"}) {
		t.Errorf("a 32-byte share named secp384r1: faults %q, want [key_share]", f.Faults)
	}
	binary.BigEndian.PutUint16(hello[p:], 0x0a0a)
	if f := FactsOf("tls", hello); len(f.Faults) != 0 {
		t.Errorf("a share of a group b4 does not know (GREASE) is no fault, got %q", f.Faults)
	}
}

func payloadsJSONOnDisk(t *testing.T, m *Manager) map[string]map[string]map[string]any {
	t.Helper()
	raw, err := os.ReadFile(m.metadataFile)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]map[string]map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSavedPayloadsCarryTheirFacts(t *testing.T) {
	m := testManager(t)
	if err := m.GenerateCapture("generated.facts.example", "tls"); err != nil {
		t.Fatal(err)
	}
	if err := m.SaveUploadedCapture("quic", "uploaded.facts.example", config.FakeQUIC1, true); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SaveImportedPayload("tls", "imported.facts.example", config.FakeSNI2); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"generated.facts.example": KindTLSClientHello,
		"uploaded.facts.example":  KindQUICInitial,
		"imported.facts.example":  KindTLSClientHello,
	}
	for _, c := range m.ListCaptures() {
		if c.Facts == nil || c.Facts.Kind != want[c.Domain] {
			t.Errorf("the %s payload %s is listed with facts %s, want kind %s", c.Protocol, c.Domain, factsSummary(c.Facts), want[c.Domain])
		}
	}
	for name, protocols := range payloadsJSONOnDisk(t, m) {
		for protocol, entry := range protocols {
			facts, _ := entry["facts"].(map[string]any)
			if facts["kind"] != want[name] {
				t.Errorf("payloads.json holds no facts for the %s payload %s: %v", protocol, name, entry)
			}
		}
	}
}

func TestEntriesSavedWithoutFactsGetThemOnceAtLoad(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tls_old_facts_example.bin"), config.FakeSNI1, 0644); err != nil {
		t.Fatal(err)
	}
	old := `{"old.facts.example":{"tls":{"timestamp":"2026-03-02T00:00:00Z","size":1235,"filepath":"tls_old_facts_example.bin"}},` +
		`"gone.facts.example":{"tls":{"timestamp":"2026-03-02T00:00:00Z","size":10,"filepath":"tls_gone_facts_example.bin"}}}`
	if err := os.WriteFile(filepath.Join(dir, "payloads.json"), []byte(old), 0644); err != nil {
		t.Fatal(err)
	}
	m := testManagerIn(t, dir)
	m.loadMetadata()

	listed := map[string]*Capture{}
	for _, c := range m.ListCaptures() {
		listed[c.Domain] = c
	}
	if c := listed["old.facts.example"]; c == nil || c.Facts == nil || c.Facts.SNI != "www.google.com" {
		t.Errorf("an entry saved before facts existed is listed without them after a load: %+v", c)
	}
	if c := listed["gone.facts.example"]; c == nil || c.Facts != nil {
		t.Errorf("an entry whose file cannot be read must stay listed without facts: %+v", c)
	}
	onDisk := payloadsJSONOnDisk(t, m)
	if facts, _ := onDisk["old.facts.example"]["tls"]["facts"].(map[string]any); facts["sni"] != "www.google.com" {
		t.Errorf("the facts computed at load were not saved back to payloads.json: %v", onDisk["old.facts.example"])
	}
	if !strings.Contains(factsSummary(listed["old.facts.example"].Facts), `"kind":"tls_client_hello"`) {
		t.Errorf("unexpected facts %s", factsSummary(listed["old.facts.example"].Facts))
	}
}
