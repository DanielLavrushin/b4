package handler

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	traceNamePrefix  = "b4-trace-"
	traceStampLayout = "20060102-150405"
	tracePartSuffix  = ".log"
	traceEndName     = "end"
	traceFooterStart = "=== b4 trace ended: "
	traceFooterStop  = " ==="
	traceGapLine     = "=== lines left out here: the trace reached its size limit, so it keeps its first and its newest lines ===\n"
	traceCutMark     = " [cut]\n"
	traceDirAttempts = 3
)

var (
	traceMaxBytes   int64 = 8 << 20
	traceMinBytes   int64 = 256 << 10
	traceSpaceShare int64 = 4
	traceFreeSpace        = freeTraceSpace
	createTracePart       = (*traceDir).createPart
)

var traceNewline = []byte{'\n'}

type traceEnd string

const (
	traceEndUser        traceEnd = "user"
	traceEndTimeLimit   traceEnd = "time_limit"
	traceEndWriteFailed traceEnd = "write_failed"
	traceEndShutdown    traceEnd = "shutdown"
	traceEndInterrupted traceEnd = "interrupted"
)

var traceEndText = map[traceEnd]string{
	traceEndUser:        "stopped by user",
	traceEndTimeLimit:   "auto-stopped (max duration reached)",
	traceEndWriteFailed: "stopped, the trace could not be written",
	traceEndShutdown:    "b4 was stopped or restarted",
	traceEndInterrupted: "interrupted, b4 ended without closing the trace",
}

var errTraceTaken = errors.New("the trace folder belongs to another b4")

type traceDir struct {
	path string
	root *os.Root
	lock *os.File
}

func newTraceDir(startedAt time.Time) (*traceDir, error) {
	var err error
	for range traceDirAttempts {
		var path string
		path, err = os.MkdirTemp("", traceNamePrefix+startedAt.Format(traceStampLayout)+"-*")
		if err != nil {
			return nil, err
		}
		var info os.FileInfo
		if info, err = os.Lstat(path); err != nil {
			continue
		}
		var d *traceDir
		if d, err = openTraceDir(path, info); err == nil {
			return d, nil
		}
		if !errors.Is(err, errTraceTaken) && !errors.Is(err, os.ErrNotExist) {
			_ = os.Remove(path)
			return nil, err
		}
	}
	return nil, err
}

func openTraceDir(path string, info os.FileInfo) (*traceDir, error) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	d := &traceDir{path: path, root: root}
	if d.lock, err = root.Open("."); err != nil {
		d.close()
		return nil, err
	}
	if !d.take(info) {
		d.close()
		return nil, errTraceTaken
	}
	return d, nil
}

func (d *traceDir) take(seen os.FileInfo) bool {
	opened, err := d.lock.Stat()
	if err != nil || !os.SameFile(seen, opened) {
		return false
	}
	if errors.Is(unix.Flock(int(d.lock.Fd()), unix.LOCK_EX|unix.LOCK_NB), unix.EWOULDBLOCK) {
		return false
	}
	current, err := os.Lstat(d.path)
	return err == nil && os.SameFile(current, opened)
}

func privateDir(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && info.IsDir() && info.Mode().Perm()&0o077 == 0 && int(st.Uid) == os.Geteuid()
}

func (d *traceDir) close() {
	_ = d.lock.Close()
	_ = d.root.Close()
}

func (d *traceDir) remove() {
	parts, _ := d.parts()
	for _, part := range parts {
		_ = d.root.Remove(tracePartName(part))
	}
	_ = d.root.Remove(traceEndName)
	_ = os.Remove(d.path)
	d.close()
}

func tracePartName(part int) string {
	return fmt.Sprintf("%06d%s", part, tracePartSuffix)
}

func (d *traceDir) createPart(part int) (*os.File, error) {
	return d.root.OpenFile(tracePartName(part), os.O_CREATE|os.O_EXCL|os.O_WRONLY|os.O_APPEND, 0600)
}

func (d *traceDir) parts() ([]int, error) {
	entries, err := fs.ReadDir(d.root.FS(), ".")
	if err != nil {
		return nil, err
	}
	parts := make([]int, 0, len(entries))
	for _, e := range entries {
		part, err := strconv.Atoi(strings.TrimSuffix(e.Name(), tracePartSuffix))
		if err == nil && e.Type().IsRegular() && e.Name() == tracePartName(part) {
			parts = append(parts, part)
		}
	}
	return parts, nil
}

func (d *traceDir) open() ([]io.ReadCloser, error) {
	parts, err := d.parts()
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, os.ErrNotExist
	}
	var out []io.ReadCloser
	next := 0
	for _, part := range parts {
		f, err := d.root.Open(tracePartName(part))
		if err != nil {
			closeTrace(out)
			return nil, err
		}
		if part != next {
			out = append(out, io.NopCloser(strings.NewReader(traceGapLine)))
		}
		out = append(out, f)
		next = part + 1
	}
	footer, _, _ := d.ending()
	return append(out, io.NopCloser(strings.NewReader(footer))), nil
}

func closeTrace(readers []io.ReadCloser) {
	for _, r := range readers {
		_ = r.Close()
	}
}

func (d *traceDir) ending() (footer string, end traceEnd, ended bool) {
	data, err := d.root.ReadFile(traceEndName)
	if err != nil {
		return "", "", false
	}
	footer, _, _ = strings.Cut(string(data), "\x00")
	line := strings.Trim(footer, "\n")
	if !strings.HasPrefix(line, traceFooterStart) || !strings.HasSuffix(line, traceFooterStop) {
		return "", "", false
	}
	for end, text := range traceEndText {
		if strings.HasSuffix(line, "reason: "+text+traceFooterStop) {
			return footer, end, true
		}
	}
	return footer, "", true
}

func (d *traceDir) finish(endFile *os.File, part int, summary string, end traceEnd) {
	footer := traceFooterStart + summary + "  reason: " + traceEndText[end] + traceFooterStop + "\n"
	if !d.endsWithNewline(part) {
		footer = "\n" + footer
	}
	if _, err := endFile.WriteAt([]byte(footer), 0); err == nil {
		_ = endFile.Truncate(int64(len(footer)))
		_ = endFile.Sync()
	}
	_ = endFile.Close()
}

func (d *traceDir) endsWithNewline(part int) bool {
	f, err := d.root.Open(tracePartName(part))
	if err != nil {
		return true
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() == 0 {
		return true
	}
	last := make([]byte, 1)
	_, err = f.ReadAt(last, st.Size()-1)
	return err != nil || last[0] == '\n'
}

type traceWriter struct {
	dir          *traceDir
	file         *os.File
	endFile      *os.File
	part         int
	partSize     int64
	room         int64
	lines        atomic.Int64
	leftOut      int64
	partLines    int64
	previousPart int64
	err          error
	failed       func(error)
}

func newTraceWriter(dir *traceDir, header []byte, limit int64) (*traceWriter, error) {
	t := &traceWriter{dir: dir, partSize: limit / 4, room: limit/2 - int64(len(header))}
	if err := t.begin(header); err != nil {
		_ = t.file.Close()
		_ = t.endFile.Close()
		dir.remove()
		return nil, err
	}
	return t, nil
}

func (t *traceWriter) begin(header []byte) (err error) {
	if t.file, err = createTracePart(t.dir, 0); err != nil {
		return err
	}
	if _, err = t.file.Write(header); err != nil {
		return err
	}
	if t.endFile, err = t.dir.root.OpenFile(traceEndName, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600); err != nil {
		return err
	}
	_, err = t.endFile.Write(make([]byte, os.Getpagesize()))
	return err
}

func (t *traceWriter) Write(p []byte) (int, error) {
	if t.err != nil {
		return len(p), nil
	}
	if t.err = t.write(p); t.err != nil && t.failed != nil {
		go t.failed(t.err)
	}
	return len(p), nil
}

func (t *traceWriter) write(p []byte) error {
	p = cutTrace(p, t.partSize)
	if int64(len(p)) > t.room {
		if err := t.nextPart(); err != nil {
			return err
		}
	}
	n, err := t.file.Write(p)
	t.room -= int64(n)
	if err != nil {
		return err
	}
	lines := int64(bytes.Count(p, traceNewline))
	t.lines.Add(lines)
	t.partLines += lines
	return nil
}

func cutTrace(p []byte, most int64) []byte {
	if int64(len(p)) <= most {
		return p
	}
	return slices.Concat(p[:max(0, most-int64(len(traceCutMark)))], []byte(traceCutMark))
}

func (t *traceWriter) nextPart() error {
	if err := t.file.Close(); err != nil {
		return err
	}
	if t.part > 1 {
		if err := t.dir.root.Remove(tracePartName(t.part - 1)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		t.leftOut += t.previousPart
	}
	t.previousPart, t.partLines = t.partLines, 0
	f, err := createTracePart(t.dir, t.part+1)
	if err != nil {
		return err
	}
	t.file, t.room = f, t.partSize
	t.part++
	return nil
}

func (t *traceWriter) finish(startedAt, endedAt time.Time, end traceEnd) *finishedTrace {
	lines := strconv.FormatInt(t.lines.Load(), 10)
	if t.leftOut > 0 {
		lines += fmt.Sprintf(" (%d left out)", t.leftOut)
	}
	_ = t.file.Close()
	t.dir.finish(t.endFile, t.part, fmt.Sprintf("%s  duration: %s  lines: %s",
		endedAt.UTC().Format(time.RFC3339), endedAt.Sub(startedAt).Round(time.Millisecond), lines), end)
	return &finishedTrace{dir: t.dir, name: traceDownloadName(t.dir.path), end: end}
}

func traceSizeLimit(dir string, freed int64) int64 {
	limit := traceMaxBytes
	if free, known := traceFreeSpace(dir); known {
		limit = min(limit, (free+freed)/traceSpaceShare)
	}
	return limit
}

func freeTraceSpace(dir string) (int64, bool) {
	var stat unix.Statfs_t
	if err := unix.Statfs(dir, &stat); err != nil {
		return 0, false
	}
	free, known := int64(stat.Bavail)*int64(stat.Bsize), stat.Blocks > 0
	switch uint32(stat.Type) {
	case unix.TMPFS_MAGIC, unix.RAMFS_MAGIC:
		if mem, ok := availableMemory(); ok && (!known || mem < free) {
			return mem, true
		}
	}
	return free, known
}

func availableMemory() (int64, bool) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, false
	}
	defer f.Close()
	return parseAvailableMemory(f)
}

func parseAvailableMemory(meminfo io.Reader) (int64, bool) {
	kb := map[string]int64{}
	scanner := bufio.NewScanner(meminfo)
	for scanner.Scan() {
		name, _, _ := strings.Cut(scanner.Text(), ":")
		kb[name] = int64(parseMemInfoKB(scanner.Text()))
	}
	if available, ok := kb["MemAvailable"]; ok {
		return available * 1024, true
	}
	free, ok := kb["MemFree"]
	return (free + kb["Active(file)"] + kb["Inactive(file)"]) * 1024, ok
}

type finishedTrace struct {
	dir  *traceDir
	name string
	end  traceEnd
}

func (t *finishedTrace) remove() {
	if t != nil {
		t.dir.remove()
	}
}

func (t *finishedTrace) size() int64 {
	if t == nil {
		return 0
	}
	var total int64
	parts, _ := t.dir.parts()
	for _, part := range parts {
		if info, err := t.dir.root.Lstat(tracePartName(part)); err == nil {
			total += info.Size()
		}
	}
	return total
}

func traceDownloadName(dir string) string {
	at, ok := traceStamp(dir)
	if !ok {
		return "b4-trace.log"
	}
	return traceNamePrefix + at.Format(traceStampLayout) + tracePartSuffix
}

func traceStamp(dir string) (time.Time, bool) {
	name := strings.TrimPrefix(filepath.Base(dir), traceNamePrefix)
	i := strings.LastIndexByte(name, '-')
	if i < 0 || !allDigits(name[i+1:]) {
		return time.Time{}, false
	}
	at, err := time.Parse(traceStampLayout, name[:i])
	return at, err == nil
}

func allDigits(s string) bool {
	_, err := strconv.ParseUint(s, 10, 64)
	return err == nil
}

func traceFileOfAnOlderVersion(path string, info os.FileInfo) bool {
	number, found := strings.CutSuffix(strings.TrimPrefix(filepath.Base(path), traceNamePrefix), tracePartSuffix)
	return found && allDigits(number) && info.Mode().IsRegular()
}

func leftoverTrace() *finishedTrace {
	matches, err := filepath.Glob(filepath.Join(os.TempDir(), traceNamePrefix+"*"))
	if err != nil {
		return nil
	}
	var newest *finishedTrace
	var newestPart leftoverPart
	for _, path := range matches {
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		if traceFileOfAnOlderVersion(path, info) {
			_ = os.Remove(path)
			continue
		}
		if _, stamped := traceStamp(path); !stamped || !privateDir(info) {
			continue
		}
		dir, err := openTraceDir(path, info)
		if err != nil {
			continue
		}
		last, ok := dir.lastPart()
		found := &finishedTrace{dir: dir, name: traceDownloadName(path), end: last.end}
		if !ok || found.end == traceEndUser || (newest != nil && !last.written.After(newestPart.written)) {
			found.remove()
			continue
		}
		newest.remove()
		newest, newestPart = found, last
	}
	if newest != nil && !newestPart.ended {
		newest.end = traceEndInterrupted
		if endFile, err := newest.dir.root.OpenFile(traceEndName, os.O_CREATE|os.O_RDWR, 0600); err == nil {
			newest.dir.finish(endFile, newestPart.number, newestPart.written.UTC().Format(time.RFC3339), traceEndInterrupted)
		}
	}
	return newest
}

type leftoverPart struct {
	number  int
	written time.Time
	end     traceEnd
	ended   bool
}

func (d *traceDir) lastPart() (leftoverPart, bool) {
	parts, err := d.parts()
	if err != nil || len(parts) == 0 {
		return leftoverPart{}, false
	}
	last := leftoverPart{number: parts[len(parts)-1]}
	info, err := d.root.Lstat(tracePartName(last.number))
	if err != nil {
		return leftoverPart{}, false
	}
	last.written = info.ModTime()
	_, last.end, last.ended = d.ending()
	return last, true
}
