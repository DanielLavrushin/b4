package tables

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type kmodFixture struct {
	sysRoot string
	modDir  string
	calls   []string
}

func newKmodFixture(t *testing.T) *kmodFixture {
	t.Helper()
	fx := &kmodFixture{}
	root := t.TempDir()
	fx.sysRoot = filepath.Join(root, "sys")
	lib := filepath.Join(root, "lib")
	fx.modDir = filepath.Join(lib, "4.9-ndm-5", "kernel", "net", "netfilter")
	if err := os.MkdirAll(fx.sysRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fx.modDir, 0o755); err != nil {
		t.Fatal(err)
	}

	origSys, origLib, origRel, origRun, origDeps := kmodSysRoot, kmodLibRoot, kmodRelease, run, kmodDependsOf
	kmodSysRoot = fx.sysRoot
	kmodLibRoot = lib
	kmodRelease = func() string { return "4.9-ndm-5" }
	kmodDependsOf = func(string) []string { return nil }
	kmodResetForTest()
	t.Cleanup(func() {
		kmodSysRoot, kmodLibRoot, kmodRelease, run, kmodDependsOf = origSys, origLib, origRel, origRun, origDeps
		kmodResetForTest()
	})
	return fx
}

func kmodResetForTest() {
	kmodIndexMu.Lock()
	kmodIndex = nil
	kmodIndexMu.Unlock()
	kmodErrMu.Lock()
	kmodErrs = map[string]string{}
	kmodErrMu.Unlock()
}

func (fx *kmodFixture) module(name string) string {
	path := filepath.Join(fx.modDir, name)
	if err := os.WriteFile(path, []byte("ko"), 0o644); err != nil {
		panic(err)
	}
	return path
}

func (fx *kmodFixture) loaded(name string) {
	if err := os.MkdirAll(filepath.Join(fx.sysRoot, name), 0o755); err != nil {
		panic(err)
	}
}

func (fx *kmodFixture) stubRun(modprobeOK bool, insmodErr string) {
	run = func(args ...string) (string, error) {
		fx.calls = append(fx.calls, strings.Join(args, " "))
		switch args[0] {
		case "modprobe":
			if modprobeOK {
				return "", nil
			}
			return "modprobe: ERROR: Module " + args[2] + " not found.", errors.New("exit status 1")
		case "insmod":
			if insmodErr != "" {
				return insmodErr, errors.New("exit status 1")
			}
			return "", nil
		}
		return "", nil
	}
}

func TestKmodParseDepends(t *testing.T) {
	blob := []byte("license=GPL\x00depends=nf_tproxy_ipv4,nf_tproxy_ipv6, nf_defrag_ipv4\x00vermagic=4.19.294 SMP\x00")
	got := kmodParseDepends(blob)
	want := []string{"nf_tproxy_ipv4", "nf_tproxy_ipv6", "nf_defrag_ipv4"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("depends = %v, want %v", got, want)
	}
	if got := kmodParseDepends([]byte("depends=\x00")); len(got) != 0 {
		t.Fatalf("empty depends parsed as %v", got)
	}
}

func TestKmodAlreadyLoadedSkipsCommands(t *testing.T) {
	fx := newKmodFixture(t)
	fx.loaded("xt_socket")
	fx.stubRun(false, "")
	if err := loadKernelModule("xt_socket"); err != nil {
		t.Fatal(err)
	}
	if len(fx.calls) != 0 {
		t.Fatalf("commands run for a loaded module: %v", fx.calls)
	}
}

func TestKmodModprobeSuccessNeedsNoFallback(t *testing.T) {
	fx := newKmodFixture(t)
	fx.stubRun(true, "")
	if err := loadKernelModule("xt_TPROXY"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(fx.calls, "|") != "modprobe -q xt_TPROXY" {
		t.Fatalf("calls = %v", fx.calls)
	}
	if KernelModuleLoadError("xt_TPROXY") != "" {
		t.Fatal("load error recorded after success")
	}
}

func TestKmodInsmodFallbackLoadsDependenciesFirst(t *testing.T) {
	fx := newKmodFixture(t)
	socket := fx.module("xt_socket.ko")
	defrag := fx.module("nf_defrag_ipv4.ko")
	kmodDependsOf = func(path string) []string {
		if path == socket {
			return []string{"nf_defrag_ipv4", "nf_conntrack"}
		}
		return nil
	}
	fx.loaded("nf_conntrack")
	fx.stubRun(false, "")
	if err := loadKernelModule("xt_socket"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"modprobe -q xt_socket",
		"modprobe -q nf_defrag_ipv4",
		"insmod " + defrag,
		"insmod " + socket,
	}
	if strings.Join(fx.calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls = %v\nwant %v", fx.calls, want)
	}
}

func TestKmodMissingModuleReportsPackages(t *testing.T) {
	fx := newKmodFixture(t)
	fx.stubRun(false, "")
	err := loadKernelModule("xt_socket")
	if err == nil {
		t.Fatal("expected an error for a module that is nowhere on disk")
	}
	if !strings.Contains(err.Error(), "no xt_socket.ko under") || !strings.Contains(err.Error(), "4.9-ndm-5") {
		t.Fatalf("error = %v", err)
	}
	if KernelModuleOnDisk("xt_socket") {
		t.Fatal("module reported on disk")
	}
	pkgs := tproxyPkgsFor([]string{"xt_socket"})
	if strings.Join(pkgs, " ") != "kmod-ipt-socket" {
		t.Fatalf("pkgs = %v", pkgs)
	}
	if got := KernelModuleReasons([]string{"xt_socket"}); len(got) != 1 || !strings.HasPrefix(got[0], "xt_socket: ") {
		t.Fatalf("reasons = %v", got)
	}
}

func TestKmodInsmodFailureKeepsReasonAndDropsPackageHint(t *testing.T) {
	fx := newKmodFixture(t)
	path := fx.module("xt_TPROXY.ko")
	fx.stubRun(false, "insmod: can't insert '"+path+"': unknown symbol in module")
	err := loadKernelModule("xt_TPROXY")
	if err == nil {
		t.Fatal("expected insmod failure to surface")
	}
	if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "insmod failed") || !strings.Contains(err.Error(), "unknown symbol") {
		t.Fatalf("error = %v", err)
	}
	if pkgs := tproxyPkgsFor([]string{"xt_TPROXY"}); len(pkgs) != 0 {
		t.Fatalf("package hint offered for a module that is already on disk: %v", pkgs)
	}
	if !strings.Contains(kmodMissingHint([]string{"xt_TPROXY"}, nil), "insmod failed") {
		t.Fatal("hint lacks the insmod reason")
	}
}

func TestKmodMissingHintKeepsReasonsNextToPackages(t *testing.T) {
	fx := newKmodFixture(t)
	path := fx.module("xt_TPROXY.ko")
	fx.stubRun(false, "insmod: can't insert '"+path+"': invalid module format")
	_ = loadKernelModule("xt_TPROXY")
	_ = loadKernelModule("xt_socket")
	missing := []string{"xt_socket", "xt_TPROXY"}
	hint := kmodMissingHint(missing, tproxyPkgsFor(missing))
	if !strings.HasPrefix(hint, "Required package(s): kmod-ipt-socket") {
		t.Fatalf("hint = %q", hint)
	}
	if !strings.Contains(hint, "xt_TPROXY: ") || !strings.Contains(hint, "invalid module format") {
		t.Fatalf("hint dropped the insmod failure: %q", hint)
	}
}

func TestKmodInsmodFileExistsCountsAsLoaded(t *testing.T) {
	fx := newKmodFixture(t)
	fx.module("xt_TPROXY.ko")
	fx.stubRun(false, "insmod: can't insert: File exists")
	if err := loadKernelModule("xt_TPROXY"); err != nil {
		t.Fatal(err)
	}
}

func TestKmodIndexPrefersUncompressedAndCanonicalisesNames(t *testing.T) {
	fx := newKmodFixture(t)
	fx.module("nf_defrag_ipv4.ko.gz")
	plain := fx.module("nf_defrag_ipv4.ko")
	other := filepath.Join(fx.modDir, "..", "ipv4")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	dashed := filepath.Join(other, "nf-dup-ipv4.ko.xz")
	if err := os.WriteFile(dashed, []byte("ko"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fx.modDir, "README.kobold"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := kmodPath("nf_defrag_ipv4"); got != plain {
		t.Fatalf("path = %q, want %q", got, plain)
	}
	if got := kmodPath("nf_dup_ipv4"); got != dashed {
		t.Fatalf("path = %q, want %q", got, dashed)
	}
	if got := kmodPath("README"); got != "" {
		t.Fatalf("non-module file indexed as %q", got)
	}
}
