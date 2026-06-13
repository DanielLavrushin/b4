package handler

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/tables"
	"golang.org/x/sys/unix"
)

// @Summary Get system diagnostics
// @Tags System
// @Produce json
// @Success 200 {object} DiagnosticsResponse
// @Security BearerAuth
// @Router /system/diagnostics [get]
func (api *API) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	cfg := api.getCfg()
	serviceManager := api.getServiceManager()

	diag := Diagnostics{
		System:   collectSystemInfo(),
		B4:       collectB4Info(cfg.ConfigPath, serviceManager),
		Kernel:   collectKernelModules(),
		Tools:    collectTools(),
		Network:  collectNetworkInterfaces(),
		Firewall: collectFirewallInfo(),
		Geodata:  api.collectGeodataInfo(),
		Storage:  collectStorage(),
		Paths:    collectPaths(cfg.ConfigPath, cfg.System.Logging.ErrorFilePath(), cfg.System.Geo.GeoSitePath, cfg.System.Geo.GeoIpPath),
	}

	sendResponse(w, DiagnosticsResponse{Success: true, Data: diag})
}

func collectSystemInfo() DiagSystem {
	var uts unix.Utsname
	_ = unix.Uname(&uts)

	hostname, _ := os.Hostname()

	info := DiagSystem{
		Hostname: hostname,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Kernel:   unix.ByteSliceToString(uts.Release[:]),
		CPUCores: runtime.NumCPU(),
		IsDocker: isDockerEnvironment(),
	}

	info.Distro = readDistroName()

	if f, err := os.Open("/proc/meminfo"); err == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "MemTotal:") {
				info.MemTotal = parseMemInfoKB(line) / 1024
			} else if strings.HasPrefix(line, "MemAvailable:") {
				info.MemAvail = parseMemInfoKB(line) / 1024
			}
		}
	}

	return info
}

func readDistroName() string {
	for _, path := range []string{"/etc/os-release", "/etc/openwrt_release"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"'")
			}
			if strings.HasPrefix(line, "DISTRIB_DESCRIPTION=") {
				return strings.Trim(strings.TrimPrefix(line, "DISTRIB_DESCRIPTION="), "\"'")
			}
		}
	}
	return ""
}

func parseMemInfoKB(line string) uint64 {
	fields := strings.Fields(line)
	if len(fields) >= 2 {
		v, _ := strconv.ParseUint(fields[1], 10, 64)
		return v
	}
	return 0
}

func collectB4Info(configPath, serviceManager string) DiagB4 {
	info := DiagB4{
		Version:        Version,
		Commit:         Commit,
		BuildDate:      Date,
		ServiceManager: serviceManager,
		ConfigPath:     configPath,
		Running:        true,
		PID:            os.Getpid(),
	}

	pid := os.Getpid()
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid)); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "VmRSS:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					kb, _ := strconv.ParseFloat(fields[1], 64)
					info.MemoryMB = fmt.Sprintf("%.1f", kb/1024)
				}
			}
		}
	}

	if btime := readBootTime(); btime > 0 {
		if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
			fields := strings.Fields(string(data))
			if len(fields) > 21 {
				startTicks, _ := strconv.ParseUint(fields[21], 10, 64)
				clkTck := getClkTck()
				startSec := btime + int64(startTicks/clkTck)
				runningSecs := time.Now().Unix() - startSec
				if runningSecs > 0 {
					if runningSecs < 3600 {
						info.Uptime = fmt.Sprintf("%dm", runningSecs/60)
					} else {
						info.Uptime = fmt.Sprintf("%dh %dm", runningSecs/3600, (runningSecs%3600)/60)
					}
				}
			}
		}
	}

	return info
}

func readBootTime() int64 {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "btime ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				v, _ := strconv.ParseInt(fields[1], 10, 64)
				return v
			}
		}
	}
	return 0
}

func getClkTck() uint64 {
	data, err := os.ReadFile("/proc/self/auxv")
	if err != nil {
		return 100
	}
	const atClkTck = 17
	wordSize := 8
	if runtime.GOARCH == "arm" || runtime.GOARCH == "mipsle" || runtime.GOARCH == "mips" || runtime.GOARCH == "386" {
		wordSize = 4
	}
	entrySize := wordSize * 2
	for i := 0; i+entrySize <= len(data); i += entrySize {
		var tag, val uint64
		if wordSize == 8 {
			tag = uint64(data[i]) | uint64(data[i+1])<<8 | uint64(data[i+2])<<16 | uint64(data[i+3])<<24 |
				uint64(data[i+4])<<32 | uint64(data[i+5])<<40 | uint64(data[i+6])<<48 | uint64(data[i+7])<<56
			val = uint64(data[i+8]) | uint64(data[i+9])<<8 | uint64(data[i+10])<<16 | uint64(data[i+11])<<24 |
				uint64(data[i+12])<<32 | uint64(data[i+13])<<40 | uint64(data[i+14])<<48 | uint64(data[i+15])<<56
		} else {
			tag = uint64(data[i]) | uint64(data[i+1])<<8 | uint64(data[i+2])<<16 | uint64(data[i+3])<<24
			val = uint64(data[i+4]) | uint64(data[i+5])<<8 | uint64(data[i+6])<<16 | uint64(data[i+7])<<24
		}
		if tag == atClkTck && val > 0 {
			return val
		}
		if tag == 0 {
			break
		}
	}
	return 100
}

func collectPaths(configPath, errorLog, geositePath, geoipPath string) DiagPaths {
	binary, _ := os.Executable()
	if resolved, err := filepath.EvalSymlinks(binary); err == nil {
		binary = resolved
	}

	paths := DiagPaths{
		Binary:  binary,
		Config:  configPath,
		Geosite: geositePath,
		Geoip:   geoipPath,
	}

	if errorLog != "" {
		paths.ErrorLog = errorLog
	}

	if configPath != "" {
		paths.DataDir = filepath.Dir(configPath)
	}

	return paths
}

func collectNetworkInterfaces() DiagNetwork {
	ifaces, err := net.Interfaces()
	if err != nil {
		return DiagNetwork{}
	}

	var result []DiagInterface
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		di := DiagInterface{
			Name: iface.Name,
			Up:   iface.Flags&net.FlagUp != 0,
			MTU:  iface.MTU,
		}

		if len(iface.HardwareAddr) > 0 {
			di.MAC = iface.HardwareAddr.String()
		}

		if addrs, err := iface.Addrs(); err == nil {
			for _, addr := range addrs {
				di.Addrs = append(di.Addrs, addr.String())
			}
		}

		result = append(result, di)
	}

	return DiagNetwork{Interfaces: result}
}

func collectFirewallInfo() DiagFirewall {
	info := DiagFirewall{}

	if _, err := exec.LookPath("nft"); err == nil {
		out, err := exec.Command("nft", "list", "table", "inet", "b4_mangle").CombinedOutput()
		if err == nil && len(out) > 0 {
			info.Backend = "nftables"
			for _, line := range strings.Split(string(out), "\n") {
				line = strings.TrimSpace(line)
				if line != "" && !strings.HasPrefix(line, "table") && !strings.HasPrefix(line, "}") && !strings.HasPrefix(line, "chain") {
					info.ActiveRules = append(info.ActiveRules, line)
				}
			}
		}
	}

	if info.Backend == "" {
		for _, bin := range []string{"iptables", "iptables-legacy"} {
			if _, err := exec.LookPath(bin); err != nil {
				continue
			}
			out, err := exec.Command(bin, append(tables.WaitArgs(bin), "-t", "mangle", "-S", "B4")...).CombinedOutput()
			if err == nil && len(out) > 0 {
				if bin == "iptables-legacy" {
					info.Backend = "iptables-legacy"
				} else {
					info.Backend = "iptables"
				}
				for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
					if line != "" {
						info.ActiveRules = append(info.ActiveRules, line)
					}
				}
				break
			}
		}
	}

	if info.Backend == "" {
		info.Backend = "none"
	}

	info.NFQueueWorks = testNFQueue(info.Backend)
	info.FlowOffload = detectFlowOffload()

	return info
}

// detectFlowOffload reports whether netfilter flow offloading is active on the
// system. Offloaded flows take a fast path that skips the forward/postrouting
// hooks where b4's NFQUEUE rules live, so an active flowtable means b4 never
// sees the traffic. Returns "hardware", "software" or "off".
func detectFlowOffload() string {
	if _, err := exec.LookPath("nft"); err == nil {
		out, err := exec.Command("nft", "list", "ruleset").CombinedOutput()
		if err == nil {
			s := string(out)
			if strings.Contains(s, "flow add @") || strings.Contains(s, "flow offload @") {
				for _, line := range strings.Split(s, "\n") {
					l := strings.TrimSpace(line)
					if strings.HasPrefix(l, "flags") && strings.Contains(l, "offload") {
						return "hardware"
					}
				}
				return "software"
			}
		}
	}

	for _, bin := range []string{"iptables", "iptables-legacy"} {
		if _, err := exec.LookPath(bin); err != nil {
			continue
		}
		out, err := exec.Command(bin, append(tables.WaitArgs(bin), "-t", "filter", "-S")...).CombinedOutput()
		if err == nil && strings.Contains(string(out), "FLOWOFFLOAD") {
			if strings.Contains(string(out), "--hw") {
				return "hardware"
			}
			return "software"
		}
	}

	return "off"
}

func testNFQueue(backend string) bool {
	switch backend {
	case "nftables":
		if _, err := exec.Command("nft", "add", "table", "inet", "_b4_diag_test").CombinedOutput(); err != nil {
			return false
		}
		defer exec.Command("nft", "delete", "table", "inet", "_b4_diag_test").CombinedOutput()
		exec.Command("nft", "add", "chain", "inet", "_b4_diag_test", "test_chain").CombinedOutput()
		_, err := exec.Command("nft", "add", "rule", "inet", "_b4_diag_test", "test_chain", "queue", "num", "0").CombinedOutput()
		return err == nil

	case "iptables", "iptables-legacy":
		bin := "iptables"
		if backend == "iptables-legacy" {
			bin = "iptables-legacy"
		}
		iptArgs := func(rest ...string) []string {
			return append(append([]string{}, tables.WaitArgs(bin)...), rest...)
		}
		if _, err := exec.Command(bin, iptArgs("-t", "mangle", "-N", "B4_DIAG_TEST")...).CombinedOutput(); err != nil {
			return false
		}
		defer func() {
			exec.Command(bin, iptArgs("-t", "mangle", "-F", "B4_DIAG_TEST")...).CombinedOutput()
			exec.Command(bin, iptArgs("-t", "mangle", "-X", "B4_DIAG_TEST")...).CombinedOutput()
		}()
		_, err := exec.Command(bin, iptArgs("-t", "mangle", "-A", "B4_DIAG_TEST", "-j", "NFQUEUE", "--queue-num", "0")...).CombinedOutput()
		return err == nil
	}

	return false
}

func (api *API) collectGeodataInfo() DiagGeodata {
	info := DiagGeodata{
		GeositeConfigured: api.geodataManager.IsGeositeConfigured(),
		GeoipConfigured:   api.geodataManager.IsGeoipConfigured(),
	}

	if info.GeositeConfigured {
		path := api.geodataManager.GetGeositePath()
		info.GeositePath = path
		if fi, err := os.Stat(path); err == nil {
			info.GeositeSize = formatBytes(uint64(fi.Size()))
		}
	}

	if info.GeoipConfigured {
		path := api.geodataManager.GetGeoipPath()
		info.GeoipPath = path
		if fi, err := os.Stat(path); err == nil {
			info.GeoipSize = formatBytes(uint64(fi.Size()))
		}
	}

	cfg := api.getCfg()
	for _, set := range cfg.Sets {
		info.TotalDomains += len(set.Targets.SNIDomains)
		info.TotalIPs += len(set.Targets.IPs)

		if len(set.Targets.GeoSiteCategories) > 0 && api.geodataManager.IsGeositeConfigured() {
			counts, err := api.geodataManager.GetGeositeCategoryCounts(set.Targets.GeoSiteCategories)
			if err == nil {
				for _, c := range counts {
					info.TotalDomains += c
				}
			}
		}

		if len(set.Targets.GeoIpCategories) > 0 && api.geodataManager.IsGeoipConfigured() {
			counts, err := api.geodataManager.GetGeoipCategoryCounts(set.Targets.GeoIpCategories)
			if err == nil {
				for _, c := range counts {
					info.TotalIPs += c
				}
			}
		}
	}

	return info
}

func collectKernelModules() DiagKernel {
	modules := []string{"xt_NFQUEUE", "nfnetlink_queue", "xt_connbytes", "xt_multiport", "nf_conntrack"}
	result := DiagKernel{Modules: make([]DiagModule, 0, len(modules))}

	lsmodOutput := ""
	if data, err := exec.Command("lsmod").Output(); err == nil {
		lsmodOutput = string(data)
	}

	builtinPaths := []string{
		"/lib/modules/" + unameRelease() + "/modules.builtin",
		"/sys/module",
	}

	for _, mod := range modules {
		dm := DiagModule{Name: mod}
		modUnderscore := strings.ReplaceAll(mod, "-", "_")

		if strings.Contains(lsmodOutput, modUnderscore) {
			dm.Status = "loaded"
		} else if isModuleBuiltin(mod, builtinPaths) {
			dm.Status = "built-in"
		} else {
			dm.Status = "missing"
		}

		result.Modules = append(result.Modules, dm)
	}

	return result
}

func unameRelease() string {
	var uts unix.Utsname
	if err := unix.Uname(&uts); err != nil {
		return ""
	}
	return unix.ByteSliceToString(uts.Release[:])
}

func isModuleBuiltin(mod string, paths []string) bool {
	modUnderscore := strings.ReplaceAll(mod, "-", "_")
	for _, p := range paths {
		if strings.HasSuffix(p, "modules.builtin") {
			if data, err := os.ReadFile(p); err == nil {
				if strings.Contains(string(data), modUnderscore) {
					return true
				}
			}
		} else if strings.HasSuffix(p, "/sys/module") {
			if _, err := os.Stat(p + "/" + modUnderscore); err == nil {
				return true
			}
		}
	}
	return false
}

func collectTools() DiagTools {
	firewallTools := []string{"iptables", "iptables-legacy", "nft"}

	required := []struct {
		name    string
		missing string
	}{
		{"tar", "required for install"},
		{"curl", "required for download"},
	}

	optional := []struct {
		name    string
		missing string
	}{
		{"jq", "config editing won't work"},
		{"sha256sum", "checksum verify skipped"},
		{"nohup", "service may stop on session close"},
		{"modprobe", "kernel modules loaded via insmod"},
		{"ipset", "needed for routing on iptables systems"},
		{"wget", "fallback downloader"},
	}

	result := DiagTools{
		Firewall: make([]DiagTool, 0, len(firewallTools)),
		Required: make([]DiagTool, 0, len(required)),
		Optional: make([]DiagTool, 0, len(optional)),
	}

	for _, name := range firewallTools {
		dt := DiagTool{Name: name}
		if path, err := exec.LookPath(name); err == nil {
			dt.Found = true
			dt.Detail = path
		}
		result.Firewall = append(result.Firewall, dt)
	}

	for _, t := range required {
		dt := DiagTool{Name: t.name}
		if path, err := exec.LookPath(t.name); err == nil {
			dt.Found = true
			dt.Detail = path
		} else {
			dt.Detail = t.missing
		}
		result.Required = append(result.Required, dt)
	}

	for _, t := range optional {
		dt := DiagTool{Name: t.name}
		if path, err := exec.LookPath(t.name); err == nil {
			dt.Found = true
			dt.Detail = path
		} else {
			dt.Detail = t.missing
		}
		result.Optional = append(result.Optional, dt)
	}

	return result
}

func collectStorage() []DiagMount {
	dirs := []string{"/", "/opt", "/tmp", "/jffs", "/mnt/sda1", "/etc/storage"}
	seen := make(map[string]bool)
	var mounts []DiagMount

	for _, dir := range dirs {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		m := mountInfo(dir)
		if m == nil {
			continue
		}
		key := m.Available + m.Path
		if seen[key] {
			continue
		}
		seen[key] = true
		mounts = append(mounts, *m)
	}

	return mounts
}

func mountInfo(dir string) *DiagMount {
	var stat unix.Statfs_t
	if err := unix.Statfs(dir, &stat); err != nil {
		return nil
	}

	availBytes := stat.Bavail * uint64(stat.Bsize)
	avail := formatBytes(availBytes)

	writable := unix.Access(dir, unix.W_OK) == nil

	return &DiagMount{
		Path:      dir,
		Available: avail,
		Writable:  writable,
	}
}

func formatBytes(b uint64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
