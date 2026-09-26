package tables

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func captureTablesLog(t *testing.T) *lockedBuffer {
	t.Helper()
	buf := &lockedBuffer{}
	previous := log.Level(log.CurLevel.Load())
	log.Init(io.Discard, log.LevelInfo, true)
	log.StartCapture(buf)
	t.Cleanup(func() {
		log.StopCapture(buf)
		log.Init(os.Stderr, previous, true)
	})
	return buf
}

func TestRouteWarnNoDestinationNamesUnresolvedTargets(t *testing.T) {
	buf := captureTablesLog(t)

	unresolved := &config.SetConfig{Id: "nodest-asn", Name: "asn-set"}
	unresolved.Targets.ASNs = []string{"15169"}
	empty := &config.SetConfig{Id: "nodest-empty", Name: "empty-set"}
	t.Cleanup(func() {
		routeForgetNoDestinationWarning(unresolved.Id)
		routeForgetNoDestinationWarning(empty.Id)
	})

	routeWarnNoDestination(unresolved)
	routeWarnNoDestination(unresolved)
	routeWarnNoDestination(empty)

	out := buf.String()
	var asnLine, emptyLine string
	asnLines := 0
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(line, "'asn-set'"):
			asnLine = line
			asnLines++
		case strings.Contains(line, "'empty-set'"):
			emptyLine = line
		}
	}
	if asnLines != 1 {
		t.Errorf("the warning must be logged once per state, got %d lines:\n%s", asnLines, out)
	}
	if !strings.Contains(asnLine, "resolve to no domain or address yet") {
		t.Errorf("a set with an unresolved ASN must be told its targets have not resolved, got %q", asnLine)
	}
	if strings.Contains(asnLine, "Match any IP address") {
		t.Errorf("a set with declared targets must not be steered towards a catch-all, got %q", asnLine)
	}
	if !strings.Contains(emptyLine, "has no domain or IP target") || !strings.Contains(emptyLine, "Match any IP address") {
		t.Errorf("a set with no target at all keeps its original advice, got %q", emptyLine)
	}
}
