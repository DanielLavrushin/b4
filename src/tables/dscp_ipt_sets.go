package tables

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const (
	dscpIptSetPrefix    = "b4d_"
	dscpIptMaxElem      = 262144
	dscpIptLearnTimeout = 3600
	dscpIptReprobe      = 10 * time.Minute
	dscpIptBatch        = 10000
	dscpIptSingleStreak = 16
)

type dscpIptState struct {
	chains    map[string][][]string
	bins      []string
	sets      []string
	pending   []string
	incapable map[string]string
	retry     bool
}

type dscpIptWant struct {
	name    string
	sid     string
	v6      bool
	learned bool
	keys    []string
}

type dscpIptShape struct {
	globals int
	guard   bool
	sets    int
}

type dscpIptProbe struct {
	err error
	at  time.Time
}

type dscpIptSetRuleError struct {
	spec []string
	text string
}

func (e *dscpIptSetRuleError) Error() string {
	return fmt.Sprintf("'%s' was rejected: %s", strings.Join(e.spec, " "), e.text)
}

var (
	dscpIptNow      = time.Now
	dscpIptProbeMu  sync.Mutex
	dscpIptProbes   = map[string]dscpIptProbe{}
	dscpIptRecordMu sync.Mutex
	dscpIptRecord   = map[string]map[string]bool{}
)

func dscpIptBinV6(bin string) bool {
	return strings.HasPrefix(bin, "ip6")
}

func dscpIptSetName(kind string, v6 bool) string {
	if v6 {
		return dscpIptSetPrefix + kind + "_v6"
	}
	return dscpIptSetPrefix + kind + "_v4"
}

func dscpIptUnionSet(v6 bool) string { return dscpIptSetName("u", v6) }

func dscpIptLearnedUnionSet(v6 bool) string { return dscpIptSetName("ul", v6) }

func dscpIptValueSet(value int, v6 bool) string { return dscpIptSetName("s"+strconv.Itoa(value), v6) }

func dscpIptLearnedSet(sid string, v6 bool) string { return dscpIptSetName("l_"+sid, v6) }

func dscpIptRanges(plan *dscpPlan, v6 bool) []dscpRange {
	if v6 {
		return plan.static6
	}
	return plan.static4
}

func dscpIptScopes(ifaces []string) [][]string {
	if len(ifaces) == 0 {
		return [][]string{nil}
	}
	scopes := make([][]string, len(ifaces))
	for i, iface := range ifaces {
		scopes[i] = []string{"-o", iface}
	}
	return scopes
}

func dscpIptStampSpec(scope []string, set string, value int) []string {
	spec := append([]string(nil), scope...)
	return append(spec, "-m", "set", "--match-set", set, "dst", "-j", "DSCP", "--set-dscp", strconv.Itoa(value))
}

func dscpIptGuardSpec(v6 bool) []string {
	return []string{
		"-m", "set", "!", "--match-set", dscpIptUnionSet(v6), "dst",
		"-m", "set", "!", "--match-set", dscpIptLearnedUnionSet(v6), "dst",
		"-j", "RETURN",
	}
}

func dscpIptSetStamps(plan *dscpPlan, v6 bool) [][]string {
	if plan.empty() {
		return nil
	}
	values := dscpRangeValues(dscpIptRanges(plan, v6))
	learning := plan.learning()
	slices.SortStableFunc(learning, func(a, b dscpPlanSet) int { return cmp.Compare(b.index, a.index) })
	scopes := dscpIptScopes(plan.scopes)
	var stamps [][]string
	for _, scope := range scopes {
		for _, value := range values {
			stamps = append(stamps, dscpIptStampSpec(scope, dscpIptValueSet(value, v6), value))
		}
	}
	for _, scope := range scopes {
		for _, set := range learning {
			stamps = append(stamps, dscpIptStampSpec(scope, dscpIptLearnedSet(set.sid, v6), set.value))
		}
	}
	return stamps
}

func dscpIptRender(plan *dscpPlan, v6, capable bool) [][]string {
	specs := dscpIptSpecs(plan.global, plan.scopes)
	if !plan.globalOn {
		specs = specs[:dscpReturnRules:dscpReturnRules]
	}
	if !capable {
		return specs
	}
	stamps := dscpIptSetStamps(plan, v6)
	if len(stamps) == 0 {
		return specs
	}
	specs = append(specs, dscpIptGuardSpec(v6))
	return append(specs, stamps...)
}

func dscpIptKeys(prefixes []netip.Prefix) []string {
	keys := make([]string, len(prefixes))
	for i, prefix := range prefixes {
		keys[i] = dscpCanonicalKey(prefix)
	}
	return keys
}

func dscpIptWanted(plan *dscpPlan, v6 bool) []dscpIptWant {
	if plan.empty() {
		return nil
	}
	ranges := dscpIptRanges(plan, v6)
	values := dscpRangeValues(ranges)
	learning := plan.learning()
	if len(values) == 0 && len(learning) == 0 {
		return nil
	}
	byValue := make(map[int][]string, len(values))
	for _, r := range ranges {
		byValue[r.value] = append(byValue[r.value], dscpIptKeys(dscpRangeCover(r))...)
	}
	wants := []dscpIptWant{
		{name: dscpIptUnionSet(v6), v6: v6, keys: dscpIptKeys(dscpUnionCover(ranges))},
		{name: dscpIptLearnedUnionSet(v6), v6: v6, learned: true},
	}
	for _, value := range values {
		wants = append(wants, dscpIptWant{name: dscpIptValueSet(value, v6), v6: v6, keys: byValue[value]})
	}
	for _, set := range learning {
		wants = append(wants, dscpIptWant{name: dscpIptLearnedSet(set.sid, v6), sid: set.sid, v6: v6, learned: true})
	}
	return wants
}

func dscpIptMatchesSet(spec []string) bool {
	return slices.Contains(spec, "--match-set")
}

func dscpIptShapeOf(specs [][]string) dscpIptShape {
	var shape dscpIptShape
	for _, spec := range specs {
		stamp := iptSpecTarget(spec) == "DSCP"
		switch {
		case stamp && dscpIptMatchesSet(spec):
			shape.sets++
		case stamp:
			shape.globals++
		case dscpIptMatchesSet(spec):
			shape.guard = true
		}
	}
	return shape
}

func dscpIptStamps(specs [][]string) bool {
	shape := dscpIptShapeOf(specs)
	return shape.globals+shape.sets > 0
}

func (s dscpIptShape) rules() int {
	n := dscpReturnRules + s.globals + s.sets
	if s.guard {
		n++
	}
	return n
}

func dscpIptListedShape(listing string) (dscpIptShape, bool) {
	returns := 0
	var got dscpIptShape
	for _, line := range strings.Split(listing, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "-A ") {
			continue
		}
		fields := strings.Fields(line)
		ours := strings.Contains(line, " --match-set "+dscpIptSetPrefix)
		if !ours && slices.Contains(fields, "--match-set") {
			return got, false
		}
		switch iptSpecTarget(fields) {
		case "RETURN":
			switch {
			case ours && (got.guard || got.sets > 0):
				return got, false
			case ours:
				got.guard = true
			case got.globals > 0 || got.guard || got.sets > 0:
				return got, false
			default:
				returns++
			}
		case "DSCP":
			switch {
			case ours && !got.guard:
				return got, false
			case ours:
				got.sets++
			case got.guard || got.sets > 0:
				return got, false
			default:
				got.globals++
			}
		default:
			return got, false
		}
	}
	return got, returns == dscpReturnRules
}

func dscpIptPlanChainShape(listing string, want dscpIptShape) bool {
	got, ok := dscpIptListedShape(listing)
	return ok && got == want
}

func dscpIptPlanIntact(bin string, specs [][]string) bool {
	listing, err := run(bin, "-w", "-t", "mangle", "-L", "POSTROUTING", "-n", "--line-numbers")
	if err != nil || !dscpJumpSeated(listing) {
		return false
	}
	chain, err := run(bin, "-w", "-t", "mangle", "-S", dscpChainName)
	return err == nil && dscpIptPlanChainShape(chain, dscpIptShapeOf(specs))
}

func dscpIptCapability(im *IPTablesManager, bin string) error {
	if !hasBinary("ipset") {
		return errors.New("the ipset command is not installed")
	}
	dscpIptProbeMu.Lock()
	defer dscpIptProbeMu.Unlock()
	if p, ok := dscpIptProbes[bin]; ok && (p.err == nil || dscpIptNow().Sub(p.at) < dscpIptReprobe) {
		return p.err
	}
	loadKernelModuleList("xt_set")
	err := ipsetMatchProbe(im, bin)
	if err != nil && dscpIptProbeBusy(err) {
		return err
	}
	dscpIptProbes[bin] = dscpIptProbe{err: err, at: dscpIptNow()}
	return err
}

func dscpIptProbeBusy(err error) bool {
	msg := err.Error()
	return isXtablesLockBusy(msg, err) || strings.Contains(msg, context.DeadlineExceeded.Error())
}

func dscpIptRefuse(bin string, err error) string {
	dscpIptProbeMu.Lock()
	dscpIptProbes[bin] = dscpIptProbe{err: err, at: dscpIptNow()}
	dscpIptProbeMu.Unlock()
	return dscpIptWarnIncapable(bin, err)
}

func dscpIptWarnIncapable(bin string, err error) string {
	warnDSCPOnce(fmt.Errorf("per-set DSCP values are not written into %s packets because %s cannot use ipsets (%v); this needs the ipset command and the xt_set kernel module", iptFamilyLabel(bin), bin, err))
	return err.Error()
}

func dscpIptBusy(out string, err error) bool {
	text := strings.ToLower(out + " " + err.Error())
	return dscpTransient(text, err) || strings.Contains(text, "cannot allocate memory")
}

func dscpIptEnsure(w dscpIptWant) (bool, error) {
	family := "inet"
	if w.v6 {
		family = "inet6"
	}
	args := []string{"ipset", "create", w.name, "hash:net", "family", family}
	if w.learned {
		args = append(args, "timeout", strconv.Itoa(dscpIptLearnTimeout))
	} else {
		args = append(args, "maxelem", strconv.Itoa(dscpIptMaxElem))
	}
	out, err := run(args...)
	if err != nil && !strings.Contains(iptErrText(out, err), "already exists") {
		return dscpIptBusy(out, err), fmt.Errorf("could not create the ipset %s: %s", w.name, iptErrText(out, err))
	}
	if w.learned {
		if err == nil {
			dscpLearnForgetSet(w.sid, w.v6)
		}
		return false, nil
	}
	dscpIptRecordMu.Lock()
	defer dscpIptRecordMu.Unlock()
	if err == nil {
		dscpIptRecord[w.name] = map[string]bool{}
		return false, nil
	}
	if _, known := dscpIptRecord[w.name]; known {
		return false, nil
	}
	if out, err := run("ipset", "flush", w.name); err != nil {
		return dscpIptBusy(out, err), fmt.Errorf("could not empty the ipset %s that an earlier run left behind: %s", w.name, iptErrText(out, err))
	}
	dscpIptRecord[w.name] = map[string]bool{}
	return false, nil
}

func dscpIptRestore(op, name string, keys []string) (map[string]bool, error) {
	failed := map[string]bool{}
	var first error
	for start := 0; start < len(keys); start += dscpIptBatch {
		batch := keys[start:min(start+dscpIptBatch, len(keys))]
		var b strings.Builder
		for _, key := range batch {
			fmt.Fprintf(&b, "%s %s %s\n", op, name, key)
		}
		if err := runStdin(b.String(), "ipset", "restore", "-exist"); err == nil {
			continue
		}
		streak := 0
		for i, key := range batch {
			if streak == dscpIptSingleStreak {
				for _, rest := range batch[i:] {
					failed[rest] = true
				}
				break
			}
			out, err := run("ipset", op, name, key, "-exist")
			if err == nil {
				streak = 0
				continue
			}
			streak++
			failed[key] = true
			if first == nil {
				first = errors.New(iptErrText(out, err))
			}
		}
	}
	return failed, first
}

func dscpIptAdd(name string, keys []string) error {
	if len(keys) > dscpIptMaxElem {
		return fmt.Errorf("the ipset %s would need %d entries, more than the %d it holds, so b4 added none of the new ones", name, len(keys), dscpIptMaxElem)
	}
	dscpIptRecordMu.Lock()
	defer dscpIptRecordMu.Unlock()
	have := dscpIptRecord[name]
	if have == nil {
		have = map[string]bool{}
		dscpIptRecord[name] = have
	}
	var missing []string
	for _, key := range keys {
		if !have[key] {
			missing = append(missing, key)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	failed, first := dscpIptRestore("add", name, missing)
	for _, key := range missing {
		if !failed[key] {
			have[key] = true
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("%d of %d entries could not be added to the ipset %s, the next change tries again: %v", len(failed), len(missing), name, first)
	}
	return nil
}

func dscpIptPrune(name string, keys []string) error {
	keep := make(map[string]bool, len(keys))
	for _, key := range keys {
		keep[key] = true
	}
	dscpIptRecordMu.Lock()
	defer dscpIptRecordMu.Unlock()
	have := dscpIptRecord[name]
	var stale []string
	for key := range have {
		if !keep[key] {
			stale = append(stale, key)
		}
	}
	if len(stale) == 0 {
		return nil
	}
	slices.Sort(stale)
	failed, first := dscpIptRestore("del", name, stale)
	for _, key := range stale {
		if !failed[key] {
			delete(have, key)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("%d of %d stale entries could not be removed from the ipset %s, the next change tries again: %v", len(failed), len(stale), name, first)
	}
	return nil
}

func dscpIptForget(name string) {
	dscpIptRecordMu.Lock()
	delete(dscpIptRecord, name)
	dscpIptRecordMu.Unlock()
}

func dscpIptDestroySets(names []string) []string {
	if len(names) == 0 || !hasBinary("ipset") {
		return nil
	}
	var pending []string
	for _, name := range names {
		out, err := run("ipset", "destroy", name)
		if err == nil {
			dscpIptForget(name)
			continue
		}
		msg := iptErrText(out, err)
		if strings.Contains(msg, "does not exist") {
			dscpIptForget(name)
			continue
		}
		pending = append(pending, name)
		if strings.Contains(msg, "in use") {
			log.Tracef("DSCP stamp: the ipset %s is still referenced by a rule, b4 destroys it on the next pass", name)
			continue
		}
		warnDSCPOnce(fmt.Errorf("could not destroy the ipset %s, b4 tries again on the next pass: %s", name, msg))
	}
	return pending
}

func dscpIptPut(bin string, at int, spec []string) (bool, error) {
	args := []string{bin, "-w", "-t", "mangle", "-A", dscpChainName}
	if at > 0 {
		args = []string{bin, "-w", "-t", "mangle", "-I", dscpChainName, strconv.Itoa(at)}
	}
	out, err := run(append(args, spec...)...)
	if err == nil {
		return false, nil
	}
	if present, known := iptChainPresence(bin, "mangle", dscpChainName); dscpTransient(out, err) || !known || !present {
		return false, fmt.Errorf("the mangle table was busy or changed while b4 was filling %s: %s", dscpChainName, iptErrText(out, err))
	}
	if dscpIptMatchesSet(spec) {
		return true, &dscpIptSetRuleError{spec: spec, text: iptErrText(out, err)}
	}
	if iptSpecTarget(spec) == "DSCP" {
		kmodNoteRejected("xt_DSCP", bin, out)
		return true, fmt.Errorf("the DSCP target was rejected (%s). %s", iptErrText(out, err), kmodMissingHint([]string{"xt_DSCP"}))
	}
	return true, fmt.Errorf("'%s' was rejected: %s", strings.Join(spec, " "), iptErrText(out, err))
}

func dscpIptDeleteAt(bin string, n int) error {
	if out, err := run(bin, "-w", "-t", "mangle", "-D", dscpChainName, strconv.Itoa(n)); err != nil {
		return fmt.Errorf("could not remove the replaced rule %d from %s: %s", n, dscpChainName, iptErrText(out, err))
	}
	return nil
}

func dscpIptReplace(bin string, old int, specs [][]string) (bool, error) {
	for i, spec := range specs {
		if permanent, err := dscpIptPut(bin, i+1, spec); err != nil {
			return permanent, err
		}
	}
	n := len(specs)
	for range old - dscpReturnRules {
		if err := dscpIptDeleteAt(bin, n+dscpReturnRules+1); err != nil {
			return false, err
		}
	}
	for range dscpReturnRules {
		if err := dscpIptDeleteAt(bin, n+1); err != nil {
			return false, err
		}
	}
	return false, nil
}

func dscpIptRefill(bin string, specs [][]string, flush bool) (bool, error) {
	if flush {
		if out, err := run(bin, "-w", "-t", "mangle", "-F", dscpChainName); err != nil {
			return false, fmt.Errorf("could not flush the mangle chain %s: %s", dscpChainName, iptErrText(out, err))
		}
	} else if out, err := run(bin, "-w", "-t", "mangle", "-N", dscpChainName); err != nil {
		present, _ := iptChainPresence(bin, "mangle", dscpChainName)
		exists := present || strings.Contains(iptErrText(out, err), "already exists")
		permanent := !dscpTransient(out, err) && !exists
		return permanent, fmt.Errorf("could not create the mangle chain %s: %s", dscpChainName, iptErrText(out, err))
	}
	for _, spec := range specs {
		if permanent, err := dscpIptPut(bin, 0, spec); err != nil {
			return permanent, err
		}
	}
	return false, nil
}

func (im *IPTablesManager) dscpIptKeeps(bin, listing string, installed, specs [][]string) bool {
	shape := dscpIptShapeOf(specs)
	same := slices.EqualFunc(installed, specs, slices.Equal[[]string])
	if !same && (installed != nil || shape.guard) {
		return false
	}
	if !dscpIptPlanChainShape(listing, shape) {
		return false
	}
	for _, spec := range specs {
		if !im.existsRule(bin, "mangle", dscpChainName, spec) {
			return false
		}
	}
	return true
}

func (im *IPTablesManager) dscpIptApplyChain(bin string, installed, specs [][]string) (bool, error) {
	listing, err := run(bin, "-w", "-t", "mangle", "-S", dscpChainName)
	old, known := dscpIptListedShape(listing)
	if installed != nil {
		known = known && old == dscpIptShapeOf(installed)
	}
	var permanent bool
	switch {
	case err != nil:
		permanent, err = dscpIptRefill(bin, specs, false)
	case im.dscpIptKeeps(bin, listing, installed, specs):
	case known && (old.guard || dscpIptShapeOf(specs).guard):
		permanent, err = dscpIptReplace(bin, old.rules(), specs)
	default:
		permanent, err = dscpIptRefill(bin, specs, true)
	}
	if err != nil {
		return permanent, err
	}
	return false, iptSeatDSCPJump(bin)
}

func (st *dscpIptState) prepare(im *IPTablesManager, bin string, wants []dscpIptWant, touched map[string]bool) (bool, error) {
	if len(wants) == 0 {
		return false, nil
	}
	if err := dscpIptCapability(im, bin); err != nil {
		if dscpIptProbeBusy(err) {
			st.retry = true
			return false, fmt.Errorf("%s could not test the ipset match because the firewall was busy, the firewall monitor tries again: %w", bin, err)
		}
		st.incapable[bin] = dscpIptWarnIncapable(bin, err)
		return false, nil
	}
	for _, w := range wants {
		if busy, err := dscpIptEnsure(w); err != nil {
			if busy {
				st.retry = true
				return false, fmt.Errorf("%s could not prepare its ipsets, the firewall monitor tries again: %w", bin, err)
			}
			st.incapable[bin] = dscpIptRefuse(bin, err)
			return false, nil
		}
		touched[w.name] = true
	}
	var errs []error
	for _, w := range wants {
		if w.learned {
			continue
		}
		if err := dscpIptAdd(w.name, w.keys); err != nil {
			errs = append(errs, err)
		}
	}
	return true, errors.Join(errs...)
}

func (st *dscpIptState) drop(im *IPTablesManager, bin string) {
	if gone, _ := im.teardownDSCPChain(bin); !gone {
		st.retry = true
	}
}

func (st *dscpIptState) install(im *IPTablesManager, bin string, plan *dscpPlan, capable bool, installed [][]string) (bool, error) {
	v6 := dscpIptBinV6(bin)
	specs := dscpIptRender(plan, v6, capable)
	if !dscpIptStamps(specs) {
		st.drop(im, bin)
		return false, nil
	}
	permanent, err := im.dscpIptApplyChain(bin, installed, specs)
	var refused *dscpIptSetRuleError
	if errors.As(err, &refused) {
		st.incapable[bin] = dscpIptRefuse(bin, refused)
		specs = dscpIptRender(plan, v6, false)
		if !dscpIptStamps(specs) {
			st.drop(im, bin)
			return false, nil
		}
		permanent, err = im.dscpIptApplyChain(bin, nil, specs)
	}
	perSet := dscpIptShapeOf(specs).guard
	switch {
	case err == nil:
		st.chains[bin] = specs
		st.bins = append(st.bins, bin)
		return perSet, nil
	case permanent:
		im.teardownDSCPChain(bin)
		return false, fmt.Errorf("%s, so %s packets go out without the DSCP value: %w", bin, iptFamilyLabel(bin), err)
	}
	st.bins = append(st.bins, bin)
	st.retry = true
	return perSet, fmt.Errorf("%s could not finish the DSCP rules for %s packets, the firewall monitor tries again: %w", bin, iptFamilyLabel(bin), err)
}

func (st *dscpIptState) logApplied(plan *dscpPlan) {
	var installed, perSet []string
	for _, bin := range st.bins {
		specs, ok := st.chains[bin]
		if !ok {
			continue
		}
		installed = append(installed, bin)
		if dscpIptShapeOf(specs).guard {
			perSet = append(perSet, bin)
		}
	}
	if plan.globalOn && len(installed) > 0 {
		log.Infof("IPTABLES: DSCP %d is written into the packets this host sends out (%s; %s)", plan.global, dscpScopeLabel(plan.scopes), strings.Join(installed, ", "))
	}
	if len(perSet) > 0 {
		log.Infof("IPTABLES: the sets' own DSCP values are written into the packets this host sends to their addresses (%s; %s)", dscpScopeLabel(plan.scopes), strings.Join(perSet, ", "))
	}
}

func dscpIptApplyPlan(cfg *config.Config, backend string, plan *dscpPlan, prev *dscpIptState) (*dscpIptState, error) {
	if cfg.System.Tables.SkipSetup {
		return prev, nil
	}
	im := NewIPTablesManager(cfg, backend == backendIPTablesLegacy)
	loadKernelModuleList("xt_DSCP")
	st := &dscpIptState{chains: map[string][][]string{}, incapable: map[string]string{}}
	bins := im.teardownBinaries()
	wants := make(map[string][]dscpIptWant, len(bins))
	capable := make(map[string]bool, len(bins))
	touched, used := map[string]bool{}, map[string]bool{}
	var errs []error
	for _, bin := range bins {
		wants[bin] = dscpIptWanted(plan, dscpIptBinV6(bin))
		ok, err := st.prepare(im, bin, wants[bin], touched)
		capable[bin] = ok
		if err != nil {
			errs = append(errs, err)
		}
	}
	for _, bin := range bins {
		var installed [][]string
		if prev != nil {
			installed = prev.chains[bin]
		}
		perSet, err := st.install(im, bin, plan, capable[bin], installed)
		if err != nil {
			errs = append(errs, err)
		}
		if !perSet {
			continue
		}
		for _, w := range wants[bin] {
			used[w.name] = true
		}
	}
	for _, bin := range bins {
		if specs, ok := st.chains[bin]; !ok || !dscpIptShapeOf(specs).guard {
			continue
		}
		for _, w := range wants[bin] {
			if w.learned {
				continue
			}
			if err := dscpIptPrune(w.name, w.keys); err != nil {
				errs = append(errs, err)
			}
		}
	}
	departed := map[string]bool{}
	if prev != nil {
		for _, name := range slices.Concat(prev.sets, prev.pending) {
			departed[name] = true
		}
	}
	maps.Copy(departed, touched)
	for name := range used {
		delete(departed, name)
	}
	st.pending = dscpIptDestroySets(slices.Sorted(maps.Keys(departed)))
	st.sets = slices.Sorted(maps.Keys(used))
	st.logApplied(plan)
	return st, errors.Join(errs...)
}
