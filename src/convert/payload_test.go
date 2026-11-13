package convert

import (
	"bytes"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func assertPendingPayload(t *testing.T, set config.SetConfig, want []byte) {
	t.Helper()
	f := set.Faking
	if f.SNIType != config.FakePayloadCapture {
		t.Fatalf("sni_type = %d, want %d (capture): fake data from the command line belongs in a payload file", f.SNIType, config.FakePayloadCapture)
	}
	if !strings.HasPrefix(f.PayloadFile, PendingPayloadPrefix) {
		t.Fatalf("payload_file = %q, want the %q placeholder that apply turns into a file", f.PayloadFile, PendingPayloadPrefix)
	}
	if !bytes.Equal(f.PayloadData, want) {
		t.Fatalf("payload bytes = % x, want % x", f.PayloadData, want)
	}
	if f.CustomPayload != "" {
		t.Fatalf("custom_payload = %q: it is a JSON string and cannot hold bytes that are not text", f.CustomPayload)
	}
}

func TestByedpi_InlineFakeDataKeepsEveryByte(t *testing.T) {
	line := `-f-1 -l :\x16\x03\x01\xfc\x00\xa5`
	res := analyze(t, line)
	assertPendingPayload(t, res.Sets[0], []byte{0x16, 0x03, 0x01, 0xfc, 0x00, 0xa5})

	n := noteFor(t, res, `-l :\x16\x03\x01\xfc\x00\xa5`)
	if n.Status != StatusMapped || n.Reason != "fakeDataMapped" || !hasField(n, "faking.payload_file") {
		t.Fatalf("got %+v", n)
	}
	assertSetsSave(t, res)
}

func TestByedpi_InlineFakeDataStillComesBeforeTheFakeSNI(t *testing.T) {
	res := analyze(t, "-f-1 -l :hello -n www.google.com")
	assertPendingPayload(t, res.Sets[0], []byte("hello"))
}

func TestZapret_AHexPayloadKeepsEveryByte(t *testing.T) {
	res := analyze(t, "--dpi-desync=fake --dpi-desync-fake-tls=0x16030100fc00a5")
	assertPendingPayload(t, res.Sets[0], []byte{0x16, 0x03, 0x01, 0x00, 0xfc, 0x00, 0xa5})

	n := noteFor(t, res, "--dpi-desync-fake-tls=0x16030100fc00a5")
	if n.Status != StatusMapped || n.Reason != "fakeDataMapped" {
		t.Fatalf("got %+v", n)
	}
	assertSetsSave(t, res)
}

func TestZapret_AFakeSNIStillBeatsAHexPayload(t *testing.T) {
	res := analyze(t, "--dpi-desync=fake --dpi-desync-fake-tls=0x0F0F0F0F "+
		"--dpi-desync-fake-tls-mod=sni=www.google.com")
	f := res.Sets[0].Faking
	if f.SNIType != config.FakePayloadDomain || f.PayloadDomain != "www.google.com" {
		t.Fatalf("sni_type %d, payload_domain %q: the SNI asked for must reach the fake", f.SNIType, f.PayloadDomain)
	}
	if len(f.PayloadData) > 0 || f.PayloadFile != "" {
		t.Fatalf("a payload that is not used must not be saved: file %q, %d bytes", f.PayloadFile, len(f.PayloadData))
	}
	n := noteFor(t, res, "--dpi-desync-fake-tls=0x0F0F0F0F")
	if n.Status != StatusApproximated || n.Reason != "fakeHexPayloadOverridden" || !hasField(n, "faking.sni_type=domain") {
		t.Fatalf("got %+v", n)
	}
}

func TestZapret_OnlyTheFirstHexPayloadIsUsed(t *testing.T) {
	res := analyze(t, "--dpi-desync=fake --dpi-desync-fake-tls=0x0101 --dpi-desync-fake-tls=0x0202")
	assertPendingPayload(t, res.Sets[0], []byte{0x01, 0x01})

	if n := noteFor(t, res, "--dpi-desync-fake-tls=0x0101"); n.Status != StatusMapped || n.Reason != "fakeDataMapped" {
		t.Fatalf("the payload in use: got %+v", n)
	}
	if n := noteFor(t, res, "--dpi-desync-fake-tls=0x0202"); n.Status != StatusApproximated || n.Reason != "fakeHexPayloadOverridden" {
		t.Fatalf("the payload left out: got %+v", n)
	}
}

func TestZapret_AnInvalidHexPayloadIsFlagged(t *testing.T) {
	for _, value := range []string{"0x0F0", "0x0G"} {
		res := analyze(t, "--dpi-desync=fake --dpi-desync-fake-tls="+value)
		if got := res.Sets[0].Faking.SNIType; got == config.FakePayloadCapture {
			t.Errorf("%s: an undecodable pattern must not become a payload", value)
		}
		if n := noteFor(t, res, "--dpi-desync-fake-tls="+value); n.Status != StatusInvalid || n.Reason != "fakeHexInvalid" {
			t.Errorf("%s: got %+v", value, n)
		}
	}
}
