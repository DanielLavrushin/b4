package capture

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func factsJSON(t *testing.T, protocol string, data []byte) map[string]any {
	t.Helper()
	out, err := json.Marshal(FactsOf(protocol, data))
	if err != nil {
		t.Fatal(err)
	}
	var facts map[string]any
	if err := json.Unmarshal(out, &facts); err != nil {
		t.Fatal(err)
	}
	return facts
}

func TestFactsSayWhetherTLSModificationChangesAClientHello(t *testing.T) {
	recordTooLong := bytes.Clone(config.FakeSNI1)
	bumpUint16(recordTooLong[3:], 1)
	recordTooShort := append(bytes.Clone(config.FakeSNI1), 0x00)

	cases := []struct {
		what   string
		data   []byte
		tlsMod bool
		fault  bool
	}{
		{"a complete ClientHello", bytes.Clone(config.FakeSNI1), true, false},
		{"a ClientHello whose record length runs one byte past the file", recordTooLong, false, true},
		{"a ClientHello whose record ends one byte before the file", recordTooShort, true, true},
	}
	for _, c := range cases {
		facts := factsJSON(t, "tls", c.data)
		if facts["kind"] != KindTLSClientHello {
			t.Errorf("%s: kind %v, want %s", c.what, facts["kind"], KindTLSClientHello)
		}
		faults, _ := facts["faults"].([]any)
		if hasRecordFault := slices.Contains(faults, any("record")); hasRecordFault != c.fault {
			t.Errorf("%s: faults %v, record fault %t, want %t", c.what, faults, hasRecordFault, c.fault)
		}
		got, present := facts["tls_mod"]
		if !present || got != c.tlsMod {
			t.Errorf("%s: tls_mod %v (present %t), want %t", c.what, got, present, c.tlsMod)
		}
	}
}

func TestOnlyAClientHelloCarriesTLSMod(t *testing.T) {
	for _, c := range []struct {
		what     string
		protocol string
		data     []byte
	}{
		{"FakeSTUN", "tls", config.FakeSTUN},
		{"FakeQUIC1", "quic", config.FakeQUIC1},
		{"bytes of no known kind", "tls", bytes.Repeat([]byte{0xab}, 64)},
	} {
		if got, present := factsJSON(t, c.protocol, c.data)["tls_mod"]; present {
			t.Errorf("the facts of %s carry tls_mod %v, which belongs to a TLS ClientHello only", c.what, got)
		}
	}
}

func TestFactsLeaveTheBytesTheyReadAsTheyAre(t *testing.T) {
	data := bytes.Clone(config.FakeSNI1)
	FactsOf("tls", data)
	if !bytes.Equal(data, config.FakeSNI1) {
		t.Error("FactsOf changed the bytes of the ClientHello it read")
	}
}
