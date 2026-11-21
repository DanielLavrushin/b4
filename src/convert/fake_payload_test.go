package convert

import (
	"encoding/binary"
	"encoding/hex"
	"slices"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sock"
	"github.com/daniellavrushin/b4/tlsgen"
)

func TestAnImportedSmallBadseqIncrementKeepsTheFakeBehindTheRealData(t *testing.T) {
	res := analyze(t, "--dpi-desync=fake --dpi-desync-fooling=badseq --dpi-desync-badseq-increment=2")
	set := &res.Sets[0]
	config.ApplySetDefaults(set)
	if !set.Faking.SNI {
		t.Fatal("the imported fake profile does not enable faking.sni")
	}
	if set.Faking.SNIType == config.FakePayloadCapture && len(set.Faking.PayloadData) == 0 {
		set.Faking.SNIType = config.FakePayloadDefault1
	}

	hello, err := tlsgen.GenerateTLSClientHello("www.example.com")
	if err != nil {
		t.Fatal(err)
	}
	pkt := make([]byte, 40+len(hello))
	pkt[0] = 0x45
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(pkt)))
	pkt[8] = 64
	pkt[9] = 6
	copy(pkt[12:16], []byte{192, 168, 1, 2})
	copy(pkt[16:20], []byte{203, 0, 113, 7})
	binary.BigEndian.PutUint16(pkt[20:22], 40000)
	binary.BigEndian.PutUint16(pkt[22:24], 443)
	binary.BigEndian.PutUint32(pkt[24:28], 1000)
	binary.BigEndian.PutUint32(pkt[28:32], 5000)
	pkt[32] = 0x50
	pkt[33] = 0x18
	copy(pkt[40:], hello)
	sock.FixIPv4Checksum(pkt[:20])
	sock.FixTCPChecksum(pkt)

	fake := sock.BuildFakeSNIPacketV4(pkt, set)
	if fake == nil {
		t.Fatal("fake not built")
	}
	start := binary.BigEndian.Uint32(fake[24:28])
	end := start + uint32(len(fake)-40)
	ack := binary.BigEndian.Uint32(fake[28:32])
	if int32(end-1000) > 0 && fake[8] == pkt[8] && sock.TCPChecksumValid(fake) && ack == 5000 {
		t.Fatalf("badseq increment 2 imported as strategy=%s seq_offset=%d apply_ttl=%v: the %d-byte fake covers seq %d..%d over the real data at 1000, with the real ACK, full TTL and a valid checksum, so the server takes %d fake bytes as the start of the stream",
			set.Faking.Strategy, set.Faking.SeqOffset, set.Faking.ApplyTTL, len(fake)-40, start, end, int32(end-1000))
	}
}

func noteWithPrefix(t *testing.T, res *Result, prefix string) Note {
	t.Helper()
	for _, n := range res.Notes {
		if strings.HasPrefix(n.Token, prefix) {
			return n
		}
	}
	t.Fatalf("no note for a token starting %q", prefix)
	return Note{}
}

func TestAnImportedFakePayloadAboveTheLimitIsNotTheSetPayload(t *testing.T) {
	for _, c := range []struct {
		size    int
		adopted bool
	}{
		{16384, true},
		{16385, false},
		{65484, false},
	} {
		data := make([]byte, c.size)
		data[0] = 0x16
		res := analyze(t, "--dpi-desync=fake --dpi-desync-fake-tls=0x"+hex.EncodeToString(data))
		f := res.Sets[0].Faking
		got := f.SNIType == config.FakePayloadCapture && len(f.PayloadData) == c.size
		if got != c.adopted {
			t.Errorf("a %d-byte hex fake: adopted as the set's payload %v, want %v (sni_type %d, %d bytes, file %q)", c.size, got, c.adopted, f.SNIType, len(f.PayloadData), f.PayloadFile)
		}
		n := noteWithPrefix(t, res, "--dpi-desync-fake-tls=0x")
		if c.adopted {
			if n.Status != StatusMapped || n.Reason != "fakeDataMapped" {
				t.Errorf("a %d-byte hex fake is used, got note status %q reason %q", c.size, n.Status, n.Reason)
			}
			continue
		}
		if f.SNIType == config.FakePayloadCapture || len(f.PayloadData) != 0 || f.PayloadFile != "" {
			t.Errorf("a %d-byte hex fake must leave the set on a built-in payload, got sni_type %d, %d bytes, file %q", c.size, f.SNIType, len(f.PayloadData), f.PayloadFile)
		}
		if n.Status == StatusMapped || n.Reason != "fakePayloadTooLarge" || n.Params["size"] != c.size || n.Params["max"] != 16384 {
			t.Errorf("a %d-byte hex fake must be noted as too large for an import, got status %q reason %q params %v", c.size, n.Status, n.Reason, n.Params)
		}
	}

	big := strings.Repeat("x", 16385)
	res := analyze(t, "-f-1 -l :"+big)
	if f := res.Sets[0].Faking; f.SNIType == config.FakePayloadCapture || len(f.PayloadData) != 0 {
		t.Errorf("a 16385-byte inline byedpi fake must not become the set's payload, got sni_type %d, %d bytes", f.SNIType, len(f.PayloadData))
	}
	if n := noteFor(t, res, "-l :"+big); n.Status == StatusMapped || n.Reason != "fakePayloadTooLarge" {
		t.Errorf("a 16385-byte inline byedpi fake must be noted as too large, got status %q reason %q", n.Status, n.Reason)
	}
}

func TestZapretRndSNIIsNotReportedAsAnExactMapping(t *testing.T) {
	res := analyze(t, "--dpi-desync=fake --dpi-desync-fake-tls-mod=rndsni")
	n := noteFor(t, res, "--dpi-desync-fake-tls-mod=rndsni")
	if n.Status == StatusMapped {
		t.Fatalf("rndsni became tls_mod %v and the fake keeps its built-in SNI, yet the note says %q (%s)", res.Sets[0].Faking.TLSMod, n.Status, n.Reason)
	}

	res = analyze(t, "--dpi-desync=fake --dpi-desync-fake-tls-mod=rnd,rndsni")
	n = noteFor(t, res, "--dpi-desync-fake-tls-mod=rnd,rndsni")
	if n.Status != StatusApproximated || n.Reason != "fakeTLSModPartial" || n.Params["dropped"] != "rndsni" {
		t.Fatalf("rnd maps and rndsni does not, got %+v", n)
	}
	if got := res.Sets[0].Faking.TLSMod; !slices.Equal(got, []string{"rnd"}) {
		t.Fatalf("tls_mod %v, want [rnd]", got)
	}
}

func TestZapretBuiltInFakeKeepsTheModsNfqwsAppliesToIt(t *testing.T) {
	res := analyze(t, "--filter-tcp=443 --dpi-desync=fake --dpi-desync-fooling=badsum")
	set := res.Sets[0]
	if set.Faking.SNIType != config.FakePayloadDefault1 && set.Faking.SNIType != config.FakePayloadDefault2 {
		t.Fatalf("a profile with no fake given must use a built-in hello, got sni_type %d", set.Faking.SNIType)
	}
	if !slices.Contains(set.Faking.TLSMod, "rnd") || !slices.Contains(set.Faking.TLSMod, "dupsid") {
		t.Fatalf("nfqws applies rnd,rndsni,dupsid to its built-in fake when no fake or mod is given; the imported set has tls_mod %v", set.Faking.TLSMod)
	}
	var found bool
	for _, n := range res.Notes {
		if n.Profile == 0 && n.Reason == "fakeTLSModPartial" && n.Params["dropped"] == "rndsni" && hasField(n, "faking.tls_mod") {
			found = true
		}
	}
	if !found {
		t.Errorf("no note says that rndsni, the third implicit mod, is not reproduced: %+v", res.Notes)
	}

	for _, line := range []string{
		"--dpi-desync=fake --dpi-desync-fake-tls-mod=none",
		"--dpi-desync=fake --dpi-desync-fake-tls=0x16030100fc00a5",
		"--dpi-desync=fake --dpi-desync-fake-tls=!",
		"--filter-udp=443 --dpi-desync=fake",
		"--dpi-desync=multisplit",
		"-f-1",
	} {
		res := analyze(t, line)
		for i, s := range res.Sets {
			if len(s.Faking.TLSMod) != 0 {
				t.Errorf("%q, set %d: nfqws adds its implicit mods only to a TCP fake with neither a fake nor a mod given, got tls_mod %v", line, i, s.Faking.TLSMod)
			}
		}
	}

	res = analyze(t, "--filter-tcp=80 --dpi-desync=multisplit --new --filter-tcp=443 --dpi-desync=fake")
	if len(res.Sets) != 2 || !slices.Equal(res.Sets[1].Faking.TLSMod, []string{"rnd", "dupsid"}) {
		var mods [][]string
		for _, s := range res.Sets {
			mods = append(mods, s.Faking.TLSMod)
		}
		t.Errorf("each user profile with a fake gets the implicit mods, got %v", mods)
	}
}
