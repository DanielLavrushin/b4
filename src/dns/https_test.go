package dns

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"net"
	"net/netip"
	"slices"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

const cloudflareHTTPSAnswer = "1234818000010001000000000663727970746f0a636c6f7564666c61726503636f6d0000410001c00c004100010000012c00850001000001000302683200040008a29f874fa29f884f000500470045fe0d00415d002000200b14be46ee7a04bc46bd094737fb88673db29d3b550ac6fc945a9e1c862dfc4f0004000100010012636c6f7564666c6172652d6563682e636f6d000000060020260647000007000000000000a29f874f260647000007000000000000a29f884f"

func echConfigList(t *testing.T, versions []uint16, publicName string) []byte {
	t.Helper()
	var list []byte
	for _, version := range versions {
		var c []byte
		c = append(c, 0x42)
		c = binary.BigEndian.AppendUint16(c, 0x0020)
		c = binary.BigEndian.AppendUint16(c, 32)
		c = append(c, bytes.Repeat([]byte{0xab}, 32)...)
		c = binary.BigEndian.AppendUint16(c, 4)
		c = binary.BigEndian.AppendUint16(c, 0x0001)
		c = binary.BigEndian.AppendUint16(c, 0x0001)
		c = append(c, 0, byte(len(publicName)))
		c = append(c, publicName...)
		c = binary.BigEndian.AppendUint16(c, 0)
		list = binary.BigEndian.AppendUint16(list, version)
		list = binary.BigEndian.AppendUint16(list, uint16(len(c)))
		list = append(list, c...)
	}
	return append(binary.BigEndian.AppendUint16(nil, uint16(len(list))), list...)
}

func svcbRData(priority uint16, params map[uint16][]byte, order []uint16) []byte {
	rdata := binary.BigEndian.AppendUint16(nil, priority)
	rdata = append(rdata, 0)
	for _, key := range order {
		rdata = binary.BigEndian.AppendUint16(rdata, key)
		rdata = binary.BigEndian.AppendUint16(rdata, uint16(len(params[key])))
		rdata = append(rdata, params[key]...)
	}
	return rdata
}

func TestParseResponseECHRealWorld(t *testing.T) {
	resp, err := hex.DecodeString(cloudflareHTTPSAnswer)
	if err != nil {
		t.Fatal(err)
	}
	ech := ParseResponseECH(resp)
	if len(ech) != 71 || !bytes.HasPrefix(ech, []byte{0x00, 0x45, 0xfe, 0x0d}) {
		t.Fatalf("ech = %x, want the 71-byte ECHConfigList", ech)
	}
	if got := ECHPublicName(ech); got != "cloudflare-ech.com" {
		t.Fatalf("public name = %q, want cloudflare-ech.com", got)
	}
}

func TestParseResponseECHAfterCNAME(t *testing.T) {
	list := echConfigList(t, []uint16{echConfigVersion}, "cover.example.net")
	resp := buildResponse("www.example.com", TypeHTTPS, []testRR{
		{typ: rrTypeCNAME, ttl: 60, rdata: encodeName("edge.example.net")},
		{name: "edge.example.net", typ: TypeHTTPS, ttl: 60, rdata: svcbRData(1, map[uint16][]byte{
			1:           {2, 'h', '2'},
			svcParamECH: list,
			6:           bytes.Repeat([]byte{0x20}, 16),
		}, []uint16{1, svcParamECH, 6})},
	}, false, false)
	if got := ParseResponseECH(resp); !bytes.Equal(got, list) {
		t.Fatalf("ech = %x, want %x", got, list)
	}
	if got := ECHPublicName(list); got != "cover.example.net" {
		t.Fatalf("public name = %q", got)
	}
}

func TestParseResponseECHPrefersLowestServicePriority(t *testing.T) {
	low := echConfigList(t, []uint16{echConfigVersion}, "low.example.net")
	high := echConfigList(t, []uint16{echConfigVersion}, "high.example.net")
	resp := buildResponse("example.com", TypeHTTPS, []testRR{
		{typ: TypeHTTPS, ttl: 60, rdata: svcbRData(0, map[uint16][]byte{svcParamECH: high}, []uint16{svcParamECH})},
		{typ: TypeHTTPS, ttl: 60, rdata: svcbRData(2, map[uint16][]byte{svcParamECH: high}, []uint16{svcParamECH})},
		{typ: TypeHTTPS, ttl: 60, rdata: svcbRData(1, map[uint16][]byte{svcParamECH: low}, []uint16{svcParamECH})},
	}, false, false)
	if got := ECHPublicName(ParseResponseECH(resp)); got != "low.example.net" {
		t.Fatalf("picked %q, want the priority 1 record", got)
	}
}

func TestParseResponseECHAbsent(t *testing.T) {
	aliasOnly := buildResponse("example.com", TypeHTTPS, []testRR{
		{typ: TypeHTTPS, ttl: 60, rdata: svcbRData(0, map[uint16][]byte{svcParamECH: echConfigList(t, []uint16{echConfigVersion}, "x.example")}, []uint16{svcParamECH})},
	}, false, false)
	noKey := buildResponse("example.com", TypeHTTPS, []testRR{
		{typ: TypeHTTPS, ttl: 60, rdata: svcbRData(1, map[uint16][]byte{1: {2, 'h', '2'}}, []uint16{1})},
	}, false, false)
	addressOnly := buildResponse("example.com", rrTypeA, []testRR{aRecord("192.0.2.1")}, false, false)
	for name, resp := range map[string][]byte{"alias mode": aliasOnly, "no ech key": noKey, "A answer": addressOnly} {
		if got := ParseResponseECH(resp); got != nil {
			t.Errorf("%s: ech = %x, want none", name, got)
		}
	}
}

func TestParseResponseECHTruncated(t *testing.T) {
	resp, err := hex.DecodeString(cloudflareHTTPSAnswer)
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < len(resp); n++ {
		if got := ParseResponseECH(resp[:n]); got != nil {
			t.Fatalf("a %d-byte prefix of the answer yielded %x", n, got)
		}
	}
}

func TestECHPublicNameSkipsUnknownVersions(t *testing.T) {
	list := echConfigList(t, []uint16{0xfe0c, echConfigVersion}, "Cover.Example.NET")
	if got := ECHPublicName(list); got != "cover.example.net" {
		t.Fatalf("public name = %q, want the lowercased name of the fe0d config", got)
	}
	if got := ECHPublicName(echConfigList(t, []uint16{0xfe0c}, "x.example")); got != "" {
		t.Fatalf("unknown version only: %q, want none", got)
	}
	full := echConfigList(t, []uint16{echConfigVersion}, "x.example")
	for n := 0; n < len(full); n++ {
		if got := ECHPublicName(full[:n]); got != "" {
			t.Fatalf("a %d-byte prefix yielded %q", n, got)
		}
	}
}

func parsedMessage(t *testing.T, msg []byte) (dnsmessage.Header, []dnsmessage.Resource, []dnsmessage.Resource, []dnsmessage.Resource) {
	t.Helper()
	var p dnsmessage.Parser
	h, err := p.Start(msg)
	if err != nil {
		t.Fatalf("header: %v", err)
	}
	if err := p.SkipAllQuestions(); err != nil {
		t.Fatalf("questions: %v", err)
	}
	an, err := p.AllAnswers()
	if err != nil {
		t.Fatalf("answers: %v", err)
	}
	ns, err := p.AllAuthorities()
	if err != nil {
		t.Fatalf("authorities: %v", err)
	}
	ar, err := p.AllAdditionals()
	if err != nil {
		t.Fatalf("additionals: %v", err)
	}
	return h, an, ns, ar
}

func svcbOf(t *testing.T, r dnsmessage.Resource) dnsmessage.SVCBResource {
	t.Helper()
	switch body := r.Body.(type) {
	case *dnsmessage.HTTPSResource:
		return body.SVCBResource
	case *dnsmessage.SVCBResource:
		return *body
	}
	t.Fatalf("%v is not a service binding record", r.Header.Type)
	return dnsmessage.SVCBResource{}
}

func builtMessage(t *testing.T, qname string, qtype dnsmessage.Type, h dnsmessage.Header, fill func(b *dnsmessage.Builder) error) []byte {
	t.Helper()
	b := dnsmessage.NewBuilder(nil, h)
	b.EnableCompression()
	if err := b.StartQuestions(); err != nil {
		t.Fatal(err)
	}
	if err := b.Question(dnsmessage.Question{Name: dnsmessage.MustNewName(qname), Type: qtype, Class: dnsmessage.ClassINET}); err != nil {
		t.Fatal(err)
	}
	if err := fill(&b); err != nil {
		t.Fatal(err)
	}
	msg, err := b.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

func rrHeader(name string, typ dnsmessage.Type) dnsmessage.ResourceHeader {
	return dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName(name), Type: typ, Class: dnsmessage.ClassINET, TTL: 300}
}

func TestStripECHRealWorld(t *testing.T) {
	resp, err := hex.DecodeString(cloudflareHTTPSAnswer)
	if err != nil {
		t.Fatal(err)
	}
	_, before, _, _ := parsedMessage(t, resp)
	out, verdict := StripECH(resp, 0)
	if verdict != FilterRewritten {
		t.Fatalf("verdict = %d, want rewritten", verdict)
	}
	if ParseResponseECH(out) != nil {
		t.Fatal("the rewritten answer still carries an ECH key")
	}
	if len(out) != len(resp)-4-71 {
		t.Fatalf("length %d, want %d: only the ech parameter goes", len(out), len(resp)-75)
	}
	if !bytes.Equal(out[:2], resp[:2]) {
		t.Fatal("the transaction id changed")
	}
	_, after, _, _ := parsedMessage(t, out)
	if len(after) != 1 {
		t.Fatalf("%d answers, want 1", len(after))
	}
	was, now := svcbOf(t, before[0]), svcbOf(t, after[0])
	if now.Priority != was.Priority || now.Target.String() != was.Target.String() {
		t.Fatalf("record header changed: %+v -> %+v", was, now)
	}
	var want []dnsmessage.SVCParam
	for _, p := range was.Params {
		if p.Key != dnsmessage.SVCParamECH {
			want = append(want, p)
		}
	}
	if len(now.Params) != len(want) {
		t.Fatalf("params %v, want %v", now.Params, want)
	}
	for i := range want {
		if now.Params[i].Key != want[i].Key || !bytes.Equal(now.Params[i].Value, want[i].Value) {
			t.Fatalf("param %d = %v, want %v", i, now.Params[i], want[i])
		}
	}
}

func TestStripECHMandatoryList(t *testing.T) {
	list := echConfigList(t, []uint16{echConfigVersion}, "cover.example.net")
	alpn := []byte{2, 'h', '2'}
	cases := []struct {
		name          string
		mandatory     []byte
		wantMandatory []byte
	}{
		{"ech listed with alpn", []byte{0, 1, 0, 5}, []byte{0, 1}},
		{"ech listed alone", []byte{0, 5}, nil},
		{"ech not listed", []byte{0, 1}, []byte{0, 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := buildResponse("example.com", TypeHTTPS, []testRR{
				{typ: TypeHTTPS, ttl: 60, rdata: svcbRData(1, map[uint16][]byte{
					svcParamMandatory: tc.mandatory,
					1:                 alpn,
					svcParamECH:       list,
				}, []uint16{svcParamMandatory, 1, svcParamECH})},
			}, false, false)
			out, verdict := StripECH(resp, 0)
			if verdict != FilterRewritten {
				t.Fatalf("verdict = %d", verdict)
			}
			_, an, _, _ := parsedMessage(t, out)
			rr := svcbOf(t, an[0])
			got, ok := rr.GetParam(dnsmessage.SVCParamMandatory)
			if tc.wantMandatory == nil {
				if ok {
					t.Fatalf("mandatory = %x, want it gone with nothing left in it", got)
				}
			} else if !bytes.Equal(got, tc.wantMandatory) {
				t.Fatalf("mandatory = %x, want %x", got, tc.wantMandatory)
			}
			if v, ok := rr.GetParam(dnsmessage.SVCParamALPN); !ok || !bytes.Equal(v, alpn) {
				t.Fatalf("alpn = %x, %v", v, ok)
			}
			if _, ok := rr.GetParam(dnsmessage.SVCParamECH); ok {
				t.Fatal("ech still present")
			}
		})
	}
}

func TestStripECHRebuildsCompressedNames(t *testing.T) {
	list := echConfigList(t, []uint16{echConfigVersion}, "cloudflare-ech.com")
	msg := builtMessage(t, "www.example.com.", dnsmessage.TypeHTTPS, dnsmessage.Header{ID: 7, Response: true, RecursionAvailable: true}, func(b *dnsmessage.Builder) error {
		if err := b.StartAnswers(); err != nil {
			return err
		}
		if err := b.CNAMEResource(rrHeader("www.example.com.", dnsmessage.TypeCNAME), dnsmessage.CNAMEResource{CNAME: dnsmessage.MustNewName("edge.example.net.")}); err != nil {
			return err
		}
		if err := b.HTTPSResource(rrHeader("edge.example.net.", dnsmessage.TypeHTTPS), dnsmessage.HTTPSResource{SVCBResource: dnsmessage.SVCBResource{
			Priority: 1,
			Target:   dnsmessage.MustNewName("."),
			Params: []dnsmessage.SVCParam{
				{Key: dnsmessage.SVCParamALPN, Value: []byte{2, 'h', '3'}},
				{Key: dnsmessage.SVCParamECH, Value: list},
			},
		}}); err != nil {
			return err
		}
		if err := b.StartAuthorities(); err != nil {
			return err
		}
		if err := b.NSResource(rrHeader("example.net.", dnsmessage.TypeNS), dnsmessage.NSResource{NS: dnsmessage.MustNewName("ns1.example.net.")}); err != nil {
			return err
		}
		if err := b.StartAdditionals(); err != nil {
			return err
		}
		if err := b.AResource(rrHeader("edge.example.net.", dnsmessage.TypeA), dnsmessage.AResource{A: [4]byte{192, 0, 2, 1}}); err != nil {
			return err
		}
		if err := b.AAAAResource(rrHeader("edge.example.net.", dnsmessage.TypeAAAA), dnsmessage.AAAAResource{AAAA: netip.MustParseAddr("2001:db8::1").As16()}); err != nil {
			return err
		}
		if err := b.AResource(rrHeader("ns1.example.net.", dnsmessage.TypeA), dnsmessage.AResource{A: [4]byte{192, 0, 2, 53}}); err != nil {
			return err
		}
		var opt dnsmessage.ResourceHeader
		if err := opt.SetEDNS0(1232, dnsmessage.RCodeSuccess, false); err != nil {
			return err
		}
		return b.OPTResource(opt, dnsmessage.OPTResource{})
	})

	out, verdict := StripECH(msg, 0)
	if verdict != FilterRewritten {
		t.Fatalf("verdict = %d", verdict)
	}
	h, an, ns, ar := parsedMessage(t, out)
	if h.ID != 7 || len(an) != 2 || len(ns) != 1 || len(ar) != 4 {
		t.Fatalf("id %d, sections %d/%d/%d, want 7 and 2/1/4", h.ID, len(an), len(ns), len(ar))
	}
	if got := an[0].Body.(*dnsmessage.CNAMEResource).CNAME.String(); got != "edge.example.net." {
		t.Fatalf("cname target %q", got)
	}
	if got := an[1].Header.Name.String(); got != "edge.example.net." {
		t.Fatalf("https owner %q", got)
	}
	rr := svcbOf(t, an[1])
	if _, ok := rr.GetParam(dnsmessage.SVCParamECH); ok {
		t.Fatal("ech still present")
	}
	if v, _ := rr.GetParam(dnsmessage.SVCParamALPN); !bytes.Equal(v, []byte{2, 'h', '3'}) {
		t.Fatalf("alpn = %x", v)
	}
	if got := ns[0].Body.(*dnsmessage.NSResource).NS.String(); got != "ns1.example.net." {
		t.Fatalf("ns target %q", got)
	}
	owners := []string{"edge.example.net.", "edge.example.net.", "ns1.example.net.", "."}
	for i, want := range owners {
		if got := ar[i].Header.Name.String(); got != want {
			t.Fatalf("additional %d owner %q, want %q", i, got, want)
		}
	}
	if ar[3].Header.Type != dnsmessage.TypeOPT || ar[3].Header.Class != 1232 {
		t.Fatalf("opt record changed: %+v", ar[3].Header)
	}
}

func rrsigRData(covered uint16, signer string) []byte {
	rdata := binary.BigEndian.AppendUint16(nil, covered)
	rdata = append(rdata, 13, 2)
	rdata = binary.BigEndian.AppendUint32(rdata, 300)
	rdata = binary.BigEndian.AppendUint32(rdata, 2000000000)
	rdata = binary.BigEndian.AppendUint32(rdata, 1700000000)
	rdata = binary.BigEndian.AppendUint16(rdata, 12345)
	rdata = append(rdata, encodeName(signer)...)
	return append(rdata, bytes.Repeat([]byte{0x5a}, 64)...)
}

func TestStripECHDropsItsSignaturesAndAD(t *testing.T) {
	list := echConfigList(t, []uint16{echConfigVersion}, "cloudflare-ech.com")
	resp := buildResponse("example.com", TypeHTTPS, []testRR{
		{typ: TypeHTTPS, ttl: 60, rdata: svcbRData(1, map[uint16][]byte{svcParamECH: list}, []uint16{svcParamECH})},
		{typ: rrTypeRRSIG, ttl: 60, rdata: rrsigRData(TypeHTTPS, "example.com")},
		{typ: rrTypeRRSIG, ttl: 60, rdata: rrsigRData(rrTypeA, "example.com")},
	}, true, true)
	resp[3] |= headerFlagAD

	out, verdict := StripECH(resp, 0)
	if verdict != FilterRewritten {
		t.Fatalf("verdict = %d", verdict)
	}
	if out[3]&headerFlagAD != 0 {
		t.Fatal("AD still set on an answer b4 changed")
	}
	_, an, _, ar := parsedMessage(t, out)
	if len(an) != 2 || len(ar) != 1 {
		t.Fatalf("%d answers and %d additionals, want 2 and 1", len(an), len(ar))
	}
	sig := an[1].Body.(*dnsmessage.UnknownResource)
	if an[1].Header.Type != dnsmessage.Type(rrTypeRRSIG) || binary.BigEndian.Uint16(sig.Data) != rrTypeA {
		t.Fatalf("kept %v, want only the signature over the A set", an[1].Header.Type)
	}
}

func TestStripECHServiceModeSVCB(t *testing.T) {
	list := echConfigList(t, []uint16{echConfigVersion}, "cover.example.net")
	resp := buildResponse("_dns.resolver.arpa", TypeSVCB, []testRR{
		{typ: TypeSVCB, ttl: 60, rdata: svcbRData(1, map[uint16][]byte{svcParamECH: list}, []uint16{svcParamECH})},
	}, false, false)
	out, verdict := StripECH(resp, 0)
	if verdict != FilterRewritten || ParseResponseECH(out) != nil {
		t.Fatalf("verdict = %d, ech = %x", verdict, ParseResponseECH(out))
	}
}

func TestStripECHLeavesOtherAnswersAlone(t *testing.T) {
	list := echConfigList(t, []uint16{echConfigVersion}, "cover.example.net")
	withECH := func() []byte {
		return buildResponse("example.com", TypeHTTPS, []testRR{
			{typ: TypeHTTPS, ttl: 60, rdata: svcbRData(1, map[uint16][]byte{svcParamECH: list}, []uint16{svcParamECH})},
		}, false, false)
	}
	truncated := withECH()
	truncated[2] |= 0x02
	servfail := withECH()
	servfail[3] |= 0x02
	query := withECH()
	query[2] &^= 0x80
	minfo := buildResponse("example.com", TypeHTTPS, []testRR{
		{typ: TypeHTTPS, ttl: 60, rdata: svcbRData(1, map[uint16][]byte{svcParamECH: list}, []uint16{svcParamECH})},
		{typ: 14, ttl: 60, rdata: append(encodeName("a.example.com"), encodeName("b.example.com")...)},
	}, false, false)
	pointerTarget := append([]byte{0, 1, 0xC0, 0x0C}, svcbRData(1, map[uint16][]byte{svcParamECH: list}, []uint16{svcParamECH})[3:]...)
	compressedTarget := buildResponse("example.com", TypeHTTPS, []testRR{
		{typ: TypeHTTPS, ttl: 60, rdata: pointerTarget},
	}, false, false)
	cases := map[string][]byte{
		"no ech key": buildResponse("example.com", TypeHTTPS, []testRR{
			{typ: TypeHTTPS, ttl: 60, rdata: svcbRData(1, map[uint16][]byte{1: {2, 'h', '2'}}, []uint16{1})},
		}, false, false),
		"address answer":     buildResponse("example.com", rrTypeA, []testRR{aRecord("192.0.2.1")}, false, false),
		"truncated":          truncated,
		"servfail":           servfail,
		"a query":            query,
		"legacy name type":   minfo,
		"compressed target":  compressedTarget,
		"question only":      withECH()[:29],
		"too short to parse": withECH()[:11],
	}
	for name, resp := range cases {
		if out, verdict := StripECH(resp, 0); out != nil || verdict != FilterUnchanged {
			t.Errorf("%s: verdict %d, out %x, want unchanged", name, verdict, out)
		}
	}
}

func TestStripECHMalformedInput(t *testing.T) {
	resp, err := hex.DecodeString(cloudflareHTTPSAnswer)
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < len(resp); n++ {
		if out, verdict := StripECH(resp[:n], 0); out != nil || verdict != FilterUnchanged {
			t.Fatalf("a %d-byte prefix gave verdict %d", n, verdict)
		}
	}
	for i := range resp {
		for _, b := range []byte{0x00, 0x3f, 0xc0, 0xff} {
			mutated := append([]byte(nil), resp...)
			mutated[i] = b
			StripECH(mutated, 0)
		}
	}
}

type testECHConfig struct {
	version    uint16
	kem        uint16
	keyLen     int
	suites     [][2]uint16
	name       string
	extensions []uint16
	noExtField bool
}

func usableECHConfig(name string) testECHConfig {
	return testECHConfig{version: echConfigVersion, kem: echKEMX25519, keyLen: 32, suites: [][2]uint16{{0x0001, 0x0001}}, name: name}
}

func buildECHConfigList(configs ...testECHConfig) []byte {
	var list []byte
	for _, tc := range configs {
		c := []byte{0x07}
		c = binary.BigEndian.AppendUint16(c, tc.kem)
		c = binary.BigEndian.AppendUint16(c, uint16(tc.keyLen))
		c = append(c, bytes.Repeat([]byte{0xab}, tc.keyLen)...)
		c = binary.BigEndian.AppendUint16(c, uint16(4*len(tc.suites)))
		for _, s := range tc.suites {
			c = binary.BigEndian.AppendUint16(c, s[0])
			c = binary.BigEndian.AppendUint16(c, s[1])
		}
		c = append(c, 0, byte(len(tc.name)))
		c = append(c, tc.name...)
		if !tc.noExtField {
			var ext []byte
			for _, typ := range tc.extensions {
				ext = binary.BigEndian.AppendUint16(ext, typ)
				ext = binary.BigEndian.AppendUint16(ext, 0)
			}
			c = binary.BigEndian.AppendUint16(c, uint16(len(ext)))
			c = append(c, ext...)
		}
		list = binary.BigEndian.AppendUint16(list, tc.version)
		list = binary.BigEndian.AppendUint16(list, uint16(len(c)))
		list = append(list, c...)
	}
	return append(binary.BigEndian.AppendUint16(nil, uint16(len(list))), list...)
}

func TestECHPublicNameNamesOnlyAKeyGoCanUse(t *testing.T) {
	with := func(change func(*testECHConfig)) testECHConfig {
		c := usableECHConfig("cover.example.net")
		change(&c)
		return c
	}
	unusable := map[string]testECHConfig{
		"P-256 key exchange":    with(func(c *testECHConfig) { c.kem = 0x0010 }),
		"no supported AEAD":     with(func(c *testECHConfig) { c.suites = [][2]uint16{{0x0001, 0xffff}} }),
		"no supported KDF":      with(func(c *testECHConfig) { c.suites = [][2]uint16{{0x0002, 0x0001}} }),
		"mandatory extension":   with(func(c *testECHConfig) { c.extensions = []uint16{0x8001} }),
		"single-label name":     with(func(c *testECHConfig) { c.name = "localhost" }),
		"hyphen at label start": with(func(c *testECHConfig) { c.name = "-cover.example" }),
		"hyphen at label end":   with(func(c *testECHConfig) { c.name = "cover-.example" }),
		"empty label":           with(func(c *testECHConfig) { c.name = "cover..example" }),
		"underscore":            with(func(c *testECHConfig) { c.name = "co_ver.example" }),
		"non-ASCII letter":      with(func(c *testECHConfig) { c.name = "Key.example" }),
		"short public key":      with(func(c *testECHConfig) { c.keyLen = 31 }),
		"no extensions field":   with(func(c *testECHConfig) { c.noExtField = true }),
	}
	for name, c := range unusable {
		if got := ECHPublicName(buildECHConfigList(c)); got != "" {
			t.Errorf("%s: public name %q, want none: Go's client fails on this key before sending anything", name, got)
		}
	}

	if got := ECHPublicName(buildECHConfigList(with(func(c *testECHConfig) {
		c.suites = [][2]uint16{{0x0001, 0xffff}, {0x0001, 0x0003}}
		c.extensions = []uint16{0x0001}
	}))); got != "cover.example.net" {
		t.Errorf("a supported suite after an unsupported one and an optional extension: %q", got)
	}
	if got := ECHPublicName(buildECHConfigList(
		with(func(c *testECHConfig) { c.kem = 0x0010; c.name = "skipped.example" }),
		usableECHConfig("used.example"),
	)); got != "used.example" {
		t.Errorf("the client skips a config it cannot use and takes the next: %q", got)
	}
	if got := ECHPublicName(buildECHConfigList(
		with(func(c *testECHConfig) { c.keyLen = 31; c.name = "picked.example" }),
		usableECHConfig("never.example"),
	)); got != "" {
		t.Errorf("Go fails on the bad key of the config it picked, it does not try the next: %q", got)
	}
	list := buildECHConfigList(usableECHConfig("cover.example.net"))
	if got := ECHPublicName(append(list, 0)); got != "" {
		t.Errorf("a list longer than its length prefix is malformed: %q", got)
	}
}

func TestResponseECHPublicNamesListsEveryKey(t *testing.T) {
	resp := buildResponse("example.com", TypeHTTPS, []testRR{
		{typ: TypeHTTPS, ttl: 60, rdata: svcbRData(1, map[uint16][]byte{svcParamECH: buildECHConfigList(usableECHConfig("a.example"))}, []uint16{svcParamECH})},
		{typ: TypeHTTPS, ttl: 60, rdata: svcbRData(1, map[uint16][]byte{svcParamECH: buildECHConfigList(
			testECHConfig{version: echConfigVersion, kem: 0x0010, keyLen: 65, suites: [][2]uint16{{1, 1}}, name: "B.Example"},
			usableECHConfig("a.example"),
		)}, []uint16{svcParamECH})},
		{typ: TypeHTTPS, ttl: 60, rdata: svcbRData(2, map[uint16][]byte{svcParamECH: {0, 9, 1}}, []uint16{svcParamECH})},
	}, false, false)
	if got := ResponseECHPublicNames(resp); !slices.Equal(got, []string{"a.example", "b.example"}) {
		t.Fatalf("names = %v, want every public name of every key, lowercased, a malformed list skipped", got)
	}

	cover := buildECHConfigList(usableECHConfig("additional.example"))
	msg := builtMessage(t, "example.com.", dnsmessage.TypeHTTPS, dnsmessage.Header{ID: 9, Response: true}, func(b *dnsmessage.Builder) error {
		if err := b.StartAnswers(); err != nil {
			return err
		}
		if err := b.HTTPSResource(rrHeader("example.com.", dnsmessage.TypeHTTPS), dnsmessage.HTTPSResource{SVCBResource: dnsmessage.SVCBResource{
			Priority: 0,
			Target:   dnsmessage.MustNewName("svc.example.net."),
		}}); err != nil {
			return err
		}
		if err := b.StartAdditionals(); err != nil {
			return err
		}
		return b.HTTPSResource(rrHeader("svc.example.net.", dnsmessage.TypeHTTPS), dnsmessage.HTTPSResource{SVCBResource: dnsmessage.SVCBResource{
			Priority: 1,
			Target:   dnsmessage.MustNewName("."),
			Params:   []dnsmessage.SVCParam{{Key: dnsmessage.SVCParamECH, Value: cover}},
		}})
	})
	if got := ResponseECHPublicNames(msg); !slices.Equal(got, []string{"additional.example"}) {
		t.Fatalf("names = %v, want the key of the record in the additional section", got)
	}
	if ParseResponseECH(msg) != nil {
		t.Fatal("the site's own key comes from the answer section only")
	}
}

func goStartsECHHandshake(t *testing.T, list []byte) bool {
	t.Helper()
	client, server := net.Pipe()
	conn := tls.Client(client, &tls.Config{
		ServerName:                     "site.example",
		MinVersion:                     tls.VersionTLS13,
		EncryptedClientHelloConfigList: list,
	})
	handshake := make(chan error, 1)
	go func() { handshake <- conn.Handshake() }()
	wrote := make(chan bool, 1)
	go func() {
		buf := make([]byte, 5)
		_, err := server.Read(buf)
		wrote <- err == nil
	}()

	var started bool
	select {
	case <-handshake:
		started = false
		client.Close()
		server.Close()
		<-wrote
	case started = <-wrote:
		client.Close()
		server.Close()
		<-handshake
	case <-time.After(5 * time.Second):
		t.Fatal("the client neither failed nor sent a ClientHello")
	}
	return started
}

func TestECHPublicNameAgreesWithGo(t *testing.T) {
	with := func(change func(*testECHConfig)) []byte {
		c := usableECHConfig("cover.example.net")
		change(&c)
		return buildECHConfigList(c)
	}
	lists := map[string][]byte{
		"usable":                with(func(*testECHConfig) {}),
		"P-256 key exchange":    with(func(c *testECHConfig) { c.kem = 0x0010 }),
		"no supported AEAD":     with(func(c *testECHConfig) { c.suites = [][2]uint16{{0x0001, 0xffff}} }),
		"no supported KDF":      with(func(c *testECHConfig) { c.suites = [][2]uint16{{0x0002, 0x0001}} }),
		"ChaCha20 only":         with(func(c *testECHConfig) { c.suites = [][2]uint16{{0x0001, 0x0003}} }),
		"mandatory extension":   with(func(c *testECHConfig) { c.extensions = []uint16{0x8001} }),
		"optional extension":    with(func(c *testECHConfig) { c.extensions = []uint16{0x0001} }),
		"single-label name":     with(func(c *testECHConfig) { c.name = "localhost" }),
		"hyphen at label end":   with(func(c *testECHConfig) { c.name = "cover-.example" }),
		"non-ASCII letter":      with(func(c *testECHConfig) { c.name = "Key.example" }),
		"short public key":      with(func(c *testECHConfig) { c.keyLen = 31 }),
		"no extensions field":   with(func(c *testECHConfig) { c.noExtField = true }),
		"unusable then usable":  buildECHConfigList(testECHConfig{version: echConfigVersion, kem: 0x0010, keyLen: 65, suites: [][2]uint16{{1, 1}}, name: "a.example"}, usableECHConfig("b.example")),
		"bad key then usable":   buildECHConfigList(testECHConfig{version: echConfigVersion, kem: echKEMX25519, keyLen: 31, suites: [][2]uint16{{1, 1}}, name: "a.example"}, usableECHConfig("b.example")),
		"unknown version only":  buildECHConfigList(testECHConfig{version: 0xfe0c, kem: echKEMX25519, keyLen: 32, suites: [][2]uint16{{1, 1}}, name: "a.example"}),
		"length prefix too big": append(with(func(*testECHConfig) {}), 0),
	}
	for name, list := range lists {
		if named, started := ECHPublicName(list) != "", goStartsECHHandshake(t, list); named != started {
			t.Errorf("%s: ECHPublicName names a key %v, Go's client starts a handshake %v", name, named, started)
		}
	}
}
