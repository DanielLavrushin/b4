package sock

import (
	"bytes"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func invertedBytes(b []byte) []byte {
	out := make([]byte, len(b))
	for i, v := range b {
		out[i] = ^v
	}
	return out
}

func TestTLSModificationLeavesAFakeThatIsNoClientHelloAsItIs(t *testing.T) {
	original := realHello(t, "www.example.com")
	both := TLSModFlags{Randomize: true, DupSessID: true}

	payloads := map[string][]byte{
		"All Zeros":         make([]byte, 1200),
		"Preset: STUN":      config.FakeSTUN,
		"an inverted hello": invertedBytes(original),
		"a QUIC Initial":    config.FakeQUIC1,
		"QUIC preset 2":     config.FakeQUIC2,
	}
	for name, payload := range payloads {
		before := cloneBytes(payload)
		if got := ApplyTLSMod(cloneBytes(payload), original, both); !bytes.Equal(got, before) {
			t.Errorf("rnd and dupsid changed %s, which is no TLS ClientHello", name)
		}
	}

	types := map[string]func(*config.FakingConfig){
		"All Zeros":         func(f *config.FakingConfig) { f.SNIType = config.FakePayloadZero },
		"Preset: STUN":      func(f *config.FakingConfig) { f.SNIType = config.FakePayloadSTUN },
		"Inverted Original": func(f *config.FakingConfig) { f.SNIType = config.FakePayloadInverted },
		"a QUIC Initial as file": func(f *config.FakingConfig) {
			f.SNIType = config.FakePayloadCapture
			f.PayloadData = cloneBytes(config.FakeQUIC1)
		},
	}
	var google, googleModded config.FakingConfig
	google.SNIType, googleModded.SNIType = config.FakePayloadDefault1, config.FakePayloadDefault1
	googleModded.TLSMod = []string{"rnd", "dupsid"}
	if bytes.Equal(fakeTCPPayload(&google, original), fakeTCPPayload(&googleModded, original)) {
		t.Fatal("control: rnd and dupsid must change Preset: Google (classic), a ClientHello")
	}

	for name, setType := range types {
		var plain, modded config.FakingConfig
		setType(&plain)
		setType(&modded)
		modded.TLSMod = []string{"rnd", "dupsid"}
		if a, b := fakeTCPPayload(&plain, original), fakeTCPPayload(&modded, original); !bytes.Equal(a, b) {
			t.Errorf("Fake Payload Type %s: the fake with rnd and dupsid differs from the one without, so the switches do something there", name)
		}
	}
}
