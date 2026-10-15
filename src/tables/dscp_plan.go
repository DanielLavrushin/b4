package tables

import (
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"math"
	"math/bits"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sni"
)

const dscpPreResolveCap = 256

type dscpRange struct {
	from  netip.Addr
	to    netip.Addr
	value int
}

type dscpPlanSet struct {
	id      string
	sid     string
	value   int
	index   int
	learn   bool
	dnsTTL  time.Duration
	tlsTTL  time.Duration
	v4      bool
	v6      bool
	domains []string
}

type dscpPlan struct {
	globalOn    bool
	global      int
	scopes      []string
	sets        []dscpPlanSet
	refusals    map[string]string
	static4     []dscpRange
	static6     []dscpRange
	values      []int
	staticKey   string
	fingerprint string
}

func dscpPlanFor(cfg *config.Config) *dscpPlan {
	plan := &dscpPlan{}
	if cfg == nil || cfg.System.Tables.SkipSetup {
		plan.seal()
		return plan
	}
	plan.global, _, plan.globalOn = cfg.DSCPStamp()
	plan.scopes = cfg.DSCPInterfaces()
	targets := 0
	for _, set := range cfg.Sets {
		if set != nil && set.Enabled && set.DSCP.Enabled {
			targets += len(set.Targets.IpsToMatch)
		}
	}
	claims := make([]dscpClaim, 0, targets)
	seen := make(map[string]bool)
	for index, set := range cfg.Sets {
		if set == nil || !set.Enabled || seen[set.Id] {
			continue
		}
		value, on := set.DSCPStamp()
		if !on {
			continue
		}
		seen[set.Id] = true
		if reason := cfg.DSCPRefusal(set); reason != "" {
			if plan.refusals == nil {
				plan.refusals = make(map[string]string)
			}
			plan.refusals[set.Id] = reason
			continue
		}
		member := dscpPlanSet{
			id:     set.Id,
			sid:    routeSanitizeSetID(set.Id),
			value:  value,
			index:  index,
			learn:  !set.Targets.DomainOnly,
			dnsTTL: set.DSCPLearnTTL(false),
			tlsTTL: set.DSCPLearnTTL(true),
			v4:     set.MatchesIPVersion(4),
			v6:     set.MatchesIPVersion(6),
		}
		if member.learn {
			member.domains = dscpLiteralDomains(set.Targets.SNIDomains)
		}
		plan.sets = append(plan.sets, member)
		for _, raw := range set.Targets.IpsToMatch {
			if prefix, ok := dscpParseTarget(raw); ok && member.matchesFamily(prefix.Addr()) {
				claims = append(claims, dscpClaimOf(prefix, index, value))
			}
		}
	}
	split := dscpPartitionFamilies(claims)
	plan.static4 = dscpFlatten(claims[:split], 32)
	plan.static6 = dscpFlatten(claims[split:], 128)
	plan.values = dscpRangeValues(plan.static4, plan.static6)
	plan.seal()
	return plan
}

func (s *dscpPlanSet) matchesFamily(addr netip.Addr) bool {
	if addr.Unmap().Is4() {
		return s.v4
	}
	return s.v6
}

func (p *dscpPlan) empty() bool {
	return p == nil || len(p.sets) == 0
}

func (p *dscpPlan) learning() []dscpPlanSet {
	var sets []dscpPlanSet
	for _, set := range p.sets {
		if set.learn {
			sets = append(sets, set)
		}
	}
	return sets
}

func (p *dscpPlan) equal(q *dscpPlan) bool {
	if p == nil || q == nil {
		return p == q
	}
	return p.fingerprint == q.fingerprint
}

func (p *dscpPlan) sameStatic(q *dscpPlan) bool {
	if p == nil || q == nil {
		return p == q
	}
	return p.staticKey == q.staticKey
}

func (p *dscpPlan) seal() {
	p.staticKey = dscpStaticKey(p.static4, p.static6)
	h := sha256.New()
	fmt.Fprintf(h, "%s %t %d %q\n", p.staticKey, p.globalOn, p.global, p.scopes)
	for _, s := range p.sets {
		fmt.Fprintf(h, "%q %q %d %d %t %d %d %t %t %q\n", s.id, s.sid, s.value, s.index, s.learn, s.dnsTTL, s.tlsTTL, s.v4, s.v6, s.domains)
	}
	for _, id := range slices.Sorted(maps.Keys(p.refusals)) {
		fmt.Fprintf(h, "%q %q\n", id, p.refusals[id])
	}
	p.fingerprint = hex.EncodeToString(h.Sum(nil))
}

func dscpStaticKey(static4, static6 []dscpRange) string {
	h := sha256.New()
	var b [33]byte
	for _, ranges := range [][]dscpRange{static4, static6} {
		binary.BigEndian.PutUint32(b[:4], uint32(len(ranges)))
		h.Write(b[:4])
		for _, r := range ranges {
			from, to := r.from.As16(), r.to.As16()
			copy(b[:16], from[:])
			copy(b[16:32], to[:])
			b[32] = byte(r.value)
			h.Write(b[:])
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func dscpLiteralDomains(entries []string) []string {
	var domains []string
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		domain, isRegex := sni.ParseDomainEntry(entry)
		if isRegex || domain == "" || seen[domain] {
			continue
		}
		seen[domain] = true
		domains = append(domains, domain)
		if len(domains) == dscpPreResolveCap {
			break
		}
	}
	return domains
}

func dscpParseTarget(raw string) (netip.Prefix, bool) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "/") {
		addr, err := netip.ParseAddr(raw)
		if err != nil || addr.Zone() != "" {
			return netip.Prefix{}, false
		}
		addr = addr.Unmap()
		return netip.PrefixFrom(addr, addr.BitLen()), true
	}
	prefix, err := netip.ParsePrefix(raw)
	if err != nil {
		return netip.Prefix{}, false
	}
	prefix = prefix.Masked()
	if prefix.Addr().Is4In6() && prefix.Bits() >= 96 {
		prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
	}
	return prefix, true
}

func dscpCanonicalKey(prefix netip.Prefix) string {
	prefix = prefix.Masked()
	if prefix.IsSingleIP() {
		return prefix.Addr().String()
	}
	return prefix.String()
}

func dscpRangeValues(lists ...[]dscpRange) []int {
	var present [config.MaxDSCPValue + 1]bool
	for _, ranges := range lists {
		for _, r := range ranges {
			present[r.value] = true
		}
	}
	var values []int
	for value, used := range present {
		if used {
			values = append(values, value)
		}
	}
	return values
}

func dscpRangeText(r dscpRange) string {
	if r.from == r.to {
		return r.from.String()
	}
	width := r.from.BitLen()
	from, to := dscpU128Of(r.from), dscpU128Of(r.to)
	if host := dscpBlockHostBits(from, to, width); from.or(dscpLowBits(host)) == to {
		return netip.PrefixFrom(r.from, width-host).String()
	}
	return r.from.String() + "-" + r.to.String()
}

func dscpRangeCover(r dscpRange) []netip.Prefix {
	width := r.from.BitLen()
	from, to := dscpU128Of(r.from), dscpU128Of(r.to)
	if width == 0 || r.to.BitLen() != width || to.less(from) {
		return nil
	}
	var cover []netip.Prefix
	for {
		host := dscpBlockHostBits(from, to, width)
		cover = append(cover, netip.PrefixFrom(from.addr(width), width-host))
		end := from.or(dscpLowBits(host))
		if end == to {
			return cover
		}
		from = end.next()
	}
}

func dscpUnionCover(ranges []dscpRange) []netip.Prefix {
	var cover []netip.Prefix
	start := 0
	for i := range ranges {
		if i+1 < len(ranges) && ranges[i].to.Next() == ranges[i+1].from {
			continue
		}
		cover = append(cover, dscpRangeCover(dscpRange{from: ranges[start].from, to: ranges[i].to})...)
		start = i + 1
	}
	return cover
}

type dscpClaim struct {
	v6    bool
	start dscpU128
	end   dscpU128
	bits  int
	index int
	value int
}

func dscpClaimOf(prefix netip.Prefix, index, value int) dscpClaim {
	start := dscpU128Of(prefix.Addr())
	return dscpClaim{
		v6:    prefix.Addr().Is6(),
		start: start,
		end:   start.or(dscpLowBits(prefix.Addr().BitLen() - prefix.Bits())),
		bits:  prefix.Bits(),
		index: index,
		value: value,
	}
}

func dscpPartitionFamilies(claims []dscpClaim) int {
	split := 0
	for i := range claims {
		if !claims[i].v6 {
			claims[split], claims[i] = claims[i], claims[split]
			split++
		}
	}
	return split
}

type dscpSpan struct {
	from  dscpU128
	to    dscpU128
	value int
}

func dscpAppendSpan(spans []dscpSpan, s dscpSpan) []dscpSpan {
	if n := len(spans); n > 0 && spans[n-1].value == s.value && spans[n-1].to.next() == s.from {
		spans[n-1].to = s.to
		return spans
	}
	return append(spans, s)
}

func dscpFlatten(claims []dscpClaim, width int) []dscpRange {
	slices.SortFunc(claims, func(a, b dscpClaim) int {
		return cmp.Or(a.start.cmp(b.start), cmp.Compare(a.bits, b.bits), cmp.Compare(a.index, b.index))
	})
	claims = slices.CompactFunc(claims, func(a, b dscpClaim) bool {
		return a.start == b.start && a.bits == b.bits
	})
	last := dscpLowBits(width)
	spans := make([]dscpSpan, 0, len(claims)+1)
	var open []dscpClaim
	var cursor dscpU128
	emit := func(to dscpU128, value int) {
		if !to.less(cursor) {
			spans = dscpAppendSpan(spans, dscpSpan{from: cursor, to: to, value: value})
		}
	}
	for _, c := range claims {
		for len(open) > 0 && open[len(open)-1].end.less(c.start) {
			top := open[len(open)-1]
			open = open[:len(open)-1]
			emit(top.end, top.value)
			cursor = top.end.next()
		}
		if len(open) > 0 && cursor.less(c.start) {
			emit(c.start.prev(), open[len(open)-1].value)
		}
		cursor = c.start
		open = append(open, c)
	}
	for len(open) > 0 {
		top := open[len(open)-1]
		open = open[:len(open)-1]
		emit(top.end, top.value)
		if top.end == last {
			break
		}
		cursor = top.end.next()
	}
	if len(spans) == 1 && spans[0].from == (dscpU128{}) && spans[0].to == last {
		half, value := dscpLowBits(width-1), spans[0].value
		spans = []dscpSpan{{to: half, value: value}, {from: half.next(), to: last, value: value}}
	}
	ranges := make([]dscpRange, len(spans))
	for i, s := range spans {
		ranges[i] = dscpRange{from: s.from.addr(width), to: s.to.addr(width), value: s.value}
	}
	return ranges
}

func dscpBlockHostBits(from, to dscpU128, width int) int {
	host := min(from.trailingZeros(), width-1)
	if count := to.sub(from).next(); count != (dscpU128{}) {
		host = min(host, count.log2())
	}
	return host
}

type dscpU128 struct {
	hi uint64
	lo uint64
}

func dscpU128Of(addr netip.Addr) dscpU128 {
	if addr.Is4() {
		b := addr.As4()
		return dscpU128{lo: uint64(binary.BigEndian.Uint32(b[:]))}
	}
	b := addr.As16()
	return dscpU128{hi: binary.BigEndian.Uint64(b[:8]), lo: binary.BigEndian.Uint64(b[8:])}
}

func dscpLowBits(n int) dscpU128 {
	switch {
	case n <= 0:
		return dscpU128{}
	case n < 64:
		return dscpU128{lo: 1<<n - 1}
	case n < 128:
		return dscpU128{hi: 1<<(n-64) - 1, lo: math.MaxUint64}
	}
	return dscpU128{hi: math.MaxUint64, lo: math.MaxUint64}
}

func (u dscpU128) addr(width int) netip.Addr {
	if width == 32 {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], uint32(u.lo))
		return netip.AddrFrom4(b)
	}
	var b [16]byte
	binary.BigEndian.PutUint64(b[:8], u.hi)
	binary.BigEndian.PutUint64(b[8:], u.lo)
	return netip.AddrFrom16(b)
}

func (u dscpU128) cmp(v dscpU128) int {
	return cmp.Or(cmp.Compare(u.hi, v.hi), cmp.Compare(u.lo, v.lo))
}

func (u dscpU128) less(v dscpU128) bool {
	return u.cmp(v) < 0
}

func (u dscpU128) or(v dscpU128) dscpU128 {
	return dscpU128{hi: u.hi | v.hi, lo: u.lo | v.lo}
}

func (u dscpU128) next() dscpU128 {
	lo, carry := bits.Add64(u.lo, 1, 0)
	return dscpU128{hi: u.hi + carry, lo: lo}
}

func (u dscpU128) prev() dscpU128 {
	lo, borrow := bits.Sub64(u.lo, 1, 0)
	return dscpU128{hi: u.hi - borrow, lo: lo}
}

func (u dscpU128) sub(v dscpU128) dscpU128 {
	lo, borrow := bits.Sub64(u.lo, v.lo, 0)
	hi, _ := bits.Sub64(u.hi, v.hi, borrow)
	return dscpU128{hi: hi, lo: lo}
}

func (u dscpU128) trailingZeros() int {
	if u.lo != 0 {
		return bits.TrailingZeros64(u.lo)
	}
	return 64 + bits.TrailingZeros64(u.hi)
}

func (u dscpU128) log2() int {
	if u.hi != 0 {
		return 127 - bits.LeadingZeros64(u.hi)
	}
	return 63 - bits.LeadingZeros64(u.lo)
}
