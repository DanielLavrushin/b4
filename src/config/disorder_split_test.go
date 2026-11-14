package config

import "testing"

func TestValidateKeepsTheDisorderSplitPositionInRange(t *testing.T) {
	for _, c := range []struct{ stored, want int }{
		{-1, 0},
		{-100000, 0},
		{0, 0},
		{256, 256},
		{MaxDisorderSplitPosition, MaxDisorderSplitPosition},
		{MaxDisorderSplitPosition + 1, MaxDisorderSplitPosition},
		{100000, MaxDisorderSplitPosition},
	} {
		cfg := NewConfig()
		set := NewSetConfig()
		set.Id, set.Name, set.Enabled = "s", "split", true
		set.Targets.SNIDomains = []string{"example.org"}
		set.Fragmentation.Strategy = "disorder"
		set.Fragmentation.MiddleSNI = false
		set.Fragmentation.Disorder.SplitPosition = c.stored
		cfg.Sets = []*SetConfig{&set}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("split position %d: a value out of range is brought into range, not refused: %v", c.stored, err)
		}
		if got := cfg.Sets[0].Fragmentation.Disorder.SplitPosition; got != c.want {
			t.Errorf("split position %d is kept as %d after Validate, want %d", c.stored, got, c.want)
		}
	}
}
