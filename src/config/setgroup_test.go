package config

import (
	"slices"
	"strings"
	"testing"
)

func TestNormalizeSetGroup(t *testing.T) {
	for in, want := range map[string]string{
		"":                             "",
		"   ":                          "",
		"  You   Tube \t":              "You Tube",
		strings.Repeat("я", 70):        strings.Repeat("я", MaxSetGroupRunes),
		strings.Repeat("a", 63) + " b": strings.Repeat("a", 63),
	} {
		if got := NormalizeSetGroup(in); got != want {
			t.Errorf("NormalizeSetGroup(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateNormalizesTheGroup(t *testing.T) {
	cfg := NewConfig()
	set := NewSetConfig()
	set.Id, set.Name, set.Group = "a", "a", "  yt  "
	cfg.Sets = []*SetConfig{&set}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if set.Group != "yt" {
		t.Errorf("group = %q, want it trimmed", set.Group)
	}
}

func TestUniqueSetGroupNumbersATakenName(t *testing.T) {
	long := strings.Repeat("a", MaxSetGroupRunes)
	sets := []*SetConfig{{Group: "YouTube"}, {Group: "youtube 2"}, {Group: long}}
	if got := UniqueSetGroup("youtube", sets); got != "youtube 3" {
		t.Errorf("got %q, want youtube 3", got)
	}
	if got := UniqueSetGroup("discord", sets); got != "discord" {
		t.Errorf("got %q, want the free name kept", got)
	}
	got := UniqueSetGroup(long, sets)
	if got == long || NormalizeSetGroup(got) != got {
		t.Errorf("got %q, want a numbered name that survives normalization", got)
	}
}

func TestResetToDefaultsKeepsTheGroup(t *testing.T) {
	set := NewSetConfig()
	set.Group = "yt"
	set.ResetToDefaults()
	if set.Group != "yt" {
		t.Errorf("group = %q after a reset, want it kept", set.Group)
	}
}

func TestGatherSetGroupKeepsEveryOtherSetInPlace(t *testing.T) {
	mk := func(spec string) []*SetConfig {
		var out []*SetConfig
		for _, item := range strings.Fields(spec) {
			id, group, _ := strings.Cut(item, ":")
			out = append(out, &SetConfig{Id: id, Group: group})
		}
		return out
	}
	ids := func(sets []*SetConfig) string {
		var out []string
		for _, s := range sets {
			out = append(out, s.Id)
		}
		return strings.Join(out, " ")
	}
	cases := []struct{ sets, group, anchor, want string }{
		{"a b:g c d:g", "g", "", "a b d c"},
		{"a b:g c d:g", "g", "d", "a c b d"},
		{"a b:g c d:g", "g", "missing", "a b d c"},
		{"a b:g c", "g", "", "a b c"},
		{"a b:g c d:g", "", "", "a b c d"},
		{"x:h a:g y:h b:g", "g", "", "x a b y"},
	}
	for _, tc := range cases {
		in := mk(tc.sets)
		before := slices.Clone(in)
		if got := ids(GatherSetGroup(in, tc.group, tc.anchor)); got != tc.want {
			t.Errorf("GatherSetGroup(%q, %q, %q) = %q, want %q", tc.sets, tc.group, tc.anchor, got, tc.want)
		}
		if !slices.Equal(in, before) {
			t.Errorf("GatherSetGroup(%q) changed its input", tc.sets)
		}
	}
}
