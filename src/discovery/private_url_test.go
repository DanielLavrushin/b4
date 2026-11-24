package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/nfq"
	"golang.org/x/sys/unix"
)

const canaryResolverPath = "/canarypath/canaryid?token=canaryquery"

func refusedAddr(t *testing.T) string {
	t.Helper()
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	if err := unix.Bind(fd, &unix.SockaddrInet4{Addr: [4]byte{127, 0, 0, 1}}); err != nil {
		t.Fatal(err)
	}
	bound, err := unix.Getsockname(fd)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("127.0.0.1:%d", bound.(*unix.SockaddrInet4).Port)
}

func deadDoHServer(t *testing.T) (url, label string) {
	t.Helper()
	label = "https://" + refusedAddr(t)
	return label + canaryResolverPath, label
}

func wholeRunLog(t *testing.T) func() string {
	t.Helper()
	log.GetDiscoveryHub().Reset()
	t.Cleanup(log.GetDiscoveryHub().Reset)
	return func() string { return strings.Join(log.GetDiscoveryHub().Snapshot(), "\n") }
}

func privateFile(t *testing.T, path string) string {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != config.ConfigFileMode {
		t.Errorf("%s is %#o, want %#o like the config it stands next to", filepath.Base(path), info.Mode().Perm(), config.ConfigFileMode)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestARunNamesADoHTrustedServerByItsHostOnly(t *testing.T) {
	previous := suiteRetention
	suiteRetention = 10 * time.Millisecond
	t.Cleanup(func() { suiteRetention = previous })
	nxdomainResolver(t)
	lines := wholeRunLog(t)
	server, label := deadDoHServer(t)

	cfg := config.NewConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")
	cfg.Queue.IsDiscovery = true
	cfg.System.Checker.ConfigPropagateMs = 0
	cfg.System.Checker.DiscoveryTimeoutSec = 1
	cfg.System.Checker.DNSServer = server
	if err := os.WriteFile(runLogPath(cfg.ConfigPath), []byte("from an older b4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pool := nfq.NewPool(&cfg)
	t.Cleanup(pool.Stop)

	ds := NewDiscoverySuite([]string{"site.example"}, pool, false, true, nil, 1, "", "", 0)
	RegisterSuite(ds.CheckSuite)
	ds.RunDiscovery()
	if ds.Status != CheckStatusFailed || ds.trusted.URL != server {
		t.Fatalf("the run must ask %q and fail, got status %q and server %q", server, ds.Status, ds.trusted.URL)
	}

	saved := privateFile(t, runLogPath(cfg.ConfigPath))
	for where, text := range map[string]string{"the live log": lines(), "the saved log": saved} {
		if !strings.Contains(text, "Trusted DNS server: "+label) || !strings.Contains(text, "gave no usable answer") {
			t.Errorf("%s must name the server and what went wrong:\n%s", where, text)
		}
		if strings.Contains(text, "canary") {
			t.Errorf("%s holds the server's path:\n%s", where, text)
		}
	}
}

func TestTheReferenceNamesADoHServerByItsHostOnly(t *testing.T) {
	lines := wholeRunLog(t)
	server, label := deadDoHServer(t)
	p := trustedProber(t, server)
	p.domain = "site.example"

	ref := p.reference(context.Background())
	if ref.source != label || ref.failure == "" {
		t.Fatalf("want the reference %q with its failure, got %+v", label, ref)
	}
	if strings.Contains(ref.failure, "canary") || strings.Contains(lines(), "canary") {
		t.Errorf("the reference's failure is kept in the history and shown to an AI client, it must not hold the path: %q\n%s", ref.failure, lines())
	}
	if p.trusted.URL != server {
		t.Errorf("the server itself must stay whole, got %q", p.trusted.URL)
	}
}

func TestADoHFixIsKeptWholeAndLoggedByItsHost(t *testing.T) {
	lines := wholeRunLog(t)
	server, label := deadDoHServer(t)

	p := trustedProber(t, server)
	p.domain = "site.example"
	p.ref = referenceAnswer{ips: []string{"198.51.100.7"}, trusted: true}
	result := &DNSDiscoveryResult{IsPoisoned: true, Reference: label}
	p.findDNSBypass(context.Background(), result, p.ref.ips)
	if result.BestDoHURL != server {
		t.Fatalf("the fix must keep the whole URL, got %q", result.BestDoHURL)
	}

	ds := unresolvedSuite(t, true, "site.example")
	ds.dnsResults["site.example"] = result
	ds.applyBestDNSConfig()
	if ds.discoveredDNS.DoHURL != server {
		t.Fatalf("the set under test must ask the whole URL, got %q", ds.discoveredDNS.DoHURL)
	}

	ds.domainResults["site.example"] = &DomainDiscoveryResult{Domain: "site.example", Results: map[string]*DomainPresetResult{}}
	ds.logDiscoverySummary()
	for _, want := range []string{"becomes the set's DNS", "Applied DNS bypass: DoH=" + label, "DNS poisoned, bypassed via " + label} {
		if !strings.Contains(lines(), want) {
			t.Errorf("the log misses %q:\n%s", want, lines())
		}
	}
	if strings.Contains(lines(), "canary") {
		t.Errorf("the log holds the path:\n%s", lines())
	}
}

func TestTheHistoryIsReadableByItsOwnerOnly(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "b4.json")
	path := historyFilePath(configPath)
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	server, _ := deadDoHServer(t)
	set := config.NewSetConfig()
	set.DNS = config.DNSConfig{Enabled: true, DoHURL: server}
	history := &DiscoveryHistory{Entries: []HistoryEntry{{Domain: "site.example", Set: &set}}}
	if err := history.Save(configPath); err != nil {
		t.Fatal(err)
	}

	privateFile(t, path)
	loaded := LoadDiscoveryHistory(configPath)
	if len(loaded.Entries) != 1 || loaded.Entries[0].Set == nil || loaded.Entries[0].Set.DNS.DoHURL != server {
		data, _ := json.Marshal(loaded)
		t.Errorf("a saved result must keep the resolver it was tested with, or applying it would set another one: %s", data)
	}
}

func TestRunsSavedByAnOlderVersionAreRestrictedAtStart(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "b4.json")
	paths := []string{historyFilePath(configPath), runLogPath(configPath)}
	RestrictSavedRuns(configPath)
	for _, path := range paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s must not be created: %v", filepath.Base(path), err)
		}
		if err := os.WriteFile(path, []byte("from an older b4"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	RestrictSavedRuns(configPath)
	for _, path := range paths {
		if got := privateFile(t, path); got != "from an older b4" {
			t.Errorf("%s must keep its content, got %q", filepath.Base(path), got)
		}
	}
	RestrictSavedRuns("")
}

func TestANewRunLogIsReadableByItsOwnerOnly(t *testing.T) {
	wholeRunLog(t)
	log.DiscoveryLogf("a line of this run")
	configPath := filepath.Join(t.TempDir(), "b4.json")
	SaveLastRunLog(configPath)
	if got := privateFile(t, runLogPath(configPath)); !strings.Contains(got, "a line of this run") {
		t.Errorf("the run log misses the run: %q", got)
	}
}
