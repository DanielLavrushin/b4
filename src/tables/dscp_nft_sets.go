package tables

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/log"
)

const (
	dscpNftFillBatch = 2000
	dscpNftFillChunk = 1000
)

type dscpNftLayout struct {
	generation int
	staticKey  string
	maps       []int
	values     []int
	learned    []string
	stamps     int
	vmaps      int
}

type dscpNftOutcome struct {
	layout  *dscpNftLayout
	pending bool
	rebuilt bool
	gone    bool
}

func (l *dscpNftLayout) perSet() bool {
	return l != nil && l.generation > 0
}

func (l *dscpNftLayout) clone() *dscpNftLayout {
	c := *l
	c.maps = slices.Clone(l.maps)
	c.values = slices.Clone(l.values)
	c.learned = slices.Clone(l.learned)
	return &c
}

func dscpNftScopeCount(plan *dscpPlan) int {
	return max(1, len(plan.scopes))
}

func dscpNftStampCount(plan *dscpPlan) int {
	stamped := len(plan.learning())
	if plan.globalOn {
		stamped++
	}
	return 2 * dscpNftScopeCount(plan) * stamped
}

func dscpNftVmapCount(plan *dscpPlan) int {
	return 2 * dscpNftScopeCount(plan)
}

func dscpNftValueChain(value int) string {
	return "v" + strconv.Itoa(value)
}

func dscpNftMap(generation int, v6 bool) string {
	if v6 {
		return "s6_" + strconv.Itoa(generation)
	}
	return "s4_" + strconv.Itoa(generation)
}

func dscpNftMapSpec(v6 bool) string {
	if v6 {
		return "{ type ipv6_addr : verdict ; flags interval ; }"
	}
	return "{ type ipv4_addr : verdict ; flags interval ; }"
}

func dscpNftLearnedSet(sid string, v6 bool) string {
	if v6 {
		return "l_" + sid + "_6"
	}
	return "l_" + sid + "_4"
}

func dscpNftLearnedSpec(v6 bool) string {
	if v6 {
		return "{ type ipv6_addr ; flags timeout ; }"
	}
	return "{ type ipv4_addr ; flags timeout ; }"
}

func dscpNftScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return []string{""}
	}
	prefixes := make([]string, len(scopes))
	for i, iface := range scopes {
		prefixes[i] = fmt.Sprintf("oifname %q ", iface)
	}
	return prefixes
}

func dscpNftLearnedIDs(plan *dscpPlan) []string {
	learning := plan.learning()
	ids := make([]string, 0, len(learning))
	for _, set := range learning {
		ids = append(ids, set.sid)
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

func dscpNftRules(plan *dscpPlan, generation int) []string {
	scopes := dscpNftScopes(plan.scopes)
	rules := []string{
		`oifname "lo" return`,
		fmt.Sprintf("meta mark & 0x%x == 0x%x return", engine.ClientMark, engine.ClientMark),
		"ct direction reply return",
	}
	if plan.globalOn {
		for _, scope := range scopes {
			rules = append(rules,
				fmt.Sprintf("%smeta nfproto ipv4 ip dscp set %d", scope, plan.global),
				fmt.Sprintf("%smeta nfproto ipv6 ip6 dscp set %d", scope, plan.global))
		}
	}
	for _, scope := range scopes {
		rules = append(rules,
			fmt.Sprintf("%sip daddr vmap @%s", scope, dscpNftMap(generation, false)),
			fmt.Sprintf("%sip6 daddr vmap @%s", scope, dscpNftMap(generation, true)))
	}
	sets := plan.learning()
	slices.SortStableFunc(sets, func(a, b dscpPlanSet) int { return cmp.Compare(b.index, a.index) })
	for _, scope := range scopes {
		for _, set := range sets {
			rules = append(rules,
				fmt.Sprintf("%sip daddr @%s ip dscp set %d", scope, dscpNftLearnedSet(set.sid, false), set.value),
				fmt.Sprintf("%sip6 daddr @%s ip6 dscp set %d", scope, dscpNftLearnedSet(set.sid, true), set.value))
		}
	}
	return rules
}

func dscpNftWriteRules(b *strings.Builder, plan *dscpPlan, generation int) {
	for _, rule := range dscpNftRules(plan, generation) {
		fmt.Fprintf(b, "add rule inet %s %s %s\n", dscpNftTable, dscpNftChain, rule)
	}
}

func dscpNftWriteValueChains(b *strings.Builder, values []int) {
	for _, value := range values {
		chain := dscpNftValueChain(value)
		fmt.Fprintf(b, "add chain inet %s %s\n", dscpNftTable, chain)
		fmt.Fprintf(b, "flush chain inet %s %s\n", dscpNftTable, chain)
		fmt.Fprintf(b, "add rule inet %s %s ip dscp set %d\n", dscpNftTable, chain, value)
		fmt.Fprintf(b, "add rule inet %s %s ip6 dscp set %d\n", dscpNftTable, chain, value)
	}
}

func dscpNftWriteMaps(b *strings.Builder, generation int, flush bool) {
	for _, v6 := range []bool{false, true} {
		name := dscpNftMap(generation, v6)
		fmt.Fprintf(b, "add map inet %s %s %s\n", dscpNftTable, name, dscpNftMapSpec(v6))
		if flush {
			fmt.Fprintf(b, "flush map inet %s %s\n", dscpNftTable, name)
		}
	}
}

func dscpNftWriteElements(b *strings.Builder, name string, ranges []dscpRange) {
	if len(ranges) == 0 {
		return
	}
	fmt.Fprintf(b, "add element inet %s %s { ", dscpNftTable, name)
	for i, r := range ranges {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(dscpRangeText(r))
		b.WriteString(" : jump ")
		b.WriteString(dscpNftValueChain(r.value))
	}
	b.WriteString(" }\n")
}

func dscpNftWriteAllElements(b *strings.Builder, plan *dscpPlan, generation int) {
	dscpNftWriteElements(b, dscpNftMap(generation, false), plan.static4)
	dscpNftWriteElements(b, dscpNftMap(generation, true), plan.static6)
}

func dscpNftWriteLearnedSets(b *strings.Builder, ids []string) {
	for _, id := range ids {
		for _, v6 := range []bool{false, true} {
			fmt.Fprintf(b, "add set inet %s %s %s\n", dscpNftTable, dscpNftLearnedSet(id, v6), dscpNftLearnedSpec(v6))
		}
	}
}

func dscpNftElementCount(plan *dscpPlan) int {
	return len(plan.static4) + len(plan.static6)
}

func dscpNftElementScripts(plan *dscpPlan, generation, chunk int) []string {
	var scripts []string
	for _, v6 := range []bool{false, true} {
		ranges := plan.static4
		if v6 {
			ranges = plan.static6
		}
		for start := 0; start < len(ranges); start += chunk {
			var b strings.Builder
			dscpNftWriteElements(&b, dscpNftMap(generation, v6), ranges[start:min(start+chunk, len(ranges))])
			scripts = append(scripts, b.String())
		}
	}
	return scripts
}

func dscpNftReplaceScript(plan *dscpPlan, elements bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "add table inet %s\n", dscpNftTable)
	fmt.Fprintf(&b, "delete table inet %s\n", dscpNftTable)
	fmt.Fprintf(&b, "add table inet %s\n", dscpNftTable)
	fmt.Fprintf(&b, "add chain inet %s %s { type filter hook postrouting priority %d ; policy accept ; }\n", dscpNftTable, dscpNftChain, dscpNftPriority)
	dscpNftWriteValueChains(&b, plan.values)
	dscpNftWriteMaps(&b, 1, false)
	if elements {
		dscpNftWriteAllElements(&b, plan, 1)
	}
	dscpNftWriteLearnedSets(&b, dscpNftLearnedIDs(plan))
	dscpNftWriteRules(&b, plan, 1)
	return b.String()
}

func dscpNftFillScript(plan *dscpPlan, generation int, fresh []int, elements bool) string {
	var b strings.Builder
	dscpNftWriteValueChains(&b, fresh)
	dscpNftWriteMaps(&b, generation, true)
	if elements {
		dscpNftWriteAllElements(&b, plan, generation)
	}
	return b.String()
}

func dscpNftSwapScript(plan *dscpPlan, generation int) string {
	var b strings.Builder
	dscpNftWriteLearnedSets(&b, dscpNftLearnedIDs(plan))
	fmt.Fprintf(&b, "flush chain inet %s %s\n", dscpNftTable, dscpNftChain)
	dscpNftWriteRules(&b, plan, generation)
	return b.String()
}

func dscpNftDropScript(maps []int, learned []string) string {
	var b strings.Builder
	for _, generation := range maps {
		for _, v6 := range []bool{false, true} {
			name := dscpNftMap(generation, v6)
			fmt.Fprintf(&b, "add map inet %s %s %s\n", dscpNftTable, name, dscpNftMapSpec(v6))
			fmt.Fprintf(&b, "delete map inet %s %s\n", dscpNftTable, name)
		}
	}
	for _, id := range learned {
		for _, v6 := range []bool{false, true} {
			name := dscpNftLearnedSet(id, v6)
			fmt.Fprintf(&b, "add set inet %s %s %s\n", dscpNftTable, name, dscpNftLearnedSpec(v6))
			fmt.Fprintf(&b, "delete set inet %s %s\n", dscpNftTable, name)
		}
	}
	return b.String()
}

func dscpNftDropChainsScript(values []int) string {
	var b strings.Builder
	for _, value := range values {
		chain := dscpNftValueChain(value)
		fmt.Fprintf(&b, "add chain inet %s %s\n", dscpNftTable, chain)
		fmt.Fprintf(&b, "flush chain inet %s %s\n", dscpNftTable, chain)
		fmt.Fprintf(&b, "delete chain inet %s %s\n", dscpNftTable, chain)
	}
	return b.String()
}

func dscpNftMissing[T cmp.Ordered](want, have []T) []T {
	var missing []T
	for _, v := range want {
		if !slices.Contains(have, v) {
			missing = append(missing, v)
		}
	}
	return missing
}

func dscpNftUnion[T cmp.Ordered](a, b []T) []T {
	out := append(slices.Clone(a), b...)
	slices.Sort(out)
	return slices.Compact(out)
}

func dscpNftTooLong(out string, err error) bool {
	return strings.Contains(out, "Message too long") || (err != nil && strings.Contains(err.Error(), "Message too long"))
}

func dscpNftLoad(script string) (bool, error) {
	out, err := runNftStdin(script)
	if err != nil {
		return dscpTransient(out, err), err
	}
	return false, nil
}

func dscpNftLoadChunks(plan *dscpPlan, generation int) (bool, error) {
	scripts := dscpNftElementScripts(plan, generation, dscpNftFillChunk)
	log.Tracef("NFTABLES: loading %d DSCP map elements into %s and %s in %d transactions", dscpNftElementCount(plan), dscpNftMap(generation, false), dscpNftMap(generation, true), len(scripts))
	for _, script := range scripts {
		if transient, err := dscpNftLoad(script); err != nil {
			return transient, err
		}
	}
	return false, nil
}

func dscpNftLoadInline(script string, elements int) (bool, bool, error) {
	out, err := runNftStdin(script)
	if err == nil {
		return true, false, nil
	}
	if elements > 0 && dscpNftTooLong(out, err) {
		log.Tracef("NFTABLES: loading %d DSCP map elements in one transaction failed (%s), loading them in batches of %d", elements, routeNftScriptError(err), dscpNftFillChunk)
		return false, false, nil
	}
	return false, dscpTransient(out, err), err
}

func dscpNftFill(plan *dscpPlan, generation int, fresh []int) (bool, error) {
	if elements := dscpNftElementCount(plan); elements <= dscpNftFillBatch {
		done, transient, err := dscpNftLoadInline(dscpNftFillScript(plan, generation, fresh, true), elements)
		if done || err != nil {
			return transient, err
		}
	}
	if transient, err := dscpNftLoad(dscpNftFillScript(plan, generation, fresh, false)); err != nil {
		return transient, err
	}
	return dscpNftLoadChunks(plan, generation)
}

func dscpNftReplace(plan *dscpPlan) (*dscpNftLayout, bool, error) {
	done := false
	if elements := dscpNftElementCount(plan); elements <= dscpNftFillBatch {
		loaded, transient, err := dscpNftLoadInline(dscpNftReplaceScript(plan, true), elements)
		if err != nil {
			return nil, transient, err
		}
		done = loaded
	}
	if !done {
		if transient, err := dscpNftLoad(dscpNftReplaceScript(plan, false)); err != nil {
			return nil, transient, err
		}
		if transient, err := dscpNftLoadChunks(plan, 1); err != nil {
			return nil, transient, err
		}
	}
	return &dscpNftLayout{
		generation: 1,
		staticKey:  plan.staticKey,
		maps:       []int{1},
		values:     slices.Clone(plan.values),
		learned:    dscpNftLearnedIDs(plan),
		stamps:     dscpNftStampCount(plan),
		vmaps:      dscpNftVmapCount(plan),
	}, false, nil
}

func dscpNftUpdate(plan *dscpPlan, prev *dscpNftLayout) (*dscpNftLayout, bool, error) {
	next := prev.clone()
	next.stamps, next.vmaps = dscpNftStampCount(plan), dscpNftVmapCount(plan)
	if plan.staticKey != prev.staticKey {
		generation := next.generation + 1
		for _, g := range next.maps {
			generation = max(generation, g+1)
		}
		fresh := dscpNftMissing(plan.values, next.values)
		if transient, err := dscpNftFill(plan, generation, fresh); err != nil {
			if transient {
				next = prev.clone()
				next.maps = append(next.maps, generation)
				return next, true, err
			}
			return nil, false, err
		}
		next.generation, next.staticKey = generation, plan.staticKey
		next.maps = append(next.maps, generation)
		next.values = dscpNftUnion(next.values, fresh)
	}
	if transient, err := dscpNftLoad(dscpNftSwapScript(plan, next.generation)); err != nil {
		if transient {
			pending := prev.clone()
			pending.maps = dscpNftUnion(pending.maps, next.maps)
			pending.values = next.values
			pending.learned = dscpNftUnion(pending.learned, dscpNftLearnedIDs(plan))
			return pending, true, err
		}
		return nil, false, err
	}
	next.learned = dscpNftUnion(next.learned, dscpNftLearnedIDs(plan))
	dscpNftTidy(plan, next)
	return next, false, nil
}

func dscpNftTidy(plan *dscpPlan, layout *dscpNftLayout) {
	ids := dscpNftLearnedIDs(plan)
	stale := dscpNftMissing(layout.maps, []int{layout.generation})
	departed := dscpNftMissing(layout.learned, ids)
	if len(stale) > 0 || len(departed) > 0 {
		if out, err := runNftStdin(dscpNftDropScript(stale, departed)); err != nil {
			log.Tracef("NFTABLES: could not delete the DSCP maps and sets the %s table no longer uses, b4 tries again at the next change: %s", dscpNftTable, iptErrText(out, err))
			return
		}
		layout.maps = []int{layout.generation}
		layout.learned = ids
	}
	unused := dscpNftMissing(layout.values, plan.values)
	if len(unused) == 0 {
		return
	}
	if out, err := runNftStdin(dscpNftDropChainsScript(unused)); err != nil {
		if !dscpTransient(out, err) {
			log.Tracef("NFTABLES: could not delete the DSCP value chains the %s table no longer uses, b4 tries again at the next change: %s", dscpNftTable, iptErrText(out, err))
			return
		}
		log.Tracef("NFTABLES: nftables did not answer while deleting the DSCP value chains the %s table no longer uses, so b4 creates them again when a set needs their value: %s", dscpNftTable, iptErrText(out, err))
	}
	layout.values = slices.Clone(plan.values)
}

func dscpNftSetValues(plan *dscpPlan) string {
	var values []string
	for _, set := range plan.sets {
		if v := strconv.Itoa(set.value); !slices.Contains(values, v) {
			values = append(values, v)
		}
	}
	return strings.Join(values, ", ")
}

func dscpNftLogApplied(plan *dscpPlan) {
	if plan.globalOn {
		log.Infof("NFTABLES: DSCP %d is written into the packets this host sends out, and the sets' own values (%s) into the packets sent to their addresses (%s)",
			plan.global, dscpNftSetValues(plan), dscpScopeLabel(plan.scopes))
		return
	}
	log.Infof("NFTABLES: the sets' own DSCP values (%s) are written into the packets this host sends to their addresses (%s)",
		dscpNftSetValues(plan), dscpScopeLabel(plan.scopes))
}

func dscpNftBusy(err error) error {
	return fmt.Errorf("nftables did not answer while loading the %s table, the firewall monitor tries again: %w", dscpNftTable, err)
}

func dscpNftApplyPlan(plan *dscpPlan, prev *dscpNftLayout) (dscpNftOutcome, error) {
	if plan.empty() {
		return dscpNftGlobalOnly(plan, nil)
	}
	var errs []error
	if prev.perSet() {
		layout, transient, err := dscpNftUpdate(plan, prev)
		switch {
		case err == nil:
			dscpNftLogApplied(plan)
			return dscpNftOutcome{layout: layout}, nil
		case transient:
			return dscpNftOutcome{layout: layout, pending: true}, dscpNftBusy(err)
		}
		errs = append(errs, fmt.Errorf("nftables rejected the change to the per-set DSCP objects in the %s table, so b4 builds the table again and the addresses it learned for the sets start over: %s", dscpNftTable, routeNftScriptError(err)))
	}
	layout, transient, err := dscpNftReplace(plan)
	switch {
	case err == nil:
		dscpNftLogApplied(plan)
		return dscpNftOutcome{layout: layout, rebuilt: true}, errors.Join(errs...)
	case transient:
		return dscpNftOutcome{pending: true, rebuilt: true}, errors.Join(append(errs, dscpNftBusy(err))...)
	case plan.globalOn:
		errs = append(errs, fmt.Errorf("nftables rejected the per-set DSCP rules of the %s table, so packets to the sets' addresses get DSCP %d like all others: %s", dscpNftTable, plan.global, routeNftScriptError(err)))
	default:
		errs = append(errs, fmt.Errorf("nftables rejected the per-set DSCP rules of the %s table, so packets to the sets' addresses go out without the sets' DSCP values: %s", dscpNftTable, routeNftScriptError(err)))
	}
	return dscpNftGlobalOnly(plan, errs)
}

func dscpNftGlobalOnly(plan *dscpPlan, errs []error) (dscpNftOutcome, error) {
	if plan != nil && plan.globalOn {
		script, stamps := dscpNftScript(plan.global, plan.scopes)
		out, err := runNftStdin(script)
		switch {
		case err == nil:
			log.Infof("NFTABLES: DSCP %d is written into the packets this host sends out (%s)", plan.global, dscpScopeLabel(plan.scopes))
			return dscpNftOutcome{layout: &dscpNftLayout{stamps: stamps}, rebuilt: true}, errors.Join(errs...)
		case dscpTransient(out, err):
			return dscpNftOutcome{pending: true, rebuilt: true}, errors.Join(append(errs, dscpNftBusy(err))...)
		}
		errs = append(errs, fmt.Errorf("nftables rejected the %s table, so neither IPv4 nor IPv6 packets get the DSCP value: %w", dscpNftTable, err))
	}
	gone, _ := removeDSCPNft()
	return dscpNftOutcome{rebuilt: true, gone: gone}, errors.Join(errs...)
}

func dscpNftSetsChainShape(listing string, stamps, vmaps int) bool {
	if !dscpNftChainShape(listing, stamps) {
		return false
	}
	returns, seen := 0, 0
	for _, line := range strings.Split(listing, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.Contains(line, "daddr vmap @s"):
			if returns < dscpReturnRules {
				return false
			}
			seen++
		case line == "return" || strings.HasSuffix(line, " return"):
			returns++
		}
	}
	return seen == vmaps
}

func dscpNftLayoutIntact(layout *dscpNftLayout) bool {
	out, err := run("nft", "list", "chain", "inet", dscpNftTable, dscpNftChain)
	return err == nil && dscpNftSetsChainShape(out, layout.stamps, layout.vmaps)
}
