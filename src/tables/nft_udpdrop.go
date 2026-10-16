package tables

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/log"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

const (
	nftUDPDropSetV4    = "b4_udp_drop4"
	nftUDPDropSetV6    = "b4_udp_drop6"
	nftUDPDropSetFlags = "flags timeout ; timeout 120s ; size 65536 ;"
	nftUDPDropDeadline = time.Second
)

var UDPDropWritersFunc func(v4, v6 func(src, dst netip.AddrPort) error)

var nftWriteUDPDrop = nftWriteUDPDropNetlink

var (
	udpDropMu          sync.Mutex
	udpDropPublishedV4 bool
	udpDropPublishedV6 bool
	udpDropWarned      sync.Map
)

func udpDropSetName(v6 bool) string {
	if v6 {
		return nftUDPDropSetV6
	}
	return nftUDPDropSetV4
}

func udpDropSetType(v6 bool) string {
	addr := "ipv4_addr"
	if v6 {
		addr = "ipv6_addr"
	}
	return addr + " . inet_service . " + addr + " . inet_service"
}

func udpDropRule(v6 bool) []string {
	l3 := "ip"
	if v6 {
		l3 = "ip6"
	}
	return []string{l3, "saddr", ".", "udp", "sport", ".", l3, "daddr", ".", "udp", "dport", "@" + udpDropSetName(v6), "counter", "drop"}
}

func (n *NFTablesManager) addUDPDropRules() (v4, v6 bool) {
	for _, fam := range []bool{false, true} {
		if !n.cfg.IPFamilyEnabled(fam) {
			continue
		}
		if err := n.addUDPDropRule(fam); err != nil {
			warnUDPDropSetOnce(fam, err)
			continue
		}
		if fam {
			v6 = true
		} else {
			v4 = true
		}
	}
	return v4, v6
}

func (n *NFTablesManager) addUDPDropRule(v6 bool) error {
	if err := n.createSet(udpDropSetName(v6), udpDropSetType(v6), nftUDPDropSetFlags); err != nil {
		return err
	}
	return n.addRule(nftChainName, udpDropRule(v6)...)
}

func warnUDPDropSetOnce(v6 bool, err error) {
	if _, seen := udpDropWarned.LoadOrStore(udpDropSetName(v6)+"\x00"+err.Error(), true); seen {
		log.Tracef("nftables: drop set %s unavailable: %v", udpDropSetName(v6), err)
		return
	}
	log.Warnf("nftables: b4 cannot keep the %s QUIC connections a set drops, rejects or blocks in its drop set %s, so it deletes their conntrack entry instead where the kernel offers conntrack netlink: %v", familyLabel(v6), udpDropSetName(v6), err)
}

func udpDropKey(v6 bool, src, dst netip.AddrPort) ([]byte, error) {
	size := 16
	if v6 {
		size = 40
	}
	key := make([]byte, 0, size)
	for _, ep := range [2]netip.AddrPort{src, dst} {
		addr := ep.Addr()
		if !addr.IsValid() || addr.Is6() != v6 {
			return nil, fmt.Errorf("%s does not belong in the %s drop set %s", ep, familyLabel(v6), udpDropSetName(v6))
		}
		key = append(key, addr.AsSlice()...)
		key = binary.BigEndian.AppendUint16(key, ep.Port())
		key = append(key, 0, 0)
	}
	return key, nil
}

func nftUDPDropMessages(set string, key []byte) ([]netlink.Message, error) {
	elem, err := nftSetElemKeysMessage(unix.NFT_MSG_NEWSETELEM, nftTableName, set, [][]byte{key}, 0)
	if err != nil {
		return nil, err
	}
	return []netlink.Message{nftBatchMessage(unix.NFNL_MSG_BATCH_BEGIN), elem, nftBatchMessage(unix.NFNL_MSG_BATCH_END)}, nil
}

func nftWriteUDPDropNetlink(set string, key []byte) error {
	msgs, err := nftUDPDropMessages(set, key)
	if err != nil {
		return err
	}
	return nftSendBatch(msgs, nftUDPDropDeadline)
}

func udpDropWriter(v6 bool) func(src, dst netip.AddrPort) error {
	set := udpDropSetName(v6)
	return func(src, dst netip.AddrPort) error {
		key, err := udpDropKey(v6, src, dst)
		if err != nil {
			return err
		}
		return nftWriteUDPDrop(set, key)
	}
}

func noteUDPDropSets(v4, v6 bool) {
	udpDropMu.Lock()
	defer udpDropMu.Unlock()
	publishUDPDropSetsLocked(v4, v6)
}

func withdrawUDPDropSets() {
	udpDropMu.Lock()
	defer udpDropMu.Unlock()
	if udpDropPublishedV4 || udpDropPublishedV6 {
		publishUDPDropSetsLocked(false, false)
	}
}

func publishUDPDropSetsLocked(v4, v6 bool) {
	udpDropPublishedV4, udpDropPublishedV6 = v4, v6
	if UDPDropWritersFunc == nil {
		return
	}
	var w4, w6 func(src, dst netip.AddrPort) error
	if v4 {
		w4 = udpDropWriter(false)
	}
	if v6 {
		w6 = udpDropWriter(true)
	}
	UDPDropWritersFunc(w4, w6)
}
