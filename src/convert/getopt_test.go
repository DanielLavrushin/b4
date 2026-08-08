package convert

import "testing"

func testTable(t *testing.T, version string) *optionTable {
	t.Helper()
	all, err := loadSpecs()
	if err != nil {
		t.Fatal(err)
	}
	spec, ok := all["byedpi"]
	if !ok {
		t.Fatal("byedpi spec not found")
	}
	return spec.tableFor(version)
}

func TestGetoptLong_Forms(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want []string
	}{
		{"attachedShortArg", []string{"-d1:11+sm"}, []string{"disorder=1:11+sm"}},
		{"separateShortArg", []string{"-d", "1"}, []string{"disorder=1"}},
		{"negativeValueAttached", []string{"-f-1"}, []string{"fake=-1"}},
		{"noArgShort", []string{"-S"}, []string{"md5sig="}},
		{"clusteredNoArg", []string{"-SY"}, []string{"md5sig=", "drop_sack="}},
		{"clusterEndsAtArgOption", []string{"-Sd1"}, []string{"md5sig=", "disorder=1"}},
		{"longWithEquals", []string{"--disorder=1"}, []string{"disorder=1"}},
		{"longWithSpace", []string{"--disorder", "1"}, []string{"disorder=1"}},
		{"longPrefixAbbrev", []string{"--disord=1"}, []string{"disorder=1"}},
		{"enumArg", []string{"-At,r,s"}, []string{"auto=t,r,s"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toks := getoptLong(tt.argv, testTable(t, "0.17"), false)
			if len(toks) != len(tt.want) {
				t.Fatalf("got %d tokens %+v, want %d", len(toks), toks, len(tt.want))
			}
			for i, w := range tt.want {
				got := toks[i].Key + "=" + toks[i].Value
				if got != w {
					t.Fatalf("token %d: got %q, want %q", i, got, w)
				}
			}
		})
	}
}

func TestGetoptLong_Errors(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want string
	}{
		{"unknownShort", []string{"-Ω"}, "unknown"},
		{"missingValue", []string{"-d"}, "missing_value"},
		{"operand", []string{"stray"}, "operand"},
		{"unexpectedValue", []string{"--md5sig=1"}, "unexpected_value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toks := getoptLong(tt.argv, testTable(t, "0.17"), false)
			if len(toks) == 0 {
				t.Fatal("expected at least one token")
			}
			if toks[0].Err != tt.want {
				t.Fatalf("got err %q, want %q", toks[0].Err, tt.want)
			}
		})
	}
}

func TestGetoptLong_VersionScopedOptions(t *testing.T) {
	v13 := getoptLong([]string{"-Qr"}, testTable(t, "0.13"), false)
	if v13[0].Err != "unknown" {
		t.Fatalf("expected -Q to be unknown in 0.13, got %+v", v13[0])
	}
	v17 := getoptLong([]string{"-Qr"}, testTable(t, "0.17"), false)
	if v17[0].Key != "fake_tls_mod" {
		t.Fatalf("expected -Q to resolve in 0.17, got %+v", v17[0])
	}

	n13 := getoptLong([]string{"-n", "example.com"}, testTable(t, "0.13"), false)
	if n13[0].Key != "tls_sni" {
		t.Fatalf("expected -n to be tls_sni in 0.13, got %q", n13[0].Key)
	}
	n17 := getoptLong([]string{"-n", "example.com"}, testTable(t, "0.17"), false)
	if n17[0].Key != "fake_sni" {
		t.Fatalf("expected -n to be fake_sni in 0.17, got %q", n17[0].Key)
	}
}

func TestDetectVersion_Markers(t *testing.T) {
	all, err := loadSpecs()
	if err != nil {
		t.Fatal(err)
	}
	spec := all["byedpi"]
	tests := []struct {
		name     string
		argv     []string
		want     string
		detected bool
	}{
		{"fakeTLSMod", []string{"-Qr"}, "0.17", true},
		{"ipOpt", []string{"-k"}, "0.13", true},
		{"posRepeats", []string{"-d1:11+sm"}, "0.17", true},
		{"ambiguousFallsBackToDefault", []string{"-s1", "-f-1"}, "0.17", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, detected := detectVersion(spec, tt.argv)
			if got != tt.want || detected != tt.detected {
				t.Fatalf("got (%s, %v), want (%s, %v)", got, detected, tt.want, tt.detected)
			}
		})
	}
}
