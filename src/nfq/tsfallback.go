package nfq

import (
	"encoding/binary"
	"net"
	"strconv"
	"sync"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

var tsFallbackLog sync.Map

func (w *Worker) warnTimestampFallback(cfg *config.SetConfig, original []byte, l3 int, src, dst net.IP) {
	if cfg.Id == "" || len(original) < l3+4 {
		return
	}
	if live := w.getConfig(); live != nil && live.Queue.IsDiscovery {
		return
	}
	if _, seen := tsFallbackLog.LoadOrStore(cfg.Id, struct{}{}); seen {
		return
	}
	sport := binary.BigEndian.Uint16(original[l3 : l3+2])
	dport := binary.BigEndian.Uint16(original[l3+2 : l3+4])
	if original[0]>>4 == IPv4 {
		src, sport = w.deviceOf(src, sport, dst, dport)
	}
	from := net.JoinHostPort(src.String(), strconv.Itoa(int(sport)))
	to := net.JoinHostPort(dst.String(), strconv.Itoa(int(dport)))
	log.Warnf("Fake SNI for set %q: the connection from %s to %s carries no TCP timestamps, because the device sent none or the server refused them, so its TCP Timestamp fake went out with a wrong TCP checksum and a wrong acknowledgment number instead. If the site stays blocked from that device, turn on TCP timestamps on it (Windows: netsh interface tcp set global timestamps=enabled) or pick another fake strategy for this set", cfg.Name, from, to)
}

func (w *Worker) deviceOf(src net.IP, sport uint16, dst net.IP, dport uint16) (net.IP, uint16) {
	if w.srcResolver == nil {
		return src, sport
	}
	lan, port, _ := w.srcResolver.attributePacket(tunSrcPacket{}, 6, src, sport, dst, dport)
	if lan == nil {
		return src, sport
	}
	if port == 0 {
		port = sport
	}
	return lan, port
}
