package tables

import (
	"errors"
	"fmt"
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
)

type dscpState struct {
	cfg     *config.Config
	backend string
	bins    []string
	stamps  int
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
	value, ifaces, on := cfg.DSCPStamp()
	if !on {
		if dscpApplied.Load() != nil {
			clearDSCPFor(cfg, backend)
		}
		return nil
	}
	if prev := dscpApplied.Load(); prev != nil && prev.backend != backend && !removeDSCPObjects(prev.cfg, prev.backend) {
		dscpStale.Store(prev)
	}
	if s := dscpStale.Load(); s != nil && s.backend == backend {
		dscpStale.CompareAndSwap(s, nil)
	}
	if backend == backendNFTables {
		return applyDSCPNft(cfg, value, ifaces)
	}
	return applyDSCPIpt(cfg, backend, value, ifaces)
}

func applyDSCPNft(cfg *config.Config, value int, ifaces []string) error {
	script, stamps := dscpNftScript(value, ifaces)
	if _, err := runNftStdin(script); err != nil {
		removeDSCPNft()
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
		im.teardownDSCPChain(bin)
		if permanent {
			errs = append(errs, fmt.Errorf("%s, so %s packets go out without the DSCP value: %w", bin, iptFamilyLabel(bin), err))
			continue
		}
		wanted = append(wanted, bin)
		errs = append(errs, fmt.Errorf("%s, so %s packets go out without the DSCP value until the firewall monitor retries: %w", bin, iptFamilyLabel(bin), err))
	}
	if len(wanted) == 0 {
		dscpApplied.Store(nil)
	} else {
		dscpApplied.Store(&dscpState{cfg: cfg, backend: backend, bins: wanted, stamps: dscpIptStampCount(ifaces)})
	}
	if len(installed) > 0 {
		log.Infof("IPTABLES: DSCP %d is written into the packets this host sends out (%s; %s)", value, dscpScopeLabel(ifaces), strings.Join(installed, ", "))
	}
	return errors.Join(errs...)
}

func (im *IPTablesManager) dscpChainMatches(bin string, specs [][]string) bool {
	out, err := run(bin, "-w", "-t", "mangle", "-S", dscpChainName)
	if err != nil {
		return false
	}
	rules := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "-A ") {
			rules++
		}
	}
	if rules != len(specs) {
		return false
	}
	for _, spec := range specs {
		if !im.existsRule(bin, "mangle", dscpChainName, spec) {
			return false
		}
	}
	return true
}

func (im *IPTablesManager) applyDSCPChain(bin string, specs [][]string) (bool, error) {
	if !im.dscpChainMatches(bin, specs) {
		if !im.existsChain(bin, "mangle", dscpChainName) {
			if out, err := run(bin, "-w", "-t", "mangle", "-N", dscpChainName); err != nil {
				permanent := !isXtablesLockBusy(out, err) && !im.existsChain(bin, "mangle", dscpChainName)
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
			if isXtablesLockBusy(out, err) || !im.existsChain(bin, "mangle", dscpChainName) {
				return false, fmt.Errorf("the mangle table changed while b4 was filling %s: %s", dscpChainName, iptErrText(out, err))
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

func iptSeatDSCPJump(bin string) error {
	listing, err := run(bin, "-w", "-t", "mangle", "-L", "POSTROUTING", "-n", "--line-numbers")
	if err != nil {
		return fmt.Errorf("could not read mangle POSTROUTING: %s", iptErrText(listing, err))
	}
	jump, capture, copies := dscpJumpPlacement(listing)
	if jump == 0 || (capture > 0 && capture < jump) {
		if copies > 0 {
			log.Infof("IPTABLES[%s]: the jump to %s sits below b4's capture jump in mangle POSTROUTING, where the packets b4 inspects would skip it; moving it to the top", bin, dscpChainName)
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

func (im *IPTablesManager) teardownDSCPChain(bin string) bool {
	if !im.existsChain(bin, "mangle", dscpChainName) {
		return true
	}
	iptDeleteJumpsTo(bin, "mangle", "POSTROUTING", dscpChainName)
	_, _ = run(bin, "-w", "-t", "mangle", "-F", dscpChainName)
	out, err := run(bin, "-w", "-t", "mangle", "-X", dscpChainName)
	if err == nil {
		return true
	}
	if !im.existsChain(bin, "mangle", dscpChainName) {
		return true
	}
	log.Warnf("IPTABLES[%s]: could not delete the mangle chain %s, the firewall monitor tries again: %s", bin, dscpChainName, iptErrText(out, err))
	return false
}

func dscpNftTablePresent() bool {
	if !hasBinary("nft") {
		return false
	}
	out, err := run("nft", "list", "tables")
	if err != nil {
		return false
	}
	want := "table inet " + dscpNftTable
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

func removeDSCPNft() bool {
	if !dscpNftTablePresent() {
		return true
	}
	if out, err := run("nft", "delete", "table", "inet", dscpNftTable); err != nil {
		log.Warnf("NFTABLES: could not delete the %s table, the firewall monitor tries again: %s", dscpNftTable, iptErrText(out, err))
		return false
	}
	return true
}

func removeDSCPObjects(cfg *config.Config, backend string) bool {
	if backend == backendNFTables {
		return removeDSCPNft()
	}
	im := NewIPTablesManager(cfg, backend == backendIPTablesLegacy)
	gone := true
	for _, bin := range im.teardownBinaries() {
		if !im.teardownDSCPChain(bin) {
			gone = false
		}
	}
	return gone
}

func clearDSCPFor(cfg *config.Config, backend string) {
	if prev := dscpApplied.Swap(nil); prev != nil && prev.backend != backend && !removeDSCPObjects(prev.cfg, prev.backend) {
		dscpStale.Store(prev)
	}
	if removeDSCPObjects(cfg, backend) {
		if s := dscpStale.Load(); s != nil && s.backend == backend {
			dscpStale.CompareAndSwap(s, nil)
		}
		return
	}
	dscpStale.Store(&dscpState{cfg: cfg, backend: backend})
}

func clearDSCPUnlessKept(cfg *config.Config, backend string) {
	if dscpKeepOnRefresh {
		if st := dscpApplied.Load(); st != nil && st.backend == backend && st.cfg.System.Tables.DSCP.Equal(cfg.System.Tables.DSCP) {
			return
		}
	}
	clearDSCPFor(cfg, backend)
}

func dscpIptIntact(bin string, stamps int) bool {
	listing, err := run(bin, "-w", "-t", "mangle", "-L", "POSTROUTING", "-n", "--line-numbers")
	if err != nil || !dscpJumpSeated(listing) {
		return false
	}
	chain, err := run(bin, "-w", "-t", "mangle", "-S", dscpChainName)
	return err == nil && strings.Count(chain, "-j DSCP") >= stamps
}

func dscpIntact(st *dscpState) bool {
	if st.backend == backendNFTables {
		out, err := run("nft", "list", "chain", "inet", dscpNftTable, dscpNftChain)
		return err == nil && strings.Count(out, "dscp set") >= st.stamps
	}
	for _, bin := range st.bins {
		if !dscpIptIntact(bin, st.stamps) {
			return false
		}
	}
	return true
}

func ensureDSCPLocked(cfg *config.Config, requested bool) bool {
	if cfg != nil && cfg.System.Tables.SkipSetup {
		return false
	}
	acted := false
	if s := dscpStale.Load(); s != nil && removeDSCPObjects(s.cfg, s.backend) {
		dscpStale.CompareAndSwap(s, nil)
		acted = true
	}
	st := dscpApplied.Load()
	if st == nil || dscpIntact(st) {
		return acted
	}
	if requested {
		log.Infof("DSCP stamp rules missing after a firewall rewrite, restoring...")
	} else {
		log.Warnf("DSCP stamp rules missing or out of place, restoring...")
	}
	if err := applyDSCPFor(st.cfg, st.backend); err != nil {
		warnDSCPOnce(err)
		if cur := dscpApplied.Load(); cur == nil || len(cur.bins) < len(st.bins) {
			dscpApplied.Store(st)
		}
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
	dscpLast.Store(nil)
	if st := dscpApplied.Swap(nil); st != nil && !removeDSCPObjects(st.cfg, st.backend) {
		dscpStale.Store(st)
	}
	if s := dscpStale.Load(); s != nil && removeDSCPObjects(s.cfg, s.backend) {
		dscpStale.CompareAndSwap(s, nil)
	}
}
