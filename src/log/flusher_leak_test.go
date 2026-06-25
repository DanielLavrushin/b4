package log

import (
	"io"
	"testing"

	"go.uber.org/goleak"
)

// TestFlusherNoLeakOnRebuild guards against the buffered-log flusher leaking a
// goroutine on every rebuild. stopFlusherLocked only stopped the ticker, which
// does not close its channel, so each rebuild orphaned the previous
// `for range t.C` goroutine forever. The loop rebuilds repeatedly and ends in
// insta mode (no flusher running), so no flusher goroutine must remain.
func TestFlusherNoLeakOnRebuild(t *testing.T) {
	defer goleak.VerifyNone(t)

	Init(io.Discard, LevelInfo, false) // buffered: starts a flusher
	for i := 0; i < 5; i++ {
		SetInstaflush(true)  // stop the flusher
		SetInstaflush(false) // start a fresh one
	}
	SetInstaflush(true) // final state: no flusher running
}
