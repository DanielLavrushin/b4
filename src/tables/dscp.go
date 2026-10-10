package tables

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/log"
)

const (
	dscpChainName    = "B4_DSCP"
	dscpCaptureChain = "B4"
	dscpNftTable     = "b4_dscp"
	dscpNftChain     = "postrouting"
	dscpNftPriority  = 150
	dscpReturnRules  = 3
)

type dscpState struct {
	cfg     *config.Config
	backend string
	bins    []string
	stamps  int
	pending bool
	plan    *dscpPlan
	ipt     *dscpIptState
	nft     *dscpNftLayout
}

func (st *dscpState) ipsets() []string {
	if st == nil || st.ipt == nil {
		return nil
	}
	return dscpNftUnion(st.ipt.sets, st.ipt.pending)
}

func (st *dscpState) perSet() bool {
	return st != nil && (!st.plan.empty() || st.nft.perSet() || len(st.ipsets()) > 0)
}

func (st *dscpState) leftover(names []string) *dscpState {
	if len(names) == 0 && st.ipt == nil {
		return st
	}
	return &dscpState{cfg: st.cfg, backend: st.backend, ipt: &dscpIptState{pending: names}}
}

func (st *dscpState) withLeftovers(from *dscpState) *dscpState {
	names := from.ipsets()
	if len(names) == 0 {
		return st
	}
	next := &dscpState{cfg: from.cfg, backend: from.backend}
	if st != nil {
		c := *st
		next = &c
	}
	ipt := &dscpIptState{}
	if next.ipt != nil {
		c := *next.ipt
		ipt = &c
	}
	ipt.pending = dscpNftUnion(ipt.pending, names)
	next.ipt = ipt
	return next
}

var (
	dscpApplied       atomic.Pointer[dscpState]
	dscpStale         atomic.Pointer[dscpState]
	dscpLast          atomic.Pointer[config.Config]
	dscpWarned        sync.Map
	dscpKeepOnRefresh bool
)

func dscpScopeLabel(ifaces []string) string {
	if len(ifaces) == 0 {
		return "every interface"
	}
	return strings.Join(ifaces, ", ")
}

func dscpIptSpecs(value int, ifaces []string) [][]string {
	set := []string{"-j", "DSCP", "--set-dscp", strconv.Itoa(value)}
	specs := [][]string{
		{"-o", "lo", "-j", "RETURN"},
		{"-m", "mark", "--mark", fmt.Sprintf("0x%x/0x%x", engine.ClientMark, engine.ClientMark), "-j", "RETURN"},
		{"-m", "conntrack", "--ctdir", "REPLY", "-j", "RETURN"},
	}
	if len(ifaces) == 0 {
		return append(specs, set)
	}
	for _, iface := range ifaces {
		specs = append(specs, append([]string{"-o", iface}, set...))
	}
	return specs
}

func dscpIptStampCount(ifaces []string) int {
	if len(ifaces) == 0 {
		return 1
	}
	return len(ifaces)
}

func dscpNftScript(value int, ifaces []string) (string, int) {
	var b strings.Builder
	fmt.Fprintf(&b, "add table inet %s\n", dscpNftTable)
	fmt.Fprintf(&b, "delete table inet %s\n", dscpNftTable)
	fmt.Fprintf(&b, "add table inet %s\n", dscpNftTable)
	fmt.Fprintf(&b, "add chain inet %s %s { type filter hook postrouting priority %d ; policy accept ; }\n", dscpNftTable, dscpNftChain, dscpNftPriority)
	fmt.Fprintf(&b, "add rule inet %s %s oifname \"lo\" return\n", dscpNftTable, dscpNftChain)
	fmt.Fprintf(&b, "add rule inet %s %s meta mark & 0x%x == 0x%x return\n", dscpNftTable, dscpNftChain, engine.ClientMark, engine.ClientMark)
	fmt.Fprintf(&b, "add rule inet %s %s ct direction reply return\n", dscpNftTable, dscpNftChain)
	scopes := []string{""}
	if len(ifaces) > 0 {
		scopes = scopes[:0]
		for _, iface := range ifaces {
			scopes = append(scopes, fmt.Sprintf("oifname %q ", iface))
		}
	}
	stamps := 0
	for _, scope := range scopes {
		fmt.Fprintf(&b, "add rule inet %s %s %smeta nfproto ipv4 ip dscp set %d\n", dscpNftTable, dscpNftChain, scope, value)
		fmt.Fprintf(&b, "add rule inet %s %s %smeta nfproto ipv6 ip6 dscp set %d\n", dscpNftTable, dscpNftChain, scope, value)
		stamps += 2
	}
	return b.String(), stamps
}

func warnDSCPOnce(err error) {
	msg := err.Error()
	if _, seen := dscpWarned.LoadOrStore(msg, true); seen {
		log.Tracef("DSCP stamp: %s", msg)
		return
	}
	log.Warnf("DSCP stamp: %s", msg)
}

func applyDSCPLogged(cfg *config.Config, backend string) {
	if err := applyDSCPFor(cfg, backend); err != nil {
		warnDSCPOnce(err)
	}
}

func applyDSCPFor(cfg *config.Config, backend string) error {
	rebuilt := false
	defer func() { dscpLearnAdopt(dscpApplied.Load(), rebuilt) }()
	value, ifaces, on := cfg.DSCPStamp()
	plan := dscpPlanFor(cfg)
	if !on && plan.empty() {
		if dscpApplied.Load() != nil {
			clearDSCPFor(cfg, backend)
		}
		return nil
	}
	prev := dscpApplied.Load()
	if prev != nil && prev.backend != backend {
		removeDSCPOrPark(prev)
		prev = nil
	}
	if s := dscpStale.Load(); s != nil && s.backend == backend && dscpStale.CompareAndSwap(s, nil) {
		prev = prev.withLeftovers(s)
	}
	switch {
	case backend == backendNFTables && plan.empty():
		return applyDSCPNft(cfg, value, ifaces)
	case backend == backendNFTables:
		var err error
		rebuilt, err = applyDSCPNftPlan(cfg, plan, prev)
		return err
	case plan.empty() && !prev.perSet():
		return applyDSCPIpt(cfg, backend, value, ifaces)
	}
	return applyDSCPIptPlan(cfg, backend, plan, prev)
}

func applyDSCPNftPlan(cfg *config.Config, plan *dscpPlan, prev *dscpState) (bool, error) {
	var layout *dscpNftLayout
	if prev != nil {
		layout = prev.nft
	}
	out, err := dscpNftApplyPlan(plan, layout)
	switch {
	case out.layout != nil:
		dscpApplied.Store(&dscpState{cfg: cfg, backend: backendNFTables, stamps: out.layout.stamps, pending: out.pending, plan: plan, nft: out.layout})
	case out.pending:
		dscpApplied.Store(&dscpState{cfg: cfg, backend: backendNFTables, pending: true, plan: plan})
	default:
		if !out.gone {
			dscpStale.Store(&dscpState{cfg: cfg, backend: backendNFTables})
		}
		dscpApplied.Store(nil)
	}
	return out.rebuilt, err
}

func applyDSCPIptPlan(cfg *config.Config, backend string, plan *dscpPlan, prev *dscpState) error {
	var installed *dscpIptState
	if prev != nil {
		installed = prev.ipt
	}
	ipt, err := dscpIptApplyPlan(cfg, backend, plan, installed)
	switch {
	case ipt == installed:
		return err
	case len(ipt.bins) == 0 && len(ipt.pending) == 0 && !ipt.retry:
		dscpApplied.Store(nil)
		return err
	}
	dscpApplied.Store(&dscpState{cfg: cfg, backend: backend, bins: ipt.bins, pending: ipt.retry, plan: plan, ipt: ipt})
	return err
}

func dscpTransient(out string, err error) bool {
	return isXtablesLockBusy(out, err) || errors.Is(err, context.DeadlineExceeded)
}

func applyDSCPNft(cfg *config.Config, value int, ifaces []string) error {
	script, stamps := dscpNftScript(value, ifaces)
	if out, err := runNftStdin(script); err != nil {
		if dscpTransient(out, err) {
			dscpApplied.Store(&dscpState{cfg: cfg, backend: backendNFTables, stamps: stamps, pending: true})
			return fmt.Errorf("nftables did not answer while loading the %s table, the firewall monitor tries again: %w", dscpNftTable, err)
		}
		if gone, _ := removeDSCPNft(); !gone {
			dscpStale.Store(&dscpState{cfg: cfg, backend: backendNFTables})
		}
		dscpApplied.Store(nil)
		return fmt.Errorf("nftables rejected the %s table, so neither IPv4 nor IPv6 packets get the DSCP value: %w", dscpNftTable, err)
	}
	dscpApplied.Store(&dscpState{cfg: cfg, backend: backendNFTables, stamps: stamps})
	log.Infof("NFTABLES: DSCP %d is written into the packets this host sends out (%s)", value, dscpScopeLabel(ifaces))
	return nil
}

func applyDSCPIpt(cfg *config.Config, backend string, value int, ifaces []string) error {
	im := NewIPTablesManager(cfg, backend == backendIPTablesLegacy)
	loadKernelModuleList("xt_DSCP")
	specs := dscpIptSpecs(value, ifaces)
	var installed, wanted []string
	var errs []error
	for _, bin := range im.teardownBinaries() {
		permanent, err := im.applyDSCPChain(bin, specs)
		if err == nil {
			installed = append(installed, bin)
			wanted = append(wanted, bin)
			continue
		}
		if permanent {
			im.teardownDSCPChain(bin)
			errs = append(errs, fmt.Errorf("%s, so %s packets go out without the DSCP value: %w", bin, iptFamilyLabel(bin), err))
			continue
		}
		wanted = append(wanted, bin)
		errs = append(errs, fmt.Errorf("%s could not finish the DSCP rules for %s packets, the firewall monitor tries again: %w", bin, iptFamilyLabel(bin), err))
	}
	if len(wanted) == 0 {
		dscpApplied.Store(nil)
	} else {
		dscpApplied.Store(&dscpState{cfg: cfg, backend: backend, bins: wanted, stamps: dscpIptStampCount(ifaces), pending: len(installed) < len(wanted)})
	}
	if len(installed) > 0 {
		log.Infof("IPTABLES: DSCP %d is written into the packets this host sends out (%s; %s)", value, dscpScopeLabel(ifaces), strings.Join(installed, ", "))
	}
	return errors.Join(errs...)
}

func (im *IPTablesManager) dscpChainMatches(bin string, specs [][]string) bool {
	out, err := run(bin, "-w", "-t", "mangle", "-S", dscpChainName)
	if err != nil || !dscpIptChainShape(out, len(specs)-dscpReturnRules) {
		return false
	}
	for _, spec := range specs {
		if !im.existsRule(bin, "mangle", dscpChainName, spec) {
			return false
		}
	}
	return true
}

var iptAbsentMarkers = []string{
	"No chain/target/match",
	"does not exist",
	"do you need to insmod",
	"Address family not supported",
	"Protocol not supported",
}

func iptChainPresence(bin, table, chain string) (present, known bool) {
	_, present, known = iptChainListing(bin, table, chain)
	return present, known
}

func iptChainListing(bin, table, chain string) (listing string, present, known bool) {
	out, err := run(bin, "-w", "-t", table, "-S", chain)
	if err == nil {
		return out, true, true
	}
	if dscpTransient(out, err) {
		return "", false, false
	}
	msg := iptErrText(out, err)
	for _, marker := range iptAbsentMarkers {
		if strings.Contains(msg, marker) {
			return "", false, true
		}
	}
	return "", false, false
}

func (im *IPTablesManager) applyDSCPChain(bin string, specs [][]string) (bool, error) {
	if !im.dscpChainMatches(bin, specs) {
		if !im.existsChain(bin, "mangle", dscpChainName) {
			if out, err := run(bin, "-w", "-t", "mangle", "-N", dscpChainName); err != nil {
				present, _ := iptChainPresence(bin, "mangle", dscpChainName)
				exists := present || strings.Contains(iptErrText(out, err), "already exists")
				permanent := !dscpTransient(out, err) && !exists
				return permanent, fmt.Errorf("could not create the mangle chain %s: %s", dscpChainName, iptErrText(out, err))
			}
		} else if out, err := run(bin, "-w", "-t", "mangle", "-F", dscpChainName); err != nil {
			return false, fmt.Errorf("could not flush the mangle chain %s: %s", dscpChainName, iptErrText(out, err))
		}
		for _, spec := range specs {
			out, err := run(append([]string{bin, "-w", "-t", "mangle", "-A", dscpChainName}, spec...)...)
			if err == nil {
				continue
			}
			if present, known := iptChainPresence(bin, "mangle", dscpChainName); dscpTransient(out, err) || !known || !present {
				return false, fmt.Errorf("the mangle table was busy or changed while b4 was filling %s: %s", dscpChainName, iptErrText(out, err))
			}
			if iptSpecTarget(spec) == "DSCP" {
				kmodNoteRejected("xt_DSCP", bin, out)
				return true, fmt.Errorf("the DSCP target was rejected (%s). %s", iptErrText(out, err), kmodMissingHint([]string{"xt_DSCP"}))
			}
			return true, fmt.Errorf("'%s' was rejected: %s", strings.Join(spec, " "), iptErrText(out, err))
		}
	}
	return false, iptSeatDSCPJump(bin)
}

func iptSpecTarget(spec []string) string {
	for i := 0; i+1 < len(spec); i++ {
		if spec[i] == "-j" {
			return spec[i+1]
		}
	}
	return ""
}

func iptErrText(out string, err error) string {
	if msg := strings.TrimSpace(out); msg != "" {
		return msg
	}
	return err.Error()
}

func dscpJumpPlacement(listing string) (jump, capture, copies int) {
	for _, r := range iptListedRules(listing) {
		switch r.target {
		case dscpChainName:
			copies++
			if jump == 0 {
				jump = r.n
			}
		case dscpCaptureChain:
			if capture == 0 {
				capture = r.n
			}
		}
	}
	return jump, capture, copies
}

func dscpJumpSeated(listing string) bool {
	jump, capture, copies := dscpJumpPlacement(listing)
	return copies == 1 && (capture == 0 || jump < capture)
}

func dscpCaptureSlot(bin string) []string {
	st := dscpApplied.Load()
	if st == nil || st.backend == backendNFTables || !slices.Contains(st.bins, bin) {
		return nil
	}
	listing, err := run(bin, "-w", "-t", "mangle", "-L", "POSTROUTING", "-n", "--line-numbers")
	if err != nil {
		return nil
	}
	jump, _, _ := dscpJumpPlacement(listing)
	if jump == 0 {
		return nil
	}
	if jump > 1 {
		if _, err := run(bin, "-w", "-t", "mangle", "-I", "POSTROUTING", "1", "-j", dscpChainName); err != nil {
			return nil
		}
		iptDropExtraJumps(bin, "mangle", "POSTROUTING", dscpChainName)
	}
	return []string{"2"}
}

func iptSeatDSCPJump(bin string) error {
	listing, err := run(bin, "-w", "-t", "mangle", "-L", "POSTROUTING", "-n", "--line-numbers")
	if err != nil {
		return fmt.Errorf("could not read mangle POSTROUTING: %s", iptErrText(listing, err))
	}
	jump, capture, copies := dscpJumpPlacement(listing)
	if jump == 0 || (capture > 0 && capture < jump) {
		if copies > 0 {
			log.Tracef("IPTABLES[%s]: the jump to %s sits below b4's capture jump in mangle POSTROUTING, where the packets b4 inspects would skip it; moving it to the top", bin, dscpChainName)
		}
		if out, err := run(bin, "-w", "-t", "mangle", "-I", "POSTROUTING", "1", "-j", dscpChainName); err != nil {
			return fmt.Errorf("could not add the mangle POSTROUTING jump to %s: %s", dscpChainName, iptErrText(out, err))
		}
		copies++
	}
	if copies > 1 {
		iptDropExtraJumps(bin, "mangle", "POSTROUTING", dscpChainName)
	}
	return nil
}

func iptDropExtraJumps(bin, table, parent, target string) {
	for round := 0; round < iptDeleteRounds; round++ {
		nums := iptJumpLineNumbers(bin, table, parent, func(t string) bool { return t == target })
		if len(nums) <= 1 {
			return
		}
		last := nums[len(nums)-1]
		if _, err := run(bin, "-w", "-t", table, "-D", parent, strconv.Itoa(last)); err != nil {
			log.Warnf("IPTABLES[%s]: -t %s -D %s %d failed, so b4 stopped removing extra jumps to %s rather than cut a rule it never read: %v",
				bin, table, parent, last, target, err)
			return
		}
	}
}

func (im *IPTablesManager) teardownDSCPChain(bin string) (gone, seen bool) {
	gone, seen, _ = im.teardownDSCPChainSets(bin)
	return gone, seen
}

func (im *IPTablesManager) teardownDSCPChainSets(bin string) (gone, seen bool, sets []string) {
	listing, present, known := iptChainListing(bin, "mangle", dscpChainName)
	if !known {
		log.Tracef("IPTABLES[%s]: could not tell whether the mangle chain %s exists", bin, dscpChainName)
		return false, false, nil
	}
	if !present {
		return true, false, nil
	}
	sets = dscpIptListedSets(listing)
	iptDeleteJumpsTo(bin, "mangle", "POSTROUTING", dscpChainName)
	_, _ = run(bin, "-w", "-t", "mangle", "-F", dscpChainName)
	out, err := run(bin, "-w", "-t", "mangle", "-X", dscpChainName)
	if err == nil {
		return true, true, sets
	}
	if present, known := iptChainPresence(bin, "mangle", dscpChainName); known && !present {
		return true, true, sets
	}
	log.Warnf("IPTABLES[%s]: could not delete the mangle chain %s, the firewall monitor tries again: %s", bin, dscpChainName, iptErrText(out, err))
	return false, true, sets
}

func dscpIptListedSets(listing string) []string {
	var sets []string
	for _, line := range strings.Split(listing, "\n") {
		fields := strings.Fields(line)
		for i := 0; i+1 < len(fields); i++ {
			if fields[i] == "--match-set" && strings.HasPrefix(fields[i+1], dscpIptSetPrefix) {
				sets = append(sets, fields[i+1])
			}
		}
	}
	return sets
}

func dscpNftTablePresence() (present, known bool) {
	if !hasBinary("nft") {
		return false, true
	}
	out, err := run("nft", "list", "tables")
	if err != nil {
		return false, false
	}
	want := "table inet " + dscpNftTable
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == want {
			return true, true
		}
	}
	return false, true
}

func dscpNftTablePresent() bool {
	present, _ := dscpNftTablePresence()
	return present
}

func removeDSCPNft() (gone, seen bool) {
	present, known := dscpNftTablePresence()
	if !known {
		return false, false
	}
	if !present {
		return true, false
	}
	if out, err := run("nft", "delete", "table", "inet", dscpNftTable); err != nil {
		log.Warnf("NFTABLES: could not delete the %s table, the firewall monitor tries again: %s", dscpNftTable, iptErrText(out, err))
		return false, true
	}
	return true, true
}

func removeDSCPObjects(st *dscpState) (gone, seen bool, left []string) {
	dscpLearnForgetBackend(st.backend)
	if st.backend == backendNFTables {
		gone, seen = removeDSCPNft()
		return gone, seen, nil
	}
	im := NewIPTablesManager(st.cfg, st.backend == backendIPTablesLegacy)
	gone = true
	sets := st.ipsets()
	for _, bin := range im.teardownBinaries() {
		g, s, listed := im.teardownDSCPChainSets(bin)
		if !g {
			gone = false
		}
		if s {
			seen = true
		}
		sets = append(sets, listed...)
	}
	slices.Sort(sets)
	return gone, seen, dscpIptDestroySets(slices.Compact(sets))
}

func removeDSCPOrPark(st *dscpState) {
	if gone, _, left := removeDSCPObjects(st); !gone || len(left) > 0 {
		dscpStale.Store(st.leftover(left))
	}
}

func retryStaleDSCP() bool {
	s := dscpStale.Load()
	if s == nil {
		return false
	}
	gone, _, left := removeDSCPObjects(s)
	if owned := dscpApplied.Load().ipsets(); len(owned) > 0 {
		left = slices.DeleteFunc(left, func(name string) bool { return slices.Contains(owned, name) })
	}
	if gone && len(left) == 0 {
		dscpStale.CompareAndSwap(s, nil)
		return true
	}
	dscpStale.CompareAndSwap(s, s.leftover(left))
	return false
}

func clearDSCPFor(cfg *config.Config, backend string) {
	defer dscpLearnAdopt(nil, false)
	prev := dscpApplied.Swap(nil)
	if prev != nil && prev.backend != backend {
		removeDSCPOrPark(prev)
	}
	target := &dscpState{cfg: cfg, backend: backend}
	if prev != nil && prev.backend == backend {
		target = target.withLeftovers(prev)
	}
	if s := dscpStale.Load(); s != nil && s.backend == backend {
		target = target.withLeftovers(s)
	}
	gone, seen, left := removeDSCPObjects(target)
	if gone && len(left) == 0 {
		if s := dscpStale.Load(); s != nil && s.backend == backend {
			dscpStale.CompareAndSwap(s, nil)
		}
		return
	}
	if seen || len(left) > 0 || (prev != nil && prev.backend == backend) {
		dscpStale.Store((&dscpState{cfg: cfg, backend: backend}).leftover(left))
	}
}

func clearDSCPUnlessKept(cfg *config.Config, backend string) {
	if dscpKeepOnRefresh {
		if st := dscpApplied.Load(); st != nil && st.backend == backend && (!st.plan.empty() || st.cfg.System.Tables.DSCP.Equal(cfg.System.Tables.DSCP)) {
			return
		}
	}
	clearDSCPFor(cfg, backend)
}

func dscpIptChainShape(listing string, stamps int) bool {
	returns, dscps := 0, 0
	for _, line := range strings.Split(listing, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "-A ") {
			continue
		}
		switch iptSpecTarget(strings.Fields(line)) {
		case "RETURN":
			if dscps > 0 {
				return false
			}
			returns++
		case "DSCP":
			dscps++
		default:
			return false
		}
	}
	return returns == dscpReturnRules && dscps == stamps
}

func dscpNftChainShape(listing string, stamps int) bool {
	returns, dscps := 0, 0
	for _, line := range strings.Split(listing, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.Contains(line, "dscp set"):
			dscps++
		case line == "return" || strings.HasSuffix(line, " return"):
			if dscps > 0 {
				return false
			}
			returns++
		}
	}
	return returns == dscpReturnRules && dscps == stamps
}

func dscpIptIntact(bin string, stamps int) bool {
	listing, err := run(bin, "-w", "-t", "mangle", "-L", "POSTROUTING", "-n", "--line-numbers")
	if err != nil || !dscpJumpSeated(listing) {
		return false
	}
	chain, err := run(bin, "-w", "-t", "mangle", "-S", dscpChainName)
	return err == nil && dscpIptChainShape(chain, stamps)
}

func dscpIntact(st *dscpState) bool {
	if st.backend == backendNFTables {
		if st.nft != nil {
			return dscpNftLayoutIntact(st.nft)
		}
		out, err := run("nft", "list", "chain", "inet", dscpNftTable, dscpNftChain)
		return err == nil && dscpNftChainShape(out, st.stamps)
	}
	for _, bin := range st.bins {
		if !st.binIntact(bin) {
			return false
		}
	}
	return true
}

func (st *dscpState) binIntact(bin string) bool {
	if st.ipt == nil {
		return dscpIptIntact(bin, st.stamps)
	}
	specs, ok := st.ipt.chains[bin]
	return ok && dscpIptPlanIntact(bin, specs)
}

func retryPendingDSCPSets(st *dscpState) {
	if st == nil || st.ipt == nil || len(st.ipt.pending) == 0 {
		return
	}
	left := dscpIptDestroySets(st.ipt.pending)
	if len(left) == len(st.ipt.pending) {
		return
	}
	next, ipt := *st, *st.ipt
	ipt.pending = left
	next.ipt = &ipt
	dscpApplied.CompareAndSwap(st, &next)
}

func ensureDSCPLocked(cfg *config.Config, requested bool) bool {
	if cfg != nil && cfg.System.Tables.SkipSetup {
		return false
	}
	acted := retryStaleDSCP()
	st := dscpApplied.Load()
	if st == nil {
		return acted
	}
	if !st.pending && dscpIntact(st) {
		retryPendingDSCPSets(st)
		return acted
	}
	switch {
	case st.pending:
		log.Tracef("Monitor: retrying the DSCP stamp rules that did not apply")
	case requested:
		log.Infof("DSCP stamp rules missing after a firewall rewrite, restoring...")
	default:
		log.Warnf("DSCP stamp rules missing or out of place, restoring...")
	}
	if err := applyDSCPFor(st.cfg, st.backend); err != nil {
		warnDSCPOnce(err)
	}
	return true
}

func reapplyDSCPLocked() {
	if st := dscpApplied.Load(); st != nil {
		if err := applyDSCPFor(st.cfg, st.backend); err != nil {
			warnDSCPOnce(err)
		}
	}
}

func ApplyDSCPOnly(cfg *config.Config) error {
	_, _, on := cfg.DSCPStamp()
	if !on {
		on = !dscpPlanFor(cfg).empty()
	}
	defer dscpLast.Store(cfg)
	if !on && dscpApplied.Load() == nil {
		return nil
	}
	if on {
		loadKernelModules()
	}
	return applyDSCPFor(cfg, detectFirewallBackend(cfg))
}

func ClearDSCPOnly(cfg *config.Config) {
	defer dscpLearnAdopt(nil, false)
	dscpLast.Store(nil)
	if st := dscpApplied.Swap(nil); st != nil {
		removeDSCPOrPark(st)
	}
	retryStaleDSCP()
}
