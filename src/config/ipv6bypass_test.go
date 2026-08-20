package config

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func stubIPv6Host(t *testing.T, present bool) {
	t.Helper()
	prevProbe := hostIPv6Probe
	hostIPv6Probe = func() bool { return present }

	hostIPv6Mu.Lock()
	hostIPv6Known = false
	hostIPv6Mu.Unlock()

	ipv6BypassMu.Lock()
	ipv6BypassWarned = false
	ipv6BypassAt = time.Time{}
	ipv6BypassMu.Unlock()

	t.Cleanup(func() {
		hostIPv6Probe = prevProbe
		hostIPv6Mu.Lock()
		hostIPv6Known = false
		hostIPv6Mu.Unlock()
		ipv6BypassMu.Lock()
		ipv6BypassWarned = false
		ipv6BypassAt = time.Time{}
		ipv6BypassMu.Unlock()
	})
}

func stubIPv6Clock(t *testing.T, at *time.Time) {
	t.Helper()
	prev := ipv6Now
	ipv6Now = func() time.Time { return *at }
	t.Cleanup(func() { ipv6Now = prev })
}

func TestIPv6BypassesSets(t *testing.T) {
	stubIPv6Host(t, true)

	cfg := NewConfig()
	if !IPv6BypassesSets(&cfg) {
		t.Errorf("a dual-stack host with IPv6 processing off must be reported as bypassable")
	}

	cfg.Queue.IPv6Enabled = true
	if IPv6BypassesSets(&cfg) {
		t.Errorf("nothing bypasses the sets once b4 processes IPv6")
	}

	if IPv6BypassesSets(nil) {
		t.Errorf("a nil config must not report a bypass")
	}
}

func TestIPv6BypassesSetsIPv4OnlyHost(t *testing.T) {
	stubIPv6Host(t, false)

	cfg := NewConfig()
	if IPv6BypassesSets(&cfg) {
		t.Errorf("a host without a global IPv6 address has nothing to bypass with")
	}
}

func TestWarnIPv6BypassRateLimitsRepeatedSaves(t *testing.T) {
	stubIPv6Host(t, true)
	now := time.Now()
	stubIPv6Clock(t, &now)

	cfg := NewConfig()
	if !WarnIPv6Bypass(&cfg) {
		t.Fatal("the first warning must be logged")
	}
	if WarnIPv6Bypass(&cfg) {
		t.Errorf("a second save in the same minute must not warn again")
	}

	now = now.Add(ipv6BypassWarnCooldown + time.Second)
	if !WarnIPv6Bypass(&cfg) {
		t.Errorf("the warning must come back once the cooldown has passed")
	}
}

func TestWarnIPv6BypassRearmsWhenIPv6IsTurnedOnAndOffAgain(t *testing.T) {
	stubIPv6Host(t, true)
	now := time.Now()
	stubIPv6Clock(t, &now)

	cfg := NewConfig()
	if !WarnIPv6Bypass(&cfg) {
		t.Fatal("the first warning must be logged")
	}

	cfg.Queue.IPv6Enabled = true
	if WarnIPv6Bypass(&cfg) {
		t.Errorf("no warning is due while b4 processes IPv6")
	}

	cfg.Queue.IPv6Enabled = false
	if !WarnIPv6Bypass(&cfg) {
		t.Errorf("turning IPv6 processing back off must warn again immediately")
	}
}

func TestWarnIPv6BypassSilentOnIPv4OnlyHost(t *testing.T) {
	stubIPv6Host(t, false)
	now := time.Now()
	stubIPv6Clock(t, &now)

	cfg := NewConfig()
	if WarnIPv6Bypass(&cfg) {
		t.Errorf("an IPv4-only host must never see the warning")
	}
}

func TestHostHasGlobalIPv6CachesTheProbe(t *testing.T) {
	stubIPv6Host(t, true)
	now := time.Now()
	stubIPv6Clock(t, &now)

	calls := 0
	hostIPv6Probe = func() bool {
		calls++
		return true
	}

	if !HostHasGlobalIPv6() || !HostHasGlobalIPv6() {
		t.Fatal("expected the host to report a global IPv6 address")
	}
	if calls != 1 {
		t.Errorf("probe ran %d times inside the cache window, want 1", calls)
	}

	now = now.Add(hostIPv6ProbeTTL + time.Second)
	HostHasGlobalIPv6()
	if calls != 2 {
		t.Errorf("probe ran %d times after the cache expired, want 2", calls)
	}
}

func TestProbeGlobalIPv6DoesNotPanic(t *testing.T) {
	probeGlobalIPv6()
}

func TestKeepIPv6AnswersDefaultsToStripping(t *testing.T) {
	cfg := NewConfig()
	if cfg.System.DNS.KeepIPv6Answers {
		t.Errorf("keep_ipv6_answers must default to false so AAAA answers are stripped")
	}

	if err := json.Unmarshal([]byte(`{"system":{"dns":{"keep_ipv6_answers":true}}}`), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !cfg.System.DNS.KeepIPv6Answers {
		t.Error("explicit keep_ipv6_answers=true must be honoured")
	}
}

func TestKeepIPv6AnswersOmittedFromSparseConfig(t *testing.T) {
	cfg := NewConfig()
	out, err := MarshalSparse(&cfg)
	if err != nil {
		t.Fatalf("MarshalSparse: %v", err)
	}
	if strings.Contains(string(out), "keep_ipv6_answers") {
		t.Errorf("the default value must not be written to the config file: %s", out)
	}
}
