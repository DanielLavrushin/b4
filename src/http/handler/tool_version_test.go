package handler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestToolVersionsAsksBusyboxOnceForAllItsApplets(t *testing.T) {
	dir := t.TempDir()
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

	hang := writeTool(t, t.TempDir(), "hang", "exec sleep 10\n")
	start := time.Now()
	if got := newToolVersions().of("hang", hang); got != "" {
		t.Errorf("version = %q, want none", got)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("waited %v for a tool that never answers", elapsed)
	}
}

func TestToolVersionsKeepsOnlyTheStartOfAFloodOfOutput(t *testing.T) {
	flood := writeTool(t, t.TempDir(), "flood", "yes 'flood v9.9' | head -n 100000\n")
	stdout, _, err := runToolVersion(flood)
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
