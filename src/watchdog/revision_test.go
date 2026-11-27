package watchdog

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestSetRevisionIgnoresTheGroup(t *testing.T) {
	set := config.NewSetConfig()
	set.Id, set.Name = "a", "a"
	plain := SetRevision(&set)

	set.Group = "YouTube"
	if got := SetRevision(&set); got != plain {
		t.Errorf("revision with a group = %s, want %s: moving a set between groups is not an edit of the set", got, plain)
	}
	if set.Group != "YouTube" {
		t.Errorf("group = %q, want it left on the set", set.Group)
	}

	set.Name = "b"
	if SetRevision(&set) == plain {
		t.Error("the revision did not change with the set's name")
	}
}
