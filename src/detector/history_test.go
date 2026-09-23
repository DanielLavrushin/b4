package detector

import (
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestUpdateHistoryKeepsEveryConcurrentRun(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	const runs = 40

	var shrank atomic.Int64
	stop := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		seen := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			n := len(LoadHistory(cfgPath).Entries)
			if n < seen {
				shrank.Add(1)
			}
			if n > seen {
				seen = n
			}
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < runs; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := &Suite{Id: fmt.Sprintf("run-%d", i), Options: Options{Scopes: []Scope{ScopeSites}}}
			SaveToHistory(s, cfgPath, StatusComplete)
		}(i)
	}
	wg.Wait()
	close(stop)
	<-readerDone

	if n := len(LoadHistory(cfgPath).Entries); n != runs {
		t.Fatalf("history holds %d of %d runs; a run finishing while another is saved, or while an entry is deleted, must not be lost", n, runs)
	}
	if shrank.Load() > 0 {
		t.Fatalf("a reader saw the history shrink %d times; a half-written file reads as empty, and a run saving right then would replace the whole history with its own entry", shrank.Load())
	}
}
