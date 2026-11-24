package hubdata

import (
	"testing"
	"time"
)

func TestSetIDCarriesOnlyTheDay(t *testing.T) {
	day := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
	seen := map[string]bool{}
	for _, at := range []time.Time{day, day.Add(9*time.Hour + 41*time.Minute + 7*time.Second + 123*time.Millisecond), day.Add(24*time.Hour - time.Millisecond)} {
		id, err := NewSetID(at)
		if err != nil {
			t.Fatal(err)
		}
		if !ValidSetID(id) || seen[id] {
			t.Fatalf("id %q must be valid and new", id)
		}
		seen[id] = true
		raw, err := crockford.DecodeString(id)
		if err != nil {
			t.Fatal(err)
		}
		var ms int64
		for _, b := range raw[:6] {
			ms = ms<<8 | int64(b)
		}
		if got := time.UnixMilli(ms).UTC(); !got.Equal(day) {
			t.Errorf("an id made at %s must carry only its day, got %s", at, got)
		}
	}
}
