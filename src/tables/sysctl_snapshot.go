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
	"golang.org/x/sys/unix"
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

var readInitNetnsLink = func() (string, error) {
	return os.Readlink("/proc/1/ns/net")
}

func sameNetnsAsInit() bool {
	own, err := readNetnsLink()
	if err != nil {
		return false
	}
	first, err := readInitNetnsLink()
	return err == nil && own == first
}

var (
	sysctlSnapOpen   = syscall.Open
	sysctlSnapUnlink = syscall.Unlinkat
)

var sysctlSnapState struct {
	sync.Mutex
	pinned *sysctlSnapDir
	done   bool
}

var sysctlSnapWarned struct {
	sync.Mutex
	keys map[string]bool
}

func forgetSysctlSnapshotState() {
	sysctlSnapState.Lock()
	if sysctlSnapState.pinned != nil {
		sysctlSnapState.pinned.close()
	}
	sysctlSnapState.pinned = nil
	sysctlSnapState.done = false
	sysctlSnapState.Unlock()
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

func sysctlSnapForgetWarning(key string) {
	sysctlSnapWarned.Lock()
	defer sysctlSnapWarned.Unlock()
	delete(sysctlSnapWarned.keys, key)
}

type sysctlSnapRefusal struct {
	msg string
}

func (e sysctlSnapRefusal) Error() string {
	return e.msg
}

func refuseSnapshot(format string, args ...any) error {
	return sysctlSnapRefusal{msg: fmt.Sprintf(format, args...)}
}

func transientSnapshotError(err error) bool {
	if err == nil {
		return false
	}
	var refusal sysctlSnapRefusal
	if errors.As(err, &refusal) {
		return false
	}
	for _, lasting := range []syscall.Errno{syscall.ENOENT, syscall.ENOTDIR, syscall.ELOOP, syscall.EACCES, syscall.EPERM, syscall.EROFS} {
		if errors.Is(err, lasting) {
			return false
		}
	}
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

func (d sysctlSnapDir) dup() (sysctlSnapDir, error) {
	fd, err := unix.FcntlInt(uintptr(d.fd), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return sysctlSnapDir{}, &os.PathError{Op: "dup", Path: d.path, Err: err}
	}
	c := d
	c.fd = fd
	return c, nil
}

func (d sysctlSnapDir) file(name string) string {
	return filepath.Join(d.path, name)
}

func (d sysctlSnapDir) same(o sysctlSnapDir) bool {
	return d.dev == o.dev && d.ino == o.ino
}

func trustSnapshotOwner(path string, st *syscall.Stat_t) error {
	if !sysctlSnapOwnerOK(st.Uid) {
		return refuseSnapshot("%s belongs to uid %d, not to root or to b4", path, st.Uid)
	}
	if st.Mode&0o022 != 0 {
		return refuseSnapshot("%s can be written by other users (mode %04o)", path, st.Mode&0o7777)
	}
	return nil
}

func trustSnapshotFile(path string, st *syscall.Stat_t, shared bool) error {
	if st.Mode&syscall.S_IFMT != syscall.S_IFREG {
		return refuseSnapshot("%s is not a regular file", path)
	}
	if err := trustSnapshotOwner(path, st); err != nil {
		return err
	}
	if shared && st.Nlink != 1 {
		return refuseSnapshot("%s has %d hard links", path, st.Nlink)
	}
	return nil
}

func openSnapshotDir(path string, noFollow, shared bool) (sysctlSnapDir, error) {
	flags := sysctlSnapDirFlags
	if noFollow {
		flags |= syscall.O_NOFOLLOW
	}
	fd, err := sysctlSnapOpen(path, flags, 0)
	if err != nil {
		if fi, lerr := os.Lstat(path); noFollow && lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
			return sysctlSnapDir{}, refuseSnapshot("%s is a symbolic link", path)
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

func chooseSysctlSnapshotDir() (sysctlSnapDir, error) {
	var refused []string
	for _, dir := range sysctlSnapRunDirs {
		d, err := openWritableSnapshotDir(dir, false)
		if err == nil {
			return d, nil
		}
		if transientSnapshotError(err) {
			return sysctlSnapDir{}, err
		}
		refused = append(refused, err.Error())
	}
	d, err := openPrivateSnapshotDir()
	if err == nil {
		return d, nil
	}
	if transientSnapshotError(err) {
		return sysctlSnapDir{}, err
	}
	refused = append(refused, err.Error())
	return sysctlSnapDir{}, refuseSnapshot("no directory that only b4 can write: %s", strings.Join(refused, "; "))
}

func openSysctlSnapshot() (sysctlSnapDir, string, error) {
	if sysctlSnapPath != "" {
		d, err := openSnapshotDir(filepath.Dir(sysctlSnapPath), false, false)
		return d, filepath.Base(sysctlSnapPath), err
	}
	name := sysctlSnapshotName()
	sysctlSnapState.Lock()
	defer sysctlSnapState.Unlock()
	if p := sysctlSnapState.pinned; p != nil {
		var st syscall.Stat_t
		if err := syscall.Fstat(p.fd, &st); err == nil && st.Nlink == 0 {
			p.close()
			sysctlSnapState.pinned = nil
		} else {
			d, err := p.dup()
			return d, name, err
		}
	}
	d, err := chooseSysctlSnapshotDir()
	if err != nil {
		return sysctlSnapDir{}, name, err
	}
	d.sweepTemps(name)
	sysctlSnapState.pinned = &d
	c, err := d.dup()
	return c, name, err
}

type sysctlSnapID struct {
	dev uint64
	ino uint64
}

func lstatAt(dirfd int, name string) (sysctlSnapID, uint32, error) {
	var st unix.Stat_t
	if err := unix.Fstatat(dirfd, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return sysctlSnapID{}, 0, err
	}
	return sysctlSnapID{dev: uint64(st.Dev), ino: uint64(st.Ino)}, uint32(st.Mode), nil
}

func (d sysctlSnapDir) read(name string) (map[string]string, *sysctlSnapID, error) {
	path := d.file(name)
	id, mode, err := lstatAt(d.fd, name)
	if err != nil {
		return nil, nil, &os.PathError{Op: "stat", Path: path, Err: err}
	}
	if mode&syscall.S_IFMT != syscall.S_IFREG {
		return nil, &id, refuseSnapshot("%s is not a regular file", path)
	}
	fd, err := syscall.Openat(d.fd, name, sysctlSnapReadFlags, 0)
	if err != nil {
		return nil, nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil {
		return nil, nil, &os.PathError{Op: "stat", Path: path, Err: err}
	}
	if uint64(st.Dev) != id.dev || uint64(st.Ino) != id.ino {
		return nil, nil, fmt.Errorf("%s changed while b4 opened it", path)
	}
	if err := trustSnapshotFile(path, &st, d.shared); err != nil {
		return nil, &id, err
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, nil, err
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, &id, refuseSnapshot("%s: %v", path, err)
	}
	if m == nil {
		m = map[string]string{}
	}
	return m, &id, nil
}

func (d sysctlSnapDir) sweepTemps(name string) {
	c, err := d.dup()
	if err != nil {
		return
	}
	f := os.NewFile(uintptr(c.fd), d.path)
	defer f.Close()
	entries, err := f.Readdirnames(-1)
	if err != nil {
		return
	}
	for _, n := range entries {
		if !strings.HasPrefix(n, name+".") {
			continue
		}
		if err := d.remove(n); err != nil {
			log.Tracef("TABLES: could not remove %v", err)
		} else {
			log.Tracef("TABLES: removed %s, left by a b4 that stopped while saving", d.file(n))
		}
	}
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

type sysctlSnapFound struct {
	dir  sysctlSnapDir
	name string
	id   sysctlSnapID
}

func (f sysctlSnapFound) remove() error {
	path := f.dir.file(f.name)
	id, _, err := lstatAt(f.dir.fd, f.name)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) {
			return nil
		}
		return &os.PathError{Op: "stat", Path: path, Err: err}
	}
	if id != f.id {
		return fmt.Errorf("%s changed after b4 read it", path)
	}
	return f.dir.remove(f.name)
}

func sysctlSnapshotOrigins(cur *sysctlSnapDir, name string) ([]sysctlSnapOrigin, func(), bool) {
	var origins []sysctlSnapOrigin
	var dirs []sysctlSnapDir
	complete := true
	add := func(path string, noFollow, shared bool, names ...string) {
		d, err := openSnapshotDir(path, noFollow, shared)
		if err != nil {
			switch {
			case errors.Is(err, fs.ErrNotExist):
			case transientSnapshotError(err):
				complete = false
				log.Tracef("TABLES: could not look for saved conntrack settings in %v", err)
			case sysctlSnapFirstWarning("origin dir " + path):
				log.Tracef("TABLES: not looking for saved conntrack settings in %v", err)
			}
			return
		}
		dirs = append(dirs, d)
		for _, n := range names {
			if cur != nil && d.same(*cur) && n == name {
				continue
			}
			origins = append(origins, sysctlSnapOrigin{dir: d, name: n})
		}
	}
	for _, dir := range sysctlSnapRunDirs {
		add(dir, false, false, name)
	}
	add(sysctlSnapPrivateDir, true, false, name)
	unkeyed := sysctlSnapBase + ".json"
	legacy := []string{name}
	if name != unkeyed && sameNetnsAsInit() {
		legacy = []string{unkeyed, name}
	}
	add(sysctlSnapLegacyDir, false, true, legacy...)
	return origins, func() {
		for _, d := range dirs {
			d.close()
		}
	}, complete
}

func takeOverEarlierSnapshots(cur *sysctlSnapDir, name string, m map[string]string) {
	sysctlSnapState.Lock()
	defer sysctlSnapState.Unlock()
	if sysctlSnapState.done {
		return
	}
	origins, closeDirs, complete := sysctlSnapshotOrigins(cur, name)
	defer closeDirs()
	var found []sysctlSnapFound
	var took []string
	for _, o := range origins {
		path := o.dir.file(o.name)
		om, id, err := o.dir.read(o.name)
		switch {
		case err == nil:
		case id != nil:
			if sysctlSnapFirstWarning("origin " + path) {
				log.Warnf("TABLES: ignoring the saved conntrack settings in %s: %v", path, err)
			}
			found = append(found, sysctlSnapFound{dir: o.dir, name: o.name, id: *id})
			continue
		case errors.Is(err, fs.ErrNotExist):
			continue
		case transientSnapshotError(err):
			complete = false
			log.Tracef("TABLES: could not read the saved conntrack settings in %s: %v", path, err)
			continue
		default:
			if sysctlSnapFirstWarning("origin " + path) {
				log.Warnf("TABLES: ignoring the saved conntrack settings in %s: %v", path, err)
			}
			continue
		}
		found = append(found, sysctlSnapFound{dir: o.dir, name: o.name, id: *id})
		for _, k := range slices.Sorted(maps.Keys(om)) {
			if _, have := m[k]; !have && isSysctlNumber(om[k]) {
				m[k] = om[k]
				took = append(took, k+"="+om[k]+" from "+path)
			}
		}
	}
	if cur == nil || !complete {
		return
	}
	if len(took) > 0 {
		if err := cur.write(name, m); err != nil {
			if sysctlSnapFirstWarning("move " + cur.file(name)) {
				log.Errorf("TABLES: could not move the conntrack settings an earlier b4 saved into %s, so b4 reads them where that b4 left them until it can: %v", cur.file(name), err)
			}
			return
		}
		log.Infof("TABLES: moved the conntrack settings an earlier b4 saved into %s: %s", cur.file(name), strings.Join(took, ", "))
	}
	for _, f := range slices.Backward(found) {
		if err := f.remove(); err != nil {
			log.Tracef("TABLES: keeping the earlier saved conntrack settings for now: %v", err)
			return
		}
	}
	sysctlSnapState.done = true
}

func loadSysctlSnapshot() map[string]string {
	m := map[string]string{}
	d, name, err := openSysctlSnapshot()
	if err != nil {
		if sysctlSnapPath == "" && !transientSnapshotError(err) {
			if sysctlSnapFirstWarning("no dir") {
				log.Warnf("TABLES: the saved conntrack settings have nowhere safe to live, so b4 only reads what an earlier b4 left: %v", err)
			}
			takeOverEarlierSnapshots(nil, name, m)
		}
		return m
	}
	defer d.close()
	cur, _, err := d.read(name)
	switch {
	case err == nil:
		m = cur
	case errors.Is(err, fs.ErrNotExist):
	case transientSnapshotError(err):
		return m
	default:
		if sysctlSnapFirstWarning("read " + d.file(name)) {
			log.Warnf("TABLES: ignoring the saved conntrack settings: %v", err)
		}
	}
	if sysctlSnapPath == "" {
		takeOverEarlierSnapshots(&d, name, m)
	}
	return m
}

func saveSysctlSnapshot(m map[string]string) error {
	d, name, err := openSysctlSnapshot()
	if err != nil {
		return err
	}
	defer d.close()
	return d.write(name, m)
}
