package tables

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

var errNftNetlinkInUnitTests = errors.New("unit tests write nft elements through the nft command")

func init() {
	nftRefreshElements = func(string, string, []netip.Addr, time.Duration) error { return errNftNetlinkInUnitTests }
}

type nftNetlinkCall struct {
	table, set string
	addrs      string
	ttl        time.Duration
}

type nftNetlinkRecorder struct {
	calls []nftNetlinkCall
	err   error
}

func recordNftNetlink(t *testing.T, err error) *nftNetlinkRecorder {
	t.Helper()
	rec := &nftNetlinkRecorder{err: err}
	orig := nftRefreshElements
	t.Cleanup(func() { nftRefreshElements = orig })
	nftRefreshElements = func(table, set string, addrs []netip.Addr, ttl time.Duration) error {
		var list []string
		for _, addr := range addrs {
			list = append(list, addr.String())
		}
		rec.calls = append(rec.calls, nftNetlinkCall{table: table, set: set, addrs: strings.Join(list, " "), ttl: ttl})
		return rec.err
	}
	return rec
}

type nftTestElem struct {
	key     string
	timeout uint64
	nested  bool
}

func nftDecodeSetElems(t *testing.T, data []byte) (string, string, []nftTestElem) {
	t.Helper()
	if len(data) < 4 || !slices.Equal(data[:4], []byte{unix.NFPROTO_INET, unix.NFNETLINK_V0, 0, 0}) {
		t.Fatalf("element message header %v, want the inet family and version 0", data[:min(4, len(data))])
	}
	ad, err := netlink.NewAttributeDecoder(data[4:])
	if err != nil {
		t.Fatal(err)
	}
	var table, set string
	var elems []nftTestElem
	for ad.Next() {
		switch ad.Type() {
		case unix.NFTA_SET_ELEM_LIST_TABLE:
			table = ad.String()
		case unix.NFTA_SET_ELEM_LIST_SET:
			set = ad.String()
		case unix.NFTA_SET_ELEM_LIST_ELEMENTS:
			ad.Nested(func(list *netlink.AttributeDecoder) error {
				for list.Next() {
					e := nftTestElem{nested: list.Type() == unix.NFTA_LIST_ELEM && list.TypeFlags() == netlink.Nested}
					list.Nested(func(elem *netlink.AttributeDecoder) error {
						for elem.Next() {
							switch elem.Type() {
							case unix.NFTA_SET_ELEM_KEY:
								elem.Nested(func(key *netlink.AttributeDecoder) error {
									for key.Next() {
										if addr, ok := netip.AddrFromSlice(key.Bytes()); ok && key.Type() == unix.NFTA_DATA_VALUE {
											e.key = addr.String()
										}
									}
									return nil
								})
							case unix.NFTA_SET_ELEM_TIMEOUT:
								e.timeout = binary.BigEndian.Uint64(elem.Bytes())
							}
						}
						return nil
					})
					elems = append(elems, e)
				}
				return nil
			})
		}
	}
	if err := ad.Err(); err != nil {
		t.Fatal(err)
	}
	return table, set, elems
}

func TestNftRefreshMessagesAreOneAddDeleteAddBatch(t *testing.T) {
	addrs := []netip.Addr{netip.MustParseAddr("203.0.113.9"), netip.MustParseAddr("2001:db8::10")}
	msgs, err := nftRefreshMessages(dscpNftTable, "l_a_4", addrs, 600*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 5 {
		t.Fatalf("%d messages, want batch begin, add, delete, add and batch end", len(msgs))
	}
	batch := []byte{unix.AF_UNSPEC, unix.NFNETLINK_V0, 0, unix.NFNL_SUBSYS_NFTABLES}
	for i, kind := range map[int]netlink.HeaderType{0: unix.NFNL_MSG_BATCH_BEGIN, 4: unix.NFNL_MSG_BATCH_END} {
		if m := msgs[i]; m.Header.Type != kind || m.Header.Flags != netlink.Request || !slices.Equal(m.Data, batch) {
			t.Errorf("message %d = type %#x flags %v data %v, want type %#x, a plain request and the nftables batch header %v", i, m.Header.Type, m.Header.Flags, m.Data, kind, batch)
		}
	}
	steps := []struct {
		kind    uint16
		flags   netlink.HeaderFlags
		timeout uint64
	}{
		{unix.NFT_MSG_NEWSETELEM, netlink.Request | netlink.Acknowledge | netlink.Create, 600000},
		{unix.NFT_MSG_DELSETELEM, netlink.Request | netlink.Acknowledge, 0},
		{unix.NFT_MSG_NEWSETELEM, netlink.Request | netlink.Acknowledge | netlink.Create, 600000},
	}
	for i, step := range steps {
		m := msgs[i+1]
		if want := netlink.HeaderType(unix.NFNL_SUBSYS_NFTABLES<<8 | step.kind); m.Header.Type != want || m.Header.Flags != step.flags {
			t.Errorf("message %d = type %#x flags %v, want type %#x flags %v", i+1, m.Header.Type, m.Header.Flags, want, step.flags)
		}
		table, set, elems := nftDecodeSetElems(t, m.Data)
		want := []nftTestElem{{"203.0.113.9", step.timeout, true}, {"2001:db8::10", step.timeout, true}}
		if table != dscpNftTable || set != "l_a_4" || !slices.Equal(elems, want) {
			t.Errorf("message %d carries table %q set %q elements %+v, want %q %q %+v", i+1, table, set, elems, dscpNftTable, "l_a_4", want)
		}
	}
}

func withDSCPNftPlanApplied(t *testing.T) {
	t.Helper()
	prev := dscpApplied.Load()
	dscpApplied.Store(&dscpState{backend: backendNFTables, nft: &dscpNftLayout{generation: 1, maps: []int{1}}})
	t.Cleanup(func() { dscpApplied.Store(prev) })
}

func TestNftLearnedRefreshGoesOverNetlink(t *testing.T) {
	learnedIPs := func() []string {
		ips := make([]string, 0, routeNftChunkSize+1)
		for i := 0; i <= routeNftChunkSize; i++ {
			ips = append(ips, fmt.Sprintf("10.0.%d.%d", i/256, i%256))
		}
		return ips
	}

	t.Run("routing without per-set DSCP keeps the nft command", func(t *testing.T) {
		prev := dscpApplied.Load()
		dscpApplied.Store(nil)
		t.Cleanup(func() { dscpApplied.Store(prev) })
		rec := recordNftScripts(t, nil, nil)
		nl := recordNftNetlink(t, nil)
		ips := learnedIPs()

		failed := (&routeNftBackend{}).addElements("b4r_x_v4", ips, 600)
		if len(failed) != 0 || len(nl.calls) != 0 {
			t.Fatalf("with no per-set DSCP objects a learned refresh went over netlink %+v or failed %v", nl.calls, failed)
		}
		want := []string{
			strings.Join(routeNftRefreshArgs(routeNftTable, "b4r_x_v4_d", ips[:routeNftChunkSize], 600), " "),
			strings.Join(routeNftRefreshArgs(routeNftTable, "b4r_x_v4_d", ips[routeNftChunkSize:], 600), " "),
		}
		if !slices.Equal(rec.calls, want) {
			t.Errorf("nft calls %q, want the add/delete/add transaction per chunk, as before per-set DSCP", rec.calls)
		}
	})

	t.Run("routing while per-set DSCP objects are applied", func(t *testing.T) {
		withDSCPNftPlanApplied(t)
		rec := recordNftScripts(t, nil, nil)
		nl := recordNftNetlink(t, nil)
		ips := learnedIPs()

		failed := (&routeNftBackend{}).addElements("b4r_x_v4", ips, 600)
		if spawned := len(rec.calls) + len(rec.scripts) + len(rec.logged); len(failed) != 0 || spawned != 0 {
			t.Fatalf("a learned refresh spawned nft %d times or failed %v; every nft process reads the elements of every interval set in the ruleset first", spawned, failed)
		}
		if len(nl.calls) != 2 || nl.calls[0].table != routeNftTable || nl.calls[0].set != "b4r_x_v4_d" || nl.calls[0].ttl != 600*time.Second ||
			nl.calls[0].addrs != strings.Join(ips[:routeNftChunkSize], " ") || nl.calls[1].addrs != ips[routeNftChunkSize] {
			t.Errorf("netlink writes %+v, want the dynamic set refreshed in chunks of %d with a 600 s timeout", nl.calls, routeNftChunkSize)
		}
	})

	t.Run("DSCP learner", func(t *testing.T) {
		f, _ := dscpLearnNftSetup(t)
		nl := recordNftNetlink(t, nil)
		a := dscpPlanTestSet("a", 31, "10.1.2.0/24")
		cfg := dscpSyncNftConfig(config.DSCPConfig{Enabled: true, Value: 7}, a)
		dscpLearnApply(t, cfg, backendNFTables)
		dscpLearnStart()
		sid := routeSanitizeSetID("a")

		dscpLearnWaitAll(t, DSCPLearn(cfg, a, dscpLearnIPs("203.0.113.10", "2001:db8::10"), false))
		if len(f.refreshes) != 0 {
			t.Errorf("the learner spawned nft %d times for its write", len(f.refreshes))
		}
		want := []nftNetlinkCall{
			{table: dscpNftTable, set: dscpNftLearnedSet(sid, false), addrs: "203.0.113.10", ttl: time.Hour},
			{table: dscpNftTable, set: dscpNftLearnedSet(sid, true), addrs: "2001:db8::10", ttl: time.Hour},
		}
		if !slices.Equal(nl.calls, want) {
			t.Errorf("netlink writes %+v, want %+v", nl.calls, want)
		}
		if got := dscpLearnExpiry(sid, "2001:db8::10"); got.IsZero() {
			t.Errorf("a netlink write was not recorded as learned")
		}
	})

	t.Run("fallback", func(t *testing.T) {
		withDSCPNftPlanApplied(t)
		rec := recordNftScripts(t, nil, nil)
		nl := recordNftNetlink(t, errors.New("netlink: operation not permitted"))

		(&routeNftBackend{}).addElements("b4r_x_v4", []string{"203.0.113.9", "203.0.113.10"}, 600)
		want := []string{"nft add element inet b4_route b4r_x_v4_d { 203.0.113.9 timeout 600s , 203.0.113.10 timeout 600s }" +
			" ; delete element inet b4_route b4r_x_v4_d { 203.0.113.9 , 203.0.113.10 }" +
			" ; add element inet b4_route b4r_x_v4_d { 203.0.113.9 timeout 600s , 203.0.113.10 timeout 600s }"}
		if len(nl.calls) != 1 || !slices.Equal(rec.calls, want) {
			t.Errorf("after a failed netlink write the chunk must go to nft as one transaction: netlink %+v, calls %q", nl.calls, rec.calls)
		}

		nl.calls, rec.calls = nil, nil
		(&routeNftBackend{}).addElements("b4r_x_v4", []string{"203.0.113.9", "bogus"}, 600)
		if len(nl.calls) != 0 || len(rec.calls) != 1 {
			t.Errorf("a chunk netlink cannot encode must go to nft unchanged: netlink %+v, calls %q", nl.calls, rec.calls)
		}
	})
}
