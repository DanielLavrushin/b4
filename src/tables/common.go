package tables

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const (
	backendNFTables        = "nftables"
	backendIPTables        = "iptables"
	backendIP6Tables       = "ip6tables"
	backendIPTablesLegacy  = "iptables-legacy"
	backendIP6TablesLegacy = "ip6tables-legacy"
)

var modulesLoaded sync.Once

func AddRules(cfg *config.Config) error {
	if cfg.System.Tables.SkipSetup {
		return nil
	}

	IPTablesLockBudgetReset()

	backend := detectFirewallBackend(cfg)
	log.Tracef("Detected firewall backend: %s", backend)

	if backend == backendNFTables {
		nft := NewNFTablesManager(cfg)
		return nft.Apply()
	}

	ipt := NewIPTablesManager(cfg, backend == backendIPTablesLegacy)

	return ipt.Apply()
}

func ClearRules(cfg *config.Config) error {
	if cfg.System.Tables.SkipSetup {
		return nil
	}

	IPTablesLockBudgetReset()

	backend := detectFirewallBackend(cfg)

	if backend == backendNFTables {
		nft := NewNFTablesManager(cfg)
		return nft.Clear()
	}

	ipt := NewIPTablesManager(cfg, backend == backendIPTablesLegacy)
	return ipt.Clear()
}

func DetectBackend(cfg *config.Config) string {
	return detectFirewallBackend(cfg)
}

func ApplyMasqueradeOnly(cfg *config.Config) error {
	if !cfg.System.Tables.Masquerade.Enabled {
		return nil
	}
	loadKernelModules()
	backend := detectFirewallBackend(cfg)
	if backend == backendNFTables {
		nft := NewNFTablesManager(cfg)
		nft.ClearMasquerade()
		return nft.ApplyMasquerade()
	}
	return NewIPTablesManager(cfg, backend == backendIPTablesLegacy).ApplyMasquerade()
}

func ApplyConntrackSysctls() {
	for _, s := range b4SysctlSettings() {
		s.Apply()
	}
}

func RevertConntrackSysctls() {
	for _, s := range b4SysctlSettings() {
		s.RevertBack()
	}
}

func ClearMasqueradeOnly(cfg *config.Config) {
	if !cfg.System.Tables.Masquerade.Enabled {
		return
	}
	backend := detectFirewallBackend(cfg)
	if backend == backendNFTables {
		NewNFTablesManager(cfg).ClearMasquerade()
		return
	}
	NewIPTablesManager(cfg, backend == backendIPTablesLegacy).ClearMasquerade()
}

func hasMSSClamp(cfg *config.Config) bool {
	global, _ := cfg.HasGlobalMSSClamp()
	return global || len(cfg.CollectDeviceMSSClamps()) > 0 || len(cfg.CollectSetMSSClamps()) > 0
}

func ApplyMSSClampOnly(cfg *config.Config) error {
	if !hasMSSClamp(cfg) {
		return nil
	}
	loadKernelModules()
	backend := detectFirewallBackend(cfg)
	if backend == backendNFTables {
		nft := NewNFTablesManager(cfg)
		if err := nft.createTable(); err != nil {
			return err
		}
		return nft.ApplyMSSClamp()
	}
	return NewIPTablesManager(cfg, backend == backendIPTablesLegacy).ApplyMSSClamp()
}

func ClearMSSClampOnly(cfg *config.Config) {
	backend := detectFirewallBackend(cfg)
	if backend == backendNFTables {
		NewNFTablesManager(cfg).ClearMSSClamp()
		return
	}
	NewIPTablesManager(cfg, backend == backendIPTablesLegacy).ClearMSSClamp()
}

var iptWaitSupport sync.Map

var iptVersionRe = regexp.MustCompile(`v(\d+)\.(\d+)(?:\.(\d+))?`)

func isIPTablesBinary(name string) bool {
	base := filepath.Base(name)
	return strings.HasPrefix(base, "iptables") || strings.HasPrefix(base, "ip6tables")
}

func iptablesSupportsWait(bin string) bool {
	if v, ok := iptWaitSupport.Load(bin); ok {
		return v.(bool)
	}
	supported := true
	out, err := exec.Command(bin, "--version").CombinedOutput()
	if err == nil {
		if m := iptVersionRe.FindStringSubmatch(string(out)); m != nil {
			maj, _ := strconv.Atoi(m[1])
			min, _ := strconv.Atoi(m[2])
			patch := 0
			if m[3] != "" {
				patch, _ = strconv.Atoi(m[3])
			}
			if maj < 1 || (maj == 1 && (min < 4 || (min == 4 && patch < 20))) {
				supported = false
			}
		}
	}
	iptWaitSupport.Store(bin, supported)
	if !supported {
		log.Warnf("IPTABLES[%s]: this iptables is too old for the '-w' lock flag (need >= 1.4.20); running without it", bin)
	}
	return supported
}

func dropWaitFlag(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if a == "-w" || a == "--wait" {
			continue
		}
		out = append(out, a)
	}
	return out
}

func WaitArgs(bin string) []string {
	if iptablesSupportsWait(bin) {
		return []string{"-w"}
	}
	return nil
}

var iptLockRetries = 8

var iptLockBackoff = func(attempt int) time.Duration {
	return time.Duration(25<<uint(attempt)) * time.Millisecond
}

var iptLockBudget = 15 * time.Second

var iptLockSpent atomic.Int64

// IPTablesLockBudgetReset starts a fresh waiting budget for one pass over the
// rules. Every command may retry, so without a shared ceiling a pass over a
// contended firewall waits per command and holds its lock for minutes.
func IPTablesLockBudgetReset() {
	iptLockSpent.Store(0)
}

func iptLockBudgetLeft() time.Duration {
	left := int64(iptLockBudget) - iptLockSpent.Load()
	if left <= 0 {
		return 0
	}
	return time.Duration(left)
}

func isXtablesLockBusy(output string, err error) bool {
	if err == nil {
		return false
	}
	o := strings.ToLower(output)
	return strings.Contains(o, "resource temporarily unavailable") ||
		strings.Contains(o, "xtables lock") ||
		strings.Contains(o, "another app is currently holding the xtables lock")
}

func runOnce(args []string) (string, error) {
	var out bytes.Buffer
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if err != nil {
		output := strings.TrimSpace(out.String())
		cmdStr := strings.Join(args, " ")
		if output != "" {
			return output, fmt.Errorf("command [%s] failed: %w (%s)", cmdStr, err, output)
		}
		return output, fmt.Errorf("command [%s] failed: %w", cmdStr, err)
	}
	return out.String(), nil
}

func iptDeletesByPosition(args []string) bool {
	for i := 0; i+2 < len(args); i++ {
		if args[i] != "-D" {
			continue
		}
		if _, err := strconv.Atoi(args[i+2]); err == nil {
			return true
		}
	}
	return false
}

func run(args ...string) (string, error) {
	if len(args) > 1 && isIPTablesBinary(args[0]) && !iptablesSupportsWait(args[0]) {
		args = dropWaitFlag(args)
	}

	out, err := runOnce(args)
	if err == nil || len(args) == 0 || !isIPTablesBinary(args[0]) {
		return out, err
	}

	if isXtablesLockBusy(out, err) && iptDeletesByPosition(args) {
		log.Warnf("IPTABLES[%s]: the table changed while b4 was reading it, so the rule numbers it had are stale and %s was not retried; retrying it would delete whatever rule now sits at that position, which on this router belongs to the firmware", args[0], strings.Join(args[1:], " "))
		return out, err
	}

	for attempt := 0; attempt < iptLockRetries && isXtablesLockBusy(out, err); attempt++ {
		wait := iptLockBackoff(attempt)
		if left := iptLockBudgetLeft(); left < wait {
			if left <= 0 {
				log.Warnf("IPTABLES[%s]: another program has held this pass up for %v already, so b4 stopped waiting and this rule was not installed: %s", args[0], iptLockBudget, strings.Join(args[1:], " "))
				break
			}
			wait = left
		}
		time.Sleep(wait)
		iptLockSpent.Add(int64(wait))
		out, err = runOnce(args)
		if err == nil {
			log.Warnf("IPTABLES[%s]: another program was rewriting the same table; the command went through after %d retries. The '-w' flag only serialises b4 against programs that take the same lock, and the firmware's own scripts do not", args[0], attempt+1)
			return out, nil
		}
	}
	if isXtablesLockBusy(out, err) {
		log.Errorf("IPTABLES[%s]: another program held the firewall lock for the whole retry budget, so this rule was never installed and b4's rule set is incomplete: %v", args[0], err)
	}
	return out, err
}

func setSysctlOrProc(name, val string) {
	_, _ = run("sh", "-c", "sysctl -w "+name+"="+val+" || echo "+val+" > /proc/sys/"+strings.ReplaceAll(name, ".", "/"))
}

func getSysctlOrProc(name string) string {
	out, _ := run("sh", "-c", "sysctl -n "+name+" 2>/dev/null || cat /proc/sys/"+strings.ReplaceAll(name, ".", "/"))
	return strings.TrimSpace(out)
}

func detectFirewallBackend(cfg *config.Config) string {
	if b := cfg.System.Tables.Engine; b != "" {
		switch strings.ToLower(b) {
		case backendNFTables, "nft":
			return backendNFTables
		case backendIPTables:
			return backendIPTables
		case backendIPTablesLegacy:
			return backendIPTablesLegacy
		default:
			log.Warnf("Unknown tables backend %q in config, auto-detecting", b)
		}
	}

	if nftWorking() {
		return backendNFTables
	}

	if hasBinary(backendIPTables) {
		out, _ := run(backendIPTables, "--version")
		if strings.Contains(out, "nf_tables") {
			if hasBinary(backendIPTablesLegacy) {
				log.Infof("nftables not functional, iptables is nft-variant; using %s", backendIPTablesLegacy)
				return backendIPTablesLegacy
			}
			log.Warnf("nftables not functional and %s not found; attempting iptables (nft-variant)", backendIPTablesLegacy)
		}
		return backendIPTables
	}

	if hasBinary(backendIPTablesLegacy) {
		return backendIPTablesLegacy
	}

	return backendIPTables
}

func nftWorking() bool {
	if !hasBinary("nft") {
		return false
	}
	_, err := run("nft", "add", "table", "inet", "_b4_test")
	if err != nil {
		log.Tracef("nftables functional test failed: %v", err)
		return false
	}
	_, _ = run("nft", "delete", "table", "inet", "_b4_test")
	return true
}

var hasBinaryCache sync.Map

func hasBinary(name string) bool {
	if v, ok := hasBinaryCache.Load(name); ok {
		return v.(bool)
	}
	_, err := exec.LookPath(name)
	found := err == nil
	hasBinaryCache.Store(name, found)
	return found
}

func runStdin(stdin string, args ...string) error {
	var out bytes.Buffer
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("command [%s] failed: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(out.String()))
	}
	return nil
}

var runLogged = runLoggedExec

func runLoggedExec(op string, args ...string) bool {
	out, err := run(args...)
	if err != nil {
		msg := strings.TrimSpace(out)
		if strings.Contains(msg, "File exists") || strings.Contains(msg, "already exists") {
			return true
		}
		if strings.Contains(msg, "No such file or directory") || strings.Contains(msg, "FIB table does not exist") ||
			strings.Contains(msg, "The set with the given name does not exist") || strings.Contains(msg, "No such process") {
			log.Tracef("%s: %s | cmd=%s", op, msg, strings.Join(args, " "))
			return false
		}
		log.Warnf("%s failed: %v | cmd=%s | out=%s", op, err, strings.Join(args, " "), strings.TrimSpace(out))
		return false
	}
	return true
}

func runEnsure(args ...string) error {
	out, err := run(args...)
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(out)
	if strings.Contains(msg, "File exists") || strings.Contains(msg, "already exists") {
		return nil
	}
	return fmt.Errorf("%v: %s", err, msg)
}

var hashlimitModuleOnce sync.Once

func loadHashlimitModule() {
	hashlimitModuleOnce.Do(func() {
		_, _ = run("sh", "-c", "modprobe -q xt_hashlimit 2>/dev/null || true")
	})
}

func loadKernelModules() {
	modulesLoaded.Do(func() {
		_, _ = run("sh", "-c", "modprobe -q nfnetlink 2>/dev/null || true")
		_, _ = run("sh", "-c", "modprobe -q nf_conntrack 2>/dev/null || true")
		_, _ = run("sh", "-c", "modprobe -q nf_conntrack_netlink 2>/dev/null || true")
		_, _ = run("sh", "-c", "modprobe -q xt_connbytes 2>/dev/null || true")
		_, _ = run("sh", "-c", "modprobe -q nfnetlink_queue 2>/dev/null || true")
		_, _ = run("sh", "-c", "modprobe -q xt_NFQUEUE 2>/dev/null || true")
		_, _ = run("sh", "-c", "modprobe -q xt_multiport 2>/dev/null || true")
		_, _ = run("sh", "-c", "modprobe -q nf_tables 2>/dev/null || true")
		_, _ = run("sh", "-c", "modprobe -q nft_queue 2>/dev/null || true")
		_, _ = run("sh", "-c", "modprobe -q nft_ct 2>/dev/null || true")
		_, _ = run("sh", "-c", "modprobe -q nf_nat 2>/dev/null || true")
		_, _ = run("sh", "-c", "modprobe -q nft_masq 2>/dev/null || true")
		_, _ = run("sh", "-c", "modprobe -q nft_tproxy 2>/dev/null || true")
		_, _ = run("sh", "-c", "modprobe -q nft_socket 2>/dev/null || true")
		_, _ = run("sh", "-c", "modprobe -q xt_MASQUERADE 2>/dev/null || true")
		_, _ = run("sh", "-c", "modprobe -q xt_set 2>/dev/null || true")
		_, _ = run("sh", "-c", "modprobe -q nft_limit 2>/dev/null || true")
		loadHashlimitModule()
	})
}
