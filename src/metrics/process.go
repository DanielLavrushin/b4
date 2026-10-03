package metrics

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"os"
	"strconv"
)

type procSampler interface {
	cpuTicks() (uint64, bool)
	rss() uint64
	threads() int
	conntrack() (Conntrack, bool)
	memTotal() uint64
}

type procfsSampler struct{}

var (
	procSelfStat       = "/proc/self/stat"
	procSelfAuxv       = "/proc/self/auxv"
	procMeminfo        = "/proc/meminfo"
	conntrackCountPath = "/proc/sys/net/netfilter/nf_conntrack_count"
	conntrackMaxPaths  = []string{"/proc/sys/net/netfilter/nf_conntrack_max", "/proc/sys/net/nf_conntrack_max"}
)

func (procfsSampler) cpuTicks() (uint64, bool) {
	data, err := os.ReadFile(procSelfStat)
	if err != nil {
		return 0, false
	}
	return parseStatTicks(data)
}

func (procfsSampler) rss() uint64 { return readRSS() }

func (procfsSampler) threads() int { return osThreads() }

func (procfsSampler) conntrack() (Conntrack, bool) {
	count, ok := readUintFile(conntrackCountPath)
	if !ok {
		return Conntrack{}, false
	}
	for _, p := range conntrackMaxPaths {
		if limit, ok := readUintFile(p); ok {
			return Conntrack{Count: count, Max: limit}, true
		}
	}
	return Conntrack{}, false
}

func (procfsSampler) memTotal() uint64 {
	f, err := os.Open(procMeminfo)
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.HasPrefix(line, []byte("MemTotal:")) {
			continue
		}
		kb, ok := parseUintField(bytes.TrimSpace(bytes.TrimSuffix(bytes.TrimSpace(line[len("MemTotal:"):]), []byte("kB"))))
		if !ok {
			return 0
		}
		return kb * 1024
	}
	return 0
}

func readUintFile(path string) (uint64, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	return parseUintField(bytes.TrimSpace(data))
}

func parseUintField(b []byte) (uint64, bool) {
	if len(b) == 0 {
		return 0, false
	}
	var v uint64
	for _, c := range b {
		if c < '0' || c > '9' {
			return 0, false
		}
		d := uint64(c - '0')
		if v > (^uint64(0)-d)/10 {
			return 0, false
		}
		v = v*10 + d
	}
	return v, true
}

func parseStatTicks(data []byte) (uint64, bool) {
	i := bytes.LastIndexByte(data, ')')
	if i < 0 {
		return 0, false
	}
	rest := data[i+1:]
	var utime, stime uint64
	field, found := 0, 0
	for j := 0; j < len(rest) && found < 2; {
		for j < len(rest) && (rest[j] == ' ' || rest[j] == '\n') {
			j++
		}
		k := j
		for k < len(rest) && rest[k] != ' ' && rest[k] != '\n' {
			k++
		}
		if k == j {
			break
		}
		switch field {
		case 11, 12:
			v, ok := parseUintField(rest[j:k])
			if !ok {
				return 0, false
			}
			if field == 11 {
				utime = v
			} else {
				stime = v
			}
			found++
		}
		field++
		j = k
	}
	if found < 2 {
		return 0, false
	}
	return utime + stime, true
}

func clkTck() uint64 {
	data, err := os.ReadFile(procSelfAuxv)
	if err != nil {
		return 100
	}
	return parseAuxvClkTck(data, strconv.IntSize/8)
}

func parseAuxvClkTck(data []byte, word int) uint64 {
	const atClkTck = 17
	for i := 0; i+2*word <= len(data); i += 2 * word {
		var tag, val uint64
		if word == 8 {
			tag = binary.NativeEndian.Uint64(data[i:])
			val = binary.NativeEndian.Uint64(data[i+8:])
		} else {
			tag = uint64(binary.NativeEndian.Uint32(data[i:]))
			val = uint64(binary.NativeEndian.Uint32(data[i+4:]))
		}
		if tag == 0 {
			break
		}
		if tag == atClkTck && val > 0 {
			return val
		}
	}
	return 100
}
