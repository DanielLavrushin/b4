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

func run(args ...string) (string, error) {
	if len(args) > 1 && isIPTablesBinary(args[0]) && !iptablesSupportsWait(args[0]) {
		args = dropWaitFlag(args)
	}
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

func hasBinary(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func runLogged(op string, args ...string) {
	out, err := run(args...)
	if err != nil {
		msg := strings.TrimSpace(out)
		if strings.Contains(msg, "File exists") || strings.Contains(msg, "already exists") {
			return
		}
		if strings.Contains(msg, "No such file or directory") || strings.Contains(msg, "FIB table does not exist") {
			log.Tracef("%s: %s | cmd=%s", op, msg, strings.Join(args, " "))
			return
		}
		log.Warnf("%s failed: %v | cmd=%s | out=%s", op, err, strings.Join(args, " "), strings.TrimSpace(out))
	}
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
	})
}
