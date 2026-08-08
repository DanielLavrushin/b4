package tables

import (
	"reflect"
	"testing"
)

func TestZeroPrefixHalves(t *testing.T) {
	tests := []struct {
		entry string
		want  []string
	}{
		{"0.0.0.0/0", zeroPrefixHalvesV4},
		{" 0.0.0.0/0 ", zeroPrefixHalvesV4},
		{"::/0", zeroPrefixHalvesV6},
		{"0.0.0.0/1", nil},
		{"0.0.0.0/32", nil},
		{"10.0.0.0/8", nil},
		{"1.2.3.4", nil},
		{"::/1", nil},
		{"2001:db8::/32", nil},
		{"", nil},
		{"garbage/0", nil},
	}

	for _, tt := range tests {
		got := zeroPrefixHalves(tt.entry)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("zeroPrefixHalves(%q) = %v, want %v", tt.entry, got, tt.want)
		}
	}
}

func TestExpandZeroPrefix(t *testing.T) {
	t.Run("passes through untouched when no catch-all", func(t *testing.T) {
		in := []string{"1.2.3.4", "10.0.0.0/8"}
		got := expandZeroPrefix(in)
		if !reflect.DeepEqual(got, in) {
			t.Errorf("expandZeroPrefix(%v) = %v, want unchanged", in, got)
		}
	})

	t.Run("splits v4 catch-all in place", func(t *testing.T) {
		got := expandZeroPrefix([]string{"1.2.3.4", "0.0.0.0/0", "10.0.0.0/8"})
		want := []string{"1.2.3.4", "0.0.0.0/1", "128.0.0.0/1", "10.0.0.0/8"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("splits v6 catch-all", func(t *testing.T) {
		got := expandZeroPrefix([]string{"::/0"})
		want := []string{"::/1", "8000::/1"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("does not repeat halves for duplicate catch-alls", func(t *testing.T) {
		got := expandZeroPrefix([]string{"0.0.0.0/0", "0.0.0.0/0", "::/0"})
		want := []string{"0.0.0.0/1", "128.0.0.0/1", "::/1", "8000::/1"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("does not mutate the input slice", func(t *testing.T) {
		in := []string{"0.0.0.0/0", "1.2.3.4"}
		expandZeroPrefix(in)
		if in[0] != "0.0.0.0/0" || in[1] != "1.2.3.4" {
			t.Errorf("input mutated: %v", in)
		}
	})
}
