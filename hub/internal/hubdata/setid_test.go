package hubdata

import "testing"

func TestTheStartOfASetIDTellsItApart(t *testing.T) {
	const shownInTheConsole = 8
	seen := map[string]bool{}
	for range 500 {
		id, err := NewSetID()
		if err != nil {
			t.Fatal(err)
		}
		if !ValidSetID(id) {
			t.Fatalf("id %q must be valid", id)
		}
		short := id[:shownInTheConsole]
		if seen[short] {
			t.Fatalf("two ids begin with %q, so they share a time and look the same in the console", short)
		}
		seen[short] = true
	}
}
