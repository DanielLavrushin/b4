package hubwire

import (
	"sort"
	"testing"
)

func TestDoHAllowlistIsSortedAndComplete(t *testing.T) {
	hosts := DoHAllowlist()
	if len(hosts) != len(knownDoHHosts) {
		t.Fatalf("allowlist has %d hosts, the table has %d", len(hosts), len(knownDoHHosts))
	}
	if !sort.StringsAreSorted(hosts) {
		t.Errorf("allowlist must be sorted")
	}
	for _, host := range hosts {
		if !knownDoHHosts[host] {
			t.Errorf("host %q is not in the table", host)
		}
	}
	if _, known := DoHHost("https://" + hosts[0] + "/dns-query"); !known {
		t.Errorf("every allowlisted host must be recognised by DoHHost")
	}
}
