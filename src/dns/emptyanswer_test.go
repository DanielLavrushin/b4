package dns

import (
	"encoding/binary"
	"net"
	"testing"
)

func aaaaRecord(ip string) testRR {
	return testRR{typ: rrTypeAAAA, ttl: 3600, rdata: net.ParseIP(ip).To16()}
}

func TestBuildEmptyAnswerKeepsQuestionAndClearsAnswers(t *testing.T) {
	resp := buildResponse("youtube.com", rrTypeAAAA, []testRR{
		aaaaRecord("2a00:1450:4010:c0d::5d"),
		aaaaRecord("2a00:1450:4010:c0d::88"),
	}, false, false)

	out := BuildEmptyAnswer(resp)
	if out == nil {
		t.Fatal("expected an empty NOERROR answer")
	}

	if binary.BigEndian.Uint16(out[0:2]) != binary.BigEndian.Uint16(resp[0:2]) {
		t.Errorf("transaction ID was not preserved")
	}
	if out[2]&0x80 == 0 {
		t.Errorf("QR bit is not set")
	}
	if rcode := out[3] & 0x0F; rcode != RcodeNoError {
		t.Errorf("RCODE = %d, want NOERROR", rcode)
	}
	if got := binary.BigEndian.Uint16(out[4:6]); got != 1 {
		t.Errorf("QDCOUNT = %d, want 1", got)
	}
	for name, off := range map[string]int{"ANCOUNT": 6, "NSCOUNT": 8, "ARCOUNT": 10} {
		if got := binary.BigEndian.Uint16(out[off : off+2]); got != 0 {
			t.Errorf("%s = %d, want 0", name, got)
		}
	}
	if len(ParseResponseIPs(out)) != 0 {
		t.Errorf("the empty answer still carries addresses")
	}

	domain, ok := ParseQueryDomain(out)
	if !ok || domain != "youtube.com" {
		t.Errorf("question = %q ok=%v, want youtube.com", domain, ok)
	}
	qtype, ok := QuestionType(out)
	if !ok || qtype != rrTypeAAAA {
		t.Errorf("QTYPE = %d ok=%v, want AAAA", qtype, ok)
	}
}

func TestBuildEmptyAnswerClearsTruncationAndKeepsRecursionDesired(t *testing.T) {
	resp := buildResponse("youtube.com", rrTypeAAAA, []testRR{aaaaRecord("2a00:1450::1")}, false, false)
	resp[2] |= 0x02
	resp[2] |= 0x01

	out := BuildEmptyAnswer(resp)
	if out == nil {
		t.Fatal("expected an empty NOERROR answer")
	}
	if out[2]&0x02 != 0 {
		t.Errorf("TC bit survived")
	}
	if out[2]&0x01 == 0 {
		t.Errorf("RD bit was dropped")
	}
	if out[3]&0x80 == 0 {
		t.Errorf("RA bit is not set")
	}
}

func TestBuildEmptyAnswerWorksFromAQuery(t *testing.T) {
	query := BuildQuery("googlevideo.com", 0x4242, rrTypeAAAA)

	out := BuildEmptyAnswer(query)
	if out == nil {
		t.Fatal("expected an empty NOERROR answer built from the query")
	}
	if out[2]&0x80 == 0 {
		t.Errorf("QR bit is not set")
	}
	domain, ok := ParseQueryDomain(out)
	if !ok || domain != "googlevideo.com" {
		t.Errorf("question = %q ok=%v, want googlevideo.com", domain, ok)
	}
}

func TestBuildEmptyAnswerRejectsMalformedMessages(t *testing.T) {
	if out := BuildEmptyAnswer(nil); out != nil {
		t.Errorf("expected no answer for an empty message")
	}
	if out := BuildEmptyAnswer(make([]byte, 8)); out != nil {
		t.Errorf("expected no answer for a truncated header")
	}

	noQuestion := make([]byte, 12)
	binary.BigEndian.PutUint16(noQuestion[2:4], 0x8180)
	if out := BuildEmptyAnswer(noQuestion); out != nil {
		t.Errorf("expected no answer when QDCOUNT is 0")
	}
}

func TestFilterAnswerIPsDropsAAAAKeepsA(t *testing.T) {
	resp := buildResponse("youtube.com", rrTypeA, []testRR{
		aRecord("142.250.74.14"),
		aaaaRecord("2a00:1450:4010:c0d::5d"),
	}, false, false)

	out, verdict := FilterAnswerIPs(resp, 0, 0, func(ip net.IP) bool { return ip.To4() == nil })
	if verdict != FilterRewritten {
		t.Fatalf("verdict = %d, want FilterRewritten", verdict)
	}
	ips := ParseResponseIPs(out)
	if len(ips) != 1 || ips[0].String() != "142.250.74.14" {
		t.Errorf("addresses = %v, want [142.250.74.14]", ips)
	}
}

func TestFilterAnswerIPsReportsAllDroppedForAAAAOnlyAnswer(t *testing.T) {
	resp := buildResponse("youtube.com", rrTypeAAAA, []testRR{
		aaaaRecord("2a00:1450:4010:c0d::5d"),
		aaaaRecord("2a00:1450:4010:c0d::88"),
	}, false, false)

	out, verdict := FilterAnswerIPs(resp, 0, 0, func(ip net.IP) bool { return ip.To4() == nil })
	if verdict != FilterAllDropped {
		t.Fatalf("verdict = %d, want FilterAllDropped", verdict)
	}
	if out != nil {
		t.Errorf("FilterAllDropped must not hand back a message to send")
	}
}
