package nfq

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/metrics"
	"github.com/daniellavrushin/b4/sock"
)

const dohRedirectTimeout = 5 * time.Second

var (
	dohClientMu   sync.Mutex
	dohClientMark int
	dohClient     *http.Client
)

func getDoHClient(mark int) *http.Client {
	dohClientMu.Lock()
	defer dohClientMu.Unlock()
	if dohClient == nil || dohClientMark != mark {
		if dohClient != nil {
			dohClient.CloseIdleConnections()
		}
		dohClient = dns.MarkedDoHClient(mark, dohRedirectTimeout)
		dohClientMark = mark
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

func (w *Worker) processDnsPacket(vc *verdictCtx, ipVersion byte, sport uint16, dport uint16, payload []byte, raw []byte, srcMac string) int {

	if dport == 53 {
		domain, ok := dns.ParseQueryDomain(payload)
		txid, txidOK := dns.ParseTransactionID(payload)
		if ok {
			domain = strings.ToLower(domain)
			matcher := w.getMatcher()
			if matchedSet, set := matcher.MatchSNIWithSource(domain, srcMac); matchedSet {
				cfg := w.getConfig()
				log.Tracef("DNS query: %s matched set %s (src %s)", domain, set.Name, srcMac)

				if set.Routing.Enabled && config.RoutingIsBlock(set.Routing.Mode) && !cfg.Queue.IsDiscovery {
					if config.NormalizeBlockAction(set.Routing.BlockAction) == config.BlockActionDrop {
						metrics.GetMetricsCollector().RecordBlock(domain, srcMac)
						vc.drop()
						return 0
					}
					ipv6Disabled := ipVersion == IPv6 && !cfg.Queue.IPv6Enabled
					if !ipv6Disabled {
						if resp := dns.BuildBlockResponse(payload); resp != nil {
							var clientIP, originalDst net.IP
							switch ipVersion {
							case IPv4:
								clientIP = append(net.IP(nil), raw[12:16]...)
								originalDst = append(net.IP(nil), raw[16:20]...)
							default:
								clientIP = append(net.IP(nil), raw[8:24]...)
								originalDst = append(net.IP(nil), raw[24:40]...)
							}
							if ipVersion == IPv4 {
								if pkt := sock.BuildUDPPacketV4(originalDst, clientIP, 53, sport, resp); pkt != nil {
									_ = w.clientSender().SendIPv4(pkt, clientIP)
								}
							} else if pkt := sock.BuildUDPPacketV6(originalDst, clientIP, 53, sport, resp); pkt != nil {
								_ = w.clientSender().SendIPv6(pkt, clientIP)
							}
							log.Tracef("DNS sinkhole: %s -> NXDOMAIN for %s (set: %s)", domain, clientIP, set.Name)
							metrics.GetMetricsCollector().RecordBlock(domain, srcMac)
							vc.drop()
							return 0
						}
					}
				}

				if txidOK && set.Routing.Enabled && !set.Targets.DomainOnly && !cfg.Queue.IsDiscovery {
					var clientIP, dnsServerIP net.IP
					switch ipVersion {
					case IPv4:
						clientIP = net.IP(raw[12:16])
						dnsServerIP = net.IP(raw[16:20])
					case IPv6:
						clientIP = net.IP(raw[8:24])
						dnsServerIP = net.IP(raw[24:40])
					}
					if clientIP != nil {
						storeDNSPendingRoute(
							dnsRouteKeyRequest(ipVersion, clientIP, sport, dnsServerIP, dport, txid, domain),
							set.Id,
						)
					}
				}

				useDoH := set.DNS.DoHURL != ""

				if !(set.DNS.Enabled && (set.DNS.TargetDNS != "" || useDoH)) {
					log.Tracef("DNS redirect: %s matched set %s but no redirect target configured, passing through", domain, set.Name)
					return vc.accept()
				}

				var targetIP net.IP
				if !useDoH {
					targetIP = net.ParseIP(set.DNS.TargetDNS)
					if targetIP == nil {
						return vc.accept()
					}
				}

				if ipVersion == IPv6 && !cfg.Queue.IPv6Enabled {
					return vc.accept()
				}

				var clientIP, originalDst net.IP
				switch ipVersion {
				case IPv4:
					clientIP = append(net.IP(nil), raw[12:16]...)
					originalDst = append(net.IP(nil), raw[16:20]...)
				default:
					clientIP = append(net.IP(nil), raw[8:24]...)
					originalDst = append(net.IP(nil), raw[24:40]...)
				}

				query := append([]byte(nil), payload...)
				ver := ipVersion
				clientPort := sport
				delay := config.ResolveSeg2Delay(set.UDP.Seg2Delay, set.UDP.Seg2DelayMax)

				target := set.DNS.TargetDNS
				if useDoH {
					target = set.DNS.DoHURL
				}
				log.Tracef("DNS redirect: intercepting %s -> %s (set %s)", domain, target, set.Name)

				vc.drop()

				w.wg.Add(1)
				go func(s *config.SetConfig, c *config.Config) {
					defer w.wg.Done()
					w.resolveDNSRedirect(ver, s, c, query, clientIP, clientPort, originalDst, targetIP, delay)
				}(set, cfg)
				return 0
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
			var clientIP net.IP
			var dnsServerIP net.IP
			if ipVersion == IPv4 {
				clientIP = net.IP(raw[16:20])
				dnsServerIP = net.IP(raw[12:16])
			} else {
				clientIP = net.IP(raw[24:40])
				dnsServerIP = net.IP(raw[8:24])
			}

			routed := false
			if domain != "" {
				clientMac := w.getMacByIp(clientIP.String())
				if matched, set := w.getMatcher().MatchSNIWithSource(domain, clientMac); matched && set.Enabled {
					ips := dns.ParseResponseIPs(payload)
					if set.Routing.Enabled && !set.Targets.DomainOnly && len(ips) > 0 {
						cfg := w.getConfig()
						if RoutingHandleDNSFunc != nil && !cfg.Queue.IsDiscovery {
							RoutingHandleDNSFunc(cfg, set, ips)
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
					if set := cfg.GetSetById(setID); set != nil && !set.Targets.DomainOnly {
						if RoutingHandleDNSFunc != nil && !cfg.Queue.IsDiscovery {
							RoutingHandleDNSFunc(cfg, set, ips)
						}
					}
				}
			}
		}
	}

	return vc.accept()
}

func (w *Worker) resolveDNSRedirect(ipVersion byte, set *config.SetConfig, cfg *config.Config, query []byte, clientIP net.IP, clientPort uint16, originalDst, targetIP net.IP, delay int) {
	var resp []byte
	var err error
	if set.DNS.DoHURL != "" {
		resp, err = w.resolveDoHRedirect(set.DNS.DoHURL, int(cfg.Queue.Mark), query)
		if err != nil {
			log.Tracef("DNS redirect: DoH %s failed: %v, answering SERVFAIL (fail-closed)", set.DNS.DoHURL, err)
			w.sendDNSResponseToClient(ipVersion, originalDst, clientIP, clientPort, dns.BuildServfailResponse(query))
			return
		}
	} else {
		resp, err = dns.ResolveUpstream(query, targetIP, dns.ForwardOptions{
			Sender:       w.sock,
			Fragment:     set.DNS.FragmentQuery,
			Seg2Delay:    delay,
			ReverseOrder: set.Fragmentation.ReverseOrder,
			Mark:         int(cfg.Queue.Mark),
		})
		if err != nil {
			log.Tracef("DNS redirect: upstream %s failed: %v, answering SERVFAIL (fail-closed)", set.DNS.TargetDNS, err)
			w.sendDNSResponseToClient(ipVersion, originalDst, clientIP, clientPort, dns.BuildServfailResponse(query))
			return
		}
	}

	w.sendDNSResponseToClient(ipVersion, originalDst, clientIP, clientPort, resp)

	if set.Routing.Enabled && !set.Targets.DomainOnly && !cfg.Queue.IsDiscovery && RoutingHandleDNSFunc != nil {
		if ips := dns.ParseResponseIPs(resp); len(ips) > 0 {
			RoutingHandleDNSFunc(cfg, set, ips)
		}
	}

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
	if ipVersion == IPv4 {
		if pkt := sock.BuildUDPPacketV4(originalDst, clientIP, 53, clientPort, resp); pkt != nil {
			_ = w.clientSender().SendIPv4(pkt, clientIP)
		}
	} else {
		if pkt := sock.BuildUDPPacketV6(originalDst, clientIP, 53, clientPort, resp); pkt != nil {
			_ = w.clientSender().SendIPv6(pkt, clientIP)
		}
	}
}

func (w *Worker) resolveDoHRedirect(serverURL string, mark int, query []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(w.ctx, dohRedirectTimeout)
	defer cancel()
	return dns.ResolveDoH(ctx, getDoHClient(mark), serverURL, query)
}
