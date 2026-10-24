package handler

import (
	"slices"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/ctnl"
	b4tun "github.com/daniellavrushin/b4/tun"
)

func TestTUNQUICDropPersistenceCapability(t *testing.T) {
	ctOK := ctnl.Status{Tested: true, Available: true}
	ctDown := ctnl.Status{Tested: true, Reason: "self-test: netlink receive: invalid argument"}

	for _, tc := range []struct {
		name      string
		st        b4tun.QUICDropPersistence
		ct        ctnl.Status
		available bool
		detail    string
	}{
		{"b4 counts the packets itself, conntrack netlink or not", b4tun.QUICDropPersistence{FirstN: true}, ctDown, true, "the TUN engine counts the packets of each connection itself"},
		{"the kernel window with conntrack netlink", b4tun.QUICDropPersistence{Conntrack: true}, ctOK, true, "b4 deletes its conntrack entry, so its later datagrams are steered into the TUN device again"},
		{"the kernel window without conntrack netlink", b4tun.QUICDropPersistence{Conntrack: true}, ctDown, false, "Conntrack netlink is unavailable: a QUIC connection that a set drops, rejects or blocks is dropped for the first datagrams steered into the TUN device"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := tunQUICDropPersistenceCapability(tc.st, tc.ct, ctnl.Counters{})
			if c.Name != "quic_drop_persistence" || c.Available != tc.available {
				t.Fatalf("capability %+v, want quic_drop_persistence available=%v", c, tc.available)
			}
			if !strings.Contains(c.Detail, tc.detail) {
				t.Fatalf("detail %q, want it to say %q", c.Detail, tc.detail)
			}
			if strings.Contains(c.Detail, "drop set") {
				t.Fatalf("detail %q: the TUN engine has no nftables drop set", c.Detail)
			}
			if tc.available {
				if len(c.Missing) != 0 || len(c.Reasons) != 0 {
					t.Fatalf("an available row lists missing %v, reasons %v", c.Missing, c.Reasons)
				}
				return
			}
			if !slices.Equal(c.Missing, []string{"nf_conntrack_netlink"}) {
				t.Fatalf("missing %v, want nf_conntrack_netlink", c.Missing)
			}
			if !slices.Contains(c.Reasons, "conntrack netlink: "+tc.ct.Reason) {
				t.Fatalf("reasons %v lack the self-test result", c.Reasons)
			}
		})
	}
}

func TestTUNQUICDropPersistenceCapabilityCountsWhatItHeld(t *testing.T) {
	counting := tunQUICDropPersistenceCapability(b4tun.QUICDropPersistence{FirstN: true, Held: 12, Unheld: 1}, ctnl.Status{}, ctnl.Counters{Deleted: 99})
	if !strings.Contains(counting.Detail, "(since start: 12 drops held, 1 drops not held)") {
		t.Fatalf("detail %q, want the drops b4's own count held and the ones it could not", counting.Detail)
	}

	st := b4tun.QUICDropPersistence{Conntrack: true, ConntrackQueued: 11, ConntrackRefused: 3, Unheld: 2}
	counts := ctnl.Counters{Deleted: 9, NotFound: 4, Busy: 1, Dropped: 1, Unavailable: 2}
	window := tunQUICDropPersistenceCapability(st, ctnl.Status{Tested: true, Available: true}, counts)
	if !strings.Contains(window.Detail, "(since start: 9 conntrack entries deleted, 6 drops not held, 1 of them offloaded)") {
		t.Fatalf("detail %q: the deletes conntrack refused are its own dropped and unavailable counts, not counted twice", window.Detail)
	}
}

func TestTheQUICDropRowFollowsTheRunningTUNEngine(t *testing.T) {
	cfg := config.NewConfig()
	prev := globalTUNEngine
	globalTUNEngine = b4tun.NewEngine(&cfg, nil)
	t.Cleanup(func() { globalTUNEngine = prev })

	if c := quicDropPersistenceRow(&cfg); !strings.Contains(c.Detail, "TUN device") || strings.Contains(c.Detail, "drop set") {
		t.Fatalf("detail %q: with the TUN engine running the row describes the TUN engine", c.Detail)
	}
}
