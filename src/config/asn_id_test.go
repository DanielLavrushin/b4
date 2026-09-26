package config

import (
	"reflect"
	"testing"
)

func TestNormalizeASNAcceptsCommonSpellings(t *testing.T) {
	cases := map[string]string{
		"15169":        "15169",
		" 15169 ":      "15169",
		"AS15169":      "15169",
		"as15169":      "15169",
		"As15169":      "15169",
		"ASN15169":     "15169",
		"asn 15169":    "15169",
		"AS 13335":     "13335",
		"015169":       "15169",
		"1":            "1",
		"23455":        "23455",
		"23457":        "23457",
		"64495":        "64495",
		"131072":       "131072",
		"4199999999":   "4199999999",
		"AS4199999999": "4199999999",
	}
	for in, want := range cases {
		got, ok := NormalizeASN(in)
		if !ok || got != want {
			t.Errorf("NormalizeASN(%q) = %q, %v; want %q, true", in, got, ok, want)
		}
	}
}

func TestNormalizeASNRejectsReservedAndMalformed(t *testing.T) {
	for _, in := range []string{
		"",
		"   ",
		"AS",
		"ASN",
		"0",
		"AS0",
		"23456",
		"64496",
		"64511",
		"64512",
		"65534",
		"65535",
		"65536",
		"65551",
		"131071",
		"4200000000",
		"4294967294",
		"4294967295",
		"4294967296",
		"99999999999999999999",
		"-15169",
		"+15169",
		"15169.5",
		"1.10",
		"AS15169x",
		"x15169",
		"AS-15169",
		"15 169",
		"ASß",
		"0x3b41",
	} {
		if got, ok := NormalizeASN(in); ok {
			t.Errorf("NormalizeASN(%q) = %q, true; want rejected", in, got)
		}
	}
}

func TestNormalizeASNsDedupsKeepsOrderDropsInvalid(t *testing.T) {
	got := NormalizeASNs([]string{"AS13335", "15169", "bogus", "13335", "as15169", "64512", "AS32934"})
	want := []string{"13335", "15169", "32934"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeASNs = %v, want %v", got, want)
	}
	if got := NormalizeASNs(nil); got == nil || len(got) != 0 {
		t.Fatalf("NormalizeASNs(nil) = %#v, want an empty non-nil slice", got)
	}
}
