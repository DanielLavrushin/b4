package dns

import (
	"encoding/binary"
	"net"
	"testing"
)

func TestBuildQueryWithECSAppendsTheClientSubnetOption(t *testing.T) {
	_, subnet, err := net.ParseCIDR("90.100.0.0/16")
	if err != nil {
		t.Fatal(err)
	}
	plain := BuildQuery("www.instagram.com", 0x1234, 1)
	query := BuildQueryWithECS("www.instagram.com", 0x1234, 1, *subnet)

	if binary.BigEndian.Uint16(query[10:12]) != 1 {
		t.Fatalf("ARCOUNT must announce the OPT record, got %d", binary.BigEndian.Uint16(query[10:12]))
	}
	if string(query[12:len(plain)]) != string(plain[12:]) {
		t.Fatal("the question section must be untouched")
	}

	opt := query[len(plain):]
	if opt[0] != 0 {
		t.Fatalf("OPT owner name must be the root, got %d", opt[0])
	}
	if typ := binary.BigEndian.Uint16(opt[1:3]); typ != optRecordType {
		t.Fatalf("OPT type = %d, want %d", typ, optRecordType)
	}
	rdLen := int(binary.BigEndian.Uint16(opt[9:11]))
	rdata := opt[11:]
	if len(rdata) != rdLen {
		t.Fatalf("RDLENGTH %d does not match %d bytes of RDATA", rdLen, len(rdata))
	}
	if code := binary.BigEndian.Uint16(rdata[0:2]); code != ecsOptionCode {
		t.Fatalf("option code = %d, want %d", code, ecsOptionCode)
	}
	if optLen := binary.BigEndian.Uint16(rdata[2:4]); optLen != 6 {
		t.Fatalf("a /16 carries two address bytes, so the option length is 6, got %d", optLen)
	}
	if family := binary.BigEndian.Uint16(rdata[4:6]); family != ecsFamilyIPv4 {
		t.Fatalf("family = %d, want IPv4", family)
	}
	if rdata[6] != 16 || rdata[7] != 0 {
		t.Fatalf("source prefix %d scope %d, want 16 and 0", rdata[6], rdata[7])
	}
	if rdata[8] != 90 || rdata[9] != 100 {
		t.Fatalf("address bytes = %v, want 90.100", rdata[8:10])
	}
	if ips := ParseResponseIPs(query); len(ips) != 0 {
		t.Fatalf("a query carries no answers, got %v", ips)
	}
}

func TestBuildQueryWithECSLeavesAnInvalidSubnetOut(t *testing.T) {
	plain := BuildQuery("example.com", 7, 1)
	query := BuildQueryWithECS("example.com", 7, 1, net.IPNet{})
	if string(query) != string(plain) {
		t.Fatal("no address means no option")
	}
}
