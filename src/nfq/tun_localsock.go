package nfq

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

const (
	procNetTCP          = "/proc/net/tcp"
	procNetUDP          = "/proc/net/udp"
	procDefaultTTL      = "/proc/sys/net/ipv4/ip_default_ttl"
	fallbackHostTTL     = 64
	procWildcardAddr    = "00000000"
	procUnconnectedPeer = "00000000:0000"
	firstUnprivileged   = 1024
)

type localSockets struct {
	tcp string
	udp string
	ttl uint8
}

func procLocalSockets() localSockets {
	return localSockets{tcp: procNetTCP, udp: procNetUDP, ttl: readHostTTL(procDefaultTTL)}
}

func readHostTTL(path string) uint8 {
	b, err := os.ReadFile(path)
	if err != nil {
		return fallbackHostTTL
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || n < 1 || n > 255 {
		return fallbackHostTTL
	}
	return uint8(n)
}

func (s localSockets) holds(ttl uint8, proto uint8, src net.IP, sport uint16, dst net.IP, dport uint16) bool {
	if s.ttl == 0 || ttl != s.ttl {
		return false
	}
	switch proto {
	case 6:
		return socketTableHolds(s.tcp, src, sport, dst, dport, false)
	case 17:
		return socketTableHolds(s.udp, src, sport, dst, dport, sport >= firstUnprivileged)
	}
	return false
}

func procHexAddr(ip net.IP) (string, bool) {
	v4 := ip.To4()
	if v4 == nil {
		return "", false
	}
	return fmt.Sprintf("%08X", binary.NativeEndian.Uint32(v4)), true
}

func socketTableHolds(path string, src net.IP, sport uint16, dst net.IP, dport uint16, unconnected bool) bool {
	if path == "" {
		return false
	}
	srcHex, ok := procHexAddr(src)
	if !ok {
		return false
	}
	dstHex, ok := procHexAddr(dst)
	if !ok {
		return false
	}
	port := fmt.Sprintf(":%04X", sport)
	bound, wildcard := srcHex+port, procWildcardAddr+port
	peer := fmt.Sprintf("%s:%04X", dstHex, dport)

	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 || (fields[1] != bound && fields[1] != wildcard) {
			continue
		}
		if fields[2] == peer || (unconnected && fields[2] == procUnconnectedPeer) {
			return true
		}
	}
	return false
}
