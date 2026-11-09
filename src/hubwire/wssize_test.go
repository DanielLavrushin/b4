package hubwire

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestWSSizeHelloCrossesAndChangesFingerprint(t *testing.T) {
	set := config.NewSetConfig()
	set.Name = "hello cutoff"
	set.Targets.SNIDomains = []string{"example.org"}
	set.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 1, Scale: 1}
	first, _, err := Build(&set, BuildOptions{B4Version: "1.91.0", Engine: "nfqueue"})
	if err != nil {
		t.Fatal(err)
	}
	set.TCP.WSSize.Cutoff = "client_hello"
	hello, _, err := Build(&set, BuildOptions{B4Version: "1.91.0", Engine: "nfqueue"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint == hello.Fingerprint || hello.MinB4 != "1.91.0" {
		t.Fatal("cutoff missing from strategy identity or version guard")
	}
	imported, err := Open(hello, OpenOptions{B4Version: "1.91.0"})
	if err != nil {
		t.Fatal(err)
	}
	if imported.Set.TCP.WSSize != set.TCP.WSSize || imported.Fingerprint != hello.Fingerprint {
		t.Fatal("window, scale or cutoff changed during hub import")
	}
	older, err := Open(hello, OpenOptions{B4Version: "1.90.0"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := warningCodes(older.Warnings)["version_too_old"]; !ok {
		t.Fatal("older router was not warned about the cutoff")
	}
}
