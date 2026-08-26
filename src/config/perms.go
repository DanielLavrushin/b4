package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/daniellavrushin/b4/log"
	"golang.org/x/sys/unix"
)

const ConfigFileMode os.FileMode = 0600

var restrictWarned sync.Map

func warnRestrictFailure(path string, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOENT) {
		log.Debugf("Not restricting permissions on %s: %v", path, err)
		return
	}
	if _, seen := restrictWarned.LoadOrStore(path, struct{}{}); seen {
		return
	}
	log.Warnf("Could not restrict permissions on %s, it stays reachable by other accounts on this device: %v", path, err)
}

func restrictFileMode(path string) error {
	dir := filepath.Dir(path)
	dirFd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(dirFd)

	return restrictFileModeAt(dirFd, filepath.Base(path), path)
}

func restrictFileModeAt(dirFd int, name, display string) error {
	fd, err := unix.Openat(dirFd, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)

	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil
	}

	current := os.FileMode(st.Mode).Perm()
	if current == ConfigFileMode {
		return nil
	}
	if err := unix.Fchmod(fd, uint32(ConfigFileMode)); err != nil {
		return err
	}

	log.Infof("Tightened permissions on %s from %#o to %#o", display, current, ConfigFileMode)
	return nil
}

func restrictDirMode(dirFd int, display string) {
	var st unix.Stat_t
	if err := unix.Fstat(dirFd, &st); err != nil {
		return
	}

	current := os.FileMode(st.Mode).Perm()
	if current&0022 == 0 {
		return
	}

	want := current &^ 0022
	if err := unix.Fchmod(dirFd, uint32(want)); err != nil {
		warnRestrictFailure(display, err)
		return
	}

	log.Infof("Tightened permissions on %s from %#o to %#o", display, current, want)
}

func restrictConfigFiles(path string) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	if dir == "" || base == "" {
		return
	}

	dirFd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return
	}
	defer unix.Close(dirFd)

	restrictDirMode(dirFd, dir)
	warnRestrictFailure(path, restrictFileModeAt(dirFd, base, path))

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		name := entry.Name()
		if name == base || !strings.HasPrefix(name, base+".") {
			continue
		}
		sibling := filepath.Join(dir, name)
		warnRestrictFailure(sibling, restrictFileModeAt(dirFd, name, sibling))
	}
}
