package tables

import (
	"bufio"
	"debug/elf"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/utils"
	"golang.org/x/sys/unix"
)

const (
	kmodDepthLimit      = 8
	kmodIndexRebuildTTL = 30 * time.Second
)

type kmodPackageSet struct {
	kernel    []string
	userspace []string
}

var kmodPackageTable = map[string]kmodPackageSet{
	"nft_queue":     {kernel: []string{"kmod-nft-queue"}},
	"nft_ct":        {kernel: []string{"kmod-nft-core"}},
	"nft_socket":    {kernel: []string{"kmod-nft-socket"}},
	"nft_tproxy":    {kernel: []string{"kmod-nft-tproxy"}},
	"xt_NFQUEUE":    {kernel: []string{"kmod-nfnetlink-queue", "kmod-ipt-nfqueue"}, userspace: []string{"iptables-mod-nfqueue"}},
	"xt_connbytes":  {kernel: []string{"kmod-ipt-conntrack-extra"}, userspace: []string{"iptables-mod-conntrack-extra"}},
	"xt_socket":     {kernel: []string{"kmod-ipt-socket"}, userspace: []string{"iptables-mod-tproxy"}},
	"xt_TPROXY":     {kernel: []string{"kmod-ipt-tproxy"}, userspace: []string{"iptables-mod-tproxy"}},
	"xt_connmark":   {kernel: []string{"kmod-ipt-conntrack-extra"}, userspace: []string{"iptables-mod-conntrack-extra"}},
	"xt_CONNMARK":   {kernel: []string{"kmod-ipt-conntrack-extra"}, userspace: []string{"iptables-mod-conntrack-extra"}},
	"xt_addrtype":   {kernel: []string{"kmod-ipt-extra"}, userspace: []string{"iptables-mod-extra"}},
	"xt_DSCP":       {kernel: []string{"kmod-ipt-ipopt"}, userspace: []string{"iptables-mod-ipopt"}},
	"nft_fib_inet":  {kernel: []string{"kmod-nft-fib"}},
	"nft_fib":       {kernel: []string{"kmod-nft-fib"}},
	"iptable_raw":   {kernel: []string{"kmod-ipt-raw"}},
	"ip6table_raw":  {kernel: []string{"kmod-ipt-raw6"}},
	"xt_NFLOG":      {kernel: []string{"kmod-ipt-nflog"}, userspace: []string{"iptables-mod-nflog"}},
	"nfnetlink_log": {kernel: []string{"kmod-nfnetlink-log"}},
	"xt_conntrack":  {kernel: []string{"kmod-ipt-conntrack"}},

	"nf_conntrack_netlink": {kernel: []string{"kmod-nf-conntrack-netlink"}},
}

const kmodKeeneticHint = `Enable the "Kernel modules for Netfilter" component of KeeneticOS`

type kmodState int

const (
	kmodStateUnknown kmodState = iota
	kmodStateAbsent
	kmodStateBroken
	kmodStatePresent
)

const (
	KernelModuleLoaded    = "loaded"
	KernelModuleBuiltin   = "built-in"
	KernelModuleNotLoaded = "not loaded"
	KernelModuleNotFound  = "not found as a module"
)

type kmodProcEvidence struct {
	files []string
	entry string
}

var (
	kmodProcTargets = []string{"net/ip_tables_targets", "net/ip6_tables_targets"}
	kmodProcMatches = []string{"net/ip_tables_matches", "net/ip6_tables_matches"}
)

var kmodProcRegistry = map[string]kmodProcEvidence{
	"xt_NFQUEUE":   {files: kmodProcTargets, entry: "NFQUEUE"},
	"xt_TPROXY":    {files: kmodProcTargets, entry: "TPROXY"},
	"xt_CONNMARK":  {files: kmodProcTargets, entry: "CONNMARK"},
	"xt_DSCP":      {files: kmodProcTargets, entry: "DSCP"},
	"xt_NFLOG":     {files: kmodProcTargets, entry: "NFLOG"},
	"xt_CT":        {files: kmodProcTargets, entry: "CT"},
	"xt_mark":      {files: kmodProcMatches, entry: "mark"},
	"xt_connbytes": {files: kmodProcMatches, entry: "connbytes"},
	"xt_conntrack": {files: kmodProcMatches, entry: "conntrack"},
	"xt_multiport": {files: kmodProcMatches, entry: "multiport"},
	"xt_socket":    {files: kmodProcMatches, entry: "socket"},
	"xt_connmark":  {files: kmodProcMatches, entry: "connmark"},
	"xt_addrtype":  {files: kmodProcMatches, entry: "addrtype"},

	"tun":             {files: []string{"misc"}, entry: "200 tun"},
	"nfnetlink_queue": {files: []string{"net/netfilter/nfnetlink_queue"}},
	"nfnetlink_log":   {files: []string{"net/netfilter/nfnetlink_log"}},
	"nf_conntrack":    {files: []string{"sys/net/netfilter/nf_conntrack_count", "sys/net/netfilter/nf_conntrack_max"}},
}

var (
	kmodSysRoot   = "/sys/module"
	kmodLibRoot   = "/lib/modules"
	kmodProcRoot  = "/proc"
	kmodRelease   = unameRelease
	kmodDependsOf = kmodDepends
	kmodNow       = time.Now

	kmodIndexMu    sync.Mutex
	kmodIndex      map[string]string
	kmodBuiltins   map[string]bool
	kmodIndexBuilt time.Time

	kmodStateMu   sync.Mutex
	kmodLoadErrs  = map[string]string{}
	kmodRejected  = map[string]string{}
	kmodAttempted = map[string]bool{}
)

func unameRelease() string {
	var uts unix.Utsname
	if err := unix.Uname(&uts); err != nil {
		return ""
	}
	return unix.ByteSliceToString(uts.Release[:])
}

func kmodCanonical(name string) string {
	return strings.ReplaceAll(name, "-", "_")
}

func kmodLoaded(name string) bool {
	_, err := os.Stat(filepath.Join(kmodSysRoot, kmodCanonical(name)))
	return err == nil
}

func kmodRegistered(name string) bool {
	ev, ok := kmodProcRegistry[kmodCanonical(name)]
	if !ok {
		return false
	}
	for _, f := range ev.files {
		path := filepath.Join(kmodProcRoot, f)
		if ev.entry == "" {
			if _, err := os.Stat(path); err == nil {
				return true
			}
			continue
		}
		if kmodFileHasLine(path, ev.entry) {
			return true
		}
	}
	return false
}

func kmodFileHasLine(path, want string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

func kmodListedInProcModules(name string) bool {
	data, err := os.ReadFile(filepath.Join(kmodProcRoot, "modules"))
	if err != nil {
		return false
	}
	key := kmodCanonical(name)
	for line := range strings.SplitSeq(string(data), "\n") {
		if f := strings.Fields(line); len(f) > 0 && f[0] == key {
			return true
		}
	}
	return false
}

func kmodPresent(name string) bool {
	return kmodLoaded(name) || kmodBuiltin(name) || kmodRegistered(name)
}

func KernelModuleStatus(name string) string {
	switch {
	case kmodListedInProcModules(name):
		return KernelModuleLoaded
	case kmodPresent(name):
		return KernelModuleBuiltin
	case KernelModuleOnDisk(name):
		return KernelModuleNotLoaded
	}
	return KernelModuleNotFound
}

func kmodModulesDir() string {
	rel := kmodRelease()
	if rel == "" {
		return ""
	}
	return filepath.Join(kmodLibRoot, rel)
}

func kmodModuleKey(base string) (string, bool) {
	idx := strings.Index(base, ".ko")
	if idx <= 0 {
		return "", false
	}
	suffix := base[idx:]
	if suffix != ".ko" && !strings.HasPrefix(suffix, ".ko.") {
		return "", false
	}
	return kmodCanonical(base[:idx]), true
}

func kmodBuildIndex() (map[string]string, map[string]bool) {
	index := map[string]string{}
	builtins := map[string]bool{}
	dir := kmodModulesDir()
	if dir == "" {
		return index, builtins
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		key, ok := kmodModuleKey(d.Name())
		if !ok {
			return nil
		}
		if prev, ok := index[key]; ok && strings.HasSuffix(prev, ".ko") {
			return nil
		}
		index[key] = path
		return nil
	})
	if f, err := os.Open(filepath.Join(dir, "modules.builtin")); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if key, ok := kmodModuleKey(filepath.Base(strings.TrimSpace(sc.Text()))); ok {
				builtins[key] = true
			}
		}
	}
	return index, builtins
}

func kmodLookup(name string) (path string, builtin bool) {
	key := kmodCanonical(name)
	kmodIndexMu.Lock()
	defer kmodIndexMu.Unlock()
	if kmodIndex == nil {
		kmodIndex, kmodBuiltins = kmodBuildIndex()
		kmodIndexBuilt = kmodNow()
	}
	path, builtin = kmodIndex[key], kmodBuiltins[key]
	if path == "" && !builtin && kmodNow().Sub(kmodIndexBuilt) > kmodIndexRebuildTTL {
		kmodIndex, kmodBuiltins = kmodBuildIndex()
		kmodIndexBuilt = kmodNow()
		path, builtin = kmodIndex[key], kmodBuiltins[key]
	}
	return path, builtin
}

func kmodPath(name string) string {
	path, _ := kmodLookup(name)
	return path
}

func kmodBuiltin(name string) bool {
	_, builtin := kmodLookup(name)
	return builtin
}

func kmodDepends(path string) []string {
	if !strings.HasSuffix(path, ".ko") {
		return nil
	}
	f, err := elf.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	sec := f.Section(".modinfo")
	if sec == nil {
		return nil
	}
	data, err := sec.Data()
	if err != nil {
		return nil
	}
	return kmodParseDepends(data)
}

func kmodParseDepends(modinfo []byte) []string {
	var deps []string
	for _, entry := range strings.Split(string(modinfo), "\x00") {
		value, ok := strings.CutPrefix(entry, "depends=")
		if !ok {
			continue
		}
		for _, dep := range strings.Split(value, ",") {
			if dep = strings.TrimSpace(dep); dep != "" {
				deps = append(deps, dep)
			}
		}
	}
	return deps
}

func loadKernelModule(name string) error {
	err := loadKernelModuleAt(name, 0)
	key := kmodCanonical(name)
	kmodStateMu.Lock()
	kmodAttempted[key] = true
	if err != nil {
		kmodLoadErrs[key] = err.Error()
	} else {
		delete(kmodLoadErrs, key)
	}
	kmodStateMu.Unlock()
	return err
}

func loadKernelModuleList(names ...string) {
	for _, name := range names {
		if err := loadKernelModule(name); err != nil {
			log.Debugf("Kernel module %s: %v", name, err)
		}
	}
}

func loadKernelModuleAt(name string, depth int) error {
	if kmodLoaded(name) {
		return nil
	}
	probeOut, probeErr := run("modprobe", "-q", name)
	if probeErr == nil || kmodPresent(name) {
		return nil
	}
	path := kmodPath(name)
	if path == "" {
		dir := kmodModulesDir()
		if dir == "" {
			dir = kmodLibRoot
		}
		return fmt.Errorf("modprobe could not load it (%s) and there is no %s.ko under %s", kmodProbeReason(probeOut, probeErr), name, dir)
	}
	var depErrs []string
	if depth < kmodDepthLimit {
		for _, dep := range kmodDependsOf(path) {
			if err := loadKernelModuleAt(dep, depth+1); err != nil {
				depErrs = append(depErrs, fmt.Sprintf("%s: %v", dep, err))
			}
		}
	}
	out, err := run("insmod", path)
	if err == nil || strings.Contains(out, "File exists") || kmodLoaded(name) {
		log.Infof("Kernel module %s loaded with insmod from %s because modprobe could not find it", name, path)
		return nil
	}
	msg := fmt.Sprintf("modprobe could not load it (%s), %s is present but insmod failed: %s", kmodProbeReason(probeOut, probeErr), path, kmodCommandOutput(out, err))
	if len(depErrs) > 0 {
		msg += "; dependencies: " + strings.Join(depErrs, "; ")
	}
	return errors.New(msg)
}

func kmodProbeReason(out string, err error) string {
	if s := kmodCommandOutput(out, err); s != "" {
		return s
	}
	return "no module index for the running kernel"
}

func kmodCommandOutput(out string, err error) string {
	if s := strings.TrimSpace(out); s != "" {
		return s
	}
	if err == nil {
		return ""
	}
	if strings.Contains(err.Error(), "executable file not found") {
		return "command not found"
	}
	if inner := errors.Unwrap(err); inner != nil {
		err = inner
	}
	return strings.TrimSpace(err.Error())
}

func kmodNoteRejected(name, tool, out string) {
	msg := strings.TrimSpace(out)
	if msg == "" {
		msg = "no error text"
	}
	kmodStateMu.Lock()
	kmodRejected[kmodCanonical(name)] = tool + " rejected the rule: " + msg
	kmodStateMu.Unlock()
}

func KernelModuleLoadError(name string) string {
	kmodStateMu.Lock()
	defer kmodStateMu.Unlock()
	return kmodLoadErrs[kmodCanonical(name)]
}

func KernelModuleOnDisk(name string) bool {
	return kmodPath(name) != ""
}

func kmodStateOf(name string) kmodState {
	key := kmodCanonical(name)
	kmodStateMu.Lock()
	attempted, loadErr := kmodAttempted[key], kmodLoadErrs[key]
	kmodStateMu.Unlock()
	if kmodPresent(name) {
		return kmodStatePresent
	}
	onDisk := KernelModuleOnDisk(name)
	switch {
	case loadErr != "" && onDisk:
		return kmodStateBroken
	case loadErr != "":
		return kmodStateAbsent
	case attempted:
		return kmodStatePresent
	case onDisk:
		return kmodStateUnknown
	}
	return kmodStateAbsent
}

func kmodPkgsFor(missing []string) []string {
	if len(missing) == 0 || dnsKernelFacts().KeeneticOS {
		return nil
	}
	var out []string
	for _, m := range missing {
		set := kmodPackageTable[kmodCanonical(m)]
		switch kmodStateOf(m) {
		case kmodStateAbsent:
			out = append(out, set.kernel...)
			out = append(out, set.userspace...)
		case kmodStatePresent, kmodStateUnknown:
			out = append(out, set.userspace...)
		}
	}
	return utils.FilterUniqueStrings(out)
}

func KernelModulePackages(missing []string) []string {
	return kmodPkgsFor(missing)
}

func KernelModuleReasons(missing []string) []string {
	return kmodReasons(missing, true)
}

func kmodReasons(missing []string, withRejection bool) []string {
	reasons := make([]string, 0, len(missing))
	for _, m := range missing {
		key := kmodCanonical(m)
		kmodStateMu.Lock()
		loadErr, rejected := kmodLoadErrs[key], kmodRejected[key]
		kmodStateMu.Unlock()
		present := rejected != "" && kmodStateOf(m) == kmodStatePresent
		switch {
		case present && withRejection:
			reasons = append(reasons, m+": the kernel module is present, but "+rejected)
		case present:
			reasons = append(reasons, m+": the kernel module is present")
		case rejected != "" && withRejection && loadErr != "":
			reasons = append(reasons, m+": "+rejected+"; "+loadErr)
		case rejected != "" && withRejection:
			reasons = append(reasons, m+": "+rejected)
		case loadErr != "":
			reasons = append(reasons, m+": "+loadErr)
		}
	}
	return reasons
}

func kmodMissingHint(missing []string) string {
	return kmodHint(missing, true)
}

func kmodHintAfterRejection(missing []string) string {
	return kmodHint(missing, false)
}

func kmodHint(missing []string, withRejection bool) string {
	var parts []string
	if pkgs := kmodPkgsFor(missing); len(pkgs) > 0 {
		parts = append(parts, "Package(s) that may provide it: "+strings.Join(pkgs, " "))
	} else if len(missing) > 0 && dnsKernelFacts().KeeneticOS {
		parts = append(parts, kmodKeeneticHint)
	}
	parts = append(parts, kmodReasons(missing, withRejection)...)
	if len(parts) == 0 {
		return "The running kernel offers no such module"
	}
	return strings.Join(parts, "; ")
}
