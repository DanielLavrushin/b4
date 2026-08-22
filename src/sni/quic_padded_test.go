package sni

import "testing"

func qCoalescedHandshake(dcid []byte) []byte {
	p := []byte{0xE0, 0x00, 0x00, 0x00, 0x01, byte(len(dcid))}
	p = append(p, dcid...)
	p = append(p, 0x00)
	body := make([]byte, 96)
	for i := range body {
		body[i] = byte(i + 1)
	}
	p = append(p, qvarint(1+len(body))...)
	p = append(p, 0x00)
	return append(p, body...)
}

func TestParseQUICClientHelloSNIIgnoresBytesPastPacketLength(t *testing.T) {
	tests := []struct {
		name string
		dcid []byte
		host string
		tail func(dcid []byte) []byte
	}{
		{
			name: "datagram padded after the packet",
			dcid: []byte{0xF0, 1, 2, 3, 4, 5, 6, 7},
			host: "padded.example.com",
			tail: func([]byte) []byte { return make([]byte, 320) },
		},
		{
			name: "coalesced handshake packet",
			dcid: []byte{0xF1, 1, 2, 3, 4, 5, 6, 7},
			host: "coalesced.example.com",
			tail: qCoalescedHandshake,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pkt := qBuildInitial(t, tc.dcid, qBuildClientHello([]byte(tc.host)))
			datagram := append(append([]byte(nil), pkt...), tc.tail(tc.dcid)...)

			got, ok := ParseQUICClientHelloSNI(datagram)
			if !ok || got != tc.host {
				t.Fatalf("ParseQUICClientHelloSNI = %q, %v; want %q, true", got, ok, tc.host)
			}
		})
	}
}
