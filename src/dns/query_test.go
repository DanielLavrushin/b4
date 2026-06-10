package dns

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestBuildAQueryDelegatesToBuildQuery(t *testing.T) {
	if !bytes.Equal(BuildAQuery("example.com", 0x1234), BuildQuery("example.com", 0x1234, 1)) {
		t.Fatal("BuildAQuery must equal BuildQuery with qtype=1 (A)")
	}
}

func TestBuildQueryQType(t *testing.T) {
	cases := []struct {
		qtype uint16
	}{{1}, {28}}
	for _, c := range cases {
		q := BuildQuery("example.com", 0, c.qtype)
		got := binary.BigEndian.Uint16(q[len(q)-4 : len(q)-2])
		if got != c.qtype {
			t.Errorf("qtype byte = %d, want %d", got, c.qtype)
		}
		if qclass := binary.BigEndian.Uint16(q[len(q)-2:]); qclass != 1 {
			t.Errorf("qclass = %d, want 1 (IN)", qclass)
		}
	}
}
