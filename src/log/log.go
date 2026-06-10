package log

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"log/syslog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

type Level int32

const (
	LevelError Level = iota
	LevelInfo
	LevelTrace
	LevelDebug
)

var (
	CurLevel         atomic.Int32
	errFile          *os.File
	errLogger        *log.Logger
	errMu            sync.Mutex
	origStderr       int
	origStderrFile   *os.File
	errSessionHeader string
	errHeaderPending bool
)

type multi struct {
	mu sync.Mutex
	ws []io.Writer
}

func (m *multi) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, w := range m.ws {
		_, _ = w.Write(p)
	}
	return len(p), nil
}

var (
	mu         sync.Mutex
	base       = &multi{ws: []io.Writer{os.Stderr}}
	buf        *bufio.Writer
	logger     *log.Logger
	flushTimer *time.Ticker
	insta      bool
)

func Init(stderr io.Writer, level Level, instaflush bool) {

	mu.Lock()
	defer mu.Unlock()
	if stderr == nil {
		stderr = os.Stderr
	}
	base.ws = []io.Writer{stderr}
	insta = instaflush
	CurLevel.Store(int32(level))
	rebuildLocked()
}

func AttachSyslog(w io.Writer) {
	if w == nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	base.ws = append(base.ws, w)
	rebuildLocked()
}

func EnableSyslog(tag string) error {
	sw, err := syslog.New(syslog.LOG_INFO|syslog.LOG_DAEMON, tag)
	if err != nil {
		return err
	}
	AttachSyslog(sw)
	return nil
}

func SetLevel(l Level) { CurLevel.Store(int32(l)) }

func SetInstaflush(v bool) {
	mu.Lock()
	defer mu.Unlock()
	if insta == v {
		return
	}
	insta = v
	if buf != nil && v {
		_ = buf.Flush()
	}
	rebuildLocked()
}

func Flush() {
	mu.Lock()
	defer mu.Unlock()
	if buf != nil {
		_ = buf.Flush()
	}
}

func InitErrorFile(path string) error {
	if path == "" {
		return nil
	}
	errMu.Lock()
	defer errMu.Unlock()
	return openErrorFileLocked(path)
}

func SetErrorFile(path string) error {
	errMu.Lock()
	defer errMu.Unlock()

	if path == "" {
		if origStderr != 0 {
			unix.Dup2(origStderr, int(os.Stderr.Fd()))
		}
		if errFile != nil {
			_ = errFile.Sync()
			_ = errFile.Close()
			errFile = nil
			errLogger = nil
		}
		errHeaderPending = false
		return nil
	}
	return openErrorFileLocked(path)
}

func openErrorFileLocked(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	if st, err := os.Stat(path); err == nil && st.Size() > 1<<20 {
		_ = os.Rename(path, path+".1")
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	// Capture the true original stderr (terminal / journald pipe) once, before
	// redirecting fd 2. Verbose console logging keeps flowing to it via the
	// MultiWriter set up in Init, so the error file stays error-level only;
	// fd 2 is redirected to the file only so Go panics/fatals are captured.
	if origStderr == 0 {
		origStderr, _ = unix.Dup(int(os.Stderr.Fd()))
	}
	unix.Dup2(int(f.Fd()), int(os.Stderr.Fd()))

	old := errFile
	errFile = f
	errLogger = log.New(f, "", log.Ldate|log.Ltime|log.Lmicroseconds)

	errSessionHeader = fmt.Sprintf("=== b4 error log opened pid=%d at %s ===\n",
		os.Getpid(), time.Now().Format(time.RFC3339))
	errHeaderPending = true

	if old != nil {
		_ = old.Sync()
		_ = old.Close()
	}

	return nil
}

func OrigStderr() *os.File {
	errMu.Lock()
	defer errMu.Unlock()
	if origStderrFile == nil {
		if origStderr == 0 {
			origStderr, _ = unix.Dup(int(os.Stderr.Fd()))
		}
		origStderrFile = os.NewFile(uintptr(origStderr), "stderr")
	}
	return origStderrFile
}

func CloseErrorFile() {
	errMu.Lock()
	defer errMu.Unlock()
	if errFile != nil {
		_ = errFile.Sync()
		_ = errFile.Close()
		errFile = nil
		errLogger = nil
	}
}

func Errorf(format string, a ...any) error {
	msg := fmt.Sprintf("[ERROR] "+format, a...)
	out("%s", msg)

	errMu.Lock()
	if errLogger != nil {
		if errHeaderPending && errFile != nil {
			_, _ = errFile.WriteString(errSessionHeader)
			errHeaderPending = false
		}
		errLogger.Println(msg)
		if errFile != nil {
			_ = errFile.Sync()
		}
	}
	errMu.Unlock()

	return fmt.Errorf(format, a...)
}

func Warnf(format string, a ...any) {
	if Level(CurLevel.Load()) >= LevelError {
		out("[WARN] "+format, a...)
	}
}

func Infof(format string, a ...any) {
	if Level(CurLevel.Load()) >= LevelInfo {
		out("[INFO] "+format, a...)
	}
}

func Tracef(format string, a ...any) {
	if Level(CurLevel.Load()) >= LevelTrace {
		out("[TRACE] "+format, a...)
	}
}

func Debugf(format string, a ...any) {
	if Level(CurLevel.Load()) >= LevelDebug {
		out("[DEBUG] "+format, a...)
	}
}

func out(format string, a ...any) {
	mu.Lock()
	defer mu.Unlock()
	if logger == nil {
		rebuildLocked()
	}
	logger.Printf(format, a...)
}

func rebuildLocked() {
	var w io.Writer = base
	if insta {
		buf = nil
		logger = log.New(w, "", log.Ldate|log.Ltime|log.Lmicroseconds)
		stopFlusherLocked()
		return
	}

	buf = bufio.NewWriterSize(w, 16*1024)
	logger = log.New(buf, "", log.Ldate|log.Ltime|log.Lmicroseconds)
	startFlusherLocked()
}

func startFlusherLocked() {
	stopFlusherLocked()
	flushTimer = time.NewTicker(2 * time.Second)
	go func(t *time.Ticker) {
		for range t.C {
			mu.Lock()
			if buf != nil {
				_ = buf.Flush()
			}
			mu.Unlock()
		}
	}(flushTimer)
}

func stopFlusherLocked() {
	if flushTimer != nil {
		flushTimer.Stop()
		flushTimer = nil
	}
}

func LevelFromVerbose(verbose int) Level {
	switch verbose {
	case 2:
		return LevelTrace
	case 1:
		return LevelInfo
	default:
		return LevelError
	}
}

func Info(a ...any)  { Infof("%s", fmt.Sprint(a...)) }
func Trace(a ...any) { Tracef("%s", fmt.Sprint(a...)) }
func Error(a ...any) { Errorf("%s", fmt.Sprint(a...)) }
