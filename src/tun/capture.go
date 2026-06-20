package tun

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/tables"
)

const (
	tunCaptureChain   = "B4_TUN"
	tunProbeChain     = "B4_TUN_PROBE"
	defaultSteerMark  = engine.TunSteerMark
	defaultClientMark = engine.TunClientMark
	captureRulePrio   = 90
)

func (r *routeManager) steerMarkStr() string {
	return fmt.Sprintf("0x%x/0x%x", defaultSteerMark, defaultSteerMark)
}

func (r *routeManager) resolveCaptureMode() string {
	if iptablesMatchSupported([]string{
		"-p", "tcp", "-m", "connbytes", "--connbytes-dir", "original",
		"--connbytes-mode", "packets", "--connbytes", "0:10", "-j", "ACCEPT",
	}) {
		return "ports"
	}
	log.Warnf("TUN: xt_connbytes not available; capturing the whole default route instead of first-N packets (install xtables-addons / linux-modules-extra for first-N capture)")
	return "default"
}

func iptablesMatchSupported(spec []string) bool {
	run("iptables", "-t", "filter", "-F", tunProbeChain)
	run("iptables", "-t", "filter", "-X", tunProbeChain)
	if _, err := run("iptables", "-t", "filter", "-N", tunProbeChain); err != nil {
		return false
	}
	defer func() {
		run("iptables", "-t", "filter", "-F", tunProbeChain)
		run("iptables", "-t", "filter", "-X", tunProbeChain)
	}()
	_, err := run(append([]string{"iptables", "-t", "filter", "-A", tunProbeChain}, spec...)...)
	return err == nil
}

func (r *routeManager) setupPortCapture(srcIP string) error {
	if err := r.setupBypassTable(); err != nil {
		return err
	}
	if err := r.setupCaptureTable(); err != nil {
		return err
	}
	r.multiport = iptablesMatchSupported([]string{"-p", "tcp", "-m", "multiport", "--dports", "80,443", "-j", "ACCEPT"})
	r.ensureCaptureChain()
	r.rebuildCaptureChain()
	r.ensureCaptureJumps()
	r.captureRulesAdded = true
	log.Infof("TUN: port-capture mode - first %d tcp / %d udp packets on ports %s + DNS routed into %s (steer mark %s, table %d; default route untouched)",
		r.tcpLimit, r.udpLimit, strings.Join(r.tcpPorts, ","), r.tunName, r.steerMarkStr(), r.captureTable)
	return nil
}

func (r *routeManager) setupCaptureTable() error {
	tableStr := strconv.Itoa(r.captureTable)
	steer := r.steerMarkStr()
	for {
		if _, err := run("ip", "rule", "del", "fwmark", steer, "lookup", tableStr); err != nil {
			break
		}
	}
	if _, err := run("ip", "rule", "add", "fwmark", steer, "lookup", tableStr, "priority", strconv.Itoa(captureRulePrio)); err != nil {
		return fmt.Errorf("ip rule add (capture steer; needs policy routing - install full iproute2): %w", err)
	}
	return r.replaceCaptureDefault(tableStr)
}

func (r *routeManager) replaceCaptureDefault(tableStr string) error {
	args := []string{"ip", "route", "replace", "default", "dev", r.tunName}
	if r.srcIP != "" {
		args = append(args, "src", r.srcIP)
	}
	args = append(args, "table", tableStr)
	if _, err := run(args...); err != nil {
		return fmt.Errorf("ip route replace default (capture table %s): %w", tableStr, err)
	}
	return nil
}

func (r *routeManager) ensureCaptureChain() {
	if _, err := run("iptables", "-t", "mangle", "-S", tunCaptureChain); err != nil {
		run("iptables", "-t", "mangle", "-N", tunCaptureChain)
	}
}

func (r *routeManager) ensureCaptureJumps() {
	for _, base := range []string{"PREROUTING", "OUTPUT"} {
		if _, err := run("iptables", "-t", "mangle", "-C", base, "-j", tunCaptureChain); err != nil {
			if _, err := run("iptables", "-t", "mangle", "-I", base, "-j", tunCaptureChain); err != nil {
				log.Warnf("TUN: failed to add capture jump from %s: %v", base, err)
			}
		}
	}
}

func (r *routeManager) desiredCaptureExclusions() []string {
	if r.skipTables {
		return nil
	}
	return tables.RoutingActiveIPSetNames(true, false)
}

func (r *routeManager) rebuildCaptureChain() {
	guard := fmt.Sprintf("0x%x/0x%x", r.mark, r.mark)
	excl := r.desiredCaptureExclusions()

	run("iptables", "-t", "mangle", "-F", tunCaptureChain)

	run("iptables", "-t", "mangle", "-A", tunCaptureChain, "-m", "mark", "--mark", guard, "-j", "RETURN")
	clientGuard := fmt.Sprintf("0x%x/0x%x", defaultClientMark, defaultClientMark)
	run("iptables", "-t", "mangle", "-A", tunCaptureChain, "-m", "mark", "--mark", clientGuard, "-j", "RETURN")

	for _, set := range excl {
		if _, err := run("iptables", "-t", "mangle", "-A", tunCaptureChain, "-m", "set", "--match-set", set, "dst", "-j", "RETURN"); err != nil {
			log.Tracef("TUN: capture exclusion for ipset %s not added (set may be absent): %v", set, err)
		}
	}

	for _, spec := range r.steerSpecs() {
		if _, err := run(append([]string{"iptables", "-t", "mangle", "-A", tunCaptureChain}, spec...)...); err != nil {
			log.Warnf("TUN: failed to add capture rule %v: %v", spec, err)
		}
	}

	r.captureExcl = excl
}

func (r *routeManager) steerSpecs() [][]string {
	steer := r.steerMarkStr()
	mark := []string{"-j", "MARK", "--set-xmark", steer}
	tcpRange := fmt.Sprintf("0:%d", r.tcpLimit)
	udpRange := fmt.Sprintf("0:%d", r.udpLimit)

	var specs [][]string
	cb := func(portRange string) []string {
		return []string{"-m", "connbytes", "--connbytes-dir", "original", "--connbytes-mode", "packets", "--connbytes", portRange}
	}

	for _, ip := range r.dupIPs {
		if r.multiport {
			for _, chunk := range chunkPorts(r.tcpPorts, 15) {
				specs = append(specs, append([]string{"-p", "tcp", "-d", ip, "-m", "multiport", "--dports", strings.Join(chunk, ",")}, mark...))
			}
		} else {
			for _, p := range r.tcpPorts {
				specs = append(specs, append([]string{"-p", "tcp", "-d", ip, "--dport", p}, mark...))
			}
		}
	}

	if r.multiport {
		for _, chunk := range chunkPorts(r.tcpPorts, 15) {
			spec := append([]string{"-p", "tcp", "-m", "multiport", "--dports", strings.Join(chunk, ",")}, cb(tcpRange)...)
			specs = append(specs, append(spec, mark...))
		}
		for _, chunk := range chunkPorts(r.udpPorts, 15) {
			spec := append([]string{"-p", "udp", "-m", "multiport", "--dports", strings.Join(chunk, ",")}, cb(udpRange)...)
			specs = append(specs, append(spec, mark...))
		}
	} else {
		for _, p := range r.tcpPorts {
			spec := append([]string{"-p", "tcp", "--dport", p}, cb(tcpRange)...)
			specs = append(specs, append(spec, mark...))
		}
		for _, p := range r.udpPorts {
			spec := append([]string{"-p", "udp", "--dport", p}, cb(udpRange)...)
			specs = append(specs, append(spec, mark...))
		}
	}

	specs = append(specs, append([]string{"-p", "udp", "--dport", "53"}, mark...))

	if r.replyCapture {
		if r.multiport {
			for _, chunk := range chunkPorts(r.tcpPorts, 15) {
				specs = append(specs, append([]string{"-p", "tcp", "-m", "multiport", "--sports", strings.Join(chunk, ","), "--tcp-flags", "RST", "RST"}, mark...))
			}
		} else {
			for _, p := range r.tcpPorts {
				specs = append(specs, append([]string{"-p", "tcp", "--sport", p, "--tcp-flags", "RST", "RST"}, mark...))
			}
		}
	}
	return specs
}

func (r *routeManager) ensurePortCapture() {
	tableStr := strconv.Itoa(r.captureTable)
	steer := r.steerMarkStr()

	if !r.steerRulePresent(steer, tableStr) {
		if _, err := run("ip", "rule", "add", "fwmark", steer, "lookup", tableStr, "priority", strconv.Itoa(captureRulePrio)); err == nil {
			log.Infof("TUN: reconcile restored capture steer rule (mark %s -> table %s)", steer, tableStr)
		}
	}
	if out, _ := run("ip", "route", "show", "table", tableStr); !strings.Contains(out, "dev "+r.tunName) {
		if err := r.replaceCaptureDefault(tableStr); err == nil {
			log.Infof("TUN: reconcile restored capture default route (table %s)", tableStr)
		}
	}

	r.ensureCaptureChain()
	r.ensureCaptureJumps()
	if desired := r.desiredCaptureExclusions(); !equalStringSet(desired, r.captureExcl) {
		log.Infof("TUN: reconcile refreshing capture exclusions (%d routing set(s))", len(desired))
		r.rebuildCaptureChain()
	}
}

func (r *routeManager) steerRulePresent(steer, tableStr string) bool {
	out, err := run("ip", "rule", "show")
	if err != nil {
		return false
	}
	bare := strings.SplitN(steer, "/", 2)[0]
	for _, line := range strings.Split(out, "\n") {
		if ruleFieldValue(line, "lookup") != tableStr {
			continue
		}
		fw := ruleFieldValue(line, "fwmark")
		if fw == steer || fw == bare || strings.HasPrefix(fw, bare+"/") {
			return true
		}
	}
	return false
}

func (r *routeManager) teardownPortCapture() {
	if !r.captureRulesAdded {
		return
	}
	tableStr := strconv.Itoa(r.captureTable)
	steer := r.steerMarkStr()

	for _, base := range []string{"PREROUTING", "OUTPUT"} {
		for {
			if _, err := run("iptables", "-t", "mangle", "-D", base, "-j", tunCaptureChain); err != nil {
				break
			}
		}
	}
	run("iptables", "-t", "mangle", "-F", tunCaptureChain)
	run("iptables", "-t", "mangle", "-X", tunCaptureChain)

	for {
		if _, err := run("ip", "rule", "del", "fwmark", steer, "lookup", tableStr); err != nil {
			break
		}
	}
	if _, err := run("ip", "route", "flush", "table", tableStr); err != nil {
		log.Tracef("TUN: capture table %s flush: %v", tableStr, err)
	}
	r.captureRulesAdded = false
}

func portMatches(sport uint16, ports []string) bool {
	for _, p := range ports {
		if i := strings.IndexByte(p, ':'); i >= 0 {
			lo, err1 := strconv.Atoi(p[:i])
			hi, err2 := strconv.Atoi(p[i+1:])
			if err1 == nil && err2 == nil && int(sport) >= lo && int(sport) <= hi {
				return true
			}
		} else if n, err := strconv.Atoi(p); err == nil && int(sport) == n {
			return true
		}
	}
	return false
}

func normalizePorts(ports []string) []string {
	out := make([]string, len(ports))
	for i, p := range ports {
		out[i] = strings.ReplaceAll(p, "-", ":")
	}
	return out
}

func chunkPorts(ports []string, size int) [][]string {
	if size < 1 {
		size = 1
	}
	var out [][]string
	for i := 0; i < len(ports); i += size {
		end := i + size
		if end > len(ports) {
			end = len(ports)
		}
		out = append(out, ports[i:end])
	}
	return out
}

func equalStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
