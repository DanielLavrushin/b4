package tun

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/tables"
)

func reinjectMarkMatch() string {
	return fmt.Sprintf("0x%x/0x%x", engine.ReinjectMarkBit, engine.ReinjectMarkBit)
}

func clientMarkMatch() string {
	return fmt.Sprintf("0x%x/0x%x", engine.ClientMark, engine.ClientMark)
}

func interfaceMTU(iface string) int {
	b, err := os.ReadFile("/sys/class/net/" + iface + "/mtu")
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0
	}
	return n
}

type routeManager struct {
	tunName       string
	tunAddr       string
	tunAddrV6     string
	outIface      string
	outGateway    string
	mark          uint
	routeTable    int
	skipTables    bool
	savedDefault  string
	savedRPFilter string
	fwdRulesAdded      bool
	snatAdded          bool
	notrackAdded       bool
	clientNotrackAdded bool

	captureTable int
	tcpPorts     []string
	udpPorts     []string
	tcpLimit     int
	udpLimit     int
	dupIPs       []string
	replyCapture bool

	devicesEnabled bool
	whiteIsBlack   bool
	selectedMACs   []string

	followDefault bool

	mu                sync.Mutex
	srcIP             string
	resolvedCapture   string
	multiport         bool
	captureRulesAdded bool
	captureExcl       []string
}

func resolveDefaultEgress(skipDev string) (iface, gw, src string, ok bool) {
	out, err := run("ip", "-4", "route", "show", "default")
	if err != nil {
		return "", "", "", false
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		dev := extractField(line, "dev")
		if dev == "" || dev == skipDev {
			continue
		}
		src = extractField(line, "src")
		if src == "" {
			src = interfacePrimaryIPv4(dev)
		}
		return dev, extractGateway(line), src, true
	}
	return "", "", "", false
}

func (r *routeManager) currentSrcIP() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.srcIP
}

func (r *routeManager) setupNAT() {
	markStr := reinjectMarkMatch()

	if r.srcIP != "" {
		snat := []string{"-o", r.tunName, "-j", "SNAT", "--to-source", r.srcIP}
		if _, err := run(append([]string{"iptables", "-t", "nat", "-C", "POSTROUTING"}, snat...)...); err != nil {
			if _, err := run(append([]string{"iptables", "-t", "nat", "-A", "POSTROUTING"}, snat...)...); err != nil {
				log.Warnf("TUN: failed to add SNAT %s -> %s: %v", r.tunName, r.srcIP, err)
			} else {
				r.snatAdded = true
				log.Infof("TUN: SNAT installed (traffic into %s -> source %s)", r.tunName, r.srcIP)
			}
		} else {
			r.snatAdded = true
		}
	} else {
		log.Warnf("TUN: no source IP derived for %s; forwarded LAN traffic will not be NAT'd and replies will not return", r.outIface)
	}

	r.notrackAdded = r.installNotrack(markStr, "re-injected packets")
	r.clientNotrackAdded = r.installNotrack(clientMarkMatch(), "client-direction re-injects")
}

func (r *routeManager) installNotrack(markStr, label string) bool {
	notrack := []string{"-m", "mark", "--mark", markStr, "-j", "CT", "--notrack"}
	if _, err := run(append([]string{"iptables", "-t", "raw", "-C", "OUTPUT"}, notrack...)...); err == nil {
		return true
	}
	if _, err := run(append([]string{"iptables", "-t", "raw", "-A", "OUTPUT"}, notrack...)...); err != nil {
		log.Infof("TUN: NOTRACK not installed for mark %s (no raw table here); conntrack sysctls + SNAT cover it, so this is harmless: %v", markStr, err)
		return false
	}
	log.Infof("TUN: NOTRACK installed for %s (mark %s)", label, markStr)
	return true
}

func (r *routeManager) removeNotrack(markStr string) {
	for {
		if _, err := run("iptables", "-t", "raw", "-D", "OUTPUT", "-m", "mark", "--mark", markStr, "-j", "CT", "--notrack"); err != nil {
			break
		}
	}
}

func (r *routeManager) removeSNAT() {
	if !r.snatAdded || r.srcIP == "" {
		return
	}
	for {
		if _, err := run("iptables", "-t", "nat", "-D", "POSTROUTING", "-o", r.tunName, "-j", "SNAT", "--to-source", r.srcIP); err != nil {
			break
		}
	}
	r.snatAdded = false
}

func (r *routeManager) teardownNAT() {
	r.removeSNAT()
	if r.notrackAdded {
		r.removeNotrack(reinjectMarkMatch())
		r.notrackAdded = false
	}
	if r.clientNotrackAdded {
		r.removeNotrack(clientMarkMatch())
		r.clientNotrackAdded = false
	}
}

func (r *routeManager) applyForwarding() int {
	added := 0
	for _, dir := range []string{"-i", "-o"} {
		if _, err := run("iptables", "-C", "FORWARD", dir, r.tunName, "-j", "ACCEPT"); err == nil {
			continue
		}
		if _, err := run("iptables", "-I", "FORWARD", dir, r.tunName, "-j", "ACCEPT"); err != nil {
			log.Warnf("TUN: failed to add FORWARD accept (%s %s): %v", dir, r.tunName, err)
		} else {
			added++
		}
	}
	r.fwdRulesAdded = true
	return added
}

func (r *routeManager) setupForwarding() {
	if r.applyForwarding() > 0 {
		log.Infof("TUN: FORWARD accept rules installed for %s", r.tunName)
	}
}

func (r *routeManager) teardownForwarding() {
	if !r.fwdRulesAdded {
		return
	}
	for _, dir := range []string{"-i", "-o"} {
		for {
			if _, err := run("iptables", "-D", "FORWARD", dir, r.tunName, "-j", "ACCEPT"); err != nil {
				break
			}
		}
	}
}

func (r *routeManager) setup() error {
	if !r.skipTables {
		if _, err := exec.LookPath("iptables"); err != nil {
			return fmt.Errorf("TUN mode needs the iptables binary (or the iptables-nft compat shim) for its SNAT/FORWARD/NOTRACK/capture rules, but it was not found (%w); this looks like an nft-only system. Install iptables/iptables-nft, or run with --skip-tables and manage NAT/forwarding yourself. Native nftables rules for TUN are a planned follow-up", err)
		}
	}

	out, err := run("ip", "-4", "route", "show", "default")
	if err != nil {
		return fmt.Errorf("failed to read current default route: %w", err)
	}
	r.savedDefault = strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
	log.Infof("TUN: saved default route: %s", r.savedDefault)

	var srcIP string
	if r.followDefault {
		iface, gw, src, found := resolveDefaultEgress(r.tunName)
		if !found {
			return fmt.Errorf("TUN follow-default mode: no usable IPv4 default route to derive the uplink; bring up the WAN/VPN first or pin queue.tun.out_interface")
		}
		r.outIface = iface
		r.outGateway = gw
		srcIP = src
		log.Infof("TUN: following default route (uplink=%s gw=%q src=%q)", iface, gw, src)
	} else {
		if r.outGateway == "" {
			r.outGateway = extractGateway(r.savedDefault)
			if r.outGateway != "" {
				log.Infof("TUN: auto-detected gateway: %s", r.outGateway)
			} else {
				log.Infof("TUN: no gateway on default route, treating %s as point-to-point", r.outIface)
			}
		}
		srcIP = interfacePrimaryIPv4(r.outIface)
		if srcIP == "" {
			srcIP = extractField(r.savedDefault, "src")
		}
	}
	r.srcIP = srcIP

	if _, err := run("ip", "addr", "add", r.tunAddr, "dev", r.tunName); err != nil {
		return fmt.Errorf("ip addr add: %w", err)
	}
	if r.tunAddrV6 != "" {
		if _, err := run("ip", "-6", "addr", "add", r.tunAddrV6, "dev", r.tunName); err != nil {
			log.Warnf("TUN: failed to add IPv6 address: %v", err)
		}
	}
	if _, err := run("ip", "link", "set", r.tunName, "up"); err != nil {
		return fmt.Errorf("ip link set up: %w", err)
	}
	mtu := interfaceMTU(r.outIface)
	if mtu <= 0 {
		mtu = 1500
	}
	if _, err := run("ip", "link", "set", r.tunName, "mtu", strconv.Itoa(mtu)); err != nil {
		log.Warnf("TUN: failed to set MTU %d: %v", mtu, err)
	} else {
		log.Infof("TUN: set %s MTU=%d (matching %s)", r.tunName, mtu, r.outIface)
	}

	if r.tunAddrV6 == "" {
		if err := os.WriteFile("/proc/sys/net/ipv6/conf/"+r.tunName+"/disable_ipv6", []byte("1\n"), 0644); err != nil {
			log.Tracef("TUN: could not disable IPv6 on %s: %v", r.tunName, err)
		}
	} else {
		log.Warnf("TUN: address_v6 %s is set, leaving IPv6 enabled on %s - note IPv6 is not yet forwarded in TUN mode, so v6 packets are dropped", r.tunAddrV6, r.tunName)
	}

	if r.skipTables {
		log.Infof("TUN: --skip-tables set; skipping rp_filter/FORWARD/SNAT/NOTRACK - manage NAT and forwarding yourself (b4 still sets up routing: device, capture, bypass table)")
	} else {
		r.loosenRPFilter()
		r.setupForwarding()
		r.setupNAT()
	}

	r.resolvedCapture = r.resolveCaptureMode()
	r.saveState()
	var capErr error
	if r.resolvedCapture == "ports" {
		capErr = r.setupPortCapture(srcIP)
	} else {
		capErr = r.setupDefaultCapture(srcIP)
	}
	if capErr != nil {
		return capErr
	}
	return nil
}

func rpFilterPath(iface string) string {
	return "/proc/sys/net/ipv4/conf/" + iface + "/rp_filter"
}

func (r *routeManager) loosenRPFilter() {
	path := rpFilterPath(r.outIface)
	cur, err := os.ReadFile(path)
	if err != nil {
		log.Warnf("TUN: cannot read rp_filter for %s: %v", r.outIface, err)
		return
	}
	r.savedRPFilter = strings.TrimSpace(string(cur))
	if r.savedRPFilter == "2" {
		return
	}
	if err := os.WriteFile(path, []byte("2\n"), 0644); err != nil {
		log.Warnf("TUN: failed to set %s rp_filter=2 (loose): %v", r.outIface, err)
		r.savedRPFilter = ""
		return
	}
	log.Infof("TUN: set %s rp_filter=2 (loose) for asymmetric routing (was %s)", r.outIface, r.savedRPFilter)
}

func (r *routeManager) restoreRPFilter() {
	if r.savedRPFilter == "" {
		return
	}
	if err := os.WriteFile(rpFilterPath(r.outIface), []byte(r.savedRPFilter+"\n"), 0644); err != nil {
		log.Warnf("TUN: failed to restore %s rp_filter: %v", r.outIface, err)
	}
}

func (r *routeManager) setupBypassTable() error {
	tableStr := fmt.Sprintf("%d", r.routeTable)
	markStr := reinjectMarkMatch()

	if existing, err := run("ip", "route", "show", "table", tableStr); err == nil && strings.TrimSpace(existing) != "" {
		if !r.ownsBypassTable(markStr, tableStr) {
			return fmt.Errorf("route table %d is already in use (likely a system table; see /etc/iproute2/rt_tables) - set queue.tun.route_table to an unused id", r.routeTable)
		}
		log.Infof("TUN: reusing route table %d left by a previous run (flushing stale entries)", r.routeTable)
		run("ip", "route", "flush", "table", tableStr)
	}

	r.delFwmarkRule(markStr, tableStr)

	if _, err := run("ip", "rule", "add", "fwmark", markStr, "lookup", tableStr, "priority", "100"); err != nil {
		return fmt.Errorf("ip rule add (whole-default capture needs policy routing; a busybox 'ip' may reject custom tables - install full iproute2, e.g. 'apk add iproute2', or set queue.tun.route_table <= 255): %w", err)
	}
	return r.addBypassDefault(tableStr)
}

func ruleFieldValue(line, key string) string {
	fields := strings.Fields(line)
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == key {
			return fields[i+1]
		}
	}
	return ""
}

func (r *routeManager) ownsBypassTable(markStr, tableStr string) bool {
	out, err := run("ip", "rule", "show")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		if ruleFieldValue(line, "lookup") != tableStr {
			continue
		}
		fw := ruleFieldValue(line, "fwmark")
		bare := strings.SplitN(markStr, "/", 2)[0]
		legacy := fmt.Sprintf("0x%x", r.mark)
		if fw == markStr || fw == bare || strings.HasPrefix(fw, bare+"/") ||
			fw == legacy || strings.HasPrefix(fw, legacy+"/") {
			return true
		}
	}
	return false
}

func (r *routeManager) delFwmarkRule(markStr, tableStr string) {
	bare := strings.SplitN(markStr, "/", 2)[0]
	legacy := fmt.Sprintf("0x%x", r.mark)
	for _, m := range []string{markStr, bare, legacy, legacy + "/" + legacy} {
		for {
			if _, err := run("ip", "rule", "del", "fwmark", m, "lookup", tableStr); err != nil {
				break
			}
		}
	}
}

func (r *routeManager) replaceDefaultIntoTun() error {
	args := []string{"ip", "route", "replace", "default", "dev", r.tunName}
	if r.srcIP != "" {
		args = append(args, "src", r.srcIP)
	}
	if _, err := run(args...); err != nil {
		if r.srcIP == "" {
			return err
		}
		log.Warnf("TUN: default route with src %s rejected (%v); retrying without src", r.srcIP, err)
		if _, err2 := run("ip", "route", "replace", "default", "dev", r.tunName); err2 != nil {
			return err2
		}
		r.srcIP = ""
	}
	return nil
}

func (r *routeManager) setupDefaultCapture(srcIP string) error {
	if err := r.setupBypassTable(); err != nil {
		return err
	}

	if err := r.replaceDefaultIntoTun(); err != nil {
		return fmt.Errorf("ip route replace default: %w", err)
	}

	log.Infof("TUN: default-capture routing configured (tun=%s, out=%s gw=%q src=%q mark=0x%x table=%d)",
		r.tunName, r.outIface, r.outGateway, r.srcIP, r.mark, r.routeTable)

	return nil
}

func (r *routeManager) refreshEgress() bool {
	if !r.followDefault {
		return false
	}
	iface, gw, src, ok := resolveDefaultEgress(r.tunName)
	if !ok || (iface == r.outIface && gw == r.outGateway) {
		return false
	}
	log.Infof("TUN: default route changed; re-pointing egress %s(gw %q) -> %s(gw %q)", r.outIface, r.outGateway, iface, gw)

	if !r.skipTables {
		r.restoreRPFilter()
	}
	r.removeSNAT()
	r.outIface = iface
	r.outGateway = gw
	if src != "" {
		r.srcIP = src
	}
	if r.resolvedCapture == "default" {
		if gw != "" {
			r.savedDefault = fmt.Sprintf("default via %s dev %s", gw, iface)
		} else {
			r.savedDefault = fmt.Sprintf("default dev %s", iface)
		}
		if src != "" {
			r.savedDefault += " src " + src
		}
	}
	if !r.skipTables {
		r.savedRPFilter = ""
		r.loosenRPFilter()
	}
	r.saveState()

	tableStr := strconv.Itoa(r.routeTable)
	run("ip", "route", "flush", "table", tableStr)
	if err := r.addBypassDefault(tableStr); err != nil {
		log.Warnf("TUN: reconcile failed to rebuild bypass route for new uplink: %v", err)
	}

	if mtu := interfaceMTU(iface); mtu > 0 {
		run("ip", "link", "set", r.tunName, "mtu", strconv.Itoa(mtu))
	}

	if r.resolvedCapture == "ports" {
		if err := r.replaceCaptureDefault(strconv.Itoa(r.captureTable)); err != nil {
			log.Warnf("TUN: reconcile failed to refresh capture src for new uplink: %v", err)
		}
	} else if err := r.replaceDefaultIntoTun(); err != nil {
		log.Warnf("TUN: reconcile failed to refresh default-capture for new uplink: %v", err)
	}

	return true
}

func (r *routeManager) reconcile() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.refreshEgress()

	newSrc := interfacePrimaryIPv4(r.outIface)
	if r.followDefault {
		if _, _, s, ok := resolveDefaultEgress(r.tunName); ok && s != "" {
			newSrc = s
		}
	}
	if newSrc != "" && newSrc != r.srcIP {
		log.Infof("TUN: uplink %s source changed %q -> %q; updating SNAT and capture source", r.outIface, r.srcIP, newSrc)
		r.removeSNAT()
		r.srcIP = newSrc
		if r.resolvedCapture == "ports" {
			if err := r.replaceCaptureDefault(strconv.Itoa(r.captureTable)); err != nil {
				log.Warnf("TUN: reconcile failed to refresh capture-table src: %v", err)
			}
		} else {
			if err := r.replaceDefaultIntoTun(); err != nil {
				log.Warnf("TUN: reconcile failed to refresh default-capture src: %v", err)
			}
		}
	}

	if !r.skipTables {
		if n := r.applyForwarding(); n > 0 {
			log.Infof("TUN: reconcile restored %d FORWARD accept rule(s) for %s", n, r.tunName)
		}
		r.ensureNAT()
	}
	r.ensureBypass()
	if r.resolvedCapture == "ports" {
		r.ensurePortCapture()
	} else {
		r.ensureDefaultCapture()
	}
}

func (r *routeManager) ensureNAT() {
	if r.srcIP != "" {
		snat := []string{"-o", r.tunName, "-j", "SNAT", "--to-source", r.srcIP}
		if _, err := run(append([]string{"iptables", "-t", "nat", "-C", "POSTROUTING"}, snat...)...); err != nil {
			if _, err := run(append([]string{"iptables", "-t", "nat", "-A", "POSTROUTING"}, snat...)...); err == nil {
				r.snatAdded = true
				log.Infof("TUN: reconcile restored SNAT (%s -> %s)", r.tunName, r.srcIP)
			}
		} else {
			r.snatAdded = true
		}
	}
	r.ensureNotrack(&r.notrackAdded, reinjectMarkMatch())
	r.ensureNotrack(&r.clientNotrackAdded, clientMarkMatch())
}

func (r *routeManager) ensureNotrack(added *bool, markStr string) {
	if !*added {
		return
	}
	notrack := []string{"-m", "mark", "--mark", markStr, "-j", "CT", "--notrack"}
	if _, err := run(append([]string{"iptables", "-t", "raw", "-C", "OUTPUT"}, notrack...)...); err != nil {
		if _, err := run(append([]string{"iptables", "-t", "raw", "-A", "OUTPUT"}, notrack...)...); err == nil {
			log.Infof("TUN: reconcile restored NOTRACK (mark %s)", markStr)
		}
	}
}

func (r *routeManager) ensureBypass() {
	markStr := reinjectMarkMatch()
	tableStr := fmt.Sprintf("%d", r.routeTable)
	if !r.ownsBypassTable(markStr, tableStr) {
		if _, err := run("ip", "rule", "add", "fwmark", markStr, "lookup", tableStr, "priority", "100"); err != nil {
			log.Warnf("TUN: reconcile failed to restore fwmark rule: %v", err)
		} else {
			log.Infof("TUN: reconcile restored fwmark rule (mark %s -> table %s)", markStr, tableStr)
		}
	}
	if out, _ := run("ip", "route", "show", "table", tableStr); strings.TrimSpace(out) == "" {
		if err := r.addBypassDefault(tableStr); err != nil {
			log.Warnf("TUN: reconcile failed to restore bypass default route: %v", err)
		} else {
			log.Infof("TUN: reconcile restored bypass default route (table %s)", tableStr)
		}
	}
}

func (r *routeManager) ensureDefaultCapture() {
	out, _ := run("ip", "-4", "route", "show", "default")
	if strings.Contains(out, "dev "+r.tunName) {
		return
	}
	log.Infof("TUN: reconcile re-capturing default route into %s (it was reverted)", r.tunName)
	if err := r.replaceDefaultIntoTun(); err != nil {
		log.Warnf("TUN: reconcile failed to re-capture default route: %v", err)
	}
}

func interfacePrimaryIPv4(iface string) string {
	out, err := run("ip", "-4", "-o", "addr", "show", "dev", iface, "scope", "global")
	if err != nil {
		return ""
	}
	fields := strings.Fields(out)
	for i, f := range fields {
		if f == "inet" && i+1 < len(fields) {
			return strings.SplitN(fields[i+1], "/", 2)[0]
		}
	}
	return ""
}

func (r *routeManager) addBypassDefault(tableStr string) error {
	args := []string{"ip", "route", "replace", "default"}
	if r.outGateway != "" {
		args = append(args, "via", r.outGateway)
	}
	args = append(args, "dev", r.outIface, "table", tableStr)
	if _, err := run(args...); err != nil {
		return fmt.Errorf("ip route replace table (whole-default capture needs policy routing; a busybox 'ip' may reject custom tables - install full iproute2, e.g. 'apk add iproute2', or set queue.tun.route_table <= 255): %w", err)
	}
	return nil
}

func (r *routeManager) teardown() {
	markStr := reinjectMarkMatch()
	tableStr := fmt.Sprintf("%d", r.routeTable)

	if r.resolvedCapture == "ports" {
		r.teardownPortCapture()
	}

	if r.resolvedCapture == "default" && r.savedDefault != "" {
		args := append([]string{"ip", "route", "replace"}, strings.Fields(r.savedDefault)...)
		if _, err := run(args...); err != nil {
			log.Errorf("TUN: failed to restore default route: %v", err)
		} else {
			log.Infof("TUN: restored default route: %s", r.savedDefault)
		}
	}

	r.delFwmarkRule(markStr, tableStr)
	if _, err := run("ip", "route", "flush", "table", tableStr); err != nil {
		log.Warnf("TUN: failed to flush route table %s: %v", tableStr, err)
	}
	if interfaceExists(r.tunName) {
		if _, err := run("ip", "link", "del", r.tunName); err != nil {
			log.Warnf("TUN: failed to delete %s: %v", r.tunName, err)
		}
	} else {
		log.Tracef("TUN: %s already gone (removed when the device fd closed)", r.tunName)
	}

	if !r.skipTables {
		r.teardownForwarding()
		r.teardownNAT()
		r.restoreRPFilter()
	}

	removeStateFile()

	log.Infof("TUN: routing teardown complete")
}

func extractField(routeLine, keyword string) string {
	parts := strings.Fields(routeLine)
	for i, p := range parts {
		if p == keyword && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func extractGateway(routeLine string) string {
	return extractField(routeLine, "via")
}

func run(args ...string) (string, error) {
	if len(args) > 0 && (args[0] == "iptables" || args[0] == "ip6tables") {
		if w := tables.WaitArgs(args[0]); len(w) > 0 {
			newArgs := make([]string, 0, len(args)+len(w))
			newArgs = append(newArgs, args[0])
			newArgs = append(newArgs, w...)
			newArgs = append(newArgs, args[1:]...)
			args = newArgs
		}
	}
	cmd := exec.Command(args[0], args[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
