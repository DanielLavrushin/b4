package handler

import "testing"

func TestGenerateMessageStatesNoByteCountItDoesNotKnow(t *testing.T) {
	for _, c := range []struct {
		size int
		want string
	}{
		{0, "Generated a TLS ClientHello for claim.generate.example"},
		{-1, "Generated a TLS ClientHello for claim.generate.example"},
		{662, "Generated a 662-byte TLS ClientHello for claim.generate.example"},
	} {
		if got := generatedPayloadMessage("claim.generate.example", c.size); got != c.want {
			t.Errorf("size %d: message = %q, want %q", c.size, got, c.want)
		}
	}
}
