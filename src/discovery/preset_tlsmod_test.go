package discovery

import (
	"bytes"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sock"
	"github.com/daniellavrushin/b4/tlsgen"
)

func presetFakePair(t *testing.T, pkt []byte, set config.SetConfig) (with, without []byte) {
	t.Helper()
	set.Faking.PayloadData = nil
	bare := set
	bare.Faking.TLSMod = nil
	with = sock.BuildFakeSNIPacketV4(pkt, &set)
	without = sock.BuildFakeSNIPacketV4(pkt, &bare)
	if len(with) <= 40 || len(without) <= 40 {
		t.Fatalf("sni_type %d: no fake built (%d and %d bytes)", set.Faking.SNIType, len(with), len(without))
	}
	return with[40:], without[40:]
}

func TestEveryPresetTLSModChangesItsFake(t *testing.T) {
	hello, err := tlsgen.GenerateTLSClientHello("www.example.com")
	if err != nil {
		t.Fatal(err)
	}
	pkt := make([]byte, 40+len(hello))
	pkt[0] = 0x45
	pkt[2], pkt[3] = byte(len(pkt)>>8), byte(len(pkt))
	pkt[8], pkt[9] = 64, 6
	pkt[32] = 0x50
	copy(pkt[40:], hello)

	control := config.NewSetConfig()
	control.Faking.SNI = true
	control.Faking.SNIType = config.FakePayloadDefault1
	control.Faking.TLSMod = []string{"rnd"}
	if a, b := presetFakePair(t, pkt, control); bytes.Equal(a, b) {
		t.Fatal("control: tls_mod rnd on the built-in google hello changes nothing, so this check cannot see a mod that works")
	}

	for _, p := range GetPhase1Presets() {
		if len(p.Config.Faking.TLSMod) == 0 || p.Config.Faking.SNIType == config.FakePayloadRandom {
			continue
		}
		if a, b := presetFakePair(t, pkt, p.Config); bytes.Equal(a, b) {
			t.Errorf("preset %q: tls_mod %v on sni_type %d changes nothing in the fake", p.Name, p.Config.Faking.TLSMod, p.Config.Faking.SNIType)
		}
	}
}
