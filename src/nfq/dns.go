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
	"github.com/daniellavrushin/b4/sock"
	"github.com/daniellavrushin/b4/utils"
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
	dnsActionBlock            = "dns-block"
	dnsActionSinkhole         = "dns-sinkhole"
	dnsActionPassthrough      = "dns-passthrough"
	dnsActionBadTarget        = "dns-bad-target"
	dnsActionFromTarget       = "dns-from-target"
	dnsActionIPv6Disabled     = "dns-ipv6-disabled"
	dnsActionIPv6Stripped     = "dns-ipv6-stripped"
	dnsActionHealStripped     = "dns-heal+ipv6-stripped"
	dnsActionECHStripped      = "dns-ech-stripped"
	dnsActionServfail         = "dns-servfail"
	dnsActionFallbackCache    = "dns-fallback-cache"
	dnsActionFallbackUpstream = "dns-fallback-upstream"
	dnsActionHeal             = "dns-heal"
	dnsActionPin              = "dns-pin"
	dnsActionPinEmpty         = "dns-pin-empty"
	dnsActionNoClient         = "dns-no-client"
	dnsActionOverload         = "dns-overload"
	dnsActionDoHPrefix        = "dns-doh->"
	dnsActionForwardPrefix    = "dns-forward->"
	dnsActionEscalatePrefix   = "dns-escalate->"
)

const maxDNSResolveInflight = 64

var dnsResolveInflight = make(chan struct{}, maxDNSResolveInflight)

var (
	errNoDNSTarget     = errors.New("set carries no usable DNS answer source")
	errQueryFromTarget = errors.New("the query comes from the set's own resolver")
)

var errDNSSourceCoolingDown = errors.New("the redirect target is still unreachable")

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

func (w *Worker) noteDNSOutcome(cfg *config.Config, set *config.SetConfig, domain, srcMac string, qtype uint16, resp []byte, from ...*ingressScope) *config.SetConfig {
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
	return w.tryEscalate(cfg, set, domain, srcMac, nil, escalateReasonDNS, 0, 0, from...)
}

func (w *Worker) escalateAfterDNS(ipVersion byte, cfg *config.Config, set *config.SetConfig, domain string, query, resp []byte, clientIP net.IP, clientPort uint16, originalDst net.IP, direct bool, scope *ingressScope) bool {
	srcMac := w.getMacByIp(clientIP.String())
	next := w.noteDNSOutcome(cfg, set, domain, srcMac, dnsQueryType(query, resp), resp, scope)
	if next == nil {
		return false
	}
	if !w.answerViaSetInline(ipVersion, cfg, next, domain, query, clientIP, clientPort, originalDst, direct, scope) {
		return false
	}
	logDNSEvent("UDP", next, domain, clientIP, originalDst, clientPort, w.getMacByIp(clientIP.String()), dnsActionEscalatePrefix+next.Name)
	return true
}

func (w *Worker) dnsAnswerSource(set *config.SetConfig, client net.IP) (net.IP, bool) {
	useDoH := set.DNS.DoHURL != ""
	if !(set.DNS.Enabled && (set.DNS.TargetDNS != "" || useDoH)) {
		return nil, false
	}
	if queryFromTarget(set, client) {
		return nil, false
	}
	if useDoH {
		return nil, true
	}
	targetIP := net.ParseIP(set.DNS.TargetDNS)
	return targetIP, targetIP != nil
}

var hostAddress = utils.IsHostIP

func redirectTarget(set *config.SetConfig) net.IP {
	if set.DNS.DoHURL != "" {
		u, err := url.Parse(set.DNS.DoHURL)
		if err != nil {
			return nil
		}
		return net.ParseIP(u.Hostname())
	}
	return net.ParseIP(set.DNS.TargetDNS)
}

func queryFromTarget(set *config.SetConfig, client net.IP) bool {
	target := redirectTarget(set)
	return target != nil && client != nil && target.Equal(client) && !hostAddress(target)
}

var (
	dnsRedirectsMu sync.Mutex
	dnsRedirects   = map[string]int{}
)

func beginDNSRedirect(query []byte) func() {
	domain, _ := dns.ParseQueryDomain(query)
	key := dnsFailureKey(strings.ToLower(domain), dnsQueryType(query, nil))
	dnsRedirectsMu.Lock()
	dnsRedirects[key]++
	dnsRedirectsMu.Unlock()
	return func() {
		dnsRedirectsMu.Lock()
		dnsRedirects[key]--
		if dnsRedirects[key] <= 0 {
			delete(dnsRedirects, key)
		}
		dnsRedirectsMu.Unlock()
	}
}

func dnsRedirectInFlight(domain string, qtype uint16) bool {
	dnsRedirectsMu.Lock()
	defer dnsRedirectsMu.Unlock()
	return dnsRedirects[dnsFailureKey(domain, qtype)] > 0
}

func (w *Worker) answerViaSetInline(ipVersion byte, cfg *config.Config, set *config.SetConfig, domain string, query []byte, clientIP net.IP, clientPort uint16, originalDst net.IP, direct bool, from ...*ingressScope) bool {
	if w == nil || cfg == nil || set == nil || len(query) == 0 {
		return false
	}
	scope := scopeFrom(from)

	if pinned := w.pinnedAnswer(set, query, domain); pinned != nil {
		_, routeWaits := w.applyPinnedAnswerAwait(cfg, set, clientIP, domain, pinned, scope.learns(domain))
		w.waitRoutesInline(routeWaits, nil)
		w.answerClient(direct, query, ipVersion, originalDst, clientIP, clientPort, pinned)
		return true
	}

	targetIP, ok := w.dnsAnswerSource(set, clientIP)
	if !ok {
		return false
	}

	delay := config.ResolveSeg2Delay(set.UDP.Seg2Delay, set.UDP.Seg2DelayMax)
	w.resolveDNSRedirect(ipVersion, set, cfg, query, clientIP, clientPort, originalDst, targetIP, delay, direct, scope)
	return true
}

func (w *Worker) answerViaSet(vc *verdictCtx, ipVersion byte, cfg *config.Config, set *config.SetConfig, domain string, query []byte, clientIP net.IP, clientPort uint16, originalDst net.IP, from ...*ingressScope) bool {
	if w == nil || cfg == nil || set == nil || len(query) == 0 {
		return false
	}
	scope := scopeFrom(from)

	if pinned := w.pinnedAnswer(set, query, domain); pinned != nil {
		_, routeWaits := w.applyPinnedAnswerAwait(cfg, set, clientIP, domain, pinned, scope.learns(domain))
		send := func() { w.answerClient(true, query, ipVersion, originalDst, clientIP, clientPort, pinned) }
		if !w.holdForRoutes(vc, routeWaits, domain, send) {
			send()
		}
		return true
	}

	targetIP, ok := w.dnsAnswerSource(set, clientIP)
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
		w.resolveDNSRedirect(ipVersion, set, cfg, query, clientIP, clientPort, originalDst, targetIP, delay, true, scope)
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
	return dns.SourceLabel(rawURL)
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
			if txidOK && DNSNames.Wanted() {
				noteDNSNameQuery(dnsRouteKeyRequest(ipVersion, pkt.src, sport, pkt.dst, dport, txid, domain))
			}
			matcher := w.getMatcher()
			keep := w.blockScope(vc, pkt, matcher)
			if matchedSet, set := matcher.MatchSNIWithSource(domain, srcMac, keep); matchedSet {
				cfg := w.getConfig()
				log.Tracef("DNS query: %s matched set %s (src %s)", dns.SafeName(domain), set.Name, srcMac)

				if escSet := w.escalatedSetFor(cfg, set, domain, srcMac, keep); escSet != nil && escSet != set {
					log.Tracef("DNS escalation hit for %s: %s -> %s", dns.SafeName(domain), set.Name, escSet.Name)
					set = escSet
				}

				clientIP := append(net.IP(nil), pkt.src...)
				originalDst := append(net.IP(nil), pkt.dst...)

				if set.Routing.Enabled && config.RoutingIsBlock(set.Routing.Mode) && !cfg.Queue.IsDiscovery &&
					config.NormalizeBlockAction(set.Routing.BlockAction) == config.BlockActionDrop {
					logDNSEvent("UDP", set, domain, clientIP, originalDst, sport, srcMac, dnsActionBlock)
					recordBlockedDNS(cfg, set, domain, srcMac)
					vc.dropUnanswered()
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
							recordBlockedDNS(cfg, set, domain, srcMac)
							vc.drop()
							return 0
						}
					}
				}

				if pinned := w.pinnedAnswer(set, payload, domain); pinned != nil {
					if !vc.drop() {
						return 0
					}
					action, routeWaits := w.applyPinnedAnswerAwait(cfg, set, clientIP, domain, pinned, vc.scope.learns(domain))
					logDNSEvent("UDP", set, domain, clientIP, originalDst, sport, srcMac, action)
					send := func() { w.sendDNSResponseToClient(ipVersion, originalDst, clientIP, sport, pinned) }
					if !w.holdForRoutes(vc, routeWaits, domain, send) {
						send()
					}
					return 0
				}

				if txidOK && vc.scope.learns(domain) && set.Routing.Enabled && !set.Targets.DomainOnly && !cfg.Queue.IsDiscovery {
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

				if queryFromTarget(set, clientIP) {
					log.Tracef("DNS redirect: %s comes from %s, the set's own resolver passing a lookup upstream, forwarding it unchanged (set %s)", dns.SafeName(domain), clientIP, set.Name)
					logDNSEvent("UDP", set, domain, clientIP, originalDst, sport, srcMac, dnsActionFromTarget)
					return vc.accept()
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
				go func(s *config.SetConfig, c *config.Config, scope *ingressScope) {
					defer func() {
						<-dnsResolveInflight
						w.wg.Done()
					}()
					w.resolveDNSRedirect(ver, s, c, query, clientIP, clientPort, originalDst, targetIP, delay, false, scope)
				}(set, cfg, vc.scope.orSnapshot(matcher))
				return 0
			} else {
				log.Tracef("DNS query: %s matched no set (src %s), forwarding unchanged", dns.SafeName(domain), srcMac)
			}
		}
	}

	if vc.copy != nil {
		return vc.accept()
	}

	if sport == 53 {
		if txid, ok := dns.ParseTransactionID(payload); ok {
			domain := replyDomain(payload)
			clientIP := pkt.dst
			dnsServerIP := pkt.src
			if w.dnsCopyGate(vc, pkt, dport, txid, domain, payload) {
				return 0
			}
			if vc.ifaceFiltered {
				return vc.accept()
			}

			routed := false
			var routeWaits []<-chan struct{}
			var healSet *config.SetConfig
			var failedSet *config.SetConfig
			clientMac := w.getMacByIp(clientIP.String())
			matcher := w.getMatcher()
			scope := clientScopeIn(matcher, clientIP, clientMac)
			keep := scope.filter()
			answersQuery := domain != "" && DNSNames.Wanted() &&
				consumeDNSNameQuery(dnsRouteKeyResponse(ipVersion, clientIP, dport, dnsServerIP, sport, txid, domain))
			if domain != "" {
				if matched, set := matcher.MatchSNIWithSource(domain, clientMac, keep); matched && set.Enabled {
					healSet = set
					failedSet = set
					if escSet := w.escalatedSetFor(w.getConfig(), set, domain, clientMac, keep); escSet != nil {
						failedSet = escSet
					}
					ips := dns.ParseResponseIPs(payload)
					if answersQuery {
						observeDNSNames(clientIP, domain, ips)
					}
					w.storeHostHints(clientIP, set, domain, ips)
					if scope.learns(domain) {
						routeWaits, routed = learnAnswerAwait(w.getConfig(), set, ips)
					}
				} else if answersQuery {
					observeDNSNames(clientIP, domain, dns.ParseResponseIPs(payload))
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
						if scope.learns(domain) && !set.Targets.DomainOnly && !config.RoutingIsBlock(set.Routing.Mode) && routingHandleDNSAvailable() && !cfg.Queue.IsDiscovery {
							routeWaits = joinWaits(routeWaits, routingHandleDNSAwait(cfg, set, ips))
						}
					}
				}
			}

			qtype, hasType := dns.QuestionType(payload)
			var next *config.SetConfig
			if failedSet == nil || !failedSet.Escalate.Active() || !dnsRedirectInFlight(domain, qtype) {
				next = w.noteDNSOutcome(w.getConfig(), failedSet, domain, clientMac, qtype, payload, scope)
			}
			if next != nil {
				cfg := w.getConfig()
				if hasType {
					query := dns.BuildQuery(domain, txid, qtype)
					client := append(net.IP(nil), clientIP...)
					server := append(net.IP(nil), dnsServerIP...)
					if w.answerViaSet(vc, ipVersion, cfg, next, domain, query, client, dport, server, scope) {
						if !vc.drop() {
							return 0
						}
						logDNSEvent("UDP", next, domain, client, server, dport, w.getMacByIp(client.String()), dnsActionEscalatePrefix+next.Name)
						return 0
					}
				}
			}

			if filtered, action, actor := w.filterDNSAnswer(w.getConfig(), healSet, domain, clientMac, payload, false, scope); filtered != nil {
				if added := addedAnswerIPs(payload, filtered); len(added) > 0 {
					if answersQuery {
						observeDNSNames(clientIP, domain, added)
					}
					w.storeHostHints(clientIP, healSet, domain, added)
					if scope.learns(domain) {
						waits, _ := learnAnswerAwait(w.getConfig(), healSet, added)
						routeWaits = joinWaits(routeWaits, waits)
					}
				}
				raw := inPlaceReply(vc, pkt, filtered)
				if raw == nil && !vc.drop() {
					return 0
				}
				logDNSEvent("UDP", actor, domain, clientIP, dnsServerIP, dport, clientMac, action)
				client := append(net.IP(nil), clientIP...)
				server := append(net.IP(nil), dnsServerIP...)
				release := func() { w.releaseFilteredReply(vc, raw, ipVersion, server, client, dport, filtered) }
				if !w.holdForRoutes(vc, routeWaits, domain, release) {
					release()
				}
				return 0
			}

			if w.holdForRoutes(vc, routeWaits, domain, func() { vc.accept() }) {
				return 0
			}
		}
	}

	return vc.accept()
}

func inPlaceReply(vc *verdictCtx, pkt *pktInfo, resp []byte) []byte {
	if _, ok := vc.q.(verdictModifier); !ok {
		return nil
	}
	return sock.ReplaceUDPPayload(pkt.raw, pkt.ihl, resp)
}

func (w *Worker) releaseFilteredReply(vc *verdictCtx, raw []byte, ver byte, server, client net.IP, dport uint16, resp []byte) {
	if raw != nil {
		if vc.replace(raw) {
			return
		}
		if !vc.drop() {
			return
		}
	}
	w.sendDNSPacket(ver, server, client, dport, resp)
}

func (w *Worker) resolveDNSRedirect(ipVersion byte, set *config.SetConfig, cfg *config.Config, query []byte, clientIP net.IP, clientPort uint16, originalDst, targetIP net.IP, delay int, direct bool, from ...*ingressScope) {
	defer beginDNSRedirect(query)()
	scope := scopeFrom(from)
	queryDomain, _ := dns.ParseQueryDomain(query)
	queryDomain = strings.ToLower(queryDomain)

	var resp []byte
	var err error
	source := set.DNS.TargetDNS
	if set.DNS.DoHURL != "" {
		source = set.DNS.DoHURL
	}

	switch {
	case !set.DNS.Strict && dnsSourceUnreachable(source):
		err = errDNSSourceCoolingDown
	case set.DNS.DoHURL != "":
		resp, err = w.resolveDoHRedirect(set.DNS.DoHURL, int(cfg.MainInjectedMark()), query)
	default:
		resp, err = dns.ResolveUpstream(query, targetIP, dns.ForwardOptions{
			Sender:       w.lookupSender(),
			Fragment:     set.DNS.FragmentQuery,
			Seg2Delay:    delay,
			ReverseOrder: set.Fragmentation.ReverseOrder,
			Mark:         int(cfg.MainInjectedMark()),
			Timeout:      cfg.DNSQueryTimeout(),
		})
	}

	servedFallback := false
	if err != nil {
		if !errors.Is(err, errDNSSourceCoolingDown) {
			noteDNSSourceFailure(source)
		}
		fallback, action := w.dnsRedirectFallback(cfg, set, queryDomain, query, originalDst, targetIP, delay)
		if fallback == nil {
			log.Warnf("DNS redirect: %s could not answer %s (%v) and nothing else could either, the client gets SERVFAIL (set: %s)",
				dnsUpstreamLabel(source), dns.SafeName(queryDomain), err, set.Name)
			if w.escalateAfterDNS(ipVersion, cfg, set, queryDomain, query, nil, clientIP, clientPort, originalDst, direct, scope) {
				return
			}
			logDNSEvent("UDP", set, queryDomain, clientIP, originalDst, clientPort, w.getMacByIp(clientIP.String()), dnsActionServfail)
			w.answerClient(direct, query, ipVersion, originalDst, clientIP, clientPort, dns.BuildServfailResponse(query))
			return
		}
		log.Warnf("DNS redirect: %s could not answer %s (%v), falling back to %s (set: %s)",
			dnsUpstreamLabel(source), dns.SafeName(queryDomain), err, action, set.Name)
		logDNSEvent("UDP", set, queryDomain, clientIP, originalDst, clientPort, w.getMacByIp(clientIP.String()), action)
		resp = fallback
		servedFallback = true
	} else {
		noteDNSSourceSuccess(source)
	}

	if !servedFallback && w.escalateAfterDNS(ipVersion, cfg, set, queryDomain, query, resp, clientIP, clientPort, originalDst, direct, scope) {
		return
	}

	w.learnAnswerAddresses(cfg, set, clientIP, queryDomain, dns.ParseResponseIPs(resp), scope.learns(queryDomain), nil)

	clientMac := w.getMacByIp(clientIP.String())
	if filtered, action, actor := w.filterDNSAnswer(cfg, set, queryDomain, clientMac, resp, false, scope); filtered != nil {
		logDNSEvent("UDP", actor, queryDomain, clientIP, originalDst, clientPort, clientMac, action)
		w.learnAnswerAddresses(cfg, set, clientIP, queryDomain, addedAnswerIPs(resp, filtered), scope.learns(queryDomain), nil)
		resp = filtered
	}

	if err == nil && classifyDNSAnswer(resp) == dnsVerdictGood {
		rememberDNSAnswer(queryDomain, query, resp)
	}

	w.answerClient(direct, query, ipVersion, originalDst, clientIP, clientPort, resp)

	upstream := set.DNS.TargetDNS
	if set.DNS.DoHURL != "" {
		upstream = set.DNS.DoHURL
	}
	log.Tracef("DNS redirect: %s -> %s answered for %s with %d IPs (set: %s)", originalDst, upstream, clientIP, len(dns.ParseResponseIPs(resp)), set.Name)
}

var dnsReplyObserver func(resp []byte)

type dnsAnswerSender interface {
	SendIPv4(packet []byte, destIP net.IP) error
	SendIPv6(packet []byte, destIP net.IP) error
}

var (
	dnsAnswerSendMu    sync.Mutex
	dnsAnswerSenderFor = func(w *Worker) dnsAnswerSender {
		if s := w.clientSender(); s != nil {
			return s
		}
		return nil
	}
)

func (w *Worker) answerClient(direct bool, query []byte, ipVersion byte, originalDst, clientIP net.IP, clientPort uint16, resp []byte) {
	if !direct {
		w.sendDNSAnswerFor(query, ipVersion, originalDst, clientIP, clientPort, resp)
		return
	}
	if len(resp) > 0 {
		w.sendDNSPacket(ipVersion, originalDst, clientIP, clientPort, resp)
	}
}

func (w *Worker) sendDNSResponseToClient(ipVersion byte, originalDst, clientIP net.IP, clientPort uint16, resp []byte) {
	w.sendDNSAnswerFor(nil, ipVersion, originalDst, clientIP, clientPort, resp)
}

func (w *Worker) sendDNSAnswerFor(query []byte, ipVersion byte, originalDst, clientIP net.IP, clientPort uint16, resp []byte) {
	if len(resp) == 0 {
		return
	}
	if w.dnsCopy != nil && w.dnsCopy.deliver(ipVersion, originalDst, clientIP, clientPort, query, resp) {
		return
	}
	w.sendDNSPacket(ipVersion, originalDst, clientIP, clientPort, resp)
}

func (w *Worker) sendDNSPacket(ipVersion byte, originalDst, clientIP net.IP, clientPort uint16, resp []byte) {
	if dnsReplyObserver != nil {
		dnsReplyObserver(resp)
	}
	sender := dnsAnswerSenderFor(w)
	if sender == nil {
		return
	}
	var pkt []byte
	if ipVersion == IPv4 {
		pkt = sock.BuildUDPPacketV4(originalDst, clientIP, 53, clientPort, resp)
	} else {
		pkt = sock.BuildUDPPacketV6(originalDst, clientIP, 53, clientPort, resp)
	}
	if pkt == nil {
		return
	}
	dnsAnswerSendMu.Lock()
	defer dnsAnswerSendMu.Unlock()
	if ipVersion == IPv4 {
		_ = sender.SendIPv4(pkt, clientIP)
	} else {
		_ = sender.SendIPv6(pkt, clientIP)
	}
}

func (w *Worker) resolveDoHRedirect(serverURL string, mark int, query []byte) ([]byte, error) {
	timeout := w.getConfig().DNSQueryTimeout()
	ctx, cancel := context.WithTimeout(w.ctx, timeout)
	defer cancel()
	return dns.ResolveDoH(ctx, getDoHClient(mark, timeout), serverURL, query)
}
