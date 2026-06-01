package utils

import (
	"net"
	"testing"
)

func TestFilterUniqueStrings(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "empty slice",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "no duplicates",
			input:    []string{"a", "b", "c"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "all duplicates",
			input:    []string{"a", "a", "a"},
			expected: []string{"a"},
		},
		{
			name:     "some duplicates",
			input:    []string{"a", "b", "a", "c", "b", "d"},
			expected: []string{"a", "b", "c", "d"},
		},
		{
			name:     "preserves order",
			input:    []string{"z", "a", "z", "m"},
			expected: []string{"z", "a", "m"},
		},
		{
			name:     "single element",
			input:    []string{"x"},
			expected: []string{"x"},
		},
		{
			name:     "nil input",
			input:    nil,
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilterUniqueStrings(tt.input)

			if len(result) != len(tt.expected) {
				t.Fatalf("length mismatch: got %d, want %d", len(result), len(tt.expected))
			}

			for i, v := range tt.expected {
				if result[i] != v {
					t.Errorf("result[%d] = %q, want %q", i, result[i], v)
				}
			}
		})
	}
}

func TestValidatePorts(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Empty/basic
		{name: "empty string", input: "", expected: ""},
		{name: "single port", input: "80", expected: "80"},
		{name: "single port with spaces", input: " 443 ", expected: "443"},

		// Multiple ports
		{name: "multiple ports", input: "80,443,8080", expected: "80,443,8080"},
		{name: "multiple with spaces", input: "80, 443, 8080", expected: "80,443,8080"},

		// Ranges with dash
		{name: "port range dash", input: "1000-2000", expected: "1000-2000"},
		{name: "port range with spaces", input: " 1000 - 2000 ", expected: "1000-2000"},

		// Ranges with colon (converted to dash)
		{name: "port range colon", input: "1000:2000", expected: "1000-2000"},

		// Mixed
		{name: "mixed ports and ranges", input: "80,443,1000-2000,8080", expected: "80,443,1000-2000,8080"},

		// Edge cases - valid
		{name: "port 1", input: "1", expected: "1"},
		{name: "port 65535", input: "65535", expected: "65535"},
		{name: "range 1-65535", input: "1-65535", expected: "1-65535"},

		// Invalid - filtered out
		{name: "port 0", input: "0", expected: ""},
		{name: "port 65536", input: "65536", expected: ""},
		{name: "negative port", input: "-1", expected: ""},
		{name: "non-numeric", input: "abc", expected: ""},
		{name: "range start >= end", input: "2000-1000", expected: ""},
		{name: "range equal", input: "1000-1000", expected: ""},
		{name: "invalid range format", input: "1000-2000-3000", expected: ""},
		{name: "range with invalid start", input: "abc-2000", expected: ""},
		{name: "range with invalid end", input: "1000-abc", expected: ""},
		{name: "range out of bounds", input: "0-65536", expected: ""},

		// Mixed valid and invalid
		{name: "mixed valid invalid", input: "80,invalid,443", expected: "80,443"},
		{name: "mixed valid invalid range", input: "80,2000-1000,443", expected: "80,443"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidatePorts(tt.input)
			if result != tt.expected {
				t.Errorf("ValidatePorts(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestRandUint16(t *testing.T) {
	const samples = 2000
	var orBits, andBits uint16 = 0x0000, 0xffff
	first := RandUint16()
	allSame := true
	for i := 0; i < samples; i++ {
		v := RandUint16()
		orBits |= v
		andBits &= v
		if v != first {
			allSame = false
		}
	}
	if allSame {
		t.Fatal("RandUint16 returned the same value every time")
	}
	if orBits != 0xffff {
		t.Errorf("RandUint16 never set some bits across %d samples: OR=%#04x (byte-packing bug?)", samples, orBits)
	}
	if andBits != 0x0000 {
		t.Errorf("RandUint16 had bits always set across %d samples: AND=%#04x", samples, andBits)
	}
}

func TestRandUint32(t *testing.T) {
	const samples = 4000
	var orBits, andBits uint32 = 0x00000000, 0xffffffff
	first := RandUint32()
	allSame := true
	for i := 0; i < samples; i++ {
		v := RandUint32()
		orBits |= v
		andBits &= v
		if v != first {
			allSame = false
		}
	}
	if allSame {
		t.Fatal("RandUint32 returned the same value every time")
	}
	if orBits != 0xffffffff {
		t.Errorf("RandUint32 never set some bits across %d samples: OR=%#08x (byte-packing bug?)", samples, orBits)
	}
	if andBits != 0x00000000 {
		t.Errorf("RandUint32 had bits always set across %d samples: AND=%#08x", samples, andBits)
	}
}

func TestSlicesAreEqual(t *testing.T) {
	tests := []struct {
		name     string
		a, b     []string
		expected bool
	}{
		{name: "both empty", a: []string{}, b: []string{}, expected: true},
		{name: "both nil", a: nil, b: nil, expected: true},
		{name: "identical", a: []string{"a", "b", "c"}, b: []string{"a", "b", "c"}, expected: true},
		{name: "same elements different order", a: []string{"a", "b", "c"}, b: []string{"c", "a", "b"}, expected: true},
		{name: "different length", a: []string{"a", "b"}, b: []string{"a", "b", "c"}, expected: false},
		{name: "same length different element", a: []string{"a", "b"}, b: []string{"a", "x"}, expected: false},
		{name: "empty vs non-empty", a: []string{}, b: []string{"a"}, expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SlicesAreEqual(tt.a, tt.b); got != tt.expected {
				t.Errorf("SlicesAreEqual(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		expected bool
	}{
		{name: "10.x", ip: "10.0.0.1", expected: true},
		{name: "172.16 low boundary", ip: "172.16.0.1", expected: true},
		{name: "172.31 high boundary", ip: "172.31.255.255", expected: true},
		{name: "172.15 below range", ip: "172.15.0.1", expected: false},
		{name: "172.32 above range", ip: "172.32.0.1", expected: false},
		{name: "192.168", ip: "192.168.1.1", expected: true},
		{name: "192.167 not private", ip: "192.167.1.1", expected: false},
		{name: "loopback v4", ip: "127.0.0.1", expected: true},
		{name: "public v4 8.8.8.8", ip: "8.8.8.8", expected: false},
		{name: "public v4 1.1.1.1", ip: "1.1.1.1", expected: false},
		{name: "loopback v6", ip: "::1", expected: true},
		{name: "link-local v6", ip: "fe80::1", expected: true},
		{name: "ULA v6", ip: "fd00::1", expected: true},
		{name: "public v6", ip: "2001:4860:4860::8888", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("could not parse %q", tt.ip)
			}
			if got := IsPrivateIP(ip); got != tt.expected {
				t.Errorf("IsPrivateIP(%s) = %v, want %v", tt.ip, got, tt.expected)
			}
		})
	}

	if IsPrivateIP(nil) {
		t.Error("IsPrivateIP(nil) = true, want false")
	}
}

func BenchmarkFilterUniqueStrings(b *testing.B) {
	input := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		input[i] = string(rune('a' + (i % 26)))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		FilterUniqueStrings(input)
	}
}

func BenchmarkValidatePorts(b *testing.B) {
	input := "80,443,8080,1000-2000,3000-4000,5000,6000,7000-8000"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ValidatePorts(input)
	}
}
