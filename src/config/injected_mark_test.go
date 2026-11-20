package config

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/log"
)

func captureValidationWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	previous := log.Level(log.CurLevel.Load())
	log.Init(io.Discard, log.LevelInfo, true)
	log.StartCapture(buf)
	injectedBitWarnedMark.Store(0)
	t.Cleanup(func() {
		log.StopCapture(buf)
		log.Init(os.Stderr, previous, true)
		injectedBitWarnedMark.Store(0)
	})
	return buf
}

func validatedMark(t *testing.T, mode string, mark uint) {
	t.Helper()
	cfg := NewConfig()
	cfg.Queue.Mode = mode
	cfg.Queue.Mark = mark
	if err := cfg.Validate(); err != nil {
		t.Fatalf("mode %q, queue mark 0x%x: a mark with the injected bit is warned about, never refused: %v", mode, mark, err)
	}
	if cfg.Queue.Mark != mark {
		t.Fatalf("mode %q: validation changed the queue mark 0x%x to 0x%x", mode, mark, cfg.Queue.Mark)
	}
}

func TestDiscoveryMarksWithTUNReservedBitsAreWarnedAboutNotRefused(t *testing.T) {
	buf := captureValidationWarnings(t)
	tunBitsWarnedMarks.Store(0)
	t.Cleanup(func() { tunBitsWarnedMarks.Store(0) })
	const warning = "the marks the TUN engine reserves"

	validate := func(mode string, flow, injected uint) {
		t.Helper()
		cfg := NewConfig()
		cfg.Queue.Mode = mode
		cfg.System.Checker.DiscoveryFlowMark = flow
		cfg.System.Checker.DiscoveryInjectedMark = injected
		if err := cfg.Validate(); err != nil {
			t.Fatalf("mode %q, Discovery marks 0x%x/0x%x: a config that only Discovery cannot use must still load, got %v", mode, flow, injected, err)
		}
	}

	for i := 0; i < 3; i++ {
		validate("tun", 0x40008001, 0x8002)
	}
	if n := strings.Count(buf.String(), warning); n != 1 {
		t.Fatalf("validating the same marks three times logged the warning %d times, want once:\n%s", n, buf.String())
	}
	for _, marks := range [][2]uint{{0x10008002, 0x8002}, {0x8001, 0x20008002}} {
		validate("tun", marks[0], marks[1])
	}
	if n := strings.Count(buf.String(), warning); n != 3 {
		t.Fatalf("each other pair with a reserved bit must be warned about, got %d warnings:\n%s", n, buf.String())
	}

	buf.Reset()
	validate("nfqueue", 0x40008001, 0x8002)
	validate("tun", 0x100001, 0x100002)
	validate("tun", 0, 0)
	if strings.Contains(buf.String(), warning) {
		t.Fatalf("NFQUEUE mode, which has no carrier, and marks clear of the reserved bits must not warn:\n%s", buf.String())
	}
}

func TestAQueueMarkWithTheInjectedBitIsKeptAndWarnedAboutOnce(t *testing.T) {
	buf := captureValidationWarnings(t)
	const warning = "has bit 0x2000000"
	mark := uint(0x8000 | engine.InjectedMarkBit)

	for i := 0; i < 3; i++ {
		validatedMark(t, "", mark)
	}
	if n := strings.Count(buf.String(), warning); n != 1 {
		t.Fatalf("validating the same mark three times, the way every config clone does, logged the warning %d times, want once:\n%s", n, buf.String())
	}
	if !strings.Contains(buf.String(), "RST protection") {
		t.Fatalf("the warning must say what the bit costs:\n%s", buf.String())
	}

	validatedMark(t, "nfqueue", 0x10000|engine.InjectedMarkBit)
	if n := strings.Count(buf.String(), warning); n != 2 {
		t.Fatalf("another mark with the bit must be warned about again, got %d warnings:\n%s", n, buf.String())
	}

	buf.Reset()
	validatedMark(t, "", 0x8000)
	validatedMark(t, "tun", 0x4000|engine.InjectedMarkBit)
	if strings.Contains(buf.String(), warning) {
		t.Fatalf("a mark without the bit, or the TUN engine, which never adds the bit, must not warn:\n%s", buf.String())
	}
}
