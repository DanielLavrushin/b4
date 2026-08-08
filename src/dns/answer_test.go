package dns

import (
	"encoding/binary"
	"fmt"
	"net"
	"testing"
)

type testRR struct {
	name  string
	typ   uint16
	ttl   uint32
	rdata []byte
}

func buildResponse(qname string, qtype uint16, answers []testRR, opt bool, dnssec bool) []byte {
	msg := make([]byte, 12)
	binary.BigEndian.PutUint16(msg[0:2], 0x1234)
	binary.BigEndian.PutUint16(msg[2:4], 0x8180)
	binary.BigEndian.PutUint16(msg[4:6], 1)
	binary.BigEndian.PutUint16(msg[6:8], uint16(len(answers)))

	msg = append(msg, encodeName(qname)...)
	var q [4]byte
	binary.BigEndian.PutUint16(q[0:2], qtype)
	binary.BigEndian.PutUint16(q[2:4], 1)
	msg = append(msg, q[:]...)

	for _, rr := range answers {
		if rr.name == "" || rr.name == qname {
			msg = append(msg, 0xC0, 0x0C)
		} else {
			msg = append(msg, encodeName(rr.name)...)
		}
		var fixed [10]byte
		binary.BigEndian.PutUint16(fixed[0:2], rr.typ)
		binary.BigEndian.PutUint16(fixed[2:4], 1)
		binary.BigEndian.PutUint32(fixed[4:8], rr.ttl)
		binary.BigEndian.PutUint16(fixed[8:10], uint16(len(rr.rdata)))
		msg = append(msg, fixed[:]...)
		msg = append(msg, rr.rdata...)
	}

	if opt {
		binary.BigEndian.PutUint16(msg[10:12], 1)
		flags := uint16(0)
		if dnssec {
			flags = ednsDNSSECOK
		}
		optRR := make([]byte, 11)
		optRR[0] = 0
		binary.BigEndian.PutUint16(optRR[1:3], rrTypeOPT)
		binary.BigEndian.PutUint16(optRR[3:5], 4096)
		binary.BigEndian.PutUint16(optRR[7:9], flags)
		msg = append(msg, optRR...)
	}

	return msg
}

func aRecord(ip string) testRR {
	return testRR{typ: rrTypeA, ttl: 3600, rdata: net.ParseIP(ip).To4()}
}

func dropList(ips ...string) func(net.IP) bool {
	set := make(map[string]bool, len(ips))
	for _, ip := range ips {
		set[ip] = true
	}
	return func(ip net.IP) bool { return set[ip.String()] }
}

func TestFilterAnswerIPsDropsOneAddress(t *testing.T) {
	resp := buildResponse("raw.githubusercontent.com", rrTypeA, []testRR{
		aRecord("185.199.108.133"),
		aRecord("185.199.109.133"),
		aRecord("185.199.110.133"),
		aRecord("185.199.111.133"),
	}, false, false)

	out, verdict := FilterAnswerIPs(resp, 60, 0, dropList("185.199.110.133"))
	if verdict != FilterRewritten {
		t.Fatalf("verdict = %d, want FilterRewritten", verdict)
	}
	if got := binary.BigEndian.Uint16(out[6:8]); got != 3 {
		t.Errorf("ANCOUNT = %d, want 3", got)
	}

	ips := ParseResponseIPs(out)
	if len(ips) != 3 {
		t.Fatalf("kept %d addresses, want 3", len(ips))
	}
	for _, ip := range ips {
		if ip.String() == "185.199.110.133" {
			t.Errorf("unreachable address survived the filter")
		}
	}
	if domain, ok := ParseQueryDomain(out); !ok || domain != "raw.githubusercontent.com" {
		t.Errorf("question section damaged: %q ok=%v", domain, ok)
	}
}

func TestFilterAnswerIPsClampsTTL(t *testing.T) {
	resp := buildResponse("example.com", rrTypeA, []testRR{
		aRecord("1.1.1.1"),
		aRecord("2.2.2.2"),
	}, false, false)

	out, verdict := FilterAnswerIPs(resp, 60, 0, dropList("2.2.2.2"))
	if verdict != FilterRewritten {
		t.Fatalf("verdict = %d, want FilterRewritten", verdict)
	}

	end, ok := skipDNSName(out, 12)
	if !ok {
		t.Fatal("question name unreadable")
	}
	rrStart, ok := skipDNSName(out, end+4)
	if !ok {
		t.Fatal("answer name unreadable")
	}
	if ttl := binary.BigEndian.Uint32(out[rrStart+4 : rrStart+8]); ttl != 60 {
		t.Errorf("TTL = %d, want it clamped to 60", ttl)
	}
}

func TestFilterAnswerIPsAllDropped(t *testing.T) {
	resp := buildResponse("example.com", rrTypeA, []testRR{
		aRecord("1.1.1.1"),
		aRecord("2.2.2.2"),
	}, false, false)

	out, verdict := FilterAnswerIPs(resp, 60, 0, dropList("1.1.1.1", "2.2.2.2"))
	if verdict != FilterAllDropped {
		t.Fatalf("verdict = %d, want FilterAllDropped", verdict)
	}
	if out != nil {
		t.Errorf("expected no rewritten message when every address is dropped")
	}
}

func TestFilterAnswerIPsNothingToDrop(t *testing.T) {
	resp := buildResponse("example.com", rrTypeA, []testRR{aRecord("1.1.1.1")}, false, false)

	out, verdict := FilterAnswerIPs(resp, 60, 0, dropList("9.9.9.9"))
	if verdict != FilterUnchanged || out != nil {
		t.Fatalf("verdict = %d out=%v, want FilterUnchanged and no rewrite", verdict, out)
	}
}

func TestFilterAnswerIPsKeepsCNAMEChain(t *testing.T) {
	resp := buildResponse("www.example.com", rrTypeA, []testRR{
		{name: "www.example.com", typ: rrTypeCNAME, ttl: 300, rdata: encodeName("cdn.example.net")},
		{name: "cdn.example.net", typ: rrTypeA, ttl: 300, rdata: net.ParseIP("1.1.1.1").To4()},
		{name: "cdn.example.net", typ: rrTypeA, ttl: 300, rdata: net.ParseIP("2.2.2.2").To4()},
	}, false, false)

	out, verdict := FilterAnswerIPs(resp, 60, 0, dropList("2.2.2.2"))
	if verdict != FilterRewritten {
		t.Fatalf("verdict = %d, want FilterRewritten", verdict)
	}
	if got := binary.BigEndian.Uint16(out[6:8]); got != 2 {
		t.Fatalf("ANCOUNT = %d, want 2 (CNAME plus one address)", got)
	}

	end, _ := skipDNSName(out, 12)
	cnameStart, ok := skipDNSName(out, end+4)
	if !ok {
		t.Fatal("first answer name unreadable")
	}
	if typ := binary.BigEndian.Uint16(out[cnameStart : cnameStart+2]); typ != rrTypeCNAME {
		t.Fatalf("first answer type = %d, want CNAME", typ)
	}
	rdLen := int(binary.BigEndian.Uint16(out[cnameStart+8 : cnameStart+10]))
	target, _, targetOK := readName(out, cnameStart+10)
	if !targetOK || target != "cdn.example.net" {
		t.Errorf("CNAME target = %q ok=%v, want cdn.example.net", target, targetOK)
	}

	addrStart, ok := skipDNSName(out, cnameStart+10+rdLen)
	if !ok {
		t.Fatal("second answer name unreadable")
	}
	name, _, _ := readName(out, cnameStart+10+rdLen)
	if name != "cdn.example.net" {
		t.Errorf("address record name = %q, want cdn.example.net", name)
	}
	if typ := binary.BigEndian.Uint16(out[addrStart : addrStart+2]); typ != rrTypeA {
		t.Errorf("second answer type = %d, want A", typ)
	}

	ips := ParseResponseIPs(out)
	if len(ips) != 1 || ips[0].String() != "1.1.1.1" {
		t.Errorf("addresses = %v, want [1.1.1.1]", ips)
	}
}

func TestFilterAnswerIPsPreservesOPT(t *testing.T) {
	resp := buildResponse("example.com", rrTypeA, []testRR{
		aRecord("1.1.1.1"),
		aRecord("2.2.2.2"),
	}, true, false)

	out, verdict := FilterAnswerIPs(resp, 60, 0, dropList("2.2.2.2"))
	if verdict != FilterRewritten {
		t.Fatalf("verdict = %d, want FilterRewritten", verdict)
	}
	if got := binary.BigEndian.Uint16(out[10:12]); got != 1 {
		t.Errorf("ARCOUNT = %d, want the OPT record kept", got)
	}
	if out[len(out)-11] != 0 || binary.BigEndian.Uint16(out[len(out)-10:len(out)-8]) != rrTypeOPT {
		t.Errorf("OPT record not intact at the tail of the message")
	}
}

func TestFilterAnswerIPsBailsOnDNSSEC(t *testing.T) {
	resp := buildResponse("example.com", rrTypeA, []testRR{
		aRecord("1.1.1.1"),
		aRecord("2.2.2.2"),
	}, true, true)

	if _, verdict := FilterAnswerIPs(resp, 60, 0, dropList("2.2.2.2")); verdict != FilterUnchanged {
		t.Errorf("verdict = %d, want FilterUnchanged when the client asked for DNSSEC", verdict)
	}
}

func TestFilterAnswerIPsBailsOnUnsafeInput(t *testing.T) {
	base := buildResponse("example.com", rrTypeA, []testRR{
		aRecord("1.1.1.1"),
		aRecord("2.2.2.2"),
	}, false, false)

	cases := map[string][]byte{
		"empty":     nil,
		"short":     base[:8],
		"truncated": append([]byte(nil), base[:len(base)-3]...),
	}
	for name, msg := range cases {
		if _, verdict := FilterAnswerIPs(msg, 60, 0, dropList("2.2.2.2")); verdict != FilterUnchanged {
			t.Errorf("%s: verdict = %d, want FilterUnchanged", name, verdict)
		}
	}

	query := append([]byte(nil), base...)
	query[2] &^= 0x80
	if _, verdict := FilterAnswerIPs(query, 60, 0, dropList("2.2.2.2")); verdict != FilterUnchanged {
		t.Errorf("query: verdict = %d, want FilterUnchanged", verdict)
	}

	truncatedFlag := append([]byte(nil), base...)
	truncatedFlag[2] |= 0x02
	if _, verdict := FilterAnswerIPs(truncatedFlag, 60, 0, dropList("2.2.2.2")); verdict != FilterUnchanged {
		t.Errorf("TC set: verdict = %d, want FilterUnchanged", verdict)
	}

	nxdomain := append([]byte(nil), base...)
	nxdomain[3] |= 0x03
	if _, verdict := FilterAnswerIPs(nxdomain, 60, 0, dropList("2.2.2.2")); verdict != FilterUnchanged {
		t.Errorf("NXDOMAIN: verdict = %d, want FilterUnchanged", verdict)
	}

	withAuthority := append([]byte(nil), base...)
	binary.BigEndian.PutUint16(withAuthority[8:10], 1)
	if _, verdict := FilterAnswerIPs(withAuthority, 60, 0, dropList("2.2.2.2")); verdict != FilterUnchanged {
		t.Errorf("authority section: verdict = %d, want FilterUnchanged", verdict)
	}

	if _, verdict := FilterAnswerIPs(base, 60, 0, nil); verdict != FilterUnchanged {
		t.Errorf("nil predicate: verdict = %d, want FilterUnchanged", verdict)
	}
}

func TestBuildAnswerFromIPs(t *testing.T) {
	resp := buildResponse("example.com", rrTypeA, []testRR{aRecord("1.1.1.1")}, false, false)

	out := BuildAnswerFromIPs(resp, 60, []net.IP{net.ParseIP("9.9.9.9"), net.ParseIP("8.8.8.8")})
	if out == nil {
		t.Fatal("BuildAnswerFromIPs returned nothing")
	}
	if got := binary.BigEndian.Uint16(out[6:8]); got != 2 {
		t.Errorf("ANCOUNT = %d, want 2", got)
	}
	ips := ParseResponseIPs(out)
	if len(ips) != 2 || ips[0].String() != "9.9.9.9" || ips[1].String() != "8.8.8.8" {
		t.Errorf("addresses = %v, want [9.9.9.9 8.8.8.8]", ips)
	}
	if domain, ok := ParseQueryDomain(out); !ok || domain != "example.com" {
		t.Errorf("question = %q ok=%v, want example.com", domain, ok)
	}
	if out[2]&0x80 == 0 {
		t.Errorf("response bit not set")
	}
}

func TestBuildAnswerFromIPsSkipsWrongFamily(t *testing.T) {
	resp := buildResponse("example.com", rrTypeA, []testRR{aRecord("1.1.1.1")}, false, false)

	if out := BuildAnswerFromIPs(resp, 60, []net.IP{net.ParseIP("2001:db8::1")}); out != nil {
		t.Errorf("expected no answer when only the wrong address family is available")
	}
}

func TestQuestionType(t *testing.T) {
	resp := buildResponse("example.com", rrTypeAAAA, nil, false, false)
	qtype, ok := QuestionType(resp)
	if !ok || qtype != rrTypeAAAA {
		t.Errorf("QuestionType = %d ok=%v, want AAAA", qtype, ok)
	}
}

func buildCompressedChain(qname, cname string, opt bool, addrs int) []byte {
	msg := make([]byte, 12)
	binary.BigEndian.PutUint16(msg[0:2], 0x1234)
	binary.BigEndian.PutUint16(msg[2:4], 0x8180)
	binary.BigEndian.PutUint16(msg[4:6], 1)
	binary.BigEndian.PutUint16(msg[6:8], uint16(addrs+1))

	msg = append(msg, encodeName(qname)...)
	var q [4]byte
	binary.BigEndian.PutUint16(q[0:2], rrTypeAAAA)
	binary.BigEndian.PutUint16(q[2:4], 1)
	msg = append(msg, q[:]...)

	cnameRD := encodeName(cname)
	msg = append(msg, 0xC0, 0x0C)
	var cfixed [10]byte
	binary.BigEndian.PutUint16(cfixed[0:2], rrTypeCNAME)
	binary.BigEndian.PutUint16(cfixed[2:4], 1)
	binary.BigEndian.PutUint32(cfixed[4:8], 300)
	binary.BigEndian.PutUint16(cfixed[8:10], uint16(len(cnameRD)))
	msg = append(msg, cfixed[:]...)
	cnameAt := len(msg)
	msg = append(msg, cnameRD...)

	for i := 0; i < addrs; i++ {
		msg = append(msg, byte(0xC0|cnameAt>>8), byte(cnameAt))
		var fixed [10]byte
		binary.BigEndian.PutUint16(fixed[0:2], rrTypeAAAA)
		binary.BigEndian.PutUint16(fixed[2:4], 1)
		binary.BigEndian.PutUint32(fixed[4:8], 300)
		binary.BigEndian.PutUint16(fixed[8:10], 16)
		msg = append(msg, fixed[:]...)
		msg = append(msg, net.ParseIP(fmt.Sprintf("2001:db8::%d", i+1)).To16()...)
	}

	if opt {
		binary.BigEndian.PutUint16(msg[10:12], 1)
		optRR := make([]byte, 11)
		binary.BigEndian.PutUint16(optRR[1:3], rrTypeOPT)
		binary.BigEndian.PutUint16(optRR[3:5], 4096)
		msg = append(msg, optRR...)
	}
	return msg
}

func TestFilterAnswerIPsDoesNotGrowACompressedChain(t *testing.T) {
	const cname = "a-rather-long-edge-hostname.global.cdn-provider.example.net"
	resp := buildCompressedChain("www.example.com", cname, false, 12)

	out, verdict := FilterAnswerIPs(resp, 60, 0, dropList("2001:db8::1"))
	if verdict != FilterRewritten {
		t.Fatalf("verdict = %d, want FilterRewritten", verdict)
	}
	if len(out) > len(resp) {
		t.Errorf("rewrite grew from %d to %d bytes; repeated names must stay compressed or a plain UDP answer can be pushed past 512", len(resp), len(out))
	}
	if got := len(ParseResponseIPs(out)); got != 11 {
		t.Fatalf("kept %d addresses, want 11", got)
	}

	end, _ := skipDNSName(out, 12)
	cnameStart, _ := skipDNSName(out, end+4)
	rdLen := int(binary.BigEndian.Uint16(out[cnameStart+8 : cnameStart+10]))
	name, _, ok := readName(out, cnameStart+10+rdLen)
	if !ok || name != cname {
		t.Errorf("first address record name = %q ok=%v, want %q: the compression pointer must resolve", name, ok, cname)
	}
}

func TestResponseSizeLimit(t *testing.T) {
	cases := []struct{ maxSize, edns, want int }{
		{MaxTCPMessageSize, 0, MaxTCPMessageSize},
		{MaxTCPMessageSize, 4096, MaxTCPMessageSize},
		{0, 4096, 4096},
		{0, 0, minUDPResponse},
		{0, 200, minUDPResponse},
	}
	for _, c := range cases {
		if got := responseSizeLimit(c.maxSize, c.edns); got != c.want {
			t.Errorf("responseSizeLimit(%d, %d) = %d, want %d", c.maxSize, c.edns, got, c.want)
		}
	}
}
