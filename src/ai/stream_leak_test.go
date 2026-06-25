package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/leaktest"
	"go.uber.org/goleak"
)

// TestOllamaStreamUnblocksOnConsumerCancel guards against the streaming
// producer goroutine leaking when the SSE consumer returns early. The server
// streams far more chunks than the channel buffer (16); the consumer reads one
// chunk, cancels, and stops reading. The producer must observe the cancellation
// instead of blocking forever on a full channel. Before the fix, ollama's sends
// were bare (`out <- ch`) and would deadlock here.
func TestOllamaStreamUnblocksOnConsumerCancel(t *testing.T) {
	defer goleak.VerifyNone(t, leaktest.Options()...)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			return
		}
		for i := 0; i < 1000; i++ {
			fmt.Fprint(w, `{"message":{"content":"x"},"done":false}`+"\n")
			fl.Flush()
		}
		<-r.Context().Done()
	}))
	defer srv.Close()

	p := &ollamaProvider{endpoint: srv.URL, model: "m", httpc: &http.Client{}}

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := p.Stream(ctx, Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	<-ch // consume exactly one chunk, then stop reading

	// Let the producer fill the 16-slot buffer and PARK on a send before we
	// cancel. This is essential: if we cancel before the buffer fills, the
	// producer exits via the ctx/error path and never exercises the send block.
	time.Sleep(200 * time.Millisecond)

	cancel() // consumer is gone; the parked producer must now unwind via ctx

	// We must NOT drain ch (that would unblock even a buggy bare-send producer),
	// so the deferred goleak.VerifyNone is what proves the goroutine exited.
	// Settle briefly so it has fully unwound before goleak inspects.
	time.Sleep(200 * time.Millisecond)
}
