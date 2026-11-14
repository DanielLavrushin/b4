package hubwire

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestBuildOfADisorderSetAsksForTheReleaseThatCutsAsItSays(t *testing.T) {
	for _, c := range []struct {
		name    string
		shuffle string
		split   int
		want    string
	}{
		{"full shuffle, its own cuts", "full", 0, BaselineVersion},
		{"reverse order, its own cuts", "reverse", 0, BaselineVersion},
		{"a split position", "full", 256, "1.91.0"},
		{"in order", "none", 0, "1.91.0"},
		{"in order with a split position", "none", 512, "1.91.0"},
	} {
		set := config.NewSetConfig()
		set.Name = "Disorder"
		set.Targets.SNIDomains = []string{"example.com"}
		set.Fragmentation.Strategy = "disorder"
		set.Fragmentation.MiddleSNI = false
		set.Fragmentation.Disorder.ShuffleMode = c.shuffle
		set.Fragmentation.Disorder.SplitPosition = c.split
		env, _, err := Build(&set, BuildOptions{B4Version: "1.91.0"})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if env.MinB4 != c.want {
			t.Errorf("%s: the envelope asks for b4 %s, want %s", c.name, env.MinB4, c.want)
		}
	}
}
