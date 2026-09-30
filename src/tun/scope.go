package tun

import (
	"strings"

	"github.com/daniellavrushin/b4/log"
)

func missingForwardRules(notrack, clientNotrack, snat bool, tunName string) []string {
	var missing []string
	if !notrack || !clientNotrack {
		missing = append(missing, "the NOTRACK rule for the packets it sends on (the raw table or the CT target is missing)")
	}
	if !snat {
		missing = append(missing, "the SNAT for "+tunName)
	}
	return missing
}

func (r *routeManager) decideScope() {
	missing := missingForwardRules(r.notrackAdded, r.clientNotrackAdded, r.snatAdded || r.srcIP == "", r.tunName)
	if len(missing) == 0 {
		r.deleteNatGuardRules()
		return
	}
	r.localOnly = true
	hadSNAT := r.snatAdded
	r.removeSNAT()
	r.installNatGuard()
	if hadSNAT && r.natGuardAdded {
		log.Infof("TUN: replaced the SNAT for %s with a NAT exemption, so captured connections keep their source address", r.tunName)
	}
	log.Warnf("TUN: b4 could not install %s, so it captures only connections this device makes itself, its SOCKS5 and MTProto proxies included; traffic this device forwards for other hosts, such as LAN clients or Docker containers on bridge networks, stays out of %s, because without those rules the packets b4 sends on for them lose their NAT mapping and the replies never arrive",
		strings.Join(missing, " and "), r.tunName)
}

func (r *routeManager) natGuardSpec() []string {
	return []string{"-o", r.tunName, "-j", "ACCEPT"}
}

func (r *routeManager) installNatGuard() {
	guard := r.natGuardSpec()
	if _, err := run(append([]string{"iptables", "-t", "nat", "-C", "POSTROUTING"}, guard...)...); err == nil {
		r.natGuardAdded = true
		r.keepNatRuleAhead(guard, "the NAT exemption")
		return
	}
	if _, err := run(append([]string{"iptables", "-t", "nat", "-I", "POSTROUTING", "1"}, guard...)...); err != nil {
		log.Warnf("TUN: failed to exempt traffic into %s from NAT: %v", r.tunName, err)
		return
	}
	r.natGuardAdded = true
}

func (r *routeManager) ensureNatGuard() {
	guard := r.natGuardSpec()
	if _, err := run(append([]string{"iptables", "-t", "nat", "-C", "POSTROUTING"}, guard...)...); err != nil {
		if _, err := run(append([]string{"iptables", "-t", "nat", "-I", "POSTROUTING", "1"}, guard...)...); err == nil {
			r.natGuardAdded = true
			log.Infof("TUN: reconcile restored the NAT exemption for %s", r.tunName)
		}
		return
	}
	r.natGuardAdded = true
	r.keepNatRuleAhead(guard, "the NAT exemption")
}

func (r *routeManager) deleteNatGuardRules() {
	guard := r.natGuardSpec()
	for {
		if _, err := run(append([]string{"iptables", "-t", "nat", "-D", "POSTROUTING"}, guard...)...); err != nil {
			return
		}
	}
}

func (r *routeManager) removeNatGuard() {
	if !r.natGuardAdded {
		return
	}
	r.deleteNatGuardRules()
	r.natGuardAdded = false
}
