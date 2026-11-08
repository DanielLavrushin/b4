package iphealth

import (
	"net"
	"sort"
	"strings"
	"time"
)

const mediaNodeSuffix = ".googlevideo.com"

type Sibling struct {
	Host string
	IP   net.IP
}

func mediaNodeCluster(host string) (string, bool) {
	host = normalizeHost(host)
	rest, ok := strings.CutSuffix(host, mediaNodeSuffix)
	if !ok {
		return "", false
	}
	server, cluster, ok := strings.Cut(rest, "---")
	if !ok {
		server, cluster, ok = strings.Cut(rest, ".")
	}
	if !ok || !mediaServerLabel(server) || !mediaClusterLabel(cluster) {
		return "", false
	}
	return cluster, true
}

func mediaServerLabel(label string) bool {
	digits := strings.TrimPrefix(strings.TrimPrefix(label, "r"), "r")
	if len(digits) == len(label) || digits == "" {
		return false
	}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func mediaClusterLabel(label string) bool {
	code, ok := strings.CutPrefix(label, "sn-")
	if !ok || code == "" {
		return false
	}
	for _, c := range code {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

func sameServerBlock(a, b net.IP) bool {
	if a4, b4 := a.To4(), b.To4(); a4 != nil || b4 != nil {
		return a4 != nil && b4 != nil && a4.Mask(net.CIDRMask(24, 32)).Equal(b4.Mask(net.CIDRMask(24, 32)))
	}
	return a.Mask(net.CIDRMask(48, 128)).Equal(b.Mask(net.CIDRMask(48, 128)))
}

func nearAny(ip net.IP, near []net.IP) bool {
	for _, n := range near {
		if n != nil && sameServerBlock(ip, n) {
			return true
		}
	}
	return false
}

func (k *KnownGood) LookupSiblings(host string, near []net.IP, usable func(net.IP) bool) []Sibling {
	if k == nil || host == "" || len(near) == 0 {
		return nil
	}
	cluster, ok := mediaNodeCluster(host)
	if !ok {
		return nil
	}
	self := normalizeHost(host)

	type candidate struct {
		Sibling
		seen time.Time
	}
	now := time.Now()
	k.mu.Lock()
	var found []candidate
	for name, entries := range k.hosts {
		if name == self {
			continue
		}
		if c, ok := mediaNodeCluster(name); !ok || c != cluster {
			continue
		}
		for _, e := range entries {
			if !e.verified || now.Sub(e.seen) > knownGoodTTL || !nearAny(e.ip, near) {
				continue
			}
			found = append(found, candidate{Sibling{Host: name, IP: append(net.IP(nil), e.ip...)}, e.seen})
		}
	}
	k.mu.Unlock()

	sort.Slice(found, func(i, j int) bool { return found[i].seen.After(found[j].seen) })
	out := make([]Sibling, 0, maxKnownGoodPerHost)
	for _, c := range found {
		if len(out) == maxKnownGoodPerHost {
			break
		}
		if usable != nil && !usable(c.IP) {
			continue
		}
		dup := false
		for _, s := range out {
			if s.IP.Equal(c.IP) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, c.Sibling)
		}
	}
	return out
}
