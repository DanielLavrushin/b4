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
