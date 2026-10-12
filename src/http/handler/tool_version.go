package handler

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/config"
	"golang.org/x/sys/unix"
)

var (
	toolVersionTimeout = 2 * time.Second
	toolVersionBudget  = 5 * time.Second
	toolVersionTrusted = config.InStandardBinDir
	ownExecutable      = os.Executable
	fileDevice         = statDevice
)

const (
	toolVersionMaxLen    = 200
	toolVersionMaxOutput = 4096
)

var busyboxBannerRe = regexp.MustCompile(`BusyBox v\S+`)

type headBuffer struct {
	buf []byte
}

func (h *headBuffer) Write(p []byte) (int, error) {
	if room := toolVersionMaxOutput - len(h.buf); room > 0 {
		h.buf = append(h.buf, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

func (h *headBuffer) String() string {
	return string(h.buf)
}

type toolVersions struct {
	deadline time.Time
	busybox  map[string]string
}

func newToolVersions() *toolVersions {
	return &toolVersions{deadline: time.Now().Add(toolVersionBudget), busybox: map[string]string{}}
}

func (v *toolVersions) of(name, path string) string {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	file := path
	if !toolVersionTrusted(path) {
		if !onOwnDrive(path, real) {
			return ""
		}
		file = real
	}
	if strings.HasPrefix(filepath.Base(real), "busybox") {
		version, seen := v.busybox[real]
		if !seen {
			stdout, stderr, _ := v.run(real, real)
			version = busyboxBannerRe.FindString(stdout + "\n" + stderr)
			v.busybox[real] = version
		}
		return version
	}
	stdout, stderr, err := v.run(file, path, "--version")
	return parseToolVersion(name, stdout, stderr, err == nil)
}

func onOwnDrive(path, real string) bool {
	drive, ok := ownDrive()
	if !ok {
		return false
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return false
	}
	for _, p := range []string{dir, real} {
		if dev, ok := fileDevice(p); !ok || dev != drive {
			return false
		}
	}
	return true
}

func ownDrive() (uint64, bool) {
	self, err := ownExecutable()
	if err != nil {
		return 0, false
	}
	drive, ok := fileDevice(self)
	if !ok {
		return 0, false
	}
	if root, ok := fileDevice("/"); !ok || root == drive {
		return 0, false
	}
	return drive, true
}

func statDevice(path string) (uint64, bool) {
	var st unix.Stat_t
	if err := unix.Stat(path, &st); err != nil {
		return 0, false
	}
	return uint64(st.Dev), true
}

func (v *toolVersions) run(file, name string, args ...string) (string, string, error) {
	timeout := min(toolVersionTimeout, time.Until(v.deadline))
	if timeout <= 0 {
		return "", "", context.DeadlineExceeded
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var stdout, stderr headBuffer
	cmd := exec.CommandContext(ctx, file, args...)
	cmd.Args[0] = name
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func parseToolVersion(name, stdout, stderr string, succeeded bool) string {
	if banner := busyboxBannerRe.FindString(stdout + "\n" + stderr); banner != "" {
		return banner
	}
	line := firstLine(stdout)
	if line == "" && succeeded {
		line = firstLine(stderr)
	}
	line = trimToolName(name, line)
	if !strings.ContainsAny(line, "0123456789") {
		return ""
	}
	if r := []rune(line); len(r) > toolVersionMaxLen {
		line = string(r[:toolVersionMaxLen])
	}
	return line
}

func firstLine(s string) string {
	if lines := nonEmptyLines(s); len(lines) > 0 {
		return strings.TrimSpace(lines[0])
	}
	return ""
}

func trimToolName(name, line string) string {
	if rest, ok := strings.CutPrefix(line, name+"-"); ok {
		return rest
	}
	word, rest, _ := strings.Cut(line, " ")
	if strings.HasPrefix(word, name) || strings.HasPrefix(name, word+"-") {
		return strings.TrimSpace(rest)
	}
	return line
}
