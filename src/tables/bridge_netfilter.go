package tables

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

var (
	bridgeNetfilterProcDir     = "/proc/sys/net/bridge"
	bridgeNetfilterNetDir      = "/sys/class/net"
	bridgeNetfilterDockerdConf = "/etc/sysctl.d/12-br-netfilter-ip.conf"
)

type BridgeNetfilter struct {
	GlobalV4  bool
	GlobalV6  bool
	BridgesV4 []string
	BridgesV6 []string
}

func ReadBridgeNetfilter() BridgeNetfilter {
	var bn BridgeNetfilter
	if _, err := os.Stat(bridgeNetfilterProcDir); err != nil {
		return bn
	}
	bn.GlobalV4 = bridgeNetfilterFlag(filepath.Join(bridgeNetfilterProcDir, "bridge-nf-call-iptables"))
	bn.GlobalV6 = bridgeNetfilterFlag(filepath.Join(bridgeNetfilterProcDir, "bridge-nf-call-ip6tables"))
	entries, err := os.ReadDir(bridgeNetfilterNetDir)
	if err != nil {
		return bn
	}
	for _, e := range entries {
		dir := filepath.Join(bridgeNetfilterNetDir, e.Name(), "bridge")
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			continue
		}
		if ports, err := os.ReadDir(filepath.Join(bridgeNetfilterNetDir, e.Name(), "brif")); err == nil && len(ports) == 0 {
			continue
		}
		if bn.GlobalV4 || bridgeNetfilterFlag(filepath.Join(dir, "nf_call_iptables")) {
			bn.BridgesV4 = append(bn.BridgesV4, e.Name())
		}
		if bn.GlobalV6 || bridgeNetfilterFlag(filepath.Join(dir, "nf_call_ip6tables")) {
			bn.BridgesV6 = append(bn.BridgesV6, e.Name())
		}
	}
	return bn
}

func bridgeNetfilterFlag(path string) bool {
	b, err := os.ReadFile(path)
	return err == nil && strings.TrimSpace(string(b)) == "1"
}

func (bn BridgeNetfilter) Bridges(ipv4, ipv6 bool) []string {
	var out []string
	if ipv4 {
		out = append(out, bn.BridgesV4...)
	}
	if ipv6 {
		out = append(out, bn.BridgesV6...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func (bn BridgeNetfilter) Switches(ipv4, ipv6 bool) (sysctls, attrs []string) {
	if ipv4 && len(bn.BridgesV4) > 0 {
		if bn.GlobalV4 {
			sysctls = append(sysctls, "net.bridge.bridge-nf-call-iptables")
		} else {
			attrs = append(attrs, "nf_call_iptables")
		}
	}
	if ipv6 && len(bn.BridgesV6) > 0 {
		if bn.GlobalV6 {
			sysctls = append(sysctls, "net.bridge.bridge-nf-call-ip6tables")
		} else {
			attrs = append(attrs, "nf_call_ip6tables")
		}
	}
	return sysctls, attrs
}

func bridgeNetfilterWarning(bn BridgeNetfilter, ipv4, ipv6 bool) string {
	bridges := bn.Bridges(ipv4, ipv6)
	if len(bridges) == 0 {
		return ""
	}
	sysctls, attrs := bn.Switches(ipv4, ipv6)

	var b strings.Builder
	fmt.Fprintf(&b, "Routing: bridge netfilter is on for %s, so proxy and mtproto-ws sets and Telegram over WebSocket cannot take over connections from the devices behind these bridges: those connections hang until they time out, and only the router's own connections get through.", strings.Join(bridges, ", "))
	if len(sysctls) > 0 {
		cmds := make([]string, 0, len(sysctls))
		lines := make([]string, 0, len(sysctls))
		for _, name := range sysctls {
			cmds = append(cmds, "sysctl -w "+name+"=0")
			lines = append(lines, name+"=0")
		}
		fmt.Fprintf(&b, " Turn it off with '%s' and add '%s' to /etc/sysctl.conf to keep it off after a reboot.", strings.Join(cmds, "; "), strings.Join(lines, "' and '"))
		if _, err := os.Stat(bridgeNetfilterDockerdConf); err == nil {
			fmt.Fprintf(&b, " Here %s, which the dockerd package installs, turns it on at boot.", bridgeNetfilterDockerdConf)
		}
	}
	if len(attrs) > 0 {
		fmt.Fprintf(&b, " Write 0 to %s under /sys/class/net/<bridge>/bridge/ for the bridges that have it on.", strings.Join(attrs, " and "))
	}
	return b.String()
}

var (
	bridgeNetfilterMu     sync.Mutex
	bridgeNetfilterLogged string
)

func RoutingCheckBridgeNetfilter() {
	routeNoteBridgeNetfilter(RoutingTProxyFamilies())
}

func RoutingTProxyFamilies() (ipv4, ipv6 bool) {
	routeMu.Lock()
	defer routeMu.Unlock()
	return routeTProxyFamiliesLocked()
}

func routeTProxyFamiliesLocked() (ipv4, ipv6 bool) {
	for _, st := range routeRuleCache {
		if !config.RoutingUsesTProxy(st.mode) {
			continue
		}
		ipv4 = ipv4 || st.ipv4
		ipv6 = ipv6 || st.ipv6
	}
	return ipv4, ipv6
}

func routeNoteBridgeNetfilter(ipv4, ipv6 bool) {
	msg := bridgeNetfilterWarning(ReadBridgeNetfilter(), ipv4, ipv6)
	bridgeNetfilterMu.Lock()
	defer bridgeNetfilterMu.Unlock()
	if msg == bridgeNetfilterLogged {
		return
	}
	cleared := bridgeNetfilterLogged != "" && msg == "" && (ipv4 || ipv6)
	bridgeNetfilterLogged = msg
	if msg != "" {
		log.Warnf("%s", msg)
		return
	}
	if cleared {
		log.Infof("Routing: bridge netfilter no longer runs on a bridge with ports, so proxy and mtproto-ws sets and Telegram over WebSocket reach the devices behind the bridges again")
	}
}
