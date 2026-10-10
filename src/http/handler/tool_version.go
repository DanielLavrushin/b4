package handler

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var toolVersionTimeout = 2 * time.Second

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
	busybox map[string]string
}

func newToolVersions() *toolVersions {
	return &toolVersions{busybox: map[string]string{}}
}

func (v *toolVersions) of(name, path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil && strings.HasPrefix(filepath.Base(real), "busybox") {
		version, seen := v.busybox[real]
		if !seen {
			stdout, stderr, _ := runToolVersion(real)
			version = busyboxBannerRe.FindString(stdout + "\n" + stderr)
			v.busybox[real] = version
		}
		return version
	}
	stdout, stderr, err := runToolVersion(path, "--version")
	return parseToolVersion(name, stdout, stderr, err == nil)
}

func runToolVersion(path string, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), toolVersionTimeout)
	defer cancel()

	var stdout, stderr headBuffer
	cmd := exec.CommandContext(ctx, path, args...)
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
