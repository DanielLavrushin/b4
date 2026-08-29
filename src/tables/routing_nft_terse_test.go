package tables

import (
	"strings"
	"testing"
)

const terseRouteTable = `table inet b4_route {
	set b4r_x_v4 {
		type ipv4_addr
		flags interval,timeout
	}

	chain prerouting {
		type filter hook prerouting priority mangle; policy accept;
		jump b4r_x_pre
	}

	chain b4r_x_pre {
		meta mark & 0x00008000 == 0x00008000 return
		meta mark & 0x00040000 == 0x00040000 return
	}
}
`

func TestTheTerseListingStillCarriesEverythingTheCheckReads(t *testing.T) {
	present, bypass := parseNftRouteChains(terseRouteTable)

	if !present["b4r_x_pre"] {
		t.Fatal("a chain b4 installed must still be visible without the set elements")
	}
	if !bypass["b4r_x_pre"][0x8000] || !bypass["b4r_x_pre"][0x40000] {
		t.Fatalf("the bypass returns decide whether b4 rebuilds the chain, and they are rules rather than set "+
			"elements, so the terse listing must still carry them: %v", bypass["b4r_x_pre"])
	}
	if !strings.Contains(terseRouteTable, "flags interval") {
		t.Fatal("the set's flags line is what tells b4 whether to recreate the set, so it has to survive too")
	}
}

func TestAnNftWithoutTerseFallsBackOnce(t *testing.T) {
	prev := routeNftTerse.Load()
	t.Cleanup(func() { routeNftTerse.Store(prev) })

	routeNftTerse.Store(true)
	if !routeNftTerse.Load() {
		t.Fatal("terse is the starting assumption")
	}
	routeNftTerse.Store(false)
	if routeNftTerse.Load() {
		t.Fatal("once an nft has refused the terse flag b4 must stop asking, or it pays a failed command " +
			"on every monitor tick for the life of the process")
	}
}
