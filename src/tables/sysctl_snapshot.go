package tables

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/daniellavrushin/b4/log"
)

const sysctlSnapBase = "b4_sysctl_snapshot"

const (
	sysctlSnapDirFlags  = syscall.O_RDONLY | syscall.O_DIRECTORY | syscall.O_NONBLOCK | syscall.O_CLOEXEC
	sysctlSnapReadFlags = syscall.O_RDONLY | syscall.O_NOFOLLOW | syscall.O_NONBLOCK | syscall.O_CLOEXEC
	sysctlSnapTempFlags = syscall.O_WRONLY | syscall.O_CREAT | syscall.O_EXCL | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
)

var (
	sysctlSnapPath       string
	sysctlSnapRunDirs    = []string{"/var/run", "/run"}
	sysctlSnapPrivateDir = "/tmp/b4-run"
	sysctlSnapLegacyDir  = "/tmp"
	sysctlSnapOwnerOK    = func(uid uint32) bool { return uid == 0 || int64(uid) == int64(os.Geteuid()) }
)

var readNetnsLink = func() (string, error) {
	return os.Readlink("/proc/self/ns/net")
}

var sysctlSnapUnlink = syscall.Unlinkat

var sysctlSnapTakeover struct {
	sync.Mutex
	done bool
	took []string
}

var sysctlSnapWarned struct {
	sync.Mutex
	keys map[string]bool
}

func forgetSysctlSnapshotState() {
	sysctlSnapTakeover.Lock()
	sysctlSnapTakeover.done = false
	sysctlSnapTakeover.took = nil
	sysctlSnapTakeover.Unlock()
	sysctlSnapWarned.Lock()
	sysctlSnapWarned.keys = nil
	sysctlSnapWarned.Unlock()
}

func sysctlSnapFirstWarning(key string) bool {
	sysctlSnapWarned.Lock()
	defer sysctlSnapWarned.Unlock()
	if sysctlSnapWarned.keys[key] {
		return false
	}
	if sysctlSnapWarned.keys == nil {
		sysctlSnapWarned.keys = map[string]bool{}
	}
	sysctlSnapWarned.keys[key] = true
	return true
}

func netnsInode() (uint64, bool) {
	link, err := readNetnsLink()
	if err != nil {
		return 0, false
	}
	id, ok := strings.CutPrefix(link, "net:[")
	if !ok {
		return 0, false
	}
	id, ok = strings.CutSuffix(id, "]")
	if !ok {
		return 0, false
	}
	ino, err := strconv.ParseUint(id, 10, 64)
	if err != nil || ino == 0 {
		return 0, false
	}
	return ino, true
}

func sysctlSnapshotName() string {
	if ino, ok := netnsInode(); ok {
		return fmt.Sprintf("%s.%d.json", sysctlSnapBase, ino)
	}
	return sysctlSnapBase + ".json"
}

type sysctlSnapDir struct {
	fd     int
	path   string
	shared bool
	dev    uint64
	ino    uint64
}

func (d sysctlSnapDir) close() {
	_ = syscall.Close(d.fd)
}

func (d sysctlSnapDir) file(name string) string {
	return filepath.Join(d.path, name)
}

func (d sysctlSnapDir) same(o sysctlSnapDir) bool {
	return d.dev == o.dev && d.ino == o.ino
}

func trustSnapshotOwner(path string, st *syscall.Stat_t) error {
	if !sysctlSnapOwnerOK(st.Uid) {
		return fmt.Errorf("%s belongs to uid %d, not to root or to b4", path, st.Uid)
	}
	if st.Mode&0o022 != 0 {
		return fmt.Errorf("%s can be written by other users (mode %04o)", path, st.Mode&0o7777)
	}
	return nil
}

func trustSnapshotFile(path string, st *syscall.Stat_t, shared bool) error {
	if st.Mode&syscall.S_IFMT != syscall.S_IFREG {
		return fmt.Errorf("%s is not a regular file", path)
	}
	if err := trustSnapshotOwner(path, st); err != nil {
		return err
	}
	if shared && st.Nlink != 1 {
		return fmt.Errorf("%s has %d hard links", path, st.Nlink)
	}
	return nil
}

func openSnapshotDir(path string, noFollow, shared bool) (sysctlSnapDir, error) {
	flags := sysctlSnapDirFlags
	if noFollow {
		flags |= syscall.O_NOFOLLOW
	}
	fd, err := syscall.Open(path, flags, 0)
	if err != nil {
		if fi, lerr := os.Lstat(path); noFollow && lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
			return sysctlSnapDir{}, fmt.Errorf("%s is a symbolic link", path)
		}
		return sysctlSnapDir{}, &os.PathError{Op: "open", Path: path, Err: err}
	}
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil {
		_ = syscall.Close(fd)
		return sysctlSnapDir{}, &os.PathError{Op: "stat", Path: path, Err: err}
	}
	if !shared {
		if err := trustSnapshotOwner(path, &st); err != nil {
			_ = syscall.Close(fd)
			return sysctlSnapDir{}, err
		}
	}
	return sysctlSnapDir{fd: fd, path: path, shared: shared, dev: uint64(st.Dev), ino: uint64(st.Ino)}, nil
}

func openWritableSnapshotDir(path string, noFollow bool) (sysctlSnapDir, error) {
	d, err := openSnapshotDir(path, noFollow, false)
	if err != nil {
		return d, err
	}
	if err := syscall.Faccessat(d.fd, ".", 2, 0); err != nil {
		d.close()
		return sysctlSnapDir{}, &os.PathError{Op: "access", Path: path, Err: err}
	}
	return d, nil
}

func openPrivateSnapshotDir() (sysctlSnapDir, error) {
	if err := os.Mkdir(sysctlSnapPrivateDir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return sysctlSnapDir{}, err
	}
	return openWritableSnapshotDir(sysctlSnapPrivateDir, true)
}

func openSysctlSnapshot() (sysctlSnapDir, string, error) {
	if sysctlSnapPath != "" {
		d, err := openSnapshotDir(filepath.Dir(sysctlSnapPath), false, false)
		return d, filepath.Base(sysctlSnapPath), err
	}
	name := sysctlSnapshotName()
	var refused []string
	for _, dir := range sysctlSnapRunDirs {
		d, err := openWritableSnapshotDir(dir, false)
		if err == nil {
			return d, name, nil
		}
		refused = append(refused, err.Error())
	}
	d, err := openPrivateSnapshotDir()
	if err != nil {
		refused = append(refused, err.Error())
		return sysctlSnapDir{}, name, fmt.Errorf("no directory that only b4 can write: %s", strings.Join(refused, "; "))
	}
	return d, name, nil
}

func (d sysctlSnapDir) read(name string) (map[string]string, error) {
	path := d.file(name)
	fd, err := syscall.Openat(d.fd, name, sysctlSnapReadFlags, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil {
		return nil, &os.PathError{Op: "stat", Path: path, Err: err}
	}
	if err := trustSnapshotFile(path, &st, d.shared); err != nil {
		return nil, err
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if m == nil {
		m = map[string]string{}
	}
	return m, nil
}

func (d sysctlSnapDir) remove(name string) error {
	if err := sysctlSnapUnlink(d.fd, name); err != nil && !errors.Is(err, syscall.ENOENT) {
		return &os.PathError{Op: "remove", Path: d.file(name), Err: err}
	}
	return nil
}

func (d sysctlSnapDir) write(name string, m map[string]string) error {
	if len(m) == 0 {
		return d.remove(name)
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tmp := name + "." + strconv.FormatUint(rand.Uint64(), 36)
	fd, err := syscall.Openat(d.fd, tmp, sysctlSnapTempFlags, 0o600)
	if err != nil {
		return &os.PathError{Op: "create", Path: d.file(tmp), Err: err}
	}
	f := os.NewFile(uintptr(fd), d.file(tmp))
	_, werr := f.Write(b)
	cerr := f.Close()
	err = errors.Join(werr, cerr)
	if err == nil {
		if rerr := syscall.Renameat(d.fd, tmp, d.fd, name); rerr != nil {
			err = &os.LinkError{Op: "rename", Old: d.file(tmp), New: d.file(name), Err: rerr}
		}
	}
	if err != nil {
		_ = syscall.Unlinkat(d.fd, tmp)
	}
	return err
}

type sysctlSnapOrigin struct {
	dir  sysctlSnapDir
	name string
}

func sysctlSnapshotOrigins(cur sysctlSnapDir, name string) ([]sysctlSnapOrigin, func()) {
	var origins []sysctlSnapOrigin
	var dirs []sysctlSnapDir
	add := func(path string, noFollow, shared bool, names ...string) {
		d, err := openSnapshotDir(path, noFollow, shared)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) && sysctlSnapFirstWarning("origin dir "+path) {
				log.Tracef("TABLES: not looking for saved conntrack settings in %v", err)
			}
			return
		}
		dirs = append(dirs, d)
		for _, n := range names {
			if d.same(cur) && n == name {
				continue
			}
			origins = append(origins, sysctlSnapOrigin{dir: d, name: n})
		}
	}
	for _, dir := range sysctlSnapRunDirs {
		add(dir, false, false, name)
	}
	add(sysctlSnapPrivateDir, true, false, name)
	legacy := []string{sysctlSnapBase + ".json"}
	if name != legacy[0] {
		legacy = append(legacy, name)
	}
	add(sysctlSnapLegacyDir, false, true, legacy...)
	return origins, func() {
		for _, d := range dirs {
			d.close()
		}
	}
}

func mergeEarlierSnapshots(cur sysctlSnapDir, name string, m map[string]string) {
	sysctlSnapTakeover.Lock()
	defer sysctlSnapTakeover.Unlock()
	if sysctlSnapTakeover.done {
		return
	}
	origins, closeDirs := sysctlSnapshotOrigins(cur, name)
	defer closeDirs()
	var took []string
	for _, o := range origins {
		path := o.dir.file(o.name)
		om, err := o.dir.read(o.name)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) && sysctlSnapFirstWarning("origin "+path) {
				log.Warnf("TABLES: ignoring the saved conntrack settings in %s: %v", path, err)
			}
			continue
		}
		for _, k := range slices.Sorted(maps.Keys(om)) {
			if _, have := m[k]; !have && isSysctlNumber(om[k]) {
				m[k] = om[k]
				took = append(took, k+"="+om[k]+" from "+path)
			}
		}
	}
	sysctlSnapTakeover.took = took
}

func retireEarlierSnapshots(cur sysctlSnapDir, name string) {
	sysctlSnapTakeover.Lock()
	defer sysctlSnapTakeover.Unlock()
	if sysctlSnapTakeover.done {
		return
	}
	sysctlSnapTakeover.done = true
	if len(sysctlSnapTakeover.took) > 0 {
		log.Infof("TABLES: moved the conntrack settings an earlier b4 saved into %s: %s", cur.file(name), strings.Join(sysctlSnapTakeover.took, ", "))
	}
	origins, closeDirs := sysctlSnapshotOrigins(cur, name)
	defer closeDirs()
	for _, o := range slices.Backward(origins) {
		if err := o.dir.remove(o.name); err != nil {
			log.Tracef("TABLES: could not remove %v", err)
		}
	}
}

func loadSysctlSnapshot() map[string]string {
	d, name, err := openSysctlSnapshot()
	if err != nil {
		return map[string]string{}
	}
	defer d.close()
	m, err := d.read(name)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) && sysctlSnapFirstWarning("read "+d.file(name)) {
			log.Warnf("TABLES: ignoring the saved conntrack settings: %v", err)
		}
		m = map[string]string{}
	}
	if sysctlSnapPath == "" {
		mergeEarlierSnapshots(d, name, m)
	}
	return m
}

func saveSysctlSnapshot(m map[string]string) error {
	d, name, err := openSysctlSnapshot()
	if err != nil {
		return err
	}
	defer d.close()
	if err := d.write(name, m); err != nil {
		return err
	}
	if sysctlSnapPath == "" {
		retireEarlierSnapshots(d, name)
	}
	return nil
}
