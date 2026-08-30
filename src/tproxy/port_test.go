package tproxy

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

const bypassBit uint32 = 0x8000

func TestMarkForSet_RangeNeverHitsBypassBit(t *testing.T) {
	if MarkBase&bypassBit != 0 {
		t.Fatalf("MarkBase %#x collides with bypass bit %#x", MarkBase, bypassBit)
	}
	if (MarkBase+MarkRange-1)&bypassBit != 0 {
		t.Fatalf("mark range [%#x,%#x) overlaps bypass bit %#x",
			MarkBase, MarkBase+MarkRange, bypassBit)
	}
}

func TestMarkForSet_PinnedReturnsAsIs(t *testing.T) {
	cases := []uint32{1, 0x100, 0x7EFF, 0x20000, 0x27FFF}
	for _, want := range cases {
		if got := MarkForSet("any-id", want); got != want {
			t.Fatalf("pinned mark: got %#x, want %#x", got, want)
		}
	}
}

func TestMarkForSet_PinnedOutsideTheRouteMaskIsNotUsed(t *testing.T) {
	hashed := MarkForSet("any-id", 0)
	for _, pinned := range []uint32{0x8000, 0x10000, 0x12345, 0x40000, 0x100000, 0xDEADBEEF} {
		got := MarkForSet("any-id", pinned)
		if got == pinned {
			t.Errorf("pinned mark %#x has bits the firewall rules and the fwmark policy rule both mask away; the ip rule then reads as fwmark 0x0/%#x and claims every packet that carries no routing mark at all", pinned, config.PerSetRouteMarkBits)
		}
		if got != hashed {
			t.Errorf("an unusable pin has to fall back to the same mark an unpinned set gets, or the routing rules and the tproxy listener disagree on which port the set uses: got %#x, want %#x", got, hashed)
		}
	}
}

func TestMarkIsUsableMatchesWhatTheRulesCanCarry(t *testing.T) {
	for _, m := range []uint32{0, 0x8000, 0x40000, 0x28000} {
		if MarkIsUsable(m) {
			t.Errorf("mark %#x survives masking by %#x as something other than itself", m, config.PerSetRouteMarkBits)
		}
	}
	for _, m := range []uint32{MarkBase, MarkBase + MarkRange - 1} {
		if !MarkIsUsable(m) {
			t.Errorf("every mark this package hands out must be pinnable too, and %#x is not", m)
		}
	}
}

func TestMarkForSet_DeterministicForSameID(t *testing.T) {
	id := "718e0020-ee8d-4055-851c-b99deeeb5abf"
	first := MarkForSet(id, 0)
	if got := MarkForSet(id, 0); got != first {
		t.Fatalf("MarkForSet not deterministic: got %#x, want %#x", got, first)
	}
}

func TestMarkForSet_RegressionOriginalFailingUUID(t *testing.T) {
	id := "718e0020-ee8d-4055-851c-b99deeeb5abf"
	m := MarkForSet(id, 0)
	if m&bypassBit != 0 {
		t.Fatalf("regression: mark %#x for the original failing UUID still hits bypass bit %#x", m, bypassBit)
	}
}
