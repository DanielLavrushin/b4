package watchdog

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestHealChangeListNamesTheWSSizeBlock(t *testing.T) {
	tuned := config.NewSetConfig()
	tuned.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 1, Scale: 6}
	discovered := config.NewSetConfig()

	changes := describeSetChanges(&tuned, &discovered)
	if len(changes) != 1 || !strings.Contains(changes[0], "tcp.wssize 1:6 -> off") {
		t.Fatalf("a heal that switches the real TCP window rewrite off must say so, got %q", changes)
	}

	leftovers := config.NewSetConfig()
	leftovers.TCP.WSSize = config.WSSizeConfig{Window: 1, Scale: 6}
	if changes := describeSetChanges(&leftovers, &discovered); len(changes) != 0 {
		t.Fatalf("numbers left behind with the switch off are not a change, got %q", changes)
	}

	same := tuned
	if changes := describeSetChanges(&tuned, &same); len(changes) != 0 {
		t.Fatalf("a strategy that keeps the block is not a change, got %q", changes)
	}

	idle := config.NewSetConfig()
	idle.TCP.WSSize = config.WSSizeConfig{Enabled: true}
	if changes := describeSetChanges(&idle, &discovered); len(changes) != 1 || !strings.Contains(changes[0], "tcp.wssize -:- -> off") {
		t.Fatalf("a heal that switches a block with no numbers off must say so, got %q", changes)
	}
}
