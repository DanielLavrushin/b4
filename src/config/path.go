package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const extraBinTTL = 15 * time.Second

var (
	extraBinMu     sync.Mutex
	extraBinCached []string
	extraBinAt     time.Time
)

var standardBinPaths = []string{
	"/opt/sbin",
	"/opt/bin",
	"/usr/local/sbin",
	"/usr/local/bin",
	"/usr/sbin",
	"/usr/bin",
	"/sbin",
	"/bin",
}

var profileFiles = []string{
	"/etc/profile",
	"/etc/profile.d",
	"/opt/etc/profile",
	"/opt/etc/profile.d",
	"/jffs/configs/profile.add",
	"/root/.profile",
	"/root/.bashrc",
}

var mountBinSuffixes = []string{
	"/opt/sbin",
	"/opt/bin",
	"/usr/local/sbin",
	"/usr/local/bin",
	"/usr/sbin",
	"/usr/bin",
	"/sbin",
	"/bin",
}

var pseudoFilesystems = map[string]struct{}{
	"autofs": {}, "bpf": {}, "binfmt_misc": {}, "cgroup": {}, "cgroup2": {},
	"configfs": {}, "debugfs": {}, "devpts": {}, "devtmpfs": {}, "efivarfs": {},
	"fuse.gvfsd-fuse": {}, "fusectl": {}, "hugetlbfs": {}, "mqueue": {}, "nsfs": {},
	"proc": {}, "pstore": {}, "rpc_pipefs": {}, "securityfs": {}, "sysfs": {},
	"tracefs": {},
	"erofs": {}, "iso9660": {}, "overlay": {}, "squashfs": {}, "udf": {},
}

var skippedMountPrefixes = []string{
	"/boot/",
	"/nix/",
	"/run/",
	"/snap/",
	"/var/lib/docker/",
	"/var/lib/snapd/",
	"/var/snap/",
}

const (
	maxProfileFileSize = 256 * 1024
	maxExtraBinPaths   = 64
)

func ExtendedPATH(current string) string {
	return mergePaths(strings.Split(current, ":"), standardBinPaths)
}

func ApplyPATH() string {
	full := mergePaths(strings.Split(os.Getenv("PATH"), ":"), standardBinPaths, ExtraBinPaths())
	os.Setenv("PATH", full)
	return full
}

func ExtraBinPaths() []string {
	extraBinMu.Lock()
	defer extraBinMu.Unlock()

	if extraBinCached != nil && time.Since(extraBinAt) < extraBinTTL {
		return extraBinCached
	}

	extraBinCached = scanExtraBinPaths()
	extraBinAt = time.Now()
	return extraBinCached
}

func resetExtraBinCache() {
	extraBinMu.Lock()
	extraBinCached = nil
	extraBinAt = time.Time{}
	extraBinMu.Unlock()
}

func scanExtraBinPaths() []string {
	dirs := append(ProfileBinPaths(), MountBinPaths()...)

	seen := make(map[string]struct{}, len(dirs))
	out := make([]string, 0, len(dirs))
	for _, d := range dirs {
		if _, ok := seen[d]; ok {
			continue
		}
		seen[d] = struct{}{}
		out = append(out, d)
		if len(out) >= maxExtraBinPaths {
			break
		}
	}

	return out
}

func MountBinPaths() []string {
	var found []string
	seen := make(map[string]struct{})

	for _, mp := range mountPoints() {
		for _, suffix := range mountBinSuffixes {
			dir := mp + suffix
			if _, ok := seen[dir]; ok {
				continue
			}
			seen[dir] = struct{}{}
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				found = append(found, dir)
			}
		}
	}

	return found
}

var mountsFiles = []string{"/proc/self/mounts", "/proc/mounts"}

func mountPoints() []string {
	for _, f := range mountsFiles {
		if data, err := os.ReadFile(f); err == nil {
			return parseMountPoints(data)
		}
	}
	return nil
}

func parseMountPoints(data []byte) []string {
	var points []string
	seen := make(map[string]struct{})

	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		if _, skip := pseudoFilesystems[fields[2]]; skip {
			continue
		}
		mp := unescapeMountPoint(fields[1])
		if mp == "/" || !strings.HasPrefix(mp, "/") {
			continue
		}
		if strings.HasPrefix(mp, "/proc/") || strings.HasPrefix(mp, "/sys/") || strings.HasPrefix(mp, "/dev/") {
			continue
		}
		if hasSkippedMountPrefix(mp) {
			continue
		}
		mp = strings.TrimSuffix(filepath.Clean(mp), "/")
		if _, ok := seen[mp]; ok {
			continue
		}
		seen[mp] = struct{}{}
		points = append(points, mp)
	}

	return points
}

func hasSkippedMountPrefix(mp string) bool {
	for _, prefix := range skippedMountPrefixes {
		if strings.HasPrefix(mp, prefix) {
			return true
		}
	}
	return false
}

var mountUnescaper = strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)

func unescapeMountPoint(s string) string {
	return mountUnescaper.Replace(s)
}

func LookupTool(name string) (string, bool) {
	if path, err := exec.LookPath(name); err == nil {
		return path, true
	}

	for _, dir := range ExtraBinPaths() {
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() || info.Mode()&0111 == 0 {
			continue
		}
		return candidate, true
	}

	return "", false
}

func ProfileBinPaths() []string {
	var found []string
	seen := make(map[string]struct{})

	for _, entry := range profileCandidates() {
		for _, dir := range parseProfilePaths(entry) {
			if _, ok := seen[dir]; ok {
				continue
			}
			seen[dir] = struct{}{}
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				found = append(found, dir)
			}
		}
	}

	return found
}

func profileCandidates() []string {
	var files []string
	add := func(p string) {
		info, err := os.Stat(p)
		if err != nil {
			return
		}
		if !info.IsDir() {
			files = append(files, p)
			return
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.IsDir() {
				files = append(files, filepath.Join(p, e.Name()))
			}
		}
	}

	for _, p := range profileFiles {
		add(p)
	}
	if home := os.Getenv("HOME"); home != "" && home != "/root" {
		add(filepath.Join(home, ".profile"))
		add(filepath.Join(home, ".bashrc"))
	}

	return files
}

func parseProfilePaths(file string) []string {
	info, err := os.Stat(file)
	if err != nil || info.Size() > maxProfileFileSize {
		return nil
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil
	}

	var dirs []string
	for _, line := range strings.Split(string(data), "\n") {
		value, ok := pathAssignment(line)
		if !ok {
			continue
		}
		for _, dir := range strings.Split(value, ":") {
			if dir := expandPathEntry(dir); dir != "" {
				dirs = append(dirs, dir)
			}
		}
	}

	return dirs
}

func pathAssignment(line string) (string, bool) {
	s := strings.TrimSpace(line)
	if i := strings.Index(s, "#"); i == 0 {
		return "", false
	}
	s = strings.TrimPrefix(s, "export ")
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "PATH=") {
		return "", false
	}
	s = strings.TrimPrefix(s, "PATH=")

	s = strings.TrimSpace(s)
	if quote := strings.IndexAny(s, "\"'"); quote == 0 {
		if end := strings.IndexByte(s[1:], s[0]); end >= 0 {
			s = s[:end+2]
		}
	} else if i := strings.IndexAny(s, " \t;"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && strings.HasSuffix(s, "\"")) || (s[0] == '\'' && strings.HasSuffix(s, "'")) {
			s = s[1 : len(s)-1]
		}
	}
	if s == "" {
		return "", false
	}
	return s, true
}

func expandPathEntry(entry string) string {
	e := strings.TrimSpace(entry)
	if e == "" {
		return ""
	}
	if home := os.Getenv("HOME"); home != "" {
		e = strings.ReplaceAll(e, "${HOME}", home)
		e = strings.ReplaceAll(e, "$HOME", home)
	}
	if strings.Contains(e, "$") || strings.ContainsAny(e, "`*?") {
		return ""
	}
	if !strings.HasPrefix(e, "/") {
		return ""
	}
	return filepath.Clean(e)
}

func mergePaths(lists ...[]string) string {
	seen := make(map[string]struct{})
	parts := make([]string, 0, 16)

	for _, list := range lists {
		for _, p := range list {
			if p == "" {
				continue
			}
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			parts = append(parts, p)
		}
	}

	return strings.Join(parts, ":")
}
