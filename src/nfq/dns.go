package nfq

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/metrics"
	"github.com/daniellavrushin/b4/sock"
)

var (
	dohClientMu      sync.Mutex
	dohClientMark    int
	dohClientTimeout time.Duration
	dohClient        *http.Client
)

func getDoHClient(mark int, timeout time.Duration) *http.Client {
	dohClientMu.Lock()
	defer dohClientMu.Unlock()
	if dohClient == nil || dohClientMark != mark || dohClientTimeout != timeout {
		if dohClient != nil {
			dohClient.CloseIdleConnections()
		}
		dohClient = dns.MarkedDoHClient(mark, timeout)
		dohClientMark = mark
		dohClientTimeout = timeout
	}
	return dohClient
}

func parseDNSName(msg []byte, offset int) (string, bool) {
	if offset < 0 || offset >= len(msg) {
		return "", false
	}
	var labels []string
	i := offset
	const maxSteps = 256
	steps := 0
	for {
		if steps >= maxSteps || i >= len(msg) {
			return "", false
		}
		steps++
		l := int(msg[i])
		if l == 0 {
			break
		}

		if l&0xC0 == 0xC0 {
			if i+1 >= len(msg) {
				return "", false
			}
			ptr := int(l&0x3F)<<8 | int(msg[i+1])
			if ptr >= len(msg) {
				return "", false
			}
			i = ptr
			continue
		}

		if i+1+l > len(msg) {
			return "", false
		}
		labels = append(labels, string(msg[i+1:i+1+l]))
		i += 1 + l
	}
	if len(labels) == 0 {
		return "", false
	}
	return strings.Join(labels, "."), true
}

const (
	dnsActionBlock          = "dns-block"
	dnsActionSinkhole       = "dns-sinkhole"
	dnsActionPassthrough    = "dns-passthrough"
	dnsActionBadTarget      = "dns-bad-target"
	dnsActionIPv6Disabled   = "dns-ipv6-disabled"
	dnsActionServfail       = "dns-servfail"
	dnsActionHeal           = "dns-heal"
	dnsActionPin            = "dns-pin"
	dnsActionNoClient       = "dns-no-client"
	dnsActionOverload       = "dns-overload"
	dnsActionDoHPrefix      = "dns-doh->"
	dnsActionForwardPrefix  = "dns-forward->"
	dnsActionEscalatePrefix = "dns-escalate->"
)

const maxDNSResolveInflight = 64

var dnsResolveInflight = make(chan struct{}, maxDNSResolveInflight)

var errNoDNSTarget = errors.New("set carries no usable DNS answer source")

type dnsVerdict int

const (
	dnsVerdictIgnore dnsVerdict = iota
	dnsVerdictGood
	dnsVerdictFailed
)

func classifyDNSAnswer(resp []byte) dnsVerdict {
	if len(resp) == 0 {
		return dnsVerdictFailed
	}
	qtype, ok := dns.QuestionType(resp)
	if !ok || (qtype != dnsTypeA && qtype != dnsTypeAAAA) {
		return dnsVerdictIgnore
	}
	rcode, ok := dns.ResponseRcode(resp)
	if !ok {
		return dnsVerdictIgnore
	}
	switch rcode {
	case dns.RcodeNXDomain, dns.RcodeServFail, dns.RcodeRefused:
		return dnsVerdictFailed
	case dns.RcodeNoError:
		if qtype == dnsTypeA && len(dns.ParseResponseIPs(resp)) == 0 {
			return dnsVerdictFailed
		}
		return dnsVerdictGood
	default:
		return dnsVerdictFailed
	}
}

func dnsAnswerFailed(resp []byte) bool {
	return classifyDNSAnswer(resp) == dnsVerdictFailed
}

func dnsFailureKey(domain string, qtype uint16) string {
	if qtype == dnsTypeAAAA {
		return domain + "|aaaa"
	}
	return domain + "|a"
}

func dnsQueryType(query, resp []byte) uint16 {
	if qtype, ok := dns.QuestionType(query); ok {
		return qtype
	}
	qtype, _ := dns.QuestionType(resp)
	return qtype
}

func (w *Worker) noteDNSOutcome(cfg *config.Config, set *config.SetConfig, domain, srcMac string, qtype uint16, resp []byte) *config.SetConfig {
	if w == nil || w.destState == nil || cfg == nil || set == nil || domain == "" || !set.Escalate.Active() {
		return nil
	}
	if qtype != dnsTypeA && qtype != dnsTypeAAAA {
		return nil
	}
	key := dnsFailureKey(domain, qtype)
	switch classifyDNSAnswer(resp) {
	case dnsVerdictIgnore:
		return nil
	case dnsVerdictGood:
		w.destState.ClearDNSFailures(key)
		return nil
	}
	if !w.destState.RecordDNSFailure(key, set.Escalate.ResolvedDNSThreshold(), DNSFailWindow) {
		return nil
	}
	return w.tryEscalate(cfg, set, domain, srcMac, nil, escalateReasonDNS)
}

func (w *Worker) escalateAfterDNS(ipVersion byte, cfg *config.Config, set *config.SetConfig, domain string, query, resp []byte, clientIP net.IP, clientPort uint16, originalDst net.IP) bool {
	srcMac := w.getMacByIp(clientIP.String())
	next := w.noteDNSOutcome(cfg, set, domain, srcMac, dnsQueryType(query, resp), resp)
	if next == nil {
		return false
	}
	if !w.answerViaSetInline(ipVersion, cfg, next, domain, query, clientIP, clientPort, originalDst) {
		return false
	}
	logDNSEvent("UDP", next, domain, clientIP, originalDst, clientPort, w.getMacByIp(clientIP.String()), dnsActionEscalatePrefix+next.Name)
	return true
}

func (w *Worker) dnsAnswerSource(set *config.SetConfig) (net.IP, bool) {
	useDoH := set.DNS.DoHURL != ""
	if !(set.DNS.Enabled && (set.DNS.TargetDNS != "" || useDoH)) {
		return nil, false
	}
	if useDoH {
		return nil, true
	}
	targetIP := net.ParseIP(set.DNS.TargetDNS)
	return targetIP, targetIP != nil
}

func (w *Worker) answerViaSetInline(ipVersion byte, cfg *config.Config, set *config.SetConfig, domain string, query []byte, clientIP net.IP, clientPort uint16, originalDst net.IP) bool {
	if w == nil || cfg == nil || set == nil || len(query) == 0 {
		return false
	}

	if pinned := w.pinnedAnswer(set, query, domain); pinned != nil {
		w.applyPinnedAnswer(cfg, set, clientIP, domain, pinned)
		w.sendDNSResponseToClient(ipVersion, originalDst, clientIP, clientPort, pinned)
		return true
	}

	targetIP, ok := w.dnsAnswerSource(set)
	if !ok {
		return false
	}

	delay := config.ResolveSeg2Delay(set.UDP.Seg2Delay, set.UDP.Seg2DelayMax)
	w.resolveDNSRedirect(ipVersion, set, cfg, query, clientIP, clientPort, originalDst, targetIP, delay)
	return true
}

func (w *Worker) answerViaSet(ipVersion byte, cfg *config.Config, set *config.SetConfig, domain string, query []byte, clientIP net.IP, clientPort uint16, originalDst net.IP) bool {
	if w == nil || cfg == nil || set == nil || len(query) == 0 {
		return false
	}

	if pinned := w.pinnedAnswer(set, query, domain); pinned != nil {
		w.applyPinnedAnswer(cfg, set, clientIP, domain, pinned)
		w.sendDNSResponseToClient(ipVersion, originalDst, clientIP, clientPort, pinned)
		return true
	}

	targetIP, ok := w.dnsAnswerSource(set)
	if !ok {
		return false
	}

	select {
	case dnsResolveInflight <- struct{}{}:
	default:
		return false
	}

	delay := config.ResolveSeg2Delay(set.UDP.Seg2Delay, set.UDP.Seg2DelayMax)
	w.wg.Add(1)
	go func() {
		defer func() {
			<-dnsResolveInflight
			w.wg.Done()
		}()
		w.resolveDNSRedirect(ipVersion, set, cfg, query, clientIP, clientPort, originalDst, targetIP, delay)
	}()
	return true
}

func (w *Worker) dnsClientAddressable(pkt *pktInfo, sport, dport uint16) bool {
	if w.srcResolver == nil || pkt.ver != IPv4 {
		return true
	}
	return w.srcResolver.addressable(pkt.proto, pkt.src, sport, pkt.dst, dport)
}

func dnsUpstreamLabel(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil && u.Host != "" {
		return u.Host
	}
	return rawURL
}

func dnsRedirectAction(set *config.SetConfig) string {
	if set.DNS.DoHURL != "" {
		return dnsActionDoHPrefix + dnsUpstreamLabel(set.DNS.DoHURL)
	}
	return dnsActionForwardPrefix + set.DNS.TargetDNS
}

func logDNSEvent(proto string, set *config.SetConfig, domain string, clientIP, serverIP net.IP, clientPort uint16, srcMac, action string) {
	if log.Level(log.CurLevel.Load()) < log.LevelInfo {
		return
	}
	if clientIP == nil || serverIP == nil {
		return
	}
	setName := ""
	if set != nil {
		setName = set.Name
	}
	log.LogConnection(proto, setName, dns.SafeName(domain), clientIP.String(), clientPort, "", serverIP.String(), 53, srcMac, "", action)
}

func (w *Worker) processDnsPacket(vc *verdictCtx, pkt *pktInfo, sport uint16, dport uint16, payload []byte) int {
	ipVersion := pkt.ver
	srcMac := pkt.srcMac

	if dport == 53 {
		domain, ok := dns.ParseQueryDomain(payload)
		txid, txidOK := dns.ParseTransactionID(payload)
		if ok {
			domain = strings.ToLower(domain)
			matcher := w.getMatcher()
			if matchedSet, set := matcher.MatchSNIWithSource(domain, srcMac); matchedSet {
				cfg := w.getConfig()
				log.Tracef("DNS query: %s matched set %s (src %s)", dns.SafeName(domain), set.Name, srcMac)

				if escSet := w.escalatedSetFor(cfg, domain, srcMac); escSet != nil && escSet != set {
					log.Tracef("DNS escalation hit for %s: %s -> %s", dns.SafeName(domain), set.Name, escSet.Name)
					set = escSet
				}

				clientIP := append(net.IP(nil), pkt.src...)
				originalDst := append(net.IP(nil), pkt.dst...)

				if set.Routing.Enabled && config.RoutingIsBlock(set.Routing.Mode) && !cfg.Queue.IsDiscovery &&
					config.NormalizeBlockAction(set.Routing.BlockAction) == config.BlockActionDrop {
					logDNSEvent("UDP", set, domain, clientIP, originalDst, sport, srcMac, dnsActionBlock)
					metrics.GetMetricsCollector().RecordBlock(domain, srcMac)
					vc.drop()
					return 0
				}

				if !w.dnsClientAddressable(pkt, sport, dport) {
					logDNSEvent("UDP", set, domain, clientIP, originalDst, sport, srcMac, dnsActionNoClient)
					return vc.accept()
				}

				if set.Routing.Enabled && config.RoutingIsBlock(set.Routing.Mode) && !cfg.Queue.IsDiscovery {
					ipv6Disabled := ipVersion == IPv6 && !cfg.Queue.IPv6Enabled
					if !ipv6Disabled {
						if resp := dns.BuildBlockResponse(payload); resp != nil {
							w.sendDNSResponseToClient(ipVersion, originalDst, clientIP, sport, resp)
							log.Tracef("DNS sinkhole: %s -> NXDOMAIN for %s (set: %s)", dns.SafeName(domain), clientIP, set.Name)
							logDNSEvent("UDP", set, domain, clientIP, originalDst, sport, srcMac, dnsActionSinkhole)
							metrics.GetMetricsCollector().RecordBlock(domain, srcMac)
							vc.drop()
							return 0
						}
					}
				}

				if pinned := w.pinnedAnswer(set, payload, domain); pinned != nil {
					if !vc.drop() {
						return 0
					}
					w.applyPinnedAnswer(cfg, set, clientIP, domain, pinned)
					logDNSEvent("UDP", set, domain, clientIP, originalDst, sport, srcMac, dnsActionPin)
					w.sendDNSResponseToClient(ipVersion, originalDst, clientIP, sport, pinned)
					return 0
				}

				if txidOK && set.Routing.Enabled && !set.Targets.DomainOnly && !cfg.Queue.IsDiscovery {
					storeDNSPendingRoute(
						dnsRouteKeyRequest(ipVersion, clientIP, sport, originalDst, dport, txid, domain),
						set.Id,
					)
				}

				useDoH := set.DNS.DoHURL != ""

				if !(set.DNS.Enabled && (set.DNS.TargetDNS != "" || useDoH)) {
					log.Tracef("DNS redirect: %s matched set %s but no redirect target configured, passing through", dns.SafeName(domain), set.Name)
					logDNSEvent("UDP", set, domain, clientIP, originalDst, sport, srcMac, dnsActionPassthrough)
					return vc.accept()
				}

				var targetIP net.IP
				if !useDoH {
					targetIP = net.ParseIP(set.DNS.TargetDNS)
					if targetIP == nil {
						logDNSEvent("UDP", set, domain, clientIP, originalDst, sport, srcMac, dnsActionBadTarget)
						return vc.accept()
					}
				}

				if ipVersion == IPv6 && !cfg.Queue.IPv6Enabled {
					logDNSEvent("UDP", set, domain, clientIP, originalDst, sport, srcMac, dnsActionIPv6Disabled)
					return vc.accept()
				}

				query := append([]byte(nil), payload...)
				ver := ipVersion
				clientPort := sport
				delay := config.ResolveSeg2Delay(set.UDP.Seg2Delay, set.UDP.Seg2DelayMax)

				target := set.DNS.TargetDNS
				if useDoH {
					target = set.DNS.DoHURL
				}
				log.Tracef("DNS redirect: intercepting %s -> %s (set %s)", dns.SafeName(domain), target, set.Name)
				logDNSEvent("UDP", set, domain, clientIP, originalDst, sport, srcMac, dnsRedirectAction(set))

				select {
				case dnsResolveInflight <- struct{}{}:
				default:
					logDNSEvent("UDP", set, domain, clientIP, originalDst, sport, srcMac, dnsActionOverload)
					return vc.accept()
				}

				vc.drop()

				w.wg.Add(1)
				go func(s *config.SetConfig, c *config.Config) {
					defer func() {
						<-dnsResolveInflight
						w.wg.Done()
					}()
					w.resolveDNSRedirect(ver, s, c, query, clientIP, clientPort, originalDst, targetIP, delay)
				}(set, cfg)
				return 0
			} else {
				log.Tracef("DNS query: %s matched no set (src %s), forwarding unchanged", dns.SafeName(domain), srcMac)
			}
		}
	}

	if sport == 53 {
		if txid, ok := dns.ParseTransactionID(payload); ok {
			domain, _ := dns.ParseQueryDomain(payload)
			if domain == "" {
				if d, ok := parseDNSName(payload, 12); ok {
					domain = d
				}
			}
			domain = strings.ToLower(domain)
			clientIP := pkt.dst
			dnsServerIP := pkt.src

			routed := false
			var healSet *config.SetConfig
			var failedSet *config.SetConfig
			clientMac := w.getMacByIp(clientIP.String())
			if domain != "" {
				if matched, set := w.getMatcher().MatchSNIWithSource(domain, clientMac); matched && set.Enabled {
					healSet = set
					failedSet = set
					if escSet := w.escalatedSetFor(w.getConfig(), domain, clientMac); escSet != nil {
						failedSet = escSet
					}
					ips := dns.ParseResponseIPs(payload)
					w.storeHostHints(clientIP, set, domain, ips)
					if set.Routing.Enabled && !set.Targets.DomainOnly && len(ips) > 0 {
						cfg := w.getConfig()
						if routingHandleDNSAvailable() && !cfg.Queue.IsDiscovery {
							routingHandleDNSAsync(cfg, set, ips)
							routed = true
						}
					}
				}
			}

			if setID, hit := consumeDNSPendingRoute(
				dnsRouteKeyResponse(ipVersion, clientIP, dport, dnsServerIP, sport, txid, domain),
			); hit && !routed {
				if ips := dns.ParseResponseIPs(payload); len(ips) > 0 {
					cfg := w.getConfig()
					if set := cfg.GetSetById(setID); set != nil {
						if healSet == nil {
							healSet = set
						}
						w.storeHostHints(clientIP, set, domain, ips)
						if !set.Targets.DomainOnly && routingHandleDNSAvailable() && !cfg.Queue.IsDiscovery {
							routingHandleDNSAsync(cfg, set, ips)
						}
					}
				}
			}

			if healed := w.healDNSResponse(w.getConfig(), healSet, domain, payload, false); healed != nil {
				if !vc.drop() {
					return 0
				}
				logDNSEvent("UDP", healSet, domain, clientIP, dnsServerIP, dport, w.getMacByIp(clientIP.String()), dnsActionHeal)
				w.sendDNSResponseToClient(ipVersion, dnsServerIP, clientIP, dport, healed)
				return 0
			}

			qtype, hasType := dns.QuestionType(payload)
			if next := w.noteDNSOutcome(w.getConfig(), failedSet, domain, clientMac, qtype, payload); next != nil {
				cfg := w.getConfig()
				if hasType {
					query := dns.BuildQuery(domain, txid, qtype)
					client := append(net.IP(nil), clientIP...)
					server := append(net.IP(nil), dnsServerIP...)
					if w.answerViaSet(ipVersion, cfg, next, domain, query, client, dport, server) {
						if !vc.drop() {
							return 0
						}
						logDNSEvent("UDP", next, domain, client, server, dport, w.getMacByIp(client.String()), dnsActionEscalatePrefix+next.Name)
						return 0
					}
				}
			}
		}
	}

	return vc.accept()
}

func (w *Worker) resolveDNSRedirect(ipVersion byte, set *config.SetConfig, cfg *config.Config, query []byte, clientIP net.IP, clientPort uint16, originalDst, targetIP net.IP, delay int) {
	queryDomain, _ := dns.ParseQueryDomain(query)
	queryDomain = strings.ToLower(queryDomain)

	var resp []byte
	var err error
	if set.DNS.DoHURL != "" {
		resp, err = w.resolveDoHRedirect(set.DNS.DoHURL, int(cfg.MainInjectedMark()), query)
		if err != nil {
			log.Tracef("DNS redirect: DoH %s failed: %v, answering SERVFAIL (fail-closed)", set.DNS.DoHURL, err)
			if w.escalateAfterDNS(ipVersion, cfg, set, queryDomain, query, nil, clientIP, clientPort, originalDst) {
				return
			}
			logDNSEvent("UDP", set, queryDomain, clientIP, originalDst, clientPort, w.getMacByIp(clientIP.String()), dnsActionServfail)
			w.sendDNSResponseToClient(ipVersion, originalDst, clientIP, clientPort, dns.BuildServfailResponse(query))
			return
		}
	} else {
		resp, err = dns.ResolveUpstream(query, targetIP, dns.ForwardOptions{
			Sender:       w.sock,
			Fragment:     set.DNS.FragmentQuery,
			Seg2Delay:    delay,
			ReverseOrder: set.Fragmentation.ReverseOrder,
			Mark:         int(cfg.MainInjectedMark()),
		})
		if err != nil {
			log.Tracef("DNS redirect: upstream %s failed: %v, answering SERVFAIL (fail-closed)", set.DNS.TargetDNS, err)
			if w.escalateAfterDNS(ipVersion, cfg, set, queryDomain, query, nil, clientIP, clientPort, originalDst) {
				return
			}
			logDNSEvent("UDP", set, queryDomain, clientIP, originalDst, clientPort, w.getMacByIp(clientIP.String()), dnsActionServfail)
			w.sendDNSResponseToClient(ipVersion, originalDst, clientIP, clientPort, dns.BuildServfailResponse(query))
			return
		}
	}

	if w.escalateAfterDNS(ipVersion, cfg, set, queryDomain, query, resp, clientIP, clientPort, originalDst) {
		return
	}

	if ips := dns.ParseResponseIPs(resp); len(ips) > 0 {
		w.storeHostHints(clientIP, set, queryDomain, ips)
		if set.Routing.Enabled && !set.Targets.DomainOnly && !cfg.Queue.IsDiscovery && RoutingHandleDNSFunc != nil {
			RoutingHandleDNSFunc(cfg, set, ips)
		}
	}

	if healed := w.healDNSResponse(cfg, set, queryDomain, resp, false); healed != nil {
		logDNSEvent("UDP", set, queryDomain, clientIP, originalDst, clientPort, w.getMacByIp(clientIP.String()), dnsActionHeal)
		resp = healed
	}

	w.sendDNSResponseToClient(ipVersion, originalDst, clientIP, clientPort, resp)

	upstream := set.DNS.TargetDNS
	if set.DNS.DoHURL != "" {
		upstream = set.DNS.DoHURL
	}
	log.Tracef("DNS redirect: %s -> %s answered for %s with %d IPs (set: %s)", originalDst, upstream, clientIP, len(dns.ParseResponseIPs(resp)), set.Name)
}

func (w *Worker) sendDNSResponseToClient(ipVersion byte, originalDst, clientIP net.IP, clientPort uint16, resp []byte) {
	if len(resp) == 0 {
		return
	}
	sender := w.clientSender()
	if sender == nil {
		return
	}
	if ipVersion == IPv4 {
		if pkt := sock.BuildUDPPacketV4(originalDst, clientIP, 53, clientPort, resp); pkt != nil {
			_ = sender.SendIPv4(pkt, clientIP)
		}
	} else {
		if pkt := sock.BuildUDPPacketV6(originalDst, clientIP, 53, clientPort, resp); pkt != nil {
			_ = sender.SendIPv6(pkt, clientIP)
		}
	}
}

func (w *Worker) resolveDoHRedirect(serverURL string, mark int, query []byte) ([]byte, error) {
	timeout := w.getConfig().DNSQueryTimeout()
	ctx, cancel := context.WithTimeout(w.ctx, timeout)
	defer cancel()
	return dns.ResolveDoH(ctx, getDoHClient(mark, timeout), serverURL, query)
}
