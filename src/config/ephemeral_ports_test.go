package config

import "testing"

func TestTCPPortsOverlapEphemeral(t *testing.T) {
	cases := []struct {
		name    string
		sets    []*SetConfig
		want    string
		overlap bool
	}{
		{name: "no sets keeps 443 only", want: "", overlap: false},
		{name: "a filter of 443", sets: []*SetConfig{{Enabled: true, TCP: TCPConfig{DPortFilter: "443"}}}, want: "", overlap: false},
		{name: "a range below the client source ports", sets: []*SetConfig{{Enabled: true, TCP: TCPConfig{DPortFilter: "443,10000-20000"}}}, want: "", overlap: false},
		{name: "the port just below the Linux range", sets: []*SetConfig{{Enabled: true, TCP: TCPConfig{DPortFilter: "32767"}}}, want: "", overlap: false},
		{name: "every port", sets: []*SetConfig{{Enabled: true, TCP: TCPConfig{DPortFilter: "1-65535"}}}, want: "32768-65535", overlap: true},
		{name: "every port written with a colon", sets: []*SetConfig{{Enabled: true, TCP: TCPConfig{DPortFilter: "1:65535"}}}, want: "32768-65535", overlap: true},
		{name: "the first port of the Linux range", sets: []*SetConfig{{Enabled: true, TCP: TCPConfig{DPortFilter: "32768"}}}, want: "32768", overlap: true},
		{name: "a single port in both ranges", sets: []*SetConfig{{Enabled: true, TCP: TCPConfig{DPortFilter: "443,50000"}}}, want: "50000", overlap: true},
		{name: "a port only Windows and Apple systems use", sets: []*SetConfig{{Enabled: true, TCP: TCPConfig{DPortFilter: "65000"}}}, want: "65000", overlap: true},
		{name: "a range reaching into the Linux range", sets: []*SetConfig{{Enabled: true, TCP: TCPConfig{DPortFilter: "30000-33000"}}}, want: "32768-33000", overlap: true},
		{name: "two overlapping pieces", sets: []*SetConfig{{Enabled: true, TCP: TCPConfig{DPortFilter: "61000,50000-50010"}}}, want: "50000-50010,61000", overlap: true},
		{
			name: "filters of two sets",
			sets: []*SetConfig{
				{Enabled: true, TCP: TCPConfig{DPortFilter: "40000"}},
				{Enabled: true, TCP: TCPConfig{DPortFilter: "20000-35000"}},
			},
			want:    "32768-35000,40000",
			overlap: true,
		},
		{name: "a disabled set", sets: []*SetConfig{{Enabled: false, TCP: TCPConfig{DPortFilter: "1-65535"}}}, want: "", overlap: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := &Config{Sets: c.sets}
			got, overlap := cfg.TCPPortsOverlapEphemeral()
			if got != c.want || overlap != c.overlap {
				t.Fatalf("TCPPortsOverlapEphemeral() = (%q, %v), want (%q, %v)", got, overlap, c.want, c.overlap)
			}
		})
	}
}

func TestTCPPortsOverlapEphemeralLeavesUDPFiltersOut(t *testing.T) {
	cfg := &Config{Sets: []*SetConfig{{Enabled: true, UDP: UDPConfig{DPortFilter: "1-65535"}}}}
	if got, overlap := cfg.TCPPortsOverlapEphemeral(); overlap {
		t.Fatalf("a UDP filter does not decide the TCP direction, got (%q, %v)", got, overlap)
	}
}
