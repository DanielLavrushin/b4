package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtendedPATHAddsMissingStandardDirs(t *testing.T) {
	got := ExtendedPATH("/usr/sbin:/usr/bin:/sbin:/bin")
	if !strings.Contains(got, "/opt/bin") || !strings.Contains(got, "/opt/sbin") {
		t.Fatalf("expected entware dirs to be appended, got %q", got)
	}
	if !strings.HasPrefix(got, "/usr/sbin:/usr/bin:/sbin:/bin") {
		t.Fatalf("expected inherited entries to keep priority, got %q", got)
	}
}

func TestExtendedPATHDeduplicatesAndDropsEmpty(t *testing.T) {
	got := ExtendedPATH("/opt/bin::/usr/bin:/opt/bin")
	seen := make(map[string]int)
	for _, p := range strings.Split(got, ":") {
		if p == "" {
			t.Fatalf("empty entry in %q", got)
		}
		seen[p]++
	}
	for p, n := range seen {
		if n > 1 {
			t.Fatalf("duplicate entry %q in %q", p, got)
		}
	}
	if !strings.HasPrefix(got, "/opt/bin:/usr/bin:") {
		t.Fatalf("expected first-occurrence order, got %q", got)
	}
}

func TestParseProfilePaths(t *testing.T) {
	dir := t.TempDir()
	optBin := filepath.Join(dir, "optbin")
	if err := os.MkdirAll(optBin, 0755); err != nil {
		t.Fatal(err)
	}

	profile := filepath.Join(dir, "profile")
	content := "# comment\n" +
		"PATH=/opt/bin:$PATH\n" +
		"export PATH=\"" + optBin + ":/usr/bin\"\n" +
		"export PATH='/data/bin'\n" +
		"PATH=$HOME/bin\n" +
		"XPATH=/should/not/match\n" +
		"export PATH=/with/trailing ; echo done\n"
	if err := os.WriteFile(profile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	got := parseProfilePaths(profile)
	want := []string{"/opt/bin", optBin, "/usr/bin", "/data/bin", "/with/trailing"}
	for _, w := range want {
		if !containsPath(got, w) {
			t.Fatalf("expected %q in %v", w, got)
		}
	}
	if containsPath(got, "/should/not/match") {
		t.Fatalf("XPATH assignment was picked up: %v", got)
	}
	for _, g := range got {
		if strings.Contains(g, "$") {
			t.Fatalf("unexpanded variable kept: %q", g)
		}
	}
}

func TestParseMountPointsSkipsPseudoAndRoot(t *testing.T) {
	data := []byte(strings.Join([]string{
		"proc /proc proc rw,nosuid 0 0",
		"sysfs /sys sysfs rw 0 0",
		"/dev/root / squashfs ro 0 0",
		"tmpfs /tmp tmpfs rw 0 0",
		"/dev/sda1 /mnt/usb-5593373d ext4 rw 0 0",
		"/dev/sda2 /tmp/mnt/my\\040disk vfat rw 0 0",
		"devtmpfs /dev devtmpfs rw 0 0",
		"cgroup2 /sys/fs/cgroup cgroup2 rw 0 0",
		"/dev/loop3 /snap/core22/2411 squashfs ro 0 0",
		"tmpfs /run/user/1000 tmpfs rw 0 0",
	}, "\n"))

	got := parseMountPoints(data)
	if !containsPath(got, "/mnt/usb-5593373d") {
		t.Fatalf("USB mount missing from %v", got)
	}
	if !containsPath(got, "/tmp/mnt/my disk") {
		t.Fatalf("escaped mountpoint not decoded: %v", got)
	}
	if !containsPath(got, "/tmp") {
		t.Fatalf("tmpfs mountpoint should be kept: %v", got)
	}
	for _, skip := range []string{"/", "/proc", "/sys", "/dev", "/sys/fs/cgroup", "/snap/core22/2411", "/run/user/1000"} {
		if containsPath(got, skip) {
			t.Fatalf("expected %q to be skipped, got %v", skip, got)
		}
	}
}

func TestLookupToolFindsEntwareOnUSBVolume(t *testing.T) {
	root := t.TempDir()
	usbRoot := filepath.Join(root, "mnt", "usb-5593373d")
	binDir := filepath.Join(usbRoot, "usr", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(binDir, "b4testusbjq")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	mounts := filepath.Join(root, "mounts")
	content := "/dev/root / squashfs ro 0 0\n/dev/sda1 " + usbRoot + " ext4 rw 0 0\n"
	if err := os.WriteFile(mounts, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	origMounts := mountsFiles
	origProfiles := profileFiles
	origPath := os.Getenv("PATH")
	mountsFiles = []string{mounts}
	profileFiles = nil
	os.Setenv("PATH", "/nonexistent-b4-test")
	resetExtraBinCache()
	t.Cleanup(func() {
		mountsFiles = origMounts
		profileFiles = origProfiles
		os.Setenv("PATH", origPath)
		resetExtraBinCache()
	})

	if !containsPath(MountBinPaths(), binDir) {
		t.Fatalf("expected %q among %v", binDir, MountBinPaths())
	}
	got, ok := LookupTool("b4testusbjq")
	if !ok || got != tool {
		t.Fatalf("expected %q, got %q (ok=%v)", tool, got, ok)
	}
}

func TestLookupToolFindsBinaryOnMountedVolume(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "usr", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(binDir, "b4testjq")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	profile := filepath.Join(root, "profile")
	if err := os.WriteFile(profile, []byte("export PATH="+binDir+":$PATH\n"), 0644); err != nil {
		t.Fatal(err)
	}

	origProfiles := profileFiles
	profileFiles = []string{profile}
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", "/nonexistent-b4-test")
	resetExtraBinCache()
	t.Cleanup(func() {
		profileFiles = origProfiles
		os.Setenv("PATH", origPath)
		resetExtraBinCache()
	})

	got, ok := LookupTool("b4testjq")
	if !ok || got != tool {
		t.Fatalf("expected %q, got %q (ok=%v)", tool, got, ok)
	}

	if _, ok := LookupTool("b4testmissing"); ok {
		t.Fatal("expected a missing tool to stay missing")
	}
}

func TestProfileBinPathsOnlyExistingDirs(t *testing.T) {
	for _, p := range ProfileBinPaths() {
		info, err := os.Stat(p)
		if err != nil || !info.IsDir() {
			t.Fatalf("returned non-directory %q", p)
		}
	}
}

func containsPath(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func TestExtendedPATHEmptyInput(t *testing.T) {
	got := ExtendedPATH("")
	if got != strings.Join(standardBinPaths, ":") {
		t.Fatalf("expected the standard list, got %q", got)
	}
}
