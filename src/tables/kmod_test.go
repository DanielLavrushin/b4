package tables

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type kmodFixture struct {
	sysRoot string
	relDir  string
	modDir  string
	calls   []string
	now     time.Time
}

func newKmodFixture(t *testing.T) *kmodFixture {
	t.Helper()
	fx := &kmodFixture{now: time.Unix(1_700_000_000, 0)}
	root := t.TempDir()
	fx.sysRoot = filepath.Join(root, "sys")
	lib := filepath.Join(root, "lib")
	fx.relDir = filepath.Join(lib, "4.9-ndm-5")
	fx.modDir = filepath.Join(fx.relDir, "kernel", "net", "netfilter")
	if err := os.MkdirAll(fx.sysRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fx.modDir, 0o755); err != nil {
		t.Fatal(err)
	}

	origSys, origLib, origRel, origRun, origDeps, origNow := kmodSysRoot, kmodLibRoot, kmodRelease, run, kmodDependsOf, kmodNow
	kmodSysRoot = fx.sysRoot
	kmodLibRoot = lib
	kmodRelease = func() string { return "4.9-ndm-5" }
	kmodDependsOf = func(string) []string { return nil }
	kmodNow = func() time.Time { return fx.now }
	kmodResetForTest()
	t.Cleanup(func() {
		kmodSysRoot, kmodLibRoot, kmodRelease, run, kmodDependsOf, kmodNow = origSys, origLib, origRel, origRun, origDeps, origNow
		kmodResetForTest()
	})
	return fx
}

func kmodResetForTest() {
	kmodIndexMu.Lock()
	kmodIndex, kmodBuiltins = nil, nil
	kmodIndexBuilt = time.Time{}
	kmodIndexMu.Unlock()
	kmodStateMu.Lock()
	kmodLoadErrs = map[string]string{}
	kmodRejected = map[string]string{}
	kmodAttempted = map[string]bool{}
	kmodStateMu.Unlock()
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

func (fx *kmodFixture) builtin(names ...string) {
	var lines []string
	for _, n := range names {
		lines = append(lines, "kernel/net/netfilter/"+n+".ko")
	}
	if err := os.WriteFile(filepath.Join(fx.relDir, "modules.builtin"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
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

func TestKmodBuiltinCountsAsPresent(t *testing.T) {
	fx := newKmodFixture(t)
	fx.builtin("xt_socket")
	fx.stubRun(false, "")
	if err := loadKernelModule("xt_socket"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(fx.calls, "|") != "modprobe -q xt_socket" {
		t.Fatalf("calls for a built-in module = %v, want a single modprobe and no insmod", fx.calls)
	}
	if got := kmodStateOf("xt_socket"); got != kmodStatePresent {
		t.Fatalf("state = %v, want present", got)
	}
	if pkgs := kmodPkgsFor([]string{"xt_socket"}); strings.Join(pkgs, " ") != "iptables-mod-tproxy" {
		t.Fatalf("pkgs = %v", pkgs)
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
	if got := kmodStateOf("xt_TPROXY"); got != kmodStatePresent {
		t.Fatalf("state = %v, want present after a successful modprobe", got)
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

func TestKmodAbsentModuleGetsKernelAndUserspacePackages(t *testing.T) {
	fx := newKmodFixture(t)
	fx.stubRun(false, "")
	err := loadKernelModule("xt_socket")
	if err == nil {
		t.Fatal("expected an error for a module that is nowhere on disk")
	}
	if !strings.Contains(err.Error(), "no xt_socket.ko under") || !strings.Contains(err.Error(), "4.9-ndm-5") {
		t.Fatalf("error = %v", err)
	}
	if got := kmodStateOf("xt_socket"); got != kmodStateAbsent {
		t.Fatalf("state = %v, want absent", got)
	}
	if pkgs := kmodPkgsFor([]string{"xt_socket"}); strings.Join(pkgs, " ") != "kmod-ipt-socket iptables-mod-tproxy" {
		t.Fatalf("pkgs = %v", pkgs)
	}
	if got := KernelModuleReasons([]string{"xt_socket"}); len(got) != 1 || !strings.HasPrefix(got[0], "xt_socket: modprobe could not load it") {
		t.Fatalf("reasons = %v", got)
	}
}

func TestKmodBrokenModuleGetsNoPackagesAndKeepsInsmodError(t *testing.T) {
	fx := newKmodFixture(t)
	path := fx.module("xt_NFQUEUE.ko")
	fx.stubRun(false, "insmod: can't insert '"+path+"': invalid module format")
	err := loadKernelModule("xt_NFQUEUE")
	if err == nil {
		t.Fatal("expected insmod failure to surface")
	}
	if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "insmod failed") || !strings.Contains(err.Error(), "invalid module format") {
		t.Fatalf("error = %v", err)
	}
	if got := kmodStateOf("xt_NFQUEUE"); got != kmodStateBroken {
		t.Fatalf("state = %v, want broken", got)
	}
	if pkgs := kmodPkgsFor([]string{"xt_NFQUEUE"}); len(pkgs) != 0 {
		t.Fatalf("package hint offered for a module that is on disk but does not load: %v", pkgs)
	}
	if !strings.Contains(kmodMissingHint([]string{"xt_NFQUEUE"}), "insmod failed") {
		t.Fatal("hint lacks the insmod reason")
	}
}

func TestKmodMissingHintKeepsReasonsNextToPackages(t *testing.T) {
	fx := newKmodFixture(t)
	path := fx.module("xt_TPROXY.ko")
	fx.stubRun(false, "insmod: can't insert '"+path+"': invalid module format")
	_ = loadKernelModule("xt_TPROXY")
	_ = loadKernelModule("xt_socket")
	hint := kmodMissingHint([]string{"xt_socket", "xt_TPROXY"})
	if !strings.HasPrefix(hint, "Package(s) that may provide it: kmod-ipt-socket iptables-mod-tproxy") {
		t.Fatalf("hint = %q", hint)
	}
	if !strings.Contains(hint, "xt_TPROXY: ") || !strings.Contains(hint, "invalid module format") {
		t.Fatalf("hint dropped the insmod failure: %q", hint)
	}
}

func TestKmodLoadedButRejectedGetsUserspacePackagesAndToolError(t *testing.T) {
	fx := newKmodFixture(t)
	fx.loaded("xt_socket")
	fx.loaded("xt_NFQUEUE")
	fx.loaded("nft_ct")
	kmodNoteRejected("xt_socket", "iptables", "iptables: No chain/target/match by that name.")
	kmodNoteRejected("xt_NFQUEUE", "iptables", "iptables v1.8.7: Couldn't load target `NFQUEUE'")
	kmodNoteRejected("nft_ct", "nft", "Error: syntax error, unexpected packets")
	pkgs := kmodPkgsFor([]string{"xt_socket", "xt_NFQUEUE", "nft_ct"})
	if strings.Join(pkgs, " ") != "iptables-mod-tproxy iptables-mod-nfqueue" {
		t.Fatalf("pkgs = %v", pkgs)
	}
	reasons := KernelModuleReasons([]string{"xt_socket", "nft_ct"})
	if len(reasons) != 2 {
		t.Fatalf("reasons = %v", reasons)
	}
	if !strings.HasPrefix(reasons[0], "xt_socket: the kernel module is present, but iptables rejected the rule: iptables: No chain") {
		t.Fatalf("reason = %q", reasons[0])
	}
	if !strings.Contains(reasons[1], "nft rejected the rule: Error: syntax error") || strings.Contains(reasons[1], "extension") {
		t.Fatalf("reason = %q", reasons[1])
	}
}

func TestKmodPkgsForDeduplicates(t *testing.T) {
	fx := newKmodFixture(t)
	fx.stubRun(false, "")
	loadKernelModuleList("xt_connmark", "xt_CONNMARK")
	pkgs := kmodPkgsFor([]string{"xt_connmark", "xt_CONNMARK"})
	if strings.Join(pkgs, " ") != "kmod-ipt-conntrack-extra iptables-mod-conntrack-extra" {
		t.Fatalf("pkgs = %v", pkgs)
	}
	if pkgs := kmodPkgsFor(nil); pkgs != nil {
		t.Fatalf("pkgs for nothing = %v", pkgs)
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

func TestKmodIndexRebuildsAfterMissWhenStale(t *testing.T) {
	fx := newKmodFixture(t)
	if got := kmodPath("xt_TPROXY"); got != "" {
		t.Fatalf("unexpected path %q", got)
	}
	path := fx.module("xt_TPROXY.ko")
	if got := kmodPath("xt_TPROXY"); got != "" {
		t.Fatalf("index rebuilt before the rebuild interval passed: %q", got)
	}
	fx.now = fx.now.Add(kmodIndexRebuildTTL + time.Second)
	if got := kmodPath("xt_TPROXY"); got != path {
		t.Fatalf("path = %q, want %q after the interval", got, path)
	}
}
