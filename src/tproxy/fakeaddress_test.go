package tproxy

import (
	"net"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestAFakeAddressWithNoNameIsClosedAtOnce(t *testing.T) {
	up := startMockSocks(t, 0, func(byte) byte { return 0 })
	l := newTestListener(t, up.port(), fakeNames{})
	set := &config.SetConfig{}
	set.DNS.FakeIP = config.FakeIPConfig{Enabled: true, CIDR: "198.18.0.0/15"}
	l.set.Store(set)

	if !l.fakeAddressWithoutName(net.ParseIP("198.18.1.5"), "") {
		t.Fatal("a fake address with no name was not recognised")
	}
	if l.fakeAddressWithoutName(net.ParseIP("198.18.1.5"), "site.example") {
		t.Fatal("a fake address that carries a name is a real dial")
	}
	if l.fakeAddressWithoutName(net.ParseIP("203.0.113.5"), "") {
		t.Fatal("an address outside the block was treated as fake")
	}
	if l.fakeAddressWithoutName(nil, "") {
		t.Fatal("a missing destination was treated as fake")
	}

	set.DNS.FakeIP.Enabled = false
	if l.fakeAddressWithoutName(net.ParseIP("198.18.1.5"), "") {
		t.Fatal("fake DNS is off, so nothing may be dropped")
	}
}
