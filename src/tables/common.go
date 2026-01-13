package tables

import (
	"bytes"
	"os/exec"
	"strings"
	"sync"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/http/handler"
	"github.com/daniellavrushin/b4/log"
)

var modulesLoaded sync.Once

func AddRules(cfg *config.Config) error {
	if cfg.System.Tables.SkipSetup {
		return nil
	}

	handler.SetTablesRefreshFunc(func() error {
		ClearRules(cfg)
		return AddRules(cfg)
	})

	backend := detectFirewallBackend()
	log.Tracef("Detected firewall backend: %s", backend)
	metrics := handler.GetMetricsCollector()
	metrics.TablesStatus = backend

	if backend == "nftables" {
		nft := NewNFTablesManager(cfg)
		return nft.Apply()
	}

	ipt := NewIPTablesManager(cfg)

	return ipt.Apply()
}

func ClearRules(cfg *config.Config) error {

	backend := detectFirewallBackend()

	if backend == "nftables" {
		nft := NewNFTablesManager(cfg)
		return nft.Clear()
	}

	ipt := NewIPTablesManager(cfg)
	return ipt.Clear()
}

func run(args ...string) (string, error) {
	var out bytes.Buffer
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

func setSysctlOrProc(name, val string) {
	_, _ = run("sh", "-c", "sysctl -w "+name+"="+val+" || echo "+val+" > /proc/sys/"+strings.ReplaceAll(name, ".", "/"))
}

func getSysctlOrProc(name string) string {
	out, _ := run("sh", "-c", "sysctl -n "+name+" 2>/dev/null || cat /proc/sys/"+strings.ReplaceAll(name, ".", "/"))
	return strings.TrimSpace(out)
}

func detectFirewallBackend() string {
	if hasBinary("nft") {
		out, err := run("nft", "list", "tables")
		if err == nil && out != "" {
			return "nftables"
		}
	}

	if hasBinary("iptables") {
		out, _ := run("iptables", "--version")
		if strings.Contains(out, "nf_tables") {
			return "nftables"
		}
		return "iptables"
	}

	return "iptables"
}

func hasBinary(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func loadKernelModules() {
	modulesLoaded.Do(func() {
		_, _ = run("sh", "-c", "modprobe -q nf_conntrack 2>/dev/null || true")
		_, _ = run("sh", "-c", "modprobe -q xt_connbytes 2>/dev/null || true")
		_, _ = run("sh", "-c", "modprobe -q xt_NFQUEUE 2>/dev/null || true")
		_, _ = run("sh", "-c", "modprobe -q xt_multiport 2>/dev/null || true")
		_, _ = run("sh", "-c", "modprobe -q nf_tables 2>/dev/null || true")
		_, _ = run("sh", "-c", "modprobe -q nft_queue 2>/dev/null || true")
		_, _ = run("sh", "-c", "modprobe -q nft_ct 2>/dev/null || true")
	})
}
