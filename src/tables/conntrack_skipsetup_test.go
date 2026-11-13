package tables

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

func stubLiberalSysctl(t *testing.T, value string) *int {
	t.Helper()
	reads := 0
	prev := getSysctl
	getSysctl = func(name string) string {
		if name == conntrackLiberalSysctl {
			reads++
			return value
		}
		return prev(name)
	}
	t.Cleanup(func() { getSysctl = prev })
	skipSetupConntrackWarned.Store(false)
	t.Cleanup(func() { skipSetupConntrackWarned.Store(false) })
	return &reads
}

func skipSetupConfig(sets ...*config.SetConfig) *config.Config {
	cfg := config.NewConfig()
	cfg.System.Tables.SkipSetup = true
	cfg.Sets = sets
	return &cfg
}

func fakeStrategySet(id, strategy string) *config.SetConfig {
	set := config.NewSetConfig()
	set.Id = id
	set.Name = id
	set.Enabled = true
	set.Faking.SNI = true
	set.Faking.Strategy = strategy
	return &set
}

func strictConntrackLines(buf *lockedBuffer) []string {
	var lines []string
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, "--skip-tables") && strings.Contains(line, conntrackLiberalSysctl) {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestSkipTablesWarnsOnceWhenStrictTrackingWouldDropMovedAckFakes(t *testing.T) {
	stubLiberalSysctl(t, "0")
	buf := captureTablesLog(t)
	cfg := skipSetupConfig(fakeStrategySet("tcp-check-set", "tcp_check"), fakeStrategySet("ts-set", "timestamp"), fakeStrategySet("pastseq-set", "pastseq"))

	WarnSkipSetupStrictConntrack(cfg)
	WarnSkipSetupStrictConntrack(cfg)

	log.Flush()
	lines := strictConntrackLines(buf)
	if len(lines) != 1 {
		t.Fatalf("want exactly one warning for the whole run, got %d:\n%s", len(lines), buf.String())
	}
	for _, want := range []string{"[WARN]", `"tcp-check-set"`, `"ts-set"`, conntrackLiberalSysctl + "=1", "acknowledge data the server never sent"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("the warning must contain %q: %s", want, lines[0])
		}
	}
	if strings.Contains(lines[0], `"pastseq-set"`) {
		t.Errorf("a Past Sequence set sends no moved-ACK fakes and must not be named: %s", lines[0])
	}
	if strings.ContainsRune(lines[0], '\u2014') {
		t.Errorf("the warning must not contain an em-dash: %s", lines[0])
	}
}

func TestASetAddedLaterIsStillWarnedAbout(t *testing.T) {
	stubLiberalSysctl(t, "0")
	buf := captureTablesLog(t)

	WarnSkipSetupStrictConntrack(skipSetupConfig(fakeStrategySet("pastseq-set", "pastseq")))
	WarnSkipSetupStrictConntrack(skipSetupConfig(fakeStrategySet("pastseq-set", "pastseq"), fakeStrategySet("added-later", "tcp_check")))

	log.Flush()
	lines := strictConntrackLines(buf)
	if len(lines) != 1 || !strings.Contains(lines[0], `"added-later"`) {
		t.Fatalf("a save that adds a TCP Check set must still warn after a start without one, got %d:\n%s", len(lines), buf.String())
	}
}

func TestATrackingSettingReadLaterIsStillWarnedAbout(t *testing.T) {
	stubLiberalSysctl(t, "")
	buf := captureTablesLog(t)
	cfg := skipSetupConfig(fakeStrategySet("tcp-check-set", "tcp_check"))

	WarnSkipSetupStrictConntrack(cfg)
	prev := getSysctl
	getSysctl = func(name string) string {
		if name == conntrackLiberalSysctl {
			return "0"
		}
		return prev(name)
	}
	WarnSkipSetupStrictConntrack(cfg)

	log.Flush()
	if lines := strictConntrackLines(buf); len(lines) != 1 {
		t.Fatalf("an unreadable setting at start must not use up the warning, got %d:\n%s", len(lines), buf.String())
	}
}

func TestNoStrictTrackingWarningWithoutTheRisk(t *testing.T) {
	disabled := fakeStrategySet("disabled-set", "tcp_check")
	disabled.Enabled = false
	noSNI := fakeStrategySet("no-sni-set", "tcp_check")
	noSNI.Faking.SNI = false
	managed := skipSetupConfig(fakeStrategySet("tcp-check-set", "tcp_check"))
	managed.System.Tables.SkipSetup = false
	discovery := skipSetupConfig(fakeStrategySet("tcp-check-set", "tcp_check"))
	discovery.Queue.IsDiscovery = true
	tun := skipSetupConfig(fakeStrategySet("tcp-check-set", "tcp_check"))
	tun.Queue.Mode = "tun"

	cases := []struct {
		name    string
		liberal string
		cfg     *config.Config
		reads   bool
	}{
		{"b4 manages the firewall", "0", managed, false},
		{"tracking is already liberal", "1", skipSetupConfig(fakeStrategySet("tcp-check-set", "tcp_check")), true},
		{"the setting cannot be read", "", skipSetupConfig(fakeStrategySet("tcp-check-set", "tcp_check")), true},
		{"no set sends such fakes", "0", skipSetupConfig(fakeStrategySet("pastseq-set", "pastseq"), disabled, noSNI), false},
		{"a Discovery run", "0", discovery, false},
		{"the TUN engine sends its fakes after NAT", "0", tun, false},
		{"no config", "0", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reads := stubLiberalSysctl(t, tc.liberal)
			buf := captureTablesLog(t)

			WarnSkipSetupStrictConntrack(tc.cfg)

			log.Flush()
			if lines := strictConntrackLines(buf); len(lines) != 0 {
				t.Fatalf("no warning expected, got:\n%s", strings.Join(lines, "\n"))
			}
			if !tc.reads && *reads != 0 {
				t.Fatalf("the sysctl must not be read when nothing is at risk, read %d times", *reads)
			}
		})
	}
}
