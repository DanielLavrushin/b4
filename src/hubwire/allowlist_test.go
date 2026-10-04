package hubwire

import (
	"sort"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestDoHAllowlistIsSortedAndComplete(t *testing.T) {
	hosts := DoHAllowlist()
	if len(hosts) != len(config.KnownDoHHosts) {
		t.Fatalf("allowlist has %d hosts, the table has %d", len(hosts), len(config.KnownDoHHosts))
	}
	if !sort.StringsAreSorted(hosts) {
		t.Errorf("allowlist must be sorted")
	}
	for _, host := range hosts {
		if !config.KnownDoHHosts[host] {
			t.Errorf("host %q is not in the table", host)
		}
	}
	if _, known := DoHHost("https://" + hosts[0] + "/dns-query"); !known {
		t.Errorf("every allowlisted host must be recognised by DoHHost")
	}
}
