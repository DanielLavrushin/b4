package handler

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/geodat"
	"github.com/daniellavrushin/b4/log"
	"golang.org/x/sys/unix"
)

func traceLimits(t *testing.T, most, least, free int64) {
	t.Helper()
	origMost, origLeast, origFree := traceMaxBytes, traceMinBytes, traceFreeSpace
	traceMaxBytes, traceMinBytes = most, least
	traceFreeSpace = func(string) (int64, bool) { return free, free >= 0 }
	t.Cleanup(func() { traceMaxBytes, traceMinBytes, traceFreeSpace = origMost, origLeast, origFree })
}

func restartTraceAPI(t *testing.T, graceful bool) *http.ServeMux {
	t.Helper()
	if graceful {
		FinishActiveTrace()
	}
	traceMu.Lock()
	if activeTrace != nil {
		activeTrace.timer.Stop()
		log.StopRecording(activeTrace.writer)
		_ = activeTrace.writer.file.Close()
		_ = activeTrace.writer.endFile.Close()
		activeTrace.writer.dir.close()
		activeTrace = nil
	}
	if lastTrace != nil {
		lastTrace.dir.close()
		lastTrace = nil
	}
	traceMu.Unlock()

	api := &API{
		cfgPtr:                 testCfgPtr(mcpTestCfg()),
		overrideServiceManager: func() string { return "standalone" },
		geodataManager:         geodat.NewGeodataManager("", ""),
		mux:                    http.NewServeMux(),
	}
	api.RegisterLogTraceApi()
	RestoreTrace()
	return api.mux
}

func tracePartFile(dir string, part int) string {
	return filepath.Join(dir, tracePartName(part))
}

func startedTraceDir(t *testing.T) *traceDir {
	t.Helper()
	dir, err := newTraceDir(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func waitForTraceEnd(t *testing.T, mux *http.ServeMux, want traceEnd) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		_, resp := doTrace(t, mux, http.MethodGet, "/api/logs/trace/status", "")
		if !resp.Active {
			if resp.EndReason != string(want) || !resp.DownloadReady {
				t.Errorf("status: %+v, want the reason %s", resp, want)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the trace is still running")
		}
	}
}

func traceBytesOnDisk(t *testing.T) int64 {
	t.Helper()
	var total int64
	dirs, _ := filepath.Glob(filepath.Join(os.TempDir(), traceNamePrefix+"*", "*"))
	for _, path := range dirs {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		total += info.Size()
	}
	return total
}

func TestAFullTraceKeepsItsFirstAndNewestLines(t *testing.T) {
	_, mux := newTraceTestAPI(t)
	traceConsole(t, log.LevelInfo)
	traceLimits(t, 40_000, 1, -1)

	startTrace(t, mux)
	peak := int64(0)
	for i := range 5000 {
		log.Debugf("numbered line %05d %s", i, strings.Repeat(".", i/50))
		if i%100 == 0 {
			peak = max(peak, traceBytesOnDisk(t))
		}
	}
	stopTrace(t, mux)

	if reserve := int64(os.Getpagesize()); peak-reserve > 40_000 {
		t.Errorf("the trace took %d bytes, the limit is 40000", peak-reserve)
	}
	trace := downloadedTrace(t, mux)
	wantLines(t, "the trace", trace, []string{"b4 trace session", "numbered line 00000", "numbered line 04999", traceGapLine}, []string{"numbered line 02500"})
	if got := strings.Count(trace, traceGapLine); got != 1 {
		t.Errorf("the gap is marked %d times", got)
	}
	_, after, _ := strings.Cut(trace, traceGapLine)
	if newest := int64(len(after)); newest < 10_000 {
		t.Errorf("only %d bytes of the newest lines are kept, want a quarter of the limit or more", newest)
	}

	kept := strings.Count(trace, "numbered line")
	var lines, leftOut int
	footer := trace[strings.LastIndex(trace, traceFooterStart):]
	if _, err := fmt.Sscanf(footer[strings.Index(footer, "lines: "):], "lines: %d (%d left out)", &lines, &leftOut); err != nil {
		t.Fatalf("footer %q: %v", footer, err)
	}
	if inFile := strings.Count(trace, "[DEBUG] ") + strings.Count(trace, "[INFO] ") + strings.Count(trace, "[TRACE] ") + strings.Count(trace, "[WARN] "); lines-leftOut != inFile {
		t.Errorf("the footer counts %d lines with %d left out, the file holds %d (%d numbered)", lines, leftOut, inFile, kept)
	}
}

func TestATraceUnderItsLimitHoldsEveryLine(t *testing.T) {
	_, mux := newTraceTestAPI(t)
	traceConsole(t, log.LevelInfo)
	traceLimits(t, 40_000, 1, -1)

	startTrace(t, mux)
	for i := range 500 {
		log.Debugf("numbered line %05d", i)
	}
	stopTrace(t, mux)

	trace := downloadedTrace(t, mux)
	if got := strings.Count(trace, "numbered line"); got != 500 {
		t.Errorf("the trace holds %d of 500 lines", got)
	}
	wantLines(t, "the trace", trace, nil, []string{traceGapLine, "left out"})
}

func TestALineLongerThanTheRoomLeftIsCut(t *testing.T) {
	_, mux := newTraceTestAPI(t)
	traceConsole(t, log.LevelInfo)
	traceLimits(t, 40_000, 1, -1)

	startTrace(t, mux)
	log.Debugf("huge %s end of the huge line", strings.Repeat("x", 200_000))
	log.Debugf("the line after it")
	stopTrace(t, mux)

	trace := downloadedTrace(t, mux)
	wantLines(t, "the trace", trace, []string{"[DEBUG] huge xxx", traceCutMark, "the line after it"}, []string{"end of the huge line"})
	if grown := strings.Count(trace, "x"); grown > 12_000 {
		t.Errorf("%d bytes of one line were kept, want a quarter of the limit at most", grown)
	}
}

func TestATraceIsSizedByTheFreeSpace(t *testing.T) {
	cases := []struct {
		free int64
		want int64
	}{
		{-1, 8 << 20},
		{1 << 30, 8 << 20},
		{8 << 20, 2 << 20},
		{1 << 20, 256 << 10},
	}
	for _, tc := range cases {
		traceLimits(t, 8<<20, 256<<10, tc.free)
		if got := traceSizeLimit(t.TempDir(), 0); got != tc.want {
			t.Errorf("free %d: limit %d, want %d", tc.free, got, tc.want)
		}
	}
	traceLimits(t, 8<<20, 256<<10, 8<<20)
	if got := traceSizeLimit(t.TempDir(), 8<<20); got != 4<<20 {
		t.Errorf("limit with 8 MB of the last trace freed = %d, want a quarter of both", got)
	}
}

func TestARefusedTraceKeepsTheLastOne(t *testing.T) {
	_, mux := newTraceTestAPI(t)
	traceConsole(t, log.LevelInfo)
	startTrace(t, mux)
	log.Debugf("from the last trace")
	stopTrace(t, mux)

	traceLimits(t, 8<<20, 256<<10, 1000)
	if rec := serve(mux, http.MethodPost, "/api/logs/trace/start", ""); rec.Code != http.StatusInsufficientStorage {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	wantLines(t, "the trace", downloadedTrace(t, mux), []string{"from the last trace"}, nil)
}

func TestATraceIsRefusedWithoutFreeSpace(t *testing.T) {
	_, mux := newTraceTestAPI(t)
	traceConsole(t, log.LevelInfo)
	traceLimits(t, 8<<20, 256<<10, 1<<20-1)

	rec := serve(mux, http.MethodPost, "/api/logs/trace/start", "")
	if rec.Code != http.StatusInsufficientStorage {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if left := traceBytesOnDisk(t); left != 0 {
		t.Errorf("a refused trace leaves %d bytes behind", left)
	}
	if got := log.Level(log.CurLevel.Load()); got != log.LevelInfo {
		t.Errorf("a refused trace records nothing, the gate is %v", got)
	}
	if _, resp := doTrace(t, mux, http.MethodGet, "/api/logs/trace/status", ""); resp.Active || resp.DownloadReady {
		t.Errorf("status after a refused start: %+v", resp)
	}
}

func TestNetnsMemoryLimitsATraceOnAFileSystemInMemory(t *testing.T) {
	for _, kind := range []string{"tmpfs", "ramfs"} {
		t.Run(kind, func(t *testing.T) {
			mount := t.TempDir()
			options := ""
			if kind == "tmpfs" {
				options = "size=64g"
			}
			if err := unix.Mount(kind, mount, kind, 0, options); err != nil {
				t.Skipf("needs the right to mount a %s: %v", kind, err)
			}
			t.Cleanup(func() { _ = unix.Unmount(mount, unix.MNT_DETACH) })
			mem, ok := availableMemory()
			if !ok || mem >= 64<<30 {
				t.Skipf("this machine has %d bytes of available memory", mem)
			}
			free, known := freeTraceSpace(mount)
			if !known || free < mem/2 || free > mem*2 {
				t.Errorf("free space %d (known %v), want about the %d bytes of available memory", free, known, mem)
			}
		})
	}
}

func TestAvailableMemoryOnOldAndNewKernels(t *testing.T) {
	cases := []struct {
		name    string
		meminfo string
		want    int64
		known   bool
	}{
		{"MemAvailable", "MemTotal: 1000 kB\nMemFree: 100 kB\nMemAvailable:  400 kB\nActive(file): 50 kB\n", 400, true},
		{"no memory left", "MemFree: 100 kB\nMemAvailable:  0 kB\nInactive(file): 300 kB\n", 0, true},
		{"before 3.14", "MemFree: 100 kB\nBuffers: 50 kB\nCached: 900 kB\nActive(file): 40 kB\nInactive(file): 60 kB\nShmem: 200 kB\n", 200, true},
		{"a value no machine has", "MemFree: 100 kB\nMemAvailable: 18446744073709551615 kB\nActive(file): 50 kB\n", 150, true},
		{"nothing known", "", 0, false},
	}
	for _, tc := range cases {
		if got, known := parseAvailableMemory(strings.NewReader(tc.meminfo)); got != tc.want*1024 || known != tc.known {
			t.Errorf("%s: %d, %v, want %d kB, %v", tc.name, got, known, tc.want, tc.known)
		}
	}
}

func TestALongHeaderIsCutToAQuarterOfTheLimit(t *testing.T) {
	diag := Diagnostics{}
	diag.B4.Version = strings.Repeat("v", 50_000)
	sets := []traceSet{{Name: "kept set"}}
	header := string(traceHeader(time.Now(), "a note", diag, sets, 10_000))
	if len(header) > 10_000 {
		t.Errorf("the header takes %d bytes", len(header))
	}
	wantLines(t, "the header", header, []string{"b4 trace session", "note: a note", traceCutMark, "kept set", "\n=========================\n"}, nil)

	whole := string(traceHeader(time.Now(), "", Diagnostics{}, sets, 1<<20))
	wantLines(t, "a short header", whole, []string{"kept set", "\n=========================\n"}, []string{"[cut]"})
}

func TestTheHeaderTakesAQuarterOfTheTraceAtMost(t *testing.T) {
	_, mux := newTraceTestAPI(t)
	traceConsole(t, log.LevelInfo)
	traceLimits(t, 4000, 1, -1)

	startTrace(t, mux)
	stopTrace(t, mux)
	header, _, found := strings.Cut(downloadedTrace(t, mux), "Log trace session started")
	if !found || strings.LastIndexByte(header, '\n') >= 1000 {
		t.Errorf("the header takes %d bytes of a 4000-byte trace", strings.LastIndexByte(header, '\n')+1)
	}
}

func TestAHugeNoteDoesNotReachTheTrace(t *testing.T) {
	_, mux := newTraceTestAPI(t)
	traceConsole(t, log.LevelInfo)
	body := `{"note":"` + strings.Repeat("n", 1<<20) + `"}`
	if rec := serve(mux, http.MethodPost, "/api/logs/trace/start", body); rec.Code != http.StatusOK {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	stopTrace(t, mux)
	wantLines(t, "the trace", downloadedTrace(t, mux), nil, []string{"nnnnnnnn"})
}

func TestATraceWithoutRoomForItsHeaderIsRefused(t *testing.T) {
	_, mux := newTraceTestAPI(t)
	traceConsole(t, log.LevelInfo)
	orig := createTracePart
	createTracePart = func(*traceDir, int) (*os.File, error) { return os.OpenFile("/dev/full", os.O_WRONLY, 0) }
	t.Cleanup(func() { createTracePart = orig })

	if rec := serve(mux, http.MethodPost, "/api/logs/trace/start", ""); rec.Code != http.StatusInsufficientStorage {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if dirs, _ := filepath.Glob(filepath.Join(os.TempDir(), traceNamePrefix+"*")); len(dirs) != 0 {
		t.Errorf("a refused trace leaves %v behind", dirs)
	}
	if got := log.Level(log.CurLevel.Load()); got != log.LevelInfo {
		t.Errorf("a refused trace records nothing, the gate is %v", got)
	}
}

func TestATraceHoldsAPageBackForItsFooter(t *testing.T) {
	_, mux := newTraceTestAPI(t)
	traceConsole(t, log.LevelInfo)
	endFile := func() (os.FileInfo, string) {
		paths, _ := filepath.Glob(filepath.Join(os.TempDir(), traceNamePrefix+"*", traceEndName))
		if len(paths) != 1 {
			t.Fatalf("end files: %v", paths)
		}
		info, err := os.Stat(paths[0])
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(paths[0])
		if err != nil {
			t.Fatal(err)
		}
		return info, string(data)
	}
	check := func(when string, before os.FileInfo, end traceEnd) {
		t.Helper()
		after, text := endFile()
		if !os.SameFile(before, after) {
			t.Errorf("%s: the footer is not written into the page held back for it", when)
		}
		if strings.Contains(text, "\x00") || !strings.HasSuffix(text, traceEndText[end]+traceFooterStop+"\n") {
			t.Errorf("%s: the end file holds %q", when, text)
		}
	}

	startTrace(t, mux)
	held, _ := endFile()
	if held.Size() != int64(os.Getpagesize()) {
		t.Errorf("while recording %d bytes are held back, want one page", held.Size())
	}
	stopTrace(t, mux)
	check("stopped", held, traceEndUser)

	startTrace(t, mux)
	held, _ = endFile()
	restartTraceAPI(t, false)
	check("closed after a crash", held, traceEndInterrupted)
}

func TestAStartThatFailsToCreateItsFolderKeepsTheLastTrace(t *testing.T) {
	_, mux := newTraceTestAPI(t)
	traceConsole(t, log.LevelInfo)
	startTrace(t, mux)
	log.Debugf("from the last trace")
	mux = restartTraceAPI(t, false)

	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	if rec := serve(mux, http.MethodPost, "/api/logs/trace/start", ""); rec.Code != http.StatusInternalServerError {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	wantLines(t, "the trace", downloadedTrace(t, mux), []string{"from the last trace"}, nil)
}

func TestNetnsAFullInodeTableDoesNotKeepTheLastTraceForever(t *testing.T) {
	mount := t.TempDir()
	if err := unix.Mount("tmpfs", mount, "tmpfs", 0, "size=64m,nr_inodes=4"); err != nil {
		t.Skipf("needs the right to mount a tmpfs: %v", err)
	}
	t.Cleanup(func() { _ = unix.Unmount(mount, unix.MNT_DETACH) })
	_, mux := newTraceTestAPI(t)
	t.Setenv("TMPDIR", mount)
	traceConsole(t, log.LevelInfo)
	traceLimits(t, 1<<20, 1, 1<<40)

	startTrace(t, mux)
	log.Debugf("from the last trace")
	mux = restartTraceAPI(t, true)
	startTrace(t, mux)
	log.Debugf("from the new trace")
	stopTrace(t, mux)
	wantLines(t, "the trace", downloadedTrace(t, mux), []string{"from the new trace"}, []string{"from the last trace"})
}

func TestNetnsAFullDiskLeavesRoomForTheReason(t *testing.T) {
	mount := t.TempDir()
	if err := unix.Mount("tmpfs", mount, "tmpfs", 0, "size=1m"); err != nil {
		t.Skipf("needs the right to mount a tmpfs: %v", err)
	}
	t.Cleanup(func() { _ = unix.Unmount(mount, unix.MNT_DETACH) })
	_, mux := newTraceTestAPI(t)
	t.Setenv("TMPDIR", mount)
	traceConsole(t, log.LevelInfo)
	traceLimits(t, 1<<30, 1, 1<<40)

	startTrace(t, mux)
	log.Debugf("before the disk filled up")
	filler, err := os.Create(filepath.Join(mount, "filler"))
	if err != nil {
		t.Fatal(err)
	}
	block := make([]byte, 4096)
	for err == nil {
		_, err = filler.Write(block)
	}
	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		defer filler.Close()
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = filler.Write(block)
			}
		}
	}()
	t.Cleanup(func() {
		close(stop)
		<-stopped
	})
	for range 200 {
		log.Debugf("a line that no longer fits %s", strings.Repeat("x", 100))
	}
	waitForTraceEnd(t, mux, traceEndWriteFailed)
	trace := downloadedTrace(t, mux)
	wantLines(t, "the trace", trace, []string{"before the disk filled up", traceEndText[traceEndWriteFailed]}, nil)

	mux = restartTraceAPI(t, true)
	if _, resp := doTrace(t, mux, http.MethodGet, "/api/logs/trace/status", ""); resp.EndReason != string(traceEndWriteFailed) {
		t.Errorf("status after the restart: %+v", resp)
	}
}

func TestTheRealFreeSpaceIsKnownHere(t *testing.T) {
	free, known := freeTraceSpace(t.TempDir())
	if !known || free <= 0 {
		t.Errorf("free space = %d, known = %v", free, known)
	}
	if mem, ok := availableMemory(); !ok || mem <= 0 {
		t.Errorf("available memory = %d, %v", mem, ok)
	}
	if _, known := freeTraceSpace(filepath.Join(t.TempDir(), "missing")); known {
		t.Error("a folder that is not there has no known free space")
	}
}

func TestATraceThatCannotBeWrittenEnds(t *testing.T) {
	_, mux := newTraceTestAPI(t)
	console := traceConsole(t, log.LevelInfo)

	startTrace(t, mux)
	log.Debugf("before the failure")
	traceMu.Lock()
	_ = activeTrace.writer.file.Close()
	traceMu.Unlock()
	for range 20 {
		log.Debugf("lost line")
	}

	waitForTraceEnd(t, mux, traceEndWriteFailed)
	wantLines(t, "the trace", downloadedTrace(t, mux), []string{"before the failure", traceEndText[traceEndWriteFailed]}, []string{"lost line"})
	for deadline := time.Now().Add(5 * time.Second); !strings.Contains(console.String(), "the trace could not be written"); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("no warning in the log: %q", console.String())
		}
	}
	if got := strings.Count(console.String(), "the trace could not be written"); got != 1 {
		t.Errorf("the failure is reported %d times", got)
	}
	if got := log.Level(log.CurLevel.Load()); got != log.LevelInfo {
		t.Errorf("gate after the failure = %v, want info", got)
	}
}

func TestAFailedWriterReportsOnceAndTakesNoMoreLines(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	w, err := newTraceWriter(startedTraceDir(t), []byte("header\n"), 40_000)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.dir.close)
	reports := make(chan error, 10)
	w.failed = func(err error) { reports <- err }

	_ = w.file.Close()
	_, _ = w.Write([]byte("lost\n"))
	if err := <-reports; err == nil {
		t.Error("the failure is reported without its error")
	}
	w.file, err = os.OpenFile(tracePartFile(w.dir.path, 0), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		_, _ = w.Write([]byte("after the failure\n"))
	}
	w.finish(time.Now(), time.Now(), traceEndWriteFailed)
	select {
	case err := <-reports:
		t.Errorf("reported again: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	data, err := os.ReadFile(tracePartFile(w.dir.path, 0))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "header\n" {
		t.Errorf("the trace holds %q", data)
	}
	if footer, end, ended := w.dir.ending(); !ended || end != traceEndWriteFailed || !strings.Contains(footer, "lines: 0  reason: ") {
		t.Errorf("ending: %q %s %v", footer, end, ended)
	}
}

func TestStoppingATraceUnderLoadLosesNoWriter(t *testing.T) {
	_, mux := newTraceTestAPI(t)
	traceConsole(t, log.LevelInfo)
	traceLimits(t, 20_000, 1, -1)

	stop := make(chan struct{})
	var loggers sync.WaitGroup
	for n := range 4 {
		loggers.Go(func() {
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
					log.Debugf("logger %d line %d", n, i)
				}
			}
		})
	}
	for range 10 {
		startTrace(t, mux)
		_, _ = doTrace(t, mux, http.MethodGet, "/api/logs/trace/status", "")
		time.Sleep(5 * time.Millisecond)
		stopTrace(t, mux)
		trace := downloadedTrace(t, mux)
		if !strings.HasSuffix(trace, traceEndText[traceEndUser]+" ===\n") {
			t.Fatalf("the trace does not end with its footer: %q", trace[max(0, len(trace)-200):])
		}
	}
	close(stop)
	loggers.Wait()
	if dirs, _ := filepath.Glob(filepath.Join(os.TempDir(), traceNamePrefix+"*")); len(dirs) != 1 {
		t.Errorf("only the last trace is kept, found %v", dirs)
	}
}

func TestACrashLeavesTheTraceUpToItsLastLine(t *testing.T) {
	_, mux := newTraceTestAPI(t)
	traceConsole(t, log.LevelInfo)

	startTrace(t, mux)
	log.Debugf("last line before the crash")
	mux = restartTraceAPI(t, false)

	_, resp := doTrace(t, mux, http.MethodGet, "/api/logs/trace/status", "")
	if resp.Active || !resp.DownloadReady || resp.EndReason != string(traceEndInterrupted) {
		t.Fatalf("status after the restart: %+v", resp)
	}
	if !strings.HasPrefix(resp.DownloadName, "b4-trace-2") || !strings.HasSuffix(resp.DownloadName, ".log") {
		t.Errorf("download name %q", resp.DownloadName)
	}
	trace := downloadedTrace(t, mux)
	wantLines(t, "the trace", trace, []string{"b4 trace session", "last line before the crash", traceEndText[traceEndInterrupted]}, nil)

	mux = restartTraceAPI(t, false)
	if again := downloadedTrace(t, mux); again != trace {
		t.Errorf("a second restart changes the trace by %d bytes", len(again)-len(trace))
	}
}

func TestAnOrderlyStopEndsTheTraceWithItsReason(t *testing.T) {
	_, mux := newTraceTestAPI(t)
	traceConsole(t, log.LevelInfo)

	startTrace(t, mux)
	log.Debugf("a line")
	mux = restartTraceAPI(t, true)

	_, resp := doTrace(t, mux, http.MethodGet, "/api/logs/trace/status", "")
	if !resp.DownloadReady || resp.EndReason != string(traceEndShutdown) {
		t.Fatalf("status after the restart: %+v", resp)
	}
	wantLines(t, "the trace", downloadedTrace(t, mux), []string{"a line", traceEndText[traceEndShutdown], "lines: "}, []string{traceEndText[traceEndInterrupted]})
}

func TestATraceTheUserStoppedIsDroppedAtTheNextStart(t *testing.T) {
	_, mux := newTraceTestAPI(t)
	traceConsole(t, log.LevelInfo)

	startTrace(t, mux)
	log.Debugf("a line")
	stopTrace(t, mux)
	mux = restartTraceAPI(t, true)

	if _, resp := doTrace(t, mux, http.MethodGet, "/api/logs/trace/status", ""); resp.DownloadReady {
		t.Errorf("status: %+v", resp)
	}
	if dirs, _ := filepath.Glob(filepath.Join(os.TempDir(), traceNamePrefix+"*")); len(dirs) != 0 {
		t.Errorf("a trace that was already saved keeps its memory: %v", dirs)
	}
}

func TestShuttingDownDoesNotWaitForABusyTraceHandler(t *testing.T) {
	_, mux := newTraceTestAPI(t)
	traceConsole(t, log.LevelInfo)
	startTrace(t, mux)
	traceMu.Lock()
	done := make(chan struct{})
	go func() {
		FinishActiveTrace()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("the shutdown waits for the handler")
	}
	traceMu.Unlock()
	<-done
}

func TestAHalfWrittenFooterCountsAsInterrupted(t *testing.T) {
	_, mux := newTraceTestAPI(t)
	traceConsole(t, log.LevelInfo)

	startTrace(t, mux)
	traceMu.Lock()
	_, _ = activeTrace.writer.endFile.WriteAt([]byte(traceFooterStart+"2026-01-01T00:00:00Z  durat"), 0)
	traceMu.Unlock()
	mux = restartTraceAPI(t, false)
	if _, resp := doTrace(t, mux, http.MethodGet, "/api/logs/trace/status", ""); resp.EndReason != string(traceEndInterrupted) {
		t.Errorf("status: %+v", resp)
	}
	wantLines(t, "the trace", downloadedTrace(t, mux), []string{traceEndText[traceEndInterrupted]}, []string{"durat", "\x00"})
}

func TestACrashBetweenTheFooterAndItsTrimKeepsTheReason(t *testing.T) {
	_, mux := newTraceTestAPI(t)
	traceConsole(t, log.LevelInfo)

	startTrace(t, mux)
	footer := traceFooterStart + "2026-01-01T00:00:00Z  duration: 1s  lines: 2  reason: " + traceEndText[traceEndTimeLimit] + traceFooterStop + "\n"
	traceMu.Lock()
	_, _ = activeTrace.writer.endFile.WriteAt([]byte(footer), 0)
	traceMu.Unlock()
	mux = restartTraceAPI(t, false)
	if _, resp := doTrace(t, mux, http.MethodGet, "/api/logs/trace/status", ""); resp.EndReason != string(traceEndTimeLimit) {
		t.Errorf("status: %+v", resp)
	}
	trace := downloadedTrace(t, mux)
	if !strings.HasSuffix(trace, footer) || strings.Contains(trace, "\x00") {
		t.Errorf("the trace ends with %q", trace[max(0, len(trace)-200):])
	}
}

func activeTraceParts(t *testing.T) (string, int) {
	t.Helper()
	traceMu.Lock()
	defer traceMu.Unlock()
	if activeTrace == nil {
		t.Fatal("the trace ended")
	}
	return activeTrace.writer.dir.path, activeTrace.writer.part
}

func TestAPartSomebodyElseRemovedDoesNotEndTheTrace(t *testing.T) {
	_, mux := newTraceTestAPI(t)
	traceConsole(t, log.LevelInfo)
	traceLimits(t, 40_000, 1, -1)

	startTrace(t, mux)
	for i := range 5000 {
		log.Debugf("numbered line %05d", i)
		if i%50 == 0 {
			dir, current := activeTraceParts(t)
			for part := 1; part < current; part++ {
				_ = os.Remove(tracePartFile(dir, part))
			}
		}
	}
	stopTrace(t, mux)
	wantLines(t, "the trace", downloadedTrace(t, mux), []string{"numbered line 04999", traceEndText[traceEndUser]}, nil)
}

func TestALastTraceThatIsGoneIsNotOffered(t *testing.T) {
	_, mux := newTraceTestAPI(t)
	traceConsole(t, log.LevelInfo)
	startTrace(t, mux)
	stopTrace(t, mux)
	dirs, _ := filepath.Glob(filepath.Join(os.TempDir(), traceNamePrefix+"*"))
	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
	}
	if _, resp := doTrace(t, mux, http.MethodGet, "/api/logs/trace/status", ""); resp.DownloadReady || resp.EndReason != "" {
		t.Errorf("status: %+v", resp)
	}
}

func TestACrashInTheMiddleOfALineKeepsTheFooterOnItsOwnLine(t *testing.T) {
	_, mux := newTraceTestAPI(t)
	traceConsole(t, log.LevelInfo)

	startTrace(t, mux)
	traceMu.Lock()
	_, _ = activeTrace.writer.file.WriteString("half a li")
	traceMu.Unlock()
	mux = restartTraceAPI(t, false)

	wantLines(t, "the trace", downloadedTrace(t, mux), []string{"half a li\n" + traceFooterStart}, nil)
}

func TestACrashAfterTheTraceFilledUpKeepsBothEnds(t *testing.T) {
	_, mux := newTraceTestAPI(t)
	traceConsole(t, log.LevelInfo)
	traceLimits(t, 40_000, 1, -1)

	startTrace(t, mux)
	for i := range 5000 {
		log.Debugf("numbered line %05d", i)
	}
	mux = restartTraceAPI(t, false)

	wantLines(t, "the trace", downloadedTrace(t, mux), []string{"numbered line 00000", traceGapLine, "numbered line 04999", traceEndText[traceEndInterrupted]}, []string{"numbered line 02500"})
}

func TestARestartKeepsOnlyTheNewestTrace(t *testing.T) {
	newTraceTestAPI(t)
	traceConsole(t, log.LevelInfo)

	plant := func(name string, perm os.FileMode, age time.Duration) string {
		dir := filepath.Join(os.TempDir(), name)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		part := tracePartFile(dir, 0)
		if err := os.WriteFile(part, []byte(name+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(-age)
		if err := os.Chtimes(part, at, at); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, perm); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	older := plant("b4-trace-20260101-000000-1", 0o700, 2*time.Hour)
	newer := plant("b4-trace-20260102-000000-2", 0o700, time.Hour)
	oldest := plant("b4-trace-20260103-000000-3", 0o700, 3*time.Hour)
	foreign := plant("b4-trace-20260104-000000-4", 0o755, time.Minute)
	empty := filepath.Join(os.TempDir(), "b4-trace-20260105-000000-5")
	if err := os.Mkdir(empty, 0o700); err != nil {
		t.Fatal(err)
	}
	oldFormat := filepath.Join(os.TempDir(), "b4-trace-123456.log")
	if err := os.WriteFile(oldFormat, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	notes := plant("b4-trace-notes", 0o700, 0)
	dated := plant("b4-trace-20260108-000000-notes", 0o700, 0)
	if err := os.WriteFile(filepath.Join(older, "kept.txt"), []byte("not a trace"), 0o600); err != nil {
		t.Fatal(err)
	}
	saved := filepath.Join(os.TempDir(), "b4-trace-20260107-000000.log")
	if err := os.WriteFile(saved, []byte("a copy the user keeps"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(os.TempDir(), "b4-trace-20260106-000000-6")
	if err := os.Symlink(filepath.Dir(target), link); err != nil {
		t.Fatal(err)
	}

	mux := restartTraceAPI(t, false)
	_, resp := doTrace(t, mux, http.MethodGet, "/api/logs/trace/status", "")
	if resp.DownloadName != "b4-trace-20260102-000000.log" {
		t.Errorf("adopted %q", resp.DownloadName)
	}
	wantLines(t, "the trace", downloadedTrace(t, mux), []string{filepath.Base(newer), traceEndText[traceEndInterrupted]}, nil)
	for _, gone := range []string{tracePartFile(older, 0), oldest, empty, oldFormat} {
		if _, err := os.Lstat(gone); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s is still there", filepath.Base(gone))
		}
	}
	for _, kept := range []string{newer, foreign, link, saved, tracePartFile(notes, 0), tracePartFile(dated, 0), filepath.Join(older, "kept.txt")} {
		if _, err := os.Lstat(kept); err != nil {
			t.Errorf("%s: %v", filepath.Base(kept), err)
		}
	}
	if got, _ := os.ReadFile(target); string(got) != "untouched" {
		t.Errorf("the link target holds %q", got)
	}
	if entries, _ := os.ReadDir(foreign); len(entries) != 1 {
		t.Errorf("a folder others can read is left alone, it holds %v", entries)
	}
}

func TestAFinishedTraceOfAnotherRunningB4IsLeftAlone(t *testing.T) {
	for _, how := range []string{"stopped", "restored"} {
		t.Run(how, func(t *testing.T) {
			_, mux := newTraceTestAPI(t)
			traceConsole(t, log.LevelInfo)
			startTrace(t, mux)
			log.Debugf("a line")
			if how == "stopped" {
				stopTrace(t, mux)
			} else {
				mux = restartTraceAPI(t, false)
			}
			if other := leftoverTrace(); other != nil {
				other.dir.close()
				t.Fatalf("a second b4 took over the trace %s offers", how)
			}
			wantLines(t, "the trace", downloadedTrace(t, mux), []string{"a line"}, nil)
		})
	}
}

func TestANewTraceDoesNotUseAFolderAnotherB4Took(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	first := startedTraceDir(t)
	info, err := os.Lstat(first.path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openTraceDir(first.path, info); !errors.Is(err, errTraceTaken) {
		t.Errorf("opening a locked folder: %v", err)
	}
	first.close()

	gone := startedTraceDir(t)
	info, err = os.Lstat(gone.path)
	if err != nil {
		t.Fatal(err)
	}
	_ = unix.Flock(int(gone.lock.Fd()), unix.LOCK_UN)
	if err := os.Remove(gone.path); err != nil {
		t.Fatal(err)
	}
	if gone.take(info) {
		t.Error("a folder somebody else removed was accepted")
	}
	gone.close()

	info, err = os.Lstat(first.path)
	if err != nil {
		t.Fatal(err)
	}
	swapped := filepath.Join(os.TempDir(), "swapped")
	if err := os.Rename(first.path, swapped); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(first.path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := openTraceDir(first.path, info); !errors.Is(err, errTraceTaken) {
		t.Errorf("opening a folder that replaced the one looked at: %v", err)
	}
}

func TestAFolderSwappedWhileRecordingIsNotWrittenTo(t *testing.T) {
	_, mux := newTraceTestAPI(t)
	traceConsole(t, log.LevelInfo)
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	startTrace(t, mux)
	log.Debugf("a line")
	dir, _ := activeTraceParts(t)
	if err := os.Rename(dir, dir+".moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{tracePartName(0), traceEndName} {
		if err := os.Symlink(victim, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	stopTrace(t, mux)

	if got, _ := os.ReadFile(victim); string(got) != "secret\n" {
		t.Errorf("stopping the trace wrote %q into a file outside it", got)
	}
	wantLines(t, "the trace", downloadedTrace(t, mux), []string{"a line", traceEndText[traceEndUser]}, []string{"secret"})
}

func TestARestartLeavesTheTraceOfAnotherRunningB4Alone(t *testing.T) {
	_, mux := newTraceTestAPI(t)
	traceConsole(t, log.LevelInfo)

	startTrace(t, mux)
	log.Debugf("a line")
	traceMu.Lock()
	other := activeTrace
	activeTrace = nil
	traceMu.Unlock()

	second := restartTraceAPI(t, false)
	if _, resp := doTrace(t, second, http.MethodGet, "/api/logs/trace/status", ""); resp.DownloadReady {
		t.Errorf("a trace that is still being written was taken over: %+v", resp)
	}
	startTrace(t, second)
	stopTrace(t, second)
	resetTraceState()

	traceMu.Lock()
	activeTrace = other
	traceMu.Unlock()
	log.Debugf("another line")
	stopTrace(t, mux)
	trace := downloadedTrace(t, mux)
	wantLines(t, "the trace", trace, []string{"a line", "another line"}, []string{traceEndText[traceEndInterrupted]})
	if got := strings.Count(trace, traceFooterStart); got != 1 {
		t.Errorf("the trace has %d footers", got)
	}
}

func TestANewTraceReplacesTheLastOne(t *testing.T) {
	_, mux := newTraceTestAPI(t)
	traceConsole(t, log.LevelInfo)

	startTrace(t, mux)
	log.Debugf("from the first trace")
	if _, resp := doTrace(t, mux, http.MethodGet, "/api/logs/trace/status", ""); resp.Lines < 2 {
		t.Errorf("status while recording counts %d lines", resp.Lines)
	}
	stopTrace(t, mux)
	startTrace(t, mux)
	if _, resp := doTrace(t, mux, http.MethodGet, "/api/logs/trace/status", ""); resp.DownloadReady || resp.EndReason != "" {
		t.Errorf("status while the second trace records: %+v", resp)
	}
	log.Debugf("from the second trace")
	stopTrace(t, mux)

	wantLines(t, "the trace", downloadedTrace(t, mux), []string{"from the second trace"}, []string{"from the first trace"})
	if dirs, _ := filepath.Glob(filepath.Join(os.TempDir(), traceNamePrefix+"*")); len(dirs) != 1 {
		t.Errorf("only the last trace is kept, found %v", dirs)
	}
}

func TestATraceIsReadableByItsOwnerOnly(t *testing.T) {
	_, mux := newTraceTestAPI(t)
	traceConsole(t, log.LevelInfo)
	startTrace(t, mux)

	dirs, _ := filepath.Glob(filepath.Join(os.TempDir(), traceNamePrefix+"*"))
	if len(dirs) != 1 {
		t.Fatalf("found %v", dirs)
	}
	for _, path := range []string{dirs[0], tracePartFile(dirs[0], 0)} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s has mode %v", path, info.Mode())
		}
	}
}

func TestATraceEndedByTheTimeLimitSaysSo(t *testing.T) {
	_, mux := newTraceTestAPI(t)
	traceConsole(t, log.LevelInfo)
	orig := traceMaxDuration
	traceMaxDuration = 20 * time.Millisecond
	t.Cleanup(func() { traceMaxDuration = orig })

	startTrace(t, mux)
	waitForTraceEnd(t, mux, traceEndTimeLimit)
	mux = restartTraceAPI(t, false)
	if _, resp := doTrace(t, mux, http.MethodGet, "/api/logs/trace/status", ""); resp.EndReason != string(traceEndTimeLimit) {
		t.Errorf("the reason is read back after a restart: %+v", resp)
	}
}
