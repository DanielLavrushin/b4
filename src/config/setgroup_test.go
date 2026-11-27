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

func TestSettleSetGroupFollowsTheNeighbours(t *testing.T) {
	cases := []struct {
		name, sets string
		at         int
		want       string
	}{
		{"between two sets of a group it joins", "a:yt x b:yt", 1, "yt"},
		{"between two sets of a group it leaves its own", "a:yt x:dc b:yt d:dc", 1, "yt"},
		{"next to its own group it stays", "a:yt x:yt c", 1, "yt"},
		{"away from its group it leaves", "a:yt c x:yt", 2, ""},
		{"at the edge of another group it stays loose", "a:yt x c", 1, ""},
		{"between two groups that are not its own it leaves", "a:yt x:old b:dc o:old", 1, ""},
		{"the only set of its group keeps the group", "a x:solo b", 1, "solo"},
		{"the last place next to its group", "a b:yt x:yt", 2, "yt"},
	}
	for _, tc := range cases {
		var sets []*SetConfig
		for _, item := range strings.Fields(tc.sets) {
			id, group, _ := strings.Cut(item, ":")
			sets = append(sets, &SetConfig{Id: id, Group: group})
		}
		SettleSetGroup(sets, tc.at)
		if got := sets[tc.at].Group; got != tc.want {
			t.Errorf("%s: %q at %d has group %q, want %q", tc.name, tc.sets, tc.at, got, tc.want)
		}
	}
	SettleSetGroup(nil, 0)
	SettleSetGroup([]*SetConfig{{Id: "a", Group: "yt"}}, 3)
}

func TestSettleSetGroupReadsANameTheWayItIsSaved(t *testing.T) {
	cases := []struct {
		name string
		sets []*SetConfig
		at   int
		want string
	}{
		{"a padded name away from its group leaves", []*SetConfig{{Id: "x", Group: " yt  "}, {Id: "c"}, {Id: "a", Group: "yt"}}, 0, ""},
		{"a padded name next to its group stays, trimmed", []*SetConfig{{Id: "a", Group: "yt"}, {Id: "x", Group: " yt "}}, 1, "yt"},
		{"another case next to the group takes its spelling", []*SetConfig{{Id: "a", Group: "YouTube"}, {Id: "x", Group: "youtube"}}, 1, "YouTube"},
		{"another case away from the group leaves", []*SetConfig{{Id: "x", Group: "youtube"}, {Id: "c"}, {Id: "a", Group: "YouTube"}}, 0, ""},
		{"an exact name wins over another case", []*SetConfig{{Id: "a", Group: "yt"}, {Id: "x", Group: "YT"}, {Id: "b", Group: "YT"}}, 1, "YT"},
		{"the only set of a padded group keeps it, trimmed", []*SetConfig{{Id: "x", Group: "  solo "}, {Id: "c"}}, 0, "solo"},
	}
	for _, tc := range cases {
		SettleSetGroup(tc.sets, tc.at)
		if got := tc.sets[tc.at].Group; got != tc.want {
			t.Errorf("%s: group %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestGatherSetGroupsPullsEveryGroupTogether(t *testing.T) {
	for in, want := range map[string]string{
		"a:yt c b:yt":                "a b c",
		"m:two n:one o:two p:one":    "m o n p",
		"a b:yt c d:yt e:dc f g:dc":  "a b d c e g f",
		"a:yt b:yt c d:dc e:dc":      "a b c d e",
		"a b c":                      "a b c",
		"x:yt a:dc y:yt b:dc z:yt c": "x y z a b c",
	} {
		var sets []*SetConfig
		for _, item := range strings.Fields(in) {
			id, group, _ := strings.Cut(item, ":")
			sets = append(sets, &SetConfig{Id: id, Group: group})
		}
		before := slices.Clone(sets)
		var got []string
		for _, set := range GatherSetGroups(sets) {
			got = append(got, set.Id)
		}
		if strings.Join(got, " ") != want {
			t.Errorf("GatherSetGroups(%q) = %q, want %q", in, strings.Join(got, " "), want)
		}
		if !slices.Equal(sets, before) {
			t.Errorf("GatherSetGroups(%q) changed the list it was given", in)
		}
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
