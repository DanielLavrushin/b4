package config

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func inodeOf(t *testing.T, path string) uint64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Sys().(*syscall.Stat_t).Ino
}

func TestWriteFileAtomicReplacesAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	before := inodeOf(t, path)

	if err := writeFileAtomic(path, []byte("new"), 0600); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "new" {
		t.Fatalf("content = %q", got)
	}
	if inodeOf(t, path) == before {
		t.Fatal("the file was rewritten in place instead of replaced")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode = %#o, want 0600", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
}

func TestWriteFileAtomicKeepsSymlink(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	if err := os.MkdirAll(realDir, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(realDir, "b4.json")
	if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "b4.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := writeFileAtomic(link, []byte("new"), ConfigFileMode); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the symlink was replaced by a regular file (%v)", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "new" {
		t.Fatalf("symlink target content = %q", got)
	}
}

func TestWriteFileAtomicFallsBackInPlaceWhenTheDirectoryIsReadOnly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "b4.json")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })

	if err := writeFileAtomic(path, []byte("new"), ConfigFileMode); err != nil {
		t.Fatalf("a writable file in a read-only directory must still be saved: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "new" {
		t.Fatalf("content = %q", got)
	}
}

const erofsChildEnv = "B4_TEST_EROFS_DIR"

func TestWriteFileAtomicFallsBackInPlaceOnAReadOnlyFilesystem(t *testing.T) {
	if dir := os.Getenv(erofsChildEnv); dir != "" {
		erofsChild(t, dir)
		return
	}
	if _, err := exec.LookPath("unshare"); err != nil {
		t.Skip("unshare is not installed")
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "ro"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "rw"), 0700); err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(dir, "rw", "b4.json")
	if err := os.WriteFile(host, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("unshare", "-rm", os.Args[0], "-test.run=^TestWriteFileAtomicFallsBackInPlaceOnAReadOnlyFilesystem$", "-test.v")
	cmd.Env = append(os.Environ(), erofsChildEnv+"="+dir)
	out, err := cmd.CombinedOutput()
	if strings.Contains(string(out), "--- SKIP") {
		t.Skipf("no read-only filesystem available: %s", out)
	}
	if err != nil {
		if !strings.Contains(string(out), "--- ") {
			t.Skipf("cannot open a user and mount namespace: %v: %s", err, out)
		}
		t.Fatalf("child failed: %v\n%s", err, out)
	}
	got, _ := os.ReadFile(host)
	if string(got) != "new" {
		t.Fatalf("the bind-mounted file behind a read-only directory was not saved, content = %q\n%s", got, out)
	}
}

func erofsChild(t *testing.T, dir string) {
	ro := filepath.Join(dir, "ro")
	target := filepath.Join(ro, "b4.json")
	if err := syscall.Mount("tmpfs", ro, "tmpfs", 0, "size=64k"); err != nil {
		t.Skipf("mount tmpfs: %v", err)
	}
	if err := os.WriteFile(target, nil, 0600); err != nil {
		t.Skipf("create the mount point: %v", err)
	}
	if err := syscall.Mount(filepath.Join(dir, "rw", "b4.json"), target, "", syscall.MS_BIND, ""); err != nil {
		t.Skipf("bind mount: %v", err)
	}
	if err := syscall.Mount("", ro, "", syscall.MS_REMOUNT|syscall.MS_RDONLY, ""); err != nil {
		t.Skipf("remount read-only: %v", err)
	}
	if f, err := os.CreateTemp(ro, "probe-*"); err == nil {
		_ = f.Close()
		t.Skip("the directory is still writable")
	} else if !errors.Is(err, syscall.EROFS) {
		t.Skipf("expected EROFS from the read-only directory, got %v", err)
	}

	if err := writeFileAtomic(target, []byte("new"), ConfigFileMode); err != nil {
		t.Fatalf("a writable bind-mounted file in a read-only directory must still be saved: %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "new" {
		t.Fatalf("content = %q", got)
	}
}

func TestWriteFileAtomicKeepsTheGroup(t *testing.T) {
	groups, err := os.Getgroups()
	if err != nil {
		t.Skipf("getgroups: %v", err)
	}
	other := -1
	for _, g := range groups {
		if g != os.Getegid() {
			other = g
			break
		}
	}
	if other < 0 {
		t.Skip("the test user belongs to no second group")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "b4.json")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(path, -1, other); err != nil {
		t.Skipf("chgrp: %v", err)
	}

	if err := writeFileAtomic(path, []byte("new"), ConfigFileMode); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if gid := int(info.Sys().(*syscall.Stat_t).Gid); gid != other {
		t.Fatalf("group = %d, want %d", gid, other)
	}
	if info.Mode().Perm() != ConfigFileMode {
		t.Fatalf("mode = %#o, want %#o", info.Mode().Perm(), ConfigFileMode)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "new" {
		t.Fatalf("content = %q", got)
	}
}

func TestWriteFileAtomicKeepsTheOwner(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("changing a file's owner needs root")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "b4.json")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(path, 12345, 12345); err != nil {
		t.Fatal(err)
	}

	if err := writeFileAtomic(path, []byte("new"), ConfigFileMode); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	st := info.Sys().(*syscall.Stat_t)
	if st.Uid != 12345 || st.Gid != 12345 {
		t.Fatalf("owner = %d:%d, want 12345:12345", st.Uid, st.Gid)
	}
	if info.Mode().Perm() != ConfigFileMode {
		t.Fatalf("mode = %#o, want %#o", info.Mode().Perm(), ConfigFileMode)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "new" {
		t.Fatalf("content = %q", got)
	}
}

func TestKeepCorruptCopy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b4.json")

	if kept, err := KeepCorruptCopy(path); err != nil || kept != "" {
		t.Fatalf("a missing file needs no copy, got %q, %v", kept, err)
	}
	if kept, err := KeepCorruptCopy(""); err != nil || kept != "" {
		t.Fatalf("an empty path needs no copy, got %q, %v", kept, err)
	}
	if kept, err := KeepCorruptCopy(dir); err != nil || kept != "" {
		t.Fatalf("a directory is not copied, got %q, %v", kept, err)
	}

	if err := os.WriteFile(path, []byte(`{"sets": [`), 0600); err != nil {
		t.Fatal(err)
	}
	kept, err := KeepCorruptCopy(path)
	if err != nil || kept != path+".corrupt" {
		t.Fatalf("first copy = %q, %v", kept, err)
	}
	info, _ := os.Stat(kept)
	if info.Mode().Perm() != ConfigFileMode {
		t.Fatalf("copy mode = %#o, want %#o", info.Mode().Perm(), ConfigFileMode)
	}

	again, err := KeepCorruptCopy(path)
	if err != nil || again != kept {
		t.Fatalf("the same broken content must not be copied twice, got %q, %v", again, err)
	}

	if err := os.WriteFile(path, []byte(`{"version": `), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := KeepCorruptCopy(path)
	if err != nil || second != path+".corrupt.1" {
		t.Fatalf("second copy = %q, %v", second, err)
	}
	first, _ := os.ReadFile(path + ".corrupt")
	if string(first) != `{"sets": [` {
		t.Fatalf("the first copy was overwritten: %q", first)
	}
	original, _ := os.ReadFile(path)
	if string(original) != `{"version": ` {
		t.Fatal("KeepCorruptCopy must leave the original in place")
	}
}

func TestLoadWithMigrationFailureLeavesAKeepableFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b4.json")
	broken := `{"version": 60, "sets": [{"id": "a", "name": "kept"`
	if err := os.WriteFile(path, []byte(broken), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := NewConfig()
	if _, err := cfg.LoadWithMigration(path); err == nil {
		t.Fatal("a truncated config must fail to load")
	}
	kept, err := KeepCorruptCopy(cfg.ConfigPath)
	if err != nil || kept == "" {
		t.Fatalf("no copy kept: %q, %v", kept, err)
	}
	data, _ := os.ReadFile(kept)
	if string(data) != broken {
		t.Fatalf("copy = %q", data)
	}
}

func TestSaveToFileReplacesAtomicallyAndSkipsIdenticalBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b4.json")

	cfg := NewConfig()
	set := NewSetConfig()
	set.Id = "s1"
	set.Name = "one"
	cfg.Sets = []*SetConfig{&set}
	if err := cfg.SaveToFile(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	firstIno := inodeOf(t, path)
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	if err := cfg.SaveToFile(path); err != nil {
		t.Fatalf("second save: %v", err)
	}
	info, _ := os.Stat(path)
	if !info.ModTime().Equal(old) || inodeOf(t, path) != firstIno {
		t.Fatal("saving identical content rewrote the file")
	}

	cfg.Sets[0].Name = "renamed"
	if err := cfg.SaveToFile(path); err != nil {
		t.Fatalf("third save: %v", err)
	}
	if inodeOf(t, path) == firstIno {
		t.Fatal("a changed config was written in place instead of replacing the file")
	}
	loaded := NewConfig()
	if err := loaded.LoadFromFile(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded.Sets) != 1 || loaded.Sets[0].Name != "renamed" {
		t.Fatalf("saved config did not round-trip: %+v", loaded.Sets)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
}

func TestSaveToFileIdenticalBytesStillTightensPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b4.json")
	cfg := NewConfig()
	if err := cfg.SaveToFile(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveToFile(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != ConfigFileMode {
		t.Fatalf("mode = %#o, want %#o", info.Mode().Perm(), ConfigFileMode)
	}
}

func TestSaveToFileThroughSymlinkKeepsTheLink(t *testing.T) {
	dir := t.TempDir()
	realPath := filepath.Join(dir, "real.json")
	if err := os.WriteFile(realPath, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "b4.json")
	if err := os.Symlink(realPath, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	cfg := NewConfig()
	if err := cfg.SaveToFile(link); err != nil {
		t.Fatalf("save: %v", err)
	}
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("saving through a symlinked config path replaced the link")
	}
	data, _ := os.ReadFile(realPath)
	if len(data) < 10 {
		t.Fatalf("the link target was not written: %q", data)
	}
}
