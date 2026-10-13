package tables

import (
	"os"
	"strconv"
	"strings"
	"sync"
)

type dnsKernel struct {
	Release     string
	KeeneticOS  bool
	TwinDropped bool
}

var (
	dnsKernelFacts = cachedDNSKernelFacts
	ndmRunDir      = "/var/run/ndm"

	dnsKernelOnce sync.Once
	dnsKernelRead dnsKernel
)

func cachedDNSKernelFacts() dnsKernel {
	dnsKernelOnce.Do(func() { dnsKernelRead = readDNSKernelFacts() })
	return dnsKernelRead
}

func readDNSKernelFacts() dnsKernel {
	release := kmodRelease()
	return dnsKernel{
		Release:     release,
		KeeneticOS:  strings.Contains(release, "-ndm") || ndmRunDirPresent() || rawOutTableListed(),
		TwinDropped: kernelDropsHeldTwin(release),
	}
}

func ndmRunDirPresent() bool {
	fi, err := os.Stat(ndmRunDir)
	return err == nil && fi.IsDir()
}

func rawOutTableListed() bool {
	for _, path := range []string{"/proc/net/ip_tables_names", "/proc/net/ip6_tables_names"} {
		raw, err := readProcNetFile(path)
		if err != nil {
			continue
		}
		for _, name := range strings.Fields(string(raw)) {
			if name == "raw_out" {
				return true
			}
		}
	}
	return false
}

func kernelVersion(release string) (major, minor, patch int, ok bool) {
	var nums [3]int
	n := 0
	rest := release
	for n < len(nums) {
		end := 0
		for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
			end++
		}
		if end == 0 {
			break
		}
		v, err := strconv.Atoi(rest[:end])
		if err != nil {
			return 0, 0, 0, false
		}
		nums[n] = v
		n++
		rest = rest[end:]
		if !strings.HasPrefix(rest, ".") {
			break
		}
		rest = rest[1:]
	}
	if n < 2 {
		return 0, 0, 0, false
	}
	return nums[0], nums[1], nums[2], true
}

func kernelDropsHeldTwin(release string) bool {
	major, minor, patch, ok := kernelVersion(release)
	switch {
	case !ok:
		return false
	case major < 4:
		return true
	case major > 4:
		return false
	case minor == 14:
		return patch < 173
	}
	return minor < 18
}

func KeeneticOS() bool {
	return dnsKernelFacts().KeeneticOS
}

func DNSQueryCopyKernel() bool {
	return dnsKernelFacts().TwinDropped
}

func RawTablePresent(ipt string) bool {
	return !dnsKernelFacts().KeeneticOS || kmodLoaded(iptRawTableModule(ipt))
}
