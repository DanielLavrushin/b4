package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/metrics"
)

func drainRestartRequests() {
	for {
		select {
		case <-restartRequests:
		default:
			return
		}
	}
}

func TestLoadEngineAttemptReadsAndClearsTheCounter(t *testing.T) {
	cases := []struct {
		raw  string
		want int
	}{
		{"1", 1},
		{"2", 2},
		{"99", len(engineRetryDelays)},
		{"-1", 0},
		{"x", 0},
	}
	for _, c := range cases {
		t.Setenv(engineRetryEnv, c.raw)
		if got := loadEngineAttempt(); got != c.want {
			t.Fatalf("%s=%q: got attempt %d, want %d", engineRetryEnv, c.raw, got, c.want)
		}
		if _, ok := os.LookupEnv(engineRetryEnv); ok {
			t.Fatalf("%s must be cleared once read, so a manual restart starts the retries over", engineRetryEnv)
		}
	}
	if got := loadEngineAttempt(); got != 0 {
		t.Fatalf("without %s the first start is attempt 0, got %d", engineRetryEnv, got)
	}
}

func TestRestartEnvCarriesTheRetryCounterOnlyForEngineRetries(t *testing.T) {
	environ := []string{"PATH=/usr/bin", engineRetryEnv + "=1", "HOME=/root"}
	counters := func(env []string) []string {
		var out []string
		for _, kv := range env {
			if strings.HasPrefix(kv, engineRetryEnv+"=") {
				out = append(out, kv)
			}
		}
		return out
	}

	manual := restartEnv(environ, restartManual, 1)
	if got := counters(manual); len(got) != 0 {
		t.Fatalf("a manual restart must start the retries over, got %v", got)
	}
	if !slices.Contains(manual, "PATH=/usr/bin") || !slices.Contains(manual, "HOME=/root") {
		t.Fatalf("the rest of the environment must survive the restart, got %v", manual)
	}

	retry := restartEnv(environ, restartEngineRetry, 1)
	if got := counters(retry); !slices.Equal(got, []string{engineRetryEnv + "=2"}) {
		t.Fatalf("an engine retry must hand the next attempt number to the new process exactly once, got %v", got)
	}
}

func TestDegradedModeSchedulesARetryWhileRetriesRemain(t *testing.T) {
	saved := engineRetryDelays
	engineRetryDelays = []time.Duration{10 * time.Millisecond, 10 * time.Millisecond}
	drainRestartRequests()
	t.Cleanup(func() {
		engineRetryDelays = saved
		drainRestartRequests()
	})

	cfg := config.NewConfig()
	mc := &metrics.MetricsCollector{}
	before := time.Now().UnixMilli()
	timer := enterDegradedMode(&cfg, errors.New("iptables rejected the NFQUEUE target"), 1, mc)
	if timer == nil {
		t.Fatal("attempt 1 of 2 must schedule a retry")
	}
	defer timer.Stop()

	f := mc.GetEngineFailure()
	if f == nil || f.Mode != "nfqueue" || f.Error != "iptables rejected the NFQUEUE target" || f.RetriesLeft != 1 {
		t.Fatalf("unexpected engine failure record: %+v", f)
	}
	if f.RetryAt < before {
		t.Fatalf("retry_at must lie ahead of the failure, got %d before %d", f.RetryAt, before)
	}
	if mc.NFQueueStatus != "error" || mc.TablesStatus != "inactive" {
		t.Fatalf("the dashboard must show the engine as failed, got nfqueue=%q tables=%q", mc.NFQueueStatus, mc.TablesStatus)
	}

	select {
	case kind := <-restartRequests:
		if kind != restartEngineRetry {
			t.Fatalf("the scheduled retry must ask for an engine-retry restart, got %d", kind)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the retry never asked for a restart")
	}
}

func TestDegradedModeStopsRetryingOnceRetriesAreSpent(t *testing.T) {
	saved := engineRetryDelays
	engineRetryDelays = []time.Duration{time.Millisecond}
	drainRestartRequests()
	t.Cleanup(func() {
		engineRetryDelays = saved
		drainRestartRequests()
	})

	cfg := config.NewConfig()
	cfg.Queue.Mode = "tun"
	mc := &metrics.MetricsCollector{}
	if timer := enterDegradedMode(&cfg, errors.New("no usable IPv4 default route"), 1, mc); timer != nil {
		timer.Stop()
		t.Fatal("no retry may be scheduled once every attempt is spent")
	}
	f := mc.GetEngineFailure()
	if f == nil || f.Mode != "tun" || f.RetryAt != 0 || f.RetriesLeft != 0 {
		t.Fatalf("unexpected engine failure record: %+v", f)
	}
	select {
	case kind := <-restartRequests:
		t.Fatalf("no restart may be requested once retries are spent, got %d", kind)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestRestartRequestsCoalesce(t *testing.T) {
	drainRestartRequests()
	t.Cleanup(drainRestartRequests)
	requestRestart(restartManual)
	requestRestart(restartEngineRetry)
	if kind := <-restartRequests; kind != restartManual {
		t.Fatalf("the first request must win, got %d", kind)
	}
	select {
	case kind := <-restartRequests:
		t.Fatalf("a second request while one is pending must be dropped, got %d", kind)
	default:
	}
}

func TestRestartIfRequestedDoesNothingWithoutARequest(t *testing.T) {
	pendingRestart.Store(int32(restartNone))
	if err := restartIfRequested(); err != nil {
		t.Fatalf("no restart was requested, got %v", err)
	}
}

func TestRestartReplacesTheProcessInPlace(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestReexecHelper$")
	cmd.Env = append(os.Environ(), "B4_TEST_REEXEC_DIR="+dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper failed: %v\n%s", err, out)
	}
	text := string(out)

	var startPID, againPID, attempt int
	for _, line := range strings.Split(text, "\n") {
		if _, err := fmt.Sscanf(line, "start pid=%d", &startPID); err == nil {
			continue
		}
		_, _ = fmt.Sscanf(line, "again pid=%d attempt=%d", &againPID, &attempt)
	}
	if startPID == 0 || againPID == 0 {
		t.Fatalf("the helper did not run twice:\n%s", text)
	}
	if startPID != againPID {
		t.Fatalf("the restart must keep the PID so service managers and containers see the same process, got %d then %d", startPID, againPID)
	}
	if attempt != 1 {
		t.Fatalf("the engine retry counter must reach the new process as 1, got %d", attempt)
	}
	if !strings.Contains(text, "stderr after restart") {
		t.Fatalf("stderr must point back at the original output after the restart:\n%s", text)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "errors.log")); strings.Contains(string(data), "stderr after restart") {
		t.Fatalf("the restarted process must not write its stderr into the error log:\n%s", data)
	}
}

func TestReexecHelper(t *testing.T) {
	dir := os.Getenv("B4_TEST_REEXEC_DIR")
	if dir == "" {
		t.Skip("helper process for TestRestartReplacesTheProcessInPlace")
	}
	if attempt := loadEngineAttempt(); attempt > 0 {
		fmt.Printf("again pid=%d attempt=%d\n", os.Getpid(), attempt)
		fmt.Fprintln(os.Stderr, "stderr after restart")
		return
	}
	fmt.Printf("start pid=%d\n", os.Getpid())
	if err := log.InitErrorFile(filepath.Join(dir, "errors.log")); err != nil {
		t.Fatal(err)
	}
	engineAttempt = 0
	pendingRestart.Store(int32(restartEngineRetry))
	t.Fatal(restartIfRequested())
}
