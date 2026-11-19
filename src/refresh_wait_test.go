package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/log"
)

func waitAndCapture(parent context.Context, active func() bool) (string, error) {
	var buf bytes.Buffer
	log.StartCapture(&buf)
	err := waitDiscoveryBeforeRefresh(parent, active)
	log.StopCapture(&buf)
	return buf.String(), err
}

func TestTablesRefreshNamesTheDiscoveryItWaitsFor(t *testing.T) {
	old := discoveryRefreshWait
	discoveryRefreshWait = 50 * time.Millisecond
	t.Cleanup(func() { discoveryRefreshWait = old })

	logged, err := waitAndCapture(context.Background(), func() bool { return true })
	if err == nil || !strings.Contains(err.Error(), "discovery did not finish") {
		t.Fatalf("the timeout does not name Discovery: %v", err)
	}
	if !strings.Contains(logged, "waiting for discovery to finish") {
		t.Fatalf("no warning when the wait starts: %q", logged)
	}

	logged, err = waitAndCapture(context.Background(), func() bool { return false })
	if err != nil {
		t.Fatalf("an idle Discovery failed the refresh: %v", err)
	}
	if strings.Contains(logged, "waiting for discovery") {
		t.Fatalf("warned with no Discovery running: %q", logged)
	}

	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := waitAndCapture(stopped, func() bool { return true }); !errors.Is(err, context.Canceled) {
		t.Fatalf("a refresh during shutdown did not stop with the app context: %v", err)
	}
}
