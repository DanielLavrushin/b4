package handler

import (
	"slices"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/ctnl"
	"github.com/daniellavrushin/b4/nfq"
)

func TestQUICDropPersistenceCapability(t *testing.T) {
	v4only := config.NewConfig()
	dual := config.NewConfig()
	dual.Queue.IPv6Enabled = true
	ctOK := ctnl.Status{Tested: true, Available: true}
	ctDown := ctnl.Status{Tested: true, Reason: "self-test: netlink receive: invalid argument"}

	for _, tc := range []struct {
		name      string
		cfg       *config.Config
		st        nfq.QUICDropPersistence
		ct        ctnl.Status
		available bool
		detail    string
	}{
		{"the drop set covers every enabled family", &dual, nfq.QUICDropPersistence{NftV4: true, NftV6: true}, ctDown, true, "nftables drop set (IPv4 and IPv6): 2 minutes, or until the firewall rules are rebuilt. Without conntrack netlink (nf_conntrack_netlink)"},
		{"the drop set covers the only enabled family", &v4only, nfq.QUICDropPersistence{NftV4: true}, ctDown, true, "nftables drop set (IPv4)"},
		{"the drop set and conntrack for every enabled family", &dual, nfq.QUICDropPersistence{NftV4: true, NftV6: true}, ctOK, true, "nftables drop set (IPv4 and IPv6) and deletes its conntrack entry"},
		{"the drop set for one family, conntrack for the other", &dual, nfq.QUICDropPersistence{NftV4: true}, ctOK, true, "adds IPv4 connections to its nftables drop set and deletes the conntrack entry of every dropped connection"},
		{"conntrack alone", &v4only, nfq.QUICDropPersistence{}, ctOK, true, "deletes its conntrack entry"},
		{"neither", &v4only, nfq.QUICDropPersistence{}, ctDown, false, "Conntrack netlink is unavailable: "},
		{"a drop set for one family without conntrack", &dual, nfq.QUICDropPersistence{NftV6: true}, ctDown, false, "drop set holds IPv6 connections only"},
		{"a drop set only for a family b4 does not handle", &v4only, nfq.QUICDropPersistence{NftV6: true}, ctDown, false, "Conntrack netlink is unavailable: "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := quicDropPersistenceCapability(tc.cfg, tc.st, tc.ct, ctnl.Counters{})
			if c.Name != "quic_drop_persistence" || c.Available != tc.available {
				t.Fatalf("capability %+v, want quic_drop_persistence available=%v", c, tc.available)
			}
			if !strings.Contains(c.Detail, tc.detail) {
				t.Fatalf("detail %q, want it to say %q", c.Detail, tc.detail)
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

func TestQUICDropPersistenceCapabilityCountsWhatItHeld(t *testing.T) {
	cfg := config.NewConfig()
	st := nfq.QUICDropPersistence{NftWritten: 4, Unpersisted: 1, ConntrackRefused: 4}
	counts := ctnl.Counters{Deleted: 7, NotFound: 30, Busy: 2, Dropped: 1, Unavailable: 3, Failed: 1}
	c := quicDropPersistenceCapability(&cfg, st, ctnl.Status{Tested: true, Available: true}, counts)
	if !strings.Contains(c.Detail, "4 added to the drop set, 7 conntrack entries deleted, 8 drops not held, 2 of them offloaded") {
		t.Fatalf("detail %q: the jobs conntrack refused are its own dropped and unavailable counts, not counted twice", c.Detail)
	}
}
