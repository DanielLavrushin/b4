package tables

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

func init() {
	nftWriteUDPDrop = func(string, []byte) error { return errNftNetlinkInUnitTests }
	nftFlushUDPDrop = func([]string) error { return errNftNetlinkInUnitTests }
}

const (
	nftDropSetDef4 = "{ type ipv4_addr . inet_service . ipv4_addr . inet_service ; flags timeout ; timeout 120s ; size 65536 ; }"
	nftDropSetDef6 = "{ type ipv6_addr . inet_service . ipv6_addr . inet_service ; flags timeout ; timeout 120s ; size 65536 ; }"
	nftDropRule4   = "ip saddr . udp sport . ip daddr . udp dport @b4_udp_drop4 counter drop"
	nftDropRule6   = "ip6 saddr . udp sport . ip6 daddr . udp dport @b4_udp_drop6 counter drop"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestUDPDropKeyPadsEveryFieldToFourBytes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		v6       bool
		src, dst string
		want     string
	}{
		{"IPv4", false, "10.0.1.2:40000", "203.0.113.2:443", "0a000102 9c400000 cb007102 01bb0000"},
		{"IPv4 as nft encodes it", false, "10.0.1.3:40001", "203.0.113.2:443", "0a000103 9c410000 cb007102 01bb0000"},
		{"IPv6", true, "[2001:db8:1::2]:40300", "[2001:db8:2::2]:443",
			"20010db8000100000000000000000002 9d6c0000 20010db8000200000000000000000002 01bb0000"},
		{"IPv4-mapped IPv6 header", true, "[::ffff:10.0.1.2]:40000", "[::ffff:203.0.113.2]:443",
			"00000000000000000000ffff0a000102 9c400000 00000000000000000000ffffcb007102 01bb0000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key, err := udpDropKey(tc.v6, netip.MustParseAddrPort(tc.src), netip.MustParseAddrPort(tc.dst))
			if err != nil {
				t.Fatal(err)
			}
			want := mustHex(t, tc.want)
			if !slices.Equal(key, want) {
				t.Errorf("key %x, want %x", key, want)
			}
			if size := map[bool]int{false: 16, true: 40}[tc.v6]; len(key) != size {
				t.Errorf("key of %d bytes, want %d, the length of the set's concatenated type", len(key), size)
			}
		})
	}
}

func TestUDPDropKeyRefusesAnotherFamily(t *testing.T) {
	v4 := netip.MustParseAddrPort("10.0.1.2:40000")
	v6 := netip.MustParseAddrPort("[2001:db8:1::2]:40000")
	mapped := netip.MustParseAddrPort("[::ffff:10.0.1.2]:40000")
	for _, tc := range []struct {
		name     string
		v6       bool
		src, dst netip.AddrPort
	}{
		{"IPv6 source into the IPv4 set", false, v6, v4},
		{"IPv6 destination into the IPv4 set", false, v4, v6},
		{"IPv4-mapped source into the IPv4 set", false, mapped, v4},
		{"IPv4 source into the IPv6 set", true, v4, v6},
		{"no source", false, netip.AddrPort{}, v4},
		{"no destination", true, v6, netip.AddrPort{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if key, err := udpDropKey(tc.v6, tc.src, tc.dst); err == nil {
				t.Errorf("encoded %s -> %s as %x", tc.src, tc.dst, key)
			}
		})
	}
}

type udpDropTestElem struct {
	key     []byte
	timeout bool
	other   []uint16
}

func decodeUDPDropElems(t *testing.T, data []byte) (string, string, []udpDropTestElem) {
	t.Helper()
	if len(data) < 4 || !slices.Equal(data[:4], []byte{unix.NFPROTO_INET, unix.NFNETLINK_V0, 0, 0}) {
		t.Fatalf("element message header %v, want the inet family and version 0", data[:min(4, len(data))])
	}
	ad, err := netlink.NewAttributeDecoder(data[4:])
	if err != nil {
		t.Fatal(err)
	}
	var table, set string
	var elems []udpDropTestElem
	for ad.Next() {
		switch ad.Type() {
		case unix.NFTA_SET_ELEM_LIST_TABLE:
			table = ad.String()
		case unix.NFTA_SET_ELEM_LIST_SET:
			set = ad.String()
		case unix.NFTA_SET_ELEM_LIST_ELEMENTS:
			ad.Nested(func(list *netlink.AttributeDecoder) error {
				for list.Next() {
					var e udpDropTestElem
					list.Nested(func(elem *netlink.AttributeDecoder) error {
						for elem.Next() {
							switch elem.Type() {
							case unix.NFTA_SET_ELEM_KEY:
								elem.Nested(func(key *netlink.AttributeDecoder) error {
									for key.Next() {
										if key.Type() == unix.NFTA_DATA_VALUE {
											e.key = key.Bytes()
										}
									}
									return nil
								})
							case unix.NFTA_SET_ELEM_TIMEOUT:
								e.timeout = true
							default:
								e.other = append(e.other, elem.Type())
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

func TestUDPDropWriteIsOneElementWithTheSetsOwnTimeout(t *testing.T) {
	key := mustHex(t, "0a000102 9c400000 cb007102 01bb0000")
	msgs, err := nftUDPDropMessages(nftUDPDropSetV4, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("%d messages, want batch begin, the new element and batch end", len(msgs))
	}
	batch := []byte{unix.AF_UNSPEC, unix.NFNETLINK_V0, 0, unix.NFNL_SUBSYS_NFTABLES}
	for i, kind := range map[int]netlink.HeaderType{0: unix.NFNL_MSG_BATCH_BEGIN, 2: unix.NFNL_MSG_BATCH_END} {
		if m := msgs[i]; m.Header.Type != kind || m.Header.Flags != netlink.Request || !slices.Equal(m.Data, batch) {
			t.Errorf("message %d = type %#x flags %v data %v, want type %#x, a plain request and the nftables batch header %v", i, m.Header.Type, m.Header.Flags, m.Data, kind, batch)
		}
	}
	m := msgs[1]
	if want := netlink.HeaderType(unix.NFNL_SUBSYS_NFTABLES<<8 | unix.NFT_MSG_NEWSETELEM); m.Header.Type != want {
		t.Errorf("type %#x, want %#x", m.Header.Type, want)
	}
	if want := netlink.Request | netlink.Acknowledge | netlink.Create; m.Header.Flags != want {
		t.Errorf("flags %v, want %v: without the acknowledgement a refused write would look like a success, and an exclusive create would refuse a flow already held", m.Header.Flags, want)
	}
	table, set, elems := decodeUDPDropElems(t, m.Data)
	if table != nftTableName || set != nftUDPDropSetV4 {
		t.Errorf("written to %s %s, want %s %s", table, set, nftTableName, nftUDPDropSetV4)
	}
	if len(elems) != 1 || !slices.Equal(elems[0].key, key) || elems[0].timeout || len(elems[0].other) != 0 {
		t.Errorf("elements %+v, want exactly the key %x and no timeout of its own, so the set's 120 s applies", elems, key)
	}
}

type udpDropWrite struct {
	set string
	key string
}

func recordUDPDropWrites(t *testing.T, err error) *[]udpDropWrite {
	t.Helper()
	var writes []udpDropWrite
	orig := nftWriteUDPDrop
	t.Cleanup(func() { nftWriteUDPDrop = orig })
	nftWriteUDPDrop = func(set string, key []byte) error {
		writes = append(writes, udpDropWrite{set: set, key: hex.EncodeToString(key)})
		return err
	}
	return &writes
}

func TestUDPDropWriterWritesTheFlowIntoItsFamilysSet(t *testing.T) {
	writes := recordUDPDropWrites(t, nil)
	if err := udpDropWriter(false)(netip.MustParseAddrPort("10.0.1.2:40000"), netip.MustParseAddrPort("203.0.113.2:443")); err != nil {
		t.Fatal(err)
	}
	if err := udpDropWriter(true)(netip.MustParseAddrPort("[2001:db8:1::2]:40300"), netip.MustParseAddrPort("[2001:db8:2::2]:443")); err != nil {
		t.Fatal(err)
	}
	want := []udpDropWrite{
		{nftUDPDropSetV4, "0a0001029c400000cb00710201bb0000"},
		{nftUDPDropSetV6, "20010db80001000000000000000000029d6c000020010db800020000000000000000000201bb0000"},
	}
	if !slices.Equal(*writes, want) {
		t.Errorf("writes %+v, want %+v", *writes, want)
	}

	*writes = nil
	if err := udpDropWriter(false)(netip.MustParseAddrPort("[2001:db8:1::2]:40300"), netip.MustParseAddrPort("[2001:db8:2::2]:443")); err == nil || len(*writes) != 0 {
		t.Errorf("an IPv6 flow handed to the IPv4 writer returned %v and wrote %+v", err, *writes)
	}

	refused := errors.New("netlink receive: no such file or directory")
	recordUDPDropWrites(t, refused)
	if err := udpDropWriter(false)(netip.MustParseAddrPort("10.0.1.2:40000"), netip.MustParseAddrPort("203.0.113.2:443")); !errors.Is(err, refused) {
		t.Errorf("a refused write returned %v, want the kernel's error so the caller falls back to conntrack", err)
	}
}

func TestUDPDropFlushEmptiesEverySetInOneBatch(t *testing.T) {
	msgs, err := nftUDPDropFlushMessages([]string{nftUDPDropSetV4, nftUDPDropSetV6})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 4 || msgs[0].Header.Type != unix.NFNL_MSG_BATCH_BEGIN || msgs[3].Header.Type != unix.NFNL_MSG_BATCH_END {
		t.Fatalf("%d messages, want batch begin, one flush per set and batch end", len(msgs))
	}
	for i, set := range []string{nftUDPDropSetV4, nftUDPDropSetV6} {
		m := msgs[i+1]
		if want := netlink.HeaderType(unix.NFNL_SUBSYS_NFTABLES<<8 | unix.NFT_MSG_DELSETELEM); m.Header.Type != want {
			t.Errorf("%s: type %#x, want %#x", set, m.Header.Type, want)
		}
		if want := netlink.Request | netlink.Acknowledge; m.Header.Flags != want {
			t.Errorf("%s: flags %v, want %v so a refused flush is reported", set, m.Header.Flags, want)
		}
		if !slices.Equal(m.Data[:4], []byte{unix.NFPROTO_INET, unix.NFNETLINK_V0, 0, 0}) {
			t.Errorf("%s: header %v, want the inet family and version 0", set, m.Data[:4])
		}
		ad, err := netlink.NewAttributeDecoder(m.Data[4:])
		if err != nil {
			t.Fatal(err)
		}
		attrs := map[uint16]string{}
		for ad.Next() {
			attrs[ad.Type()] = ad.String()
		}
		if err := ad.Err(); err != nil {
			t.Fatal(err)
		}
		want := map[uint16]string{unix.NFTA_SET_ELEM_LIST_TABLE: nftTableName, unix.NFTA_SET_ELEM_LIST_SET: set}
		if len(attrs) != len(want) || attrs[unix.NFTA_SET_ELEM_LIST_TABLE] != nftTableName || attrs[unix.NFTA_SET_ELEM_LIST_SET] != set {
			t.Errorf("%s: attributes %v, want only %v: the kernel empties the whole set only when the message carries no element list, and an empty list removes nothing", set, attrs, want)
		}
	}
}

func TestFlushUDPDropSetsEmptiesThePublishedSetsOnly(t *testing.T) {
	var flushed [][]string
	orig := nftFlushUDPDrop
	nftFlushUDPDrop = func(sets []string) error {
		flushed = append(flushed, sets)
		return nil
	}
	udpDropMu.Lock()
	origV4, origV6, origHook := udpDropPublishedV4, udpDropPublishedV6, UDPDropWritersFunc
	UDPDropWritersFunc = nil
	udpDropMu.Unlock()
	t.Cleanup(func() {
		nftFlushUDPDrop = orig
		udpDropMu.Lock()
		publishUDPDropSetsLocked(origV4, origV6)
		UDPDropWritersFunc = origHook
		udpDropMu.Unlock()
	})

	for _, c := range []struct {
		v4, v6 bool
		want   []string
	}{
		{true, true, []string{nftUDPDropSetV4, nftUDPDropSetV6}},
		{true, false, []string{nftUDPDropSetV4}},
		{false, true, []string{nftUDPDropSetV6}},
		{false, false, nil},
	} {
		noteUDPDropSets(c.v4, c.v6)
		flushed = nil
		if err := FlushUDPDropSets(); err != nil {
			t.Fatal(err)
		}
		switch {
		case c.want == nil && len(flushed) != 0:
			t.Errorf("v4=%v v6=%v: flushed %q with no set installed", c.v4, c.v6, flushed)
		case c.want != nil && (len(flushed) != 1 || !slices.Equal(flushed[0], c.want)):
			t.Errorf("v4=%v v6=%v: flushed %q, want one batch for %q", c.v4, c.v6, flushed, c.want)
		}
	}
}

type stalledBatchConn struct {
	stall          time.Duration
	events         []string
	sent           time.Time
	readDeadline   time.Time
	writeDeadline  time.Time
	acknowledgeSeq []uint32
}

func (c *stalledBatchConn) SetWriteDeadline(t time.Time) error {
	c.events = append(c.events, "write deadline")
	c.writeDeadline = t
	return nil
}

func (c *stalledBatchConn) SendMessages(msgs []netlink.Message) ([]netlink.Message, error) {
	time.Sleep(c.stall)
	c.sent = time.Now()
	c.events = append(c.events, "send")
	out := make([]netlink.Message, len(msgs))
	for i, m := range msgs {
		m.Header.Sequence = uint32(i + 1)
		if m.Header.Flags&netlink.Acknowledge != 0 {
			c.acknowledgeSeq = append(c.acknowledgeSeq, m.Header.Sequence)
		}
		out[i] = m
	}
	return out, nil
}

func (c *stalledBatchConn) SetReadDeadline(t time.Time) error {
	c.events = append(c.events, "read deadline")
	c.readDeadline = t
	return nil
}

func (c *stalledBatchConn) Receive() ([]netlink.Message, error) {
	c.events = append(c.events, "receive")
	var acks []netlink.Message
	for _, seq := range c.acknowledgeSeq {
		acks = append(acks, netlink.Message{Header: netlink.Header{Type: netlink.Error, Sequence: seq}})
	}
	return acks, nil
}

func TestTheBatchReplyDeadlineStartsWhenTheSendReturns(t *testing.T) {
	msgs, err := nftUDPDropMessages(nftUDPDropSetV4, mustHex(t, "0a000102 9c400000 cb007102 01bb0000"))
	if err != nil {
		t.Fatal(err)
	}
	const deadline = 20 * time.Millisecond
	c := &stalledBatchConn{stall: 3 * deadline}
	start := time.Now()
	if err := nftExchangeBatch(c, msgs, deadline); err != nil {
		t.Fatal(err)
	}
	if want := []string{"write deadline", "send", "read deadline", "receive"}; !slices.Equal(c.events, want) {
		t.Fatalf("calls %q, want %q", c.events, want)
	}
	if c.writeDeadline.Before(start) || c.writeDeadline.After(c.sent) {
		t.Errorf("the send must be bounded by its own deadline, set before it")
	}
	if c.readDeadline.Before(c.sent.Add(deadline)) {
		t.Errorf("the reply deadline %v ends before %v after the send returned: time spent in the send, which waits for nf_tables' commit lock while another transaction runs, would turn a done write into a reported failure", c.readDeadline.Sub(c.sent), deadline)
	}
}

type udpDropRig struct {
	*directionRig
	writers []func(src, dst netip.AddrPort) error
}

func newUDPDropRig(t *testing.T) *udpDropRig {
	t.Helper()
	r := &udpDropRig{directionRig: newDirectionRig(t)}
	UDPDropWritersFunc = func(v4, v6 func(src, dst netip.AddrPort) error) {
		event := fmt.Sprintf("drop writers v4=%v v6=%v", v4 != nil, v6 != nil)
		r.events = append(r.events, event)
		r.writers = []func(src, dst netip.AddrPort) error{v4, v6}
		r.h.record(event, "")
	}
	return r
}

func (r *udpDropRig) dropEvents(n int) []string {
	var out []string
	for _, e := range r.since(n) {
		if strings.HasPrefix(e, "drop writers ") {
			out = append(out, e)
		}
	}
	return out
}

func udpDropConfig(backend string, v4, v6 bool) *config.Config {
	cfg := directionConfig(backend)
	cfg.Queue.IPv4Enabled, cfg.Queue.IPv6Enabled = v4, v6
	return cfg
}

func TestNFTablesDropSetSitsBetweenTheMarkReturnsAndTheUDPWindow(t *testing.T) {
	for _, tc := range []struct {
		name     string
		v4, v6   bool
		sets     []string
		rules    []string
		prefixed bool
	}{
		{"both families", true, true, []string{"b4_udp_drop4 " + nftDropSetDef4, "b4_udp_drop6 " + nftDropSetDef6}, []string{nftDropRule4, nftDropRule6}, false},
		{"IPv4 only", true, false, []string{"b4_udp_drop4 " + nftDropSetDef4}, []string{nftDropRule4}, true},
		{"IPv6 only", false, true, []string{"b4_udp_drop6 " + nftDropSetDef6}, []string{nftDropRule6}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newUDPDropRig(t)
			if err := AddRules(udpDropConfig(backendNFTables, tc.v4, tc.v6)); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			lines := r.h.take()
			var sets []string
			for _, l := range lines {
				if s, ok := strings.CutPrefix(l, "exec nft add set inet "+nftTableName+" b4_udp_drop"); ok {
					sets = append(sets, "b4_udp_drop"+s)
				}
			}
			if !slices.Equal(sets, tc.sets) {
				t.Errorf("drop sets created %q, want %q", sets, tc.sets)
			}

			b4 := nftEmittedTable(lines).chains[nftChainName]
			var drops []int
			for i, rule := range b4 {
				if strings.Contains(rule, "@b4_udp_drop") {
					drops = append(drops, i)
				}
			}
			var got []string
			for _, i := range drops {
				got = append(got, b4[i])
			}
			if !slices.Equal(got, tc.rules) {
				t.Fatalf("drop rules %q, want %q: the set lookup names its family, so the rules need no family filter", got, tc.rules)
			}
			lastReturn := lastIndexFunc(b4, func(r string) bool { return strings.HasPrefix(r, "meta mark & ") && strings.HasSuffix(r, " return") })
			window := slices.IndexFunc(b4, func(r string) bool { return strings.Contains(r, "udp dport 443 ct original packets < 9 ") })
			switch {
			case lastReturn < 0 || window < 0:
				t.Fatalf("b4_chain lacks the mark returns (%d) or the UDP window (%d): %q", lastReturn, window, b4)
			case drops[0] < lastReturn:
				t.Errorf("a drop rule sits above the return for a marked packet, so it would drop b4's own packets: %q", b4)
			case drops[len(drops)-1] > window:
				t.Errorf("a drop rule sits below the UDP window, which queues the flow first: %q", b4)
			}
			if tc.prefixed && !strings.HasPrefix(b4[window], "meta nfproto ") {
				t.Errorf("the UDP window lost its family filter: %q", b4[window])
			}

			at := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "add set inet "+nftTableName+" b4_udp_drop") })
			ruleAt := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "@b4_udp_drop") })
			if at < 0 || ruleAt < at {
				t.Errorf("a drop rule went in at line %d before its set at line %d", ruleAt, at)
			}
		})
	}
}

func TestNFTablesPublishesTheDropSetWritersOnlyWhileTheRulesAreIn(t *testing.T) {
	r := newUDPDropRig(t)
	writes := recordUDPDropWrites(t, nil)
	cfg := udpDropConfig(backendNFTables, true, true)

	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	lines := r.h.take()
	if got := r.dropEvents(0); !slices.Equal(got, []string{"drop writers v4=true v6=true"}) {
		t.Fatalf("publications %q, want both writers once", got)
	}
	published := slices.Index(lines, "drop writers v4=true v6=true")
	if last := lastLine(lines, isNftRuleAdd); published < last {
		t.Errorf("the writers were published at line %d, before the last rule went in at line %d", published, last)
	}
	if err := r.writers[0](netip.MustParseAddrPort("10.0.1.2:40000"), netip.MustParseAddrPort("203.0.113.2:443")); err != nil {
		t.Fatal(err)
	}
	if err := r.writers[1](netip.MustParseAddrPort("[2001:db8:1::2]:40300"), netip.MustParseAddrPort("[2001:db8:2::2]:443")); err != nil {
		t.Fatal(err)
	}
	if len(*writes) != 2 || (*writes)[0].set != nftUDPDropSetV4 || (*writes)[1].set != nftUDPDropSetV6 {
		t.Errorf("the published writers wrote %+v, want one element into each family's set", *writes)
	}

	n := len(r.events)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	lines = r.h.take()
	if got := r.dropEvents(n); !slices.Equal(got, []string{"drop writers v4=false v6=false", "drop writers v4=true v6=true"}) {
		t.Fatalf("rebuilding published %q, want the writers withdrawn while the table is rebuilt and published again after", got)
	}
	withdrawn := slices.Index(lines, "drop writers v4=false v6=false")
	deleted := slices.Index(lines, "exec nft delete table inet "+nftTableName)
	if withdrawn < 0 || deleted < 0 || withdrawn > deleted {
		t.Errorf("withdrawn at line %d, the old table deleted at line %d: a write in between lands in a set about to go", withdrawn, deleted)
	}

	n = len(r.events)
	if err := ClearRules(cfg); err != nil {
		t.Fatalf("ClearRules: %v", err)
	}
	if got := r.dropEvents(n); !slices.Equal(got, []string{"drop writers v4=false v6=false"}) {
		t.Errorf("ClearRules published %q, want the writers withdrawn", got)
	}

	n = len(r.events)
	if err := ClearRules(cfg); err != nil {
		t.Fatalf("ClearRules again: %v", err)
	}
	if got := r.dropEvents(n); len(got) != 0 {
		t.Errorf("a clear with nothing published published %q", got)
	}
}

func TestARestoreOfAVanishedTableWithdrawsTheWritersFirst(t *testing.T) {
	r := newUDPDropRig(t)
	cfg := udpDropConfig(backendNFTables, true, true)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	if out, err := exec.Command(filepath.Join(r.h.dir, "bin", "nft"), "delete", "table", "inet", nftTableName).CombinedOutput(); err != nil {
		t.Fatalf("deleting the table behind b4's back: %v (%s)", err, out)
	}
	r.h.take()

	n := len(r.events)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("restore: %v", err)
	}
	lines := r.h.take()
	if got := r.dropEvents(n); !slices.Equal(got, []string{"drop writers v4=false v6=false", "drop writers v4=true v6=true"}) {
		t.Fatalf("the restore published %q, want the writers withdrawn before the table is built again", got)
	}
	withdrawn := slices.Index(lines, "drop writers v4=false v6=false")
	created := slices.Index(lines, "exec nft add table inet "+nftTableName)
	if created < 0 || withdrawn > created {
		t.Errorf("withdrawn at line %d, the table created again at line %d", withdrawn, created)
	}
}

func TestADroppedApplyOrAnotherBackendWithdrawsTheDropSetWriters(t *testing.T) {
	t.Run("failed apply", func(t *testing.T) {
		r := newUDPDropRig(t)
		cfg := udpDropConfig(backendNFTables, true, true)
		if err := AddRules(cfg); err != nil {
			t.Fatalf("AddRules: %v", err)
		}
		forgetQueueProbes(t)
		r.refuse = func(args []string) bool {
			return args[0] == "nft" && slices.Contains(args, "_b4_queue_probe") && slices.Contains(args, "queue")
		}
		n := len(r.events)
		if err := AddRules(cfg); err == nil {
			t.Fatal("the refused queue probe did not fail the apply; the test proves nothing")
		}
		if got := r.dropEvents(n); !slices.Equal(got, []string{"drop writers v4=false v6=false"}) {
			t.Errorf("publications after a failed apply %q, want the writers withdrawn and not published again", got)
		}
	})

	t.Run("iptables apply", func(t *testing.T) {
		r := newUDPDropRig(t)
		noteUDPDropSets(true, true)
		n := len(r.events)
		if err := AddRules(udpDropConfig(backendIPTables, true, true)); err != nil {
			t.Fatalf("AddRules: %v", err)
		}
		if got := r.dropEvents(n); !slices.Equal(got, []string{"drop writers v4=false v6=false"}) {
			t.Errorf("an iptables apply published %q, want the nftables drop set writers withdrawn", got)
		}
		for _, l := range r.h.take() {
			if strings.Contains(l, "b4_udp_drop") {
				t.Errorf("the iptables backend touched a drop set: %q", l)
			}
		}
	})
}

func refuseNftCommands(t *testing.T, h *goldenHost, match string) {
	t.Helper()
	wrap := t.TempDir()
	stub := "#!/bin/sh\ncase \"$*\" in\n*\"" + match + "\"*) printf 'Error: Could not process rule: Operation not supported\\n' >&2; exit 1 ;;\nesac\nexec '" + filepath.Join(h.dir, "bin", "nft") + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(wrap, "nft"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", wrap+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestNFTablesGoesOnWithoutADropSet(t *testing.T) {
	for _, tc := range []struct {
		name, refuse string
		v4, v6       bool
	}{
		{"set refused", "add set inet b4_mangle b4_udp_drop6", true, false},
		{"rule refused", "@b4_udp_drop4", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newUDPDropRig(t)
			buf := captureTablesLog(t)
			refuseNftCommands(t, r.h, tc.refuse)
			cfg := udpDropConfig(backendNFTables, true, true)
			for i := 0; i < 2; i++ {
				if err := AddRules(cfg); err != nil {
					t.Fatalf("apply %d: a refused drop set failed the whole apply: %v", i+1, err)
				}
			}
			want := fmt.Sprintf("drop writers v4=%v v6=%v", tc.v4, tc.v6)
			if got := r.dropEvents(0); len(got) == 0 || got[len(got)-1] != want {
				t.Errorf("publications %q, want %q last", got, want)
			}
			b4 := nftEmittedTable(r.h.take()).chains[nftChainName]
			if !slices.ContainsFunc(b4, func(r string) bool { return strings.Contains(r, "udp dport 443 ct original packets < 9 ") }) {
				t.Errorf("the UDP window did not go in after the refused drop set: %q", b4)
			}
			if got := strings.Count(buf.String(), "QUIC connections a set drops, rejects or blocks"); got != 1 {
				t.Errorf("the refusal was warned %d times over two applies, want once:\n%s", got, buf.String())
			}
		})
	}
}
