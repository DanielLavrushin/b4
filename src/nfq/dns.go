package nfq

import (
	"encoding/binary"
	"net"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sock"
	"github.com/florianl/go-nfqueue"
)

func (w *Worker) processDnsPacket(ipVersion byte, sport uint16, dport uint16, payload []byte, raw []byte, ihl int, id uint32) int {

	if dport == 53 {
		domain, ok := dns.ParseQueryDomain(payload)
		if ok {
			matcher := w.getMatcher()
			if matchedSet, set := matcher.MatchSNI(domain); matchedSet && set.DNS.Enabled && set.DNS.TargetDNS != "" {

				targetIP := net.ParseIP(set.DNS.TargetDNS)
				if targetIP == nil {
					if err := w.q.SetVerdict(id, nfqueue.NfAccept); err != nil {
						log.Tracef("failed to set verdict on packet %d: %v", id, err)
					}
					return 0
				}

				if ipVersion == IPv4 {
					targetDNS := targetIP.To4()
					if targetDNS == nil {
						// Target is IPv6 but packet is IPv4 - can't redirect
						if err := w.q.SetVerdict(id, nfqueue.NfAccept); err != nil {
							log.Tracef("failed to set verdict on packet %d: %v", id, err)
						}
						return 0
					}

					originalDst := make(net.IP, 4)
					copy(originalDst, raw[16:20])

					dns.DnsNATSet(net.IP(raw[12:16]), sport, originalDst)

					copy(raw[16:20], targetDNS)
					sock.FixIPv4Checksum(raw[:ihl])
					sock.FixUDPChecksum(raw, ihl)
					if set.DNS.FragmentQuery {
						w.sendFragmentedDNSQueryV4(set, raw, ihl, targetDNS)
					} else {
						_ = w.sock.SendIPv4(raw, targetDNS)
					}
					if err := w.q.SetVerdict(id, nfqueue.NfDrop); err != nil {
						log.Tracef("failed to set drop verdict on packet %d: %v", id, err)
					}
					log.Infof("DNS redirect: %s -> %s (set: %s)", domain, set.DNS.TargetDNS, set.Name)
					return 0

				} else { // IPv6
					cfg := w.getConfig()
					if !cfg.Queue.IPv6Enabled {
						if err := w.q.SetVerdict(id, nfqueue.NfAccept); err != nil {
							log.Tracef("failed to set verdict on packet %d: %v", id, err)
						}
						return 0
					}

					targetDNS := targetIP.To16()
					if targetDNS == nil {
						if err := w.q.SetVerdict(id, nfqueue.NfAccept); err != nil {
							log.Tracef("failed to set verdict on packet %d: %v", id, err)
						}
						return 0
					}

					// For IPv6: src at [8:24], dst at [24:40]
					originalDst := make(net.IP, 16)
					copy(originalDst, raw[24:40])

					dns.DnsNATSet(net.IP(raw[8:24]), sport, originalDst)

					copy(raw[24:40], targetDNS)
					sock.FixUDPChecksumV6(raw)
					if set.DNS.FragmentQuery {
						w.sendFragmentedDNSQueryV6(set, raw, targetDNS)
					} else {
						_ = w.sock.SendIPv6(raw, targetDNS)
					}
					if err := w.q.SetVerdict(id, nfqueue.NfDrop); err != nil {
						log.Tracef("failed to set drop verdict on packet %d: %v", id, err)
					}
					log.Infof("DNS redirect (IPv6): %s -> %s (set: %s)", domain, set.DNS.TargetDNS, set.Name)
					return 0
				}
			}
		}
	}

	if sport == 53 {
		if ipVersion == IPv4 {
			if originalDst, ok := dns.DnsNATGet(net.IP(raw[16:20]), dport); ok {
				copy(raw[12:16], originalDst.To4())
				sock.FixIPv4Checksum(raw[:ihl])
				sock.FixUDPChecksum(raw, ihl)
				dns.DnsNATDelete(net.IP(raw[16:20]), dport)
				_ = w.sock.SendIPv4(raw, net.IP(raw[16:20]))
				if err := w.q.SetVerdict(id, nfqueue.NfDrop); err != nil {
					log.Tracef("failed to set drop verdict on packet %d: %v", id, err)
				}
				return 0
			}
		} else { // IPv6
			cfg := w.getConfig()
			if cfg.Queue.IPv6Enabled {
				if originalDst, ok := dns.DnsNATGet(net.IP(raw[24:40]), dport); ok {
					copy(raw[8:24], originalDst.To16())
					sock.FixUDPChecksumV6(raw)
					dns.DnsNATDelete(net.IP(raw[24:40]), dport)
					_ = w.sock.SendIPv6(raw, net.IP(raw[24:40]))
					if err := w.q.SetVerdict(id, nfqueue.NfDrop); err != nil {
						log.Tracef("failed to set drop verdict on packet %d: %v", id, err)
					}
					return 0
				}
			}
		}
	}

	if err := w.q.SetVerdict(id, nfqueue.NfAccept); err != nil {
		log.Tracef("failed to set verdict on packet %d: %v", id, err)
	}
	return 0
}

// sendFragmentedDNSQuery fragments a DNS query to evade DPI
func (w *Worker) sendFragmentedDNSQueryV4(cfg *config.SetConfig, raw []byte, ihl int, dst net.IP) {
	udpOffset := ihl
	if len(raw) < ihl+8 {
		_ = w.sock.SendIPv4(raw, dst)
		return
	}
	udpLen := int(binary.BigEndian.Uint16(raw[udpOffset+4 : udpOffset+6]))

	if udpLen < 20 { // Too small to fragment meaningfully
		_ = w.sock.SendIPv4(raw, dst)
		return
	}

	dnsPayload := raw[udpOffset+8:]
	if len(dnsPayload) < 12 { // DNS header is 12 bytes minimum
		_ = w.sock.SendIPv4(raw, dst)
		return
	}

	splitPos := findDNSSplitPoint(dnsPayload)
	if splitPos <= 0 {
		splitPos = len(dnsPayload) / 2
	}

	frags, ok := sock.IPv4FragmentUDP(raw, splitPos)
	if !ok {
		log.Tracef("DNS frag: IP fragmentation failed, sending original")
		_ = w.sock.SendIPv4(raw, dst)
		return
	}

	seg2d := cfg.UDP.Seg2Delay

	w.SendTwoSegmentsV4(frags[0], frags[1], dst, seg2d, cfg.Fragmentation.ReverseOrder)

	log.Tracef("DNS frag: sent %d fragments for query", len(frags))
}

func (w *Worker) sendFragmentedDNSQueryV6(cfg *config.SetConfig, raw []byte, dst net.IP) {
	ipv6HdrLen := 40
	if len(raw) < ipv6HdrLen+8 {
		_ = w.sock.SendIPv6(raw, dst)
		return
	}
	udpLen := int(binary.BigEndian.Uint16(raw[ipv6HdrLen+4 : ipv6HdrLen+6]))

	if udpLen < 20 {
		_ = w.sock.SendIPv6(raw, dst)
		return
	}

	dnsPayload := raw[ipv6HdrLen+8:]
	if len(dnsPayload) < 12 {
		_ = w.sock.SendIPv6(raw, dst)
		return
	}

	splitPos := findDNSSplitPoint(dnsPayload)
	if splitPos <= 0 {
		splitPos = len(dnsPayload) / 2
	}

	frags, ok := sock.IPv6FragmentUDP(raw, splitPos)
	if !ok {
		log.Tracef("DNS frag v6: fragmentation failed, sending original")
		_ = w.sock.SendIPv6(raw, dst)
		return
	}

	seg2d := cfg.UDP.Seg2Delay

	w.SendTwoSegmentsV6(frags[0], frags[1], dst, seg2d, cfg.Fragmentation.ReverseOrder)

	log.Tracef("DNS frag v6: sent %d fragments", len(frags))
}

// findDNSSplitPoint finds optimal split point in DNS query
func findDNSSplitPoint(dnsPayload []byte) int {
	if len(dnsPayload) < 13 { // 12 byte header + at least 1 byte QNAME
		return -1
	}

	pos := 12
	qnameStart := pos
	qnameEnd := pos

	for pos < len(dnsPayload) {
		labelLen := int(dnsPayload[pos])
		if labelLen == 0 {
			qnameEnd = pos + 1 // Include the null terminator
			break
		}
		if labelLen > 63 || pos+1+labelLen > len(dnsPayload) {
			// Invalid or compressed - fallback
			return len(dnsPayload) / 2
		}
		pos += 1 + labelLen
	}

	if qnameEnd <= qnameStart {
		return len(dnsPayload) / 2
	}

	// Split in the middle of QNAME
	qnameLen := qnameEnd - qnameStart
	if qnameLen > 4 {
		return qnameStart + qnameLen/2
	}

	return len(dnsPayload) / 2
}
