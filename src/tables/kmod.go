package tables

import (
	"debug/elf"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/daniellavrushin/b4/log"
	"golang.org/x/sys/unix"
)

const kmodDepthLimit = 8

var (
	kmodSysRoot   = "/sys/module"
	kmodLibRoot   = "/lib/modules"
	kmodRelease   = unameRelease
	kmodDependsOf = kmodDepends

	kmodIndexMu sync.Mutex
	kmodIndex   map[string]string

	kmodErrMu sync.Mutex
	kmodErrs  = map[string]string{}
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

func kmodModulesDir() string {
	rel := kmodRelease()
	if rel == "" {
		return ""
	}
	return filepath.Join(kmodLibRoot, rel)
}

func kmodBuildIndex() map[string]string {
	index := map[string]string{}
	dir := kmodModulesDir()
	if dir == "" {
		return index
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		base := d.Name()
		idx := strings.Index(base, ".ko")
		if idx <= 0 {
			return nil
		}
		suffix := base[idx:]
		if suffix != ".ko" && !strings.HasPrefix(suffix, ".ko.") {
			return nil
		}
		key := kmodCanonical(base[:idx])
		if prev, ok := index[key]; ok && strings.HasSuffix(prev, ".ko") {
			return nil
		}
		index[key] = path
		return nil
	})
	return index
}

func kmodPath(name string) string {
	kmodIndexMu.Lock()
	defer kmodIndexMu.Unlock()
	if kmodIndex == nil {
		kmodIndex = kmodBuildIndex()
	}
	return kmodIndex[kmodCanonical(name)]
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
	kmodErrMu.Lock()
	if err != nil {
		kmodErrs[kmodCanonical(name)] = err.Error()
	} else {
		delete(kmodErrs, kmodCanonical(name))
	}
	kmodErrMu.Unlock()
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
	if probeErr == nil {
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
	return fmt.Errorf("%s", msg)
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

func KernelModuleLoadError(name string) string {
	kmodErrMu.Lock()
	defer kmodErrMu.Unlock()
	return kmodErrs[kmodCanonical(name)]
}

func KernelModuleOnDisk(name string) bool {
	return kmodPath(name) != ""
}

func kmodPkgsFor(missing []string, pkgs func(module string) []string) []string {
	out := make([]string, 0, len(missing))
	for _, m := range missing {
		if KernelModuleOnDisk(m) {
			continue
		}
		out = append(out, pkgs(m)...)
	}
	return out
}

func KernelModuleReasons(missing []string) []string {
	reasons := make([]string, 0, len(missing))
	for _, m := range missing {
		if reason := KernelModuleLoadError(m); reason != "" {
			reasons = append(reasons, m+": "+reason)
		}
	}
	return reasons
}

func kmodMissingHint(missing, pkgs []string) string {
	var parts []string
	if len(pkgs) > 0 {
		parts = append(parts, "Required package(s): "+strings.Join(pkgs, " "))
	}
	parts = append(parts, KernelModuleReasons(missing)...)
	if len(parts) == 0 {
		return "The running kernel offers no such module"
	}
	return strings.Join(parts, "; ")
}
