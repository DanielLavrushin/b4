package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/daniellavrushin/b4/log"
)

const maxCorruptCopies = 100

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	target := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		target = resolved
	}
	dir := filepath.Dir(target)

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(target)+".tmp-*")
	if err != nil {
		if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EROFS) {
			log.Debugf("Cannot create a temporary file next to %s (%v), writing it in place", target, err)
			return writeFileInPlace(target, data, mode)
		}
		return err
	}
	tmpName := tmp.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	keepOwner(tmp, target)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		if errors.Is(err, syscall.EBUSY) || errors.Is(err, syscall.EXDEV) {
			log.Debugf("Cannot replace %s atomically (%v), writing it in place", target, err)
			return writeFileInPlace(target, data, mode)
		}
		return err
	}
	renamed = true
	syncDir(dir)
	return nil
}

func keepOwner(tmp *os.File, target string) {
	st, err := os.Stat(target)
	if err != nil {
		return
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok || (int(sys.Uid) == os.Geteuid() && int(sys.Gid) == os.Getegid()) {
		return
	}
	if err := tmp.Chown(int(sys.Uid), int(sys.Gid)); err != nil {
		log.Debugf("Cannot keep the owner %d:%d of %s (%v), it will belong to the b4 user", sys.Uid, sys.Gid, target, err)
	}
}

func writeFileInPlace(path string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}

func corruptCopyName(path string, n int) string {
	if n == 0 {
		return path + ".corrupt"
	}
	return fmt.Sprintf("%s.corrupt.%d", path, n)
}

func KeepCorruptCopy(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for n := 0; n < maxCorruptCopies; n++ {
		name := corruptCopyName(path, n)
		existing, err := os.ReadFile(name)
		if err == nil {
			if bytes.Equal(existing, data) {
				return name, nil
			}
			continue
		}
		if !os.IsNotExist(err) {
			continue
		}
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, ConfigFileMode)
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", err
		}
		if _, err := f.Write(data); err != nil {
			_ = f.Close()
			_ = os.Remove(name)
			return "", err
		}
		if err := f.Sync(); err != nil {
			_ = f.Close()
			_ = os.Remove(name)
			return "", err
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(name)
			return "", err
		}
		return name, nil
	}
	return "", fmt.Errorf("%d copies of %s are already kept", maxCorruptCopies, path)
}

func moveCorruptAside(path string) (string, error) {
	for n := 0; n < maxCorruptCopies; n++ {
		name := corruptCopyName(path, n)
		if _, err := os.Lstat(name); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return "", err
		}
		if err := os.Rename(path, name); err != nil {
			return "", err
		}
		return name, nil
	}
	return "", fmt.Errorf("%d copies of %s are already kept", maxCorruptCopies, path)
}
