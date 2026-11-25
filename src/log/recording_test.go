package log

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
)

func recordingFixture(t *testing.T, level Level) (console, rec *bytes.Buffer) {
	t.Helper()
	previous := ConfiguredLevel()
	console, rec = &bytes.Buffer{}, &bytes.Buffer{}
	Init(console, level, true)
	t.Cleanup(func() {
		StopRecording(rec)
		Init(os.Stderr, previous, true)
	})
	return console, rec
}

func TestARecordingTakesDebugWhileTheConsoleKeepsItsLevel(t *testing.T) {
	console, rec := recordingFixture(t, LevelInfo)
	StartRecording(rec, LevelDebug)

	Infof("info line")
	Tracef("trace line")
	Debugf("debug line")

	for _, want := range []string{"[INFO] info line", "[TRACE] trace line", "[DEBUG] debug line"} {
		if !strings.Contains(rec.String(), want) {
			t.Errorf("the recording misses %q: %q", want, rec.String())
		}
	}
	if got := console.String(); !strings.Contains(got, "info line") || strings.Contains(got, "trace line") || strings.Contains(got, "debug line") {
		t.Errorf("the console must stay at info, got %q", got)
	}
}

func TestChangingTheLevelDoesNotTouchARecording(t *testing.T) {
	console, rec := recordingFixture(t, LevelInfo)
	StartRecording(rec, LevelDebug)

	SetLevel(LevelError)
	Debugf("after error")
	SetLevel(LevelDebug)
	Debugf("after debug")

	if got := ConfiguredLevel(); got != LevelDebug {
		t.Errorf("configured level = %v, want the one set last", got)
	}
	for _, want := range []string{"after error", "after debug"} {
		if !strings.Contains(rec.String(), want) {
			t.Errorf("the recording misses %q: %q", want, rec.String())
		}
	}
	if got := console.String(); strings.Contains(got, "after error") || !strings.Contains(got, "after debug") {
		t.Errorf("the console follows the configured level, got %q", got)
	}
}

func TestStoppingARecordingLowersTheGateAgain(t *testing.T) {
	_, rec := recordingFixture(t, LevelInfo)
	StartRecording(rec, LevelDebug)
	if got := Level(CurLevel.Load()); got != LevelDebug {
		t.Fatalf("gate while recording = %v, want debug", got)
	}
	StopRecording(rec)
	if got := Level(CurLevel.Load()); got != LevelInfo {
		t.Errorf("gate after the recording = %v, want info", got)
	}
	Debugf("too late")
	if strings.Contains(rec.String(), "too late") {
		t.Errorf("a stopped recording takes no more lines: %q", rec.String())
	}
}

func TestStoppingOneRecordingKeepsTheOther(t *testing.T) {
	_, rec := recordingFixture(t, LevelError)
	second := &bytes.Buffer{}
	StartRecording(rec, LevelTrace)
	StartRecording(second, LevelDebug)
	StopRecording(second)
	if got := Level(CurLevel.Load()); got != LevelTrace {
		t.Errorf("gate = %v, want the level of the recording still running", got)
	}
	if got := ConfiguredLevel(); got != LevelError {
		t.Errorf("configured level = %v, want error", got)
	}
}

func TestSilentStaysSilentWhileRecording(t *testing.T) {
	console, rec := recordingFixture(t, -1)
	StartRecording(rec, LevelDebug)

	Warnf("warn line")
	Infof("info line")
	_ = Errorf("error line")

	for _, want := range []string{"[WARN] warn line", "[INFO] info line", "[ERROR] error line"} {
		if !strings.Contains(rec.String(), want) {
			t.Errorf("the recording misses %q: %q", want, rec.String())
		}
	}
	if got := console.String(); strings.Contains(got, "warn line") || strings.Contains(got, "info line") || !strings.Contains(got, "error line") {
		t.Errorf("a silent console prints errors only, got %q", got)
	}
}

func TestARecordingDoesNotWaitForTheBufferedConsole(t *testing.T) {
	previous := ConfiguredLevel()
	rec := &bytes.Buffer{}
	Init(io.Discard, LevelInfo, false)
	t.Cleanup(func() {
		StopRecording(rec)
		Init(os.Stderr, previous, true)
	})
	StartRecording(rec, LevelDebug)
	Infof("written at once")
	if !strings.Contains(rec.String(), "written at once") {
		t.Error("a line reaches the recording when it is logged, with instaflush off too")
	}
}

type writeCounter struct{ writes int }

func (w *writeCounter) Write(p []byte) (int, error) {
	w.writes++
	return len(p), nil
}

func TestARecordingGetsOneWriteForEachLine(t *testing.T) {
	recordingFixture(t, LevelInfo)
	w := &writeCounter{}
	StartRecording(w, LevelDebug)

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 50 {
				Debugf("line")
				SetLevel(LevelInfo)
			}
		})
	}
	wg.Wait()
	StopRecording(w)
	if w.writes != 400 {
		t.Errorf("got %d writes for 400 lines", w.writes)
	}
}

func TestANilRecordingIsIgnored(t *testing.T) {
	recordingFixture(t, LevelInfo)
	StartRecording(nil, LevelDebug)
	if got := Level(CurLevel.Load()); got != LevelInfo {
		t.Errorf("gate = %v, want info", got)
	}
	StopRecording(nil)
}
