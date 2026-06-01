package nfq

import (
	"net"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sock"
	"github.com/florianl/go-nfqueue"
)

// parseDNSName parses a DNS domain name from msg starting at the given offset.
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

func (w *Worker) processDnsPacket(ipVersion byte, sport uint16, dport uint16, payload []byte, raw []byte, ihl int, id uint32, srcMac string) int {

	if dport == 53 {
		domain, ok := dns.ParseQueryDomain(payload)
		txid, txidOK := dns.ParseTransactionID(payload)
		if ok {
			domain = strings.ToLower(domain)
			matcher := w.getMatcher()
			if matchedSet, set := matcher.MatchSNIWithSource(domain, srcMac); matchedSet {
				cfg := w.getConfig()
				if txidOK && set.Routing.Enabled && !cfg.Queue.IsDiscovery {
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

				if !(set.DNS.Enabled && set.DNS.TargetDNS != "") {
					if err := w.q.SetVerdict(id, nfqueue.NfAccept); err != nil {
						log.Tracef("failed to set verdict on packet %d: %v", id, err)
					}
					return 0
				}

				targetIP := net.ParseIP(set.DNS.TargetDNS)
				if targetIP == nil {
					if err := w.q.SetVerdict(id, nfqueue.NfAccept); err != nil {
						log.Tracef("failed to set verdict on packet %d: %v", id, err)
					}
					return 0
				}

				if ipVersion == IPv6 && !cfg.Queue.IPv6Enabled {
					if err := w.q.SetVerdict(id, nfqueue.NfAccept); err != nil {
						log.Tracef("failed to set verdict on packet %d: %v", id, err)
					}
					return 0
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

				if err := w.q.SetVerdict(id, nfqueue.NfDrop); err != nil {
					log.Tracef("failed to set drop verdict on packet %d: %v", id, err)
				}

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

			if setID, hit := consumeDNSPendingRoute(
				dnsRouteKeyResponse(ipVersion, clientIP, dport, dnsServerIP, sport, txid, domain),
			); hit {
				if ips := dns.ParseResponseIPs(payload); len(ips) > 0 {
					cfg := w.getConfig()
					if set := cfg.GetSetById(setID); set != nil {
						if RoutingHandleDNSFunc != nil && !cfg.Queue.IsDiscovery {
							RoutingHandleDNSFunc(cfg, set, ips)
						}
					}
				}
			}
		}
	}

	if err := w.q.SetVerdict(id, nfqueue.NfAccept); err != nil {
		log.Tracef("failed to set verdict on packet %d: %v", id, err)
	}
	return 0
}

func (w *Worker) resolveDNSRedirect(ipVersion byte, set *config.SetConfig, cfg *config.Config, query []byte, clientIP net.IP, clientPort uint16, originalDst, targetIP net.IP, delay int) {
	resp, err := dns.ResolveUpstream(query, targetIP, dns.ForwardOptions{
		Sender:       w.sock,
		Fragment:     set.DNS.FragmentQuery,
		Seg2Delay:    delay,
		ReverseOrder: set.Fragmentation.ReverseOrder,
		Mark:         int(cfg.Queue.Mark),
	})
	if err != nil {
		log.Tracef("DNS redirect: upstream %s failed: %v", set.DNS.TargetDNS, err)
		return
	}

	if ipVersion == IPv4 {
		if pkt := sock.BuildUDPPacketV4(originalDst, clientIP, 53, clientPort, resp); pkt != nil {
			_ = w.sock.SendIPv4(pkt, clientIP)
		}
	} else {
		if pkt := sock.BuildUDPPacketV6(originalDst, clientIP, 53, clientPort, resp); pkt != nil {
			_ = w.sock.SendIPv6(pkt, clientIP)
		}
	}

	if set.Routing.Enabled && !cfg.Queue.IsDiscovery && RoutingHandleDNSFunc != nil {
		if ips := dns.ParseResponseIPs(resp); len(ips) > 0 {
			RoutingHandleDNSFunc(cfg, set, ips)
		}
	}

	log.Tracef("DNS redirect: %s -> %s answered for %s (set: %s)", originalDst, set.DNS.TargetDNS, clientIP, set.Name)
}
