package metrics

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"runtime/pprof"
	"sync/atomic"

	"github.com/daniellavrushin/b4/log"
)

const (
	maxOSThreads      = 4000
	threadDumpTrigger = maxOSThreads / 2
)

var (
	threadDumpDir  atomic.Value
	threadDumpDone atomic.Bool
)

func ApplyThreadLimit() int {
	debug.SetMaxThreads(maxOSThreads)
	return maxOSThreads
}

func SetThreadDumpDir(dir string) {
	threadDumpDir.Store(dir)
}

func osThreads() int {
	return pprof.Lookup("threadcreate").Count()
}

func checkThreadPressure(n int) {
	if n < threadDumpTrigger {
		return
	}
	if !threadDumpDone.CompareAndSwap(false, true) {
		return
	}
	writeThreadDump(n)
}

func writeThreadDump(n int) {
	log.Errorf("b4 holds %d OS threads against a limit of %d; the Go runtime stops the service once the limit is reached", n, maxOSThreads)

	dir, _ := threadDumpDir.Load().(string)
	if dir == "" {
		log.Errorf("No goroutine dump written: file logging is off (system.logging.directory is empty)")
		return
	}

	path := filepath.Join(dir, "goroutines.txt")
	f, err := os.Create(path)
	if err != nil {
		log.Errorf("Could not open %s for the goroutine dump: %v", path, err)
		return
	}
	defer f.Close()

	if err := pprof.Lookup("goroutine").WriteTo(f, 2); err != nil {
		log.Errorf("Could not write the goroutine dump to %s: %v", path, err)
		return
	}
	log.Errorf("Goroutine dump written to %s", path)
}
