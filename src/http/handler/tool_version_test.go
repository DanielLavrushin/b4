package handler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/geodat"
)

func TestParseToolVersionKeepsWhatTheToolReports(t *testing.T) {
	cases := []struct {
		name, stdout, stderr string
		succeeded            bool
		want                 string
	}{
		{"iptables", "iptables v1.8.11 (nf_tables)\n", "", true, "v1.8.11 (nf_tables)"},
		{"iptables-legacy", "iptables v1.8.11 (legacy)\n", "", true, "v1.8.11 (legacy)"},
		{"iptables", "iptables v1.4.14\n", "", true, "v1.4.14"},
		{"nft", "nftables v1.1.6 (Commodore Bullmoose #7)\n", "", true, "v1.1.6 (Commodore Bullmoose #7)"},
		{"tar", "tar (GNU tar) 1.35\nCopyright (C) 2023 Free Software Foundation, Inc.\n", "", true, "(GNU tar) 1.35"},
		{"curl", "curl 8.4.0 (aarch64-openwrt-linux-gnu) libcurl/8.4.0 mbedTLS/2.28.5\nRelease-Date: 2023-10-11\n", "", true, "8.4.0 (aarch64-openwrt-linux-gnu) libcurl/8.4.0 mbedTLS/2.28.5"},
		{"jq", "jq-1.8.1\n", "", true, "1.8.1"},
		{"sha256sum", "sha256sum (uutils coreutils) 0.10.0\n", "", true, "(uutils coreutils) 0.10.0"},
		{"modprobe", "kmod version 34.2\n+ZSTD +XZ -ZLIB +OPENSSL\n", "", true, "kmod version 34.2"},
		{"wget", "GNU Wget 1.25.0 built on linux-gnu.\n\n-cares +digest\n", "", true, "GNU Wget 1.25.0 built on linux-gnu."},
		{"ipset", "ipset v7.19, protocol version: 7\n", "ipset v7.19: Kernel error received: Operation not permitted\n", false, "v7.19, protocol version: 7"},
		{"tar", "", "BusyBox v1.36.1 (2024-09-23 12:34:46 UTC) multi-call binary.\n\nUsage: tar c|x|t [-zvf] [FILE]...\n", false, "BusyBox v1.36.1"},
		{"nohup", "", "nohup: can't execute '--version': No such file or directory\n", false, ""},
		{"foo", "", "foo 2.1\n", true, "2.1"},
		{"foo", "", "foo: unknown option -- 2\n", false, ""},
		{"foo", "usage: foo [-h]\n", "", true, ""},
	}
	for _, c := range cases {
		if got := parseToolVersion(c.name, c.stdout, c.stderr, c.succeeded); got != c.want {
			t.Errorf("%s with stdout %q and stderr %q = %q, want %q", c.name, c.stdout, c.stderr, got, c.want)
		}
	}
}

func TestParseToolVersionCapsALongLine(t *testing.T) {
	got := parseToolVersion("curl", "curl 8.18.0 "+strings.Repeat("libfoo/1.0 ", 100), "", true)
	if n := len([]rune(got)); n != toolVersionMaxLen {
		t.Errorf("version is %d characters long, want %d", n, toolVersionMaxLen)
	}
}

func writeTool(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0755); err != nil {
		t.Fatal(err)
	}
	return p
}

func trustToolDir(t *testing.T, dir string) {
	t.Helper()
	old := toolVersionTrusted
	toolVersionTrusted = func(path string) bool { return filepath.Dir(path) == dir || old(path) }
	t.Cleanup(func() { toolVersionTrusted = old })
}

const echoArgv0Env = "B4_TEST_ECHO_ARGV0"

func init() {
	if os.Getenv(echoArgv0Env) != "" {
		fmt.Println(filepath.Base(os.Args[0]) + " v1.0")
		os.Exit(0)
	}
}

func resolvedTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func fakeDrives(t *testing.T, self string, drives map[string]uint64) {
	t.Helper()
	oldExecutable, oldDevice := ownExecutable, fileDevice
	ownExecutable = func() (string, error) { return self, nil }
	fileDevice = func(path string) (uint64, bool) {
		var dev uint64
		longest := -1
		for mount, d := range drives {
			inside := path == mount || strings.HasPrefix(path, strings.TrimSuffix(mount, "/")+"/")
			if inside && len(mount) > longest {
				dev, longest = d, len(mount)
			}
		}
		return dev, longest >= 0
	}
	t.Cleanup(func() { ownExecutable, fileDevice = oldExecutable, oldDevice })
}

func recordingTool(t *testing.T, dir, name string) (string, string) {
	t.Helper()
	calls := filepath.Join(dir, name+".calls")
	return writeTool(t, dir, name, "echo run >> '"+calls+"'\necho "+name+"-1.0\n"), calls
}

func assertToolNotRun(t *testing.T, name, path, calls string) {
	t.Helper()
	if got := newToolVersions().of(name, path); got != "" {
		t.Errorf("version of %s = %q, want none", path, got)
	}
	if _, err := os.Stat(calls); !os.IsNotExist(err) {
		t.Errorf("ran %s", path)
	}
}

func TestToolVersionsDoesNotRunAToolOnAnotherDrive(t *testing.T) {
	usb, other := resolvedTempDir(t), resolvedTempDir(t)
	fakeDrives(t, filepath.Join(usb, "b4", "b4"), map[string]uint64{"/": 1, usb: 2, other: 3})
	jq, calls := recordingTool(t, other, "jq")
	assertToolNotRun(t, "jq", jq, calls)
}

func TestToolVersionsDoesNotRunALinkOnAnotherDriveToB4sDrive(t *testing.T) {
	usb, other := resolvedTempDir(t), resolvedTempDir(t)
	fakeDrives(t, filepath.Join(usb, "b4", "b4"), map[string]uint64{"/": 1, usb: 2, other: 3})
	target, calls := recordingTool(t, usb, "jq")
	link := filepath.Join(other, "jq")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	assertToolNotRun(t, "jq", link, calls)
}

func TestToolVersionsDoesNotTrustTheRootFilesystemAsB4sDrive(t *testing.T) {
	fakeDrives(t, "/usr/bin/b4", map[string]uint64{"/": 1})
	jq, calls := recordingTool(t, resolvedTempDir(t), "jq")
	assertToolNotRun(t, "jq", jq, calls)
}

func TestToolVersionsAsksAToolOnTheDriveB4RunsFrom(t *testing.T) {
	usb := resolvedTempDir(t)
	fakeDrives(t, filepath.Join(usb, "b4", "b4"), map[string]uint64{"/": 1, usb: 2})
	jq := writeTool(t, usb, "jq", "echo jq-1.8.1\n")

	if got := newToolVersions().of("jq", jq); got != "1.8.1" {
		t.Errorf("version = %q, want 1.8.1 for a tool on the drive b4 runs from", got)
	}
}

func TestToolVersionsRunsTheFileALinkOnB4sDriveResolvesTo(t *testing.T) {
	usb := resolvedTempDir(t)
	fakeDrives(t, filepath.Join(usb, "b4", "b4"), map[string]uint64{"/": 1, usb: 2})
	ran := filepath.Join(usb, "ran")
	target := writeTool(t, usb, "sha256sum-coreutils", "echo \"$0\" > '"+ran+"'\necho 'sha256sum (GNU coreutils) 8.23'\n")
	link := filepath.Join(usb, "sha256sum")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if got := newToolVersions().of("sha256sum", link); got != "(GNU coreutils) 8.23" {
		t.Errorf("version = %q, want (GNU coreutils) 8.23", got)
	}
	data, err := os.ReadFile(ran)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != target {
		t.Errorf("ran %s, want the file the link resolves to, %s, so the link cannot be swapped before it runs", got, target)
	}
}

func TestToolVersionsRunsAMultiCallBinaryOnB4sDriveUnderItsOwnName(t *testing.T) {
	self, err := os.Executable()
	if err == nil {
		self, err = filepath.EvalSymlinks(self)
	}
	if err != nil {
		t.Fatal(err)
	}
	usb := resolvedTempDir(t)
	link := filepath.Join(usb, "iptables")
	if err := os.Symlink(self, link); err != nil {
		t.Fatal(err)
	}
	fakeDrives(t, filepath.Join(usb, "b4", "b4"), map[string]uint64{"/": 1, usb: 2, self: 2})
	t.Setenv(echoArgv0Env, "1")

	if got := newToolVersions().of("iptables", link); got != "v1.0" {
		t.Errorf("version = %q, want v1.0 from a binary that answers to the name it was run as", got)
	}
}

func TestToolVersionsAsksBusyboxOnceForAllItsApplets(t *testing.T) {
	dir := t.TempDir()
	trustToolDir(t, dir)
	calls := filepath.Join(dir, "calls")
	busybox := writeTool(t, dir, "busybox", "echo run >> '"+calls+"'\n"+
		"[ $# -eq 0 ] && echo 'BusyBox v1.36.1 (2024-09-23 12:34:46 UTC) multi-call binary.'\n")
	applets := []string{"tar", "wget"}
	for _, applet := range applets {
		if err := os.Symlink(busybox, filepath.Join(dir, applet)); err != nil {
			t.Fatal(err)
		}
	}

	versions := newToolVersions()
	for _, applet := range applets {
		if got := versions.of(applet, filepath.Join(dir, applet)); got != "BusyBox v1.36.1" {
			t.Errorf("%s = %q, want BusyBox v1.36.1", applet, got)
		}
	}
	data, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(data), "run"); n != 1 {
		t.Errorf("busybox ran %d times for %d applets, want once", n, len(applets))
	}
}

func TestToolVersionsRunsAMultiCallBinaryUnderItsOwnName(t *testing.T) {
	dir := t.TempDir()
	trustToolDir(t, dir)
	multi := writeTool(t, dir, "xtables-nft-multi", "[ \"$1\" = --version ] && echo \"$(basename \"$0\") v1.8.11 (nf_tables)\"\n")
	link := filepath.Join(dir, "iptables")
	if err := os.Symlink(multi, link); err != nil {
		t.Fatal(err)
	}

	if got := newToolVersions().of("iptables", link); got != "v1.8.11 (nf_tables)" {
		t.Errorf("version = %q, want v1.8.11 (nf_tables)", got)
	}
}

func TestToolVersionsGivesUpOnAToolThatHangs(t *testing.T) {
	old := toolVersionTimeout
	toolVersionTimeout = 100 * time.Millisecond
	defer func() { toolVersionTimeout = old }()

	dir := t.TempDir()
	trustToolDir(t, dir)
	hang := writeTool(t, dir, "hang", "exec sleep 10\n")
	start := time.Now()
	if got := newToolVersions().of("hang", hang); got != "" {
		t.Errorf("version = %q, want none", got)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("waited %v for a tool that never answers", elapsed)
	}
}

func TestToolVersionsStopsAskingOnceTheBudgetIsSpent(t *testing.T) {
	oldTimeout, oldBudget := toolVersionTimeout, toolVersionBudget
	toolVersionTimeout, toolVersionBudget = time.Second, 300*time.Millisecond
	defer func() { toolVersionTimeout, toolVersionBudget = oldTimeout, oldBudget }()

	dir := t.TempDir()
	trustToolDir(t, dir)
	var hung []string
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		hung = append(hung, writeTool(t, dir, name, "exec sleep 10\n"))
	}
	healthy := writeTool(t, dir, "ok", "echo 'ok v1.0'\n")

	versions := newToolVersions()
	start := time.Now()
	for _, path := range hung {
		versions.of(filepath.Base(path), path)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("five tools that never answer took %v, want the %v budget to cap them all", elapsed, toolVersionBudget)
	}
	if got := versions.of("ok", healthy); got != "" {
		t.Errorf("a tool asked after the budget was spent reported %q, want it skipped", got)
	}
}

func TestConcurrentDiagnosticsShareOneRunPerAPI(t *testing.T) {
	dir := t.TempDir()
	trustToolDir(t, dir)
	calls := filepath.Join(dir, "calls")
	writeTool(t, dir, "jq", "echo run >> '"+calls+"'\nsleep 0.3\necho jq-1.0\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	newAPI := func(configPath string) *API {
		cfg := config.NewConfig()
		cfg.ConfigPath = configPath
		return &API{
			cfgPtr:                 testCfgPtr(&cfg),
			overrideServiceManager: func() string { return "systemd" },
			geodataManager:         geodat.NewGeodataManager("", ""),
		}
	}
	apis := []*API{newAPI("/etc/b4/a.json"), newAPI("/etc/b4/b.json")}

	start := make(chan struct{})
	reports := make([]Diagnostics, 3*len(apis))
	var wg sync.WaitGroup
	for i := range reports {
		api := apis[i%len(apis)]
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			reports[i] = api.buildDiagnostics()
		}()
	}
	close(start)
	wg.Wait()

	data, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(data), "run"); n != len(apis) {
		t.Errorf("%d concurrent diagnostics on %d APIs ran jq %d times, want once per API", len(reports), len(apis), n)
	}
	for i, report := range reports {
		if want := apis[i%len(apis)].getCfg().ConfigPath; report.B4.ConfigPath != want {
			t.Errorf("caller %d got the report of %s, want %s", i, report.B4.ConfigPath, want)
		}
		for _, tool := range report.Tools.Optional {
			if tool.Name == "jq" && tool.Version != "1.0" {
				t.Errorf("caller %d got jq version %q, want 1.0", i, tool.Version)
			}
		}
	}
}

func TestToolVersionsKeepsOnlyTheStartOfAFloodOfOutput(t *testing.T) {
	flood := writeTool(t, t.TempDir(), "flood", "yes 'flood v9.9' | head -n 100000\n")
	stdout, _, err := newToolVersions().run(flood, flood)
	if err != nil {
		t.Fatalf("the tool did not run to its end: %v", err)
	}
	if len(stdout) != toolVersionMaxOutput {
		t.Errorf("kept %d bytes of a 1.1 MB output, want %d", len(stdout), toolVersionMaxOutput)
	}
	if got := parseToolVersion("flood", stdout, "", true); got != "v9.9" {
		t.Errorf("version = %q, want v9.9", got)
	}
}
