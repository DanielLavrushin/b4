package tables

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func nftRecordingStub(t *testing.T) (argvLog, scriptLog string) {
	t.Helper()
	dir := t.TempDir()
	argvLog = filepath.Join(dir, "argv.log")
	scriptLog = filepath.Join(dir, "script.log")
	stub := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + argvLog + "\n" +
		"if [ \"$1\" = -f ] && [ \"$2\" = - ]; then cat >> " + scriptLog + "; fi\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "nft"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argvLog, scriptLog
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(raw)
}

func dupTestSet(id string, ips ...string) *config.SetConfig {
	s := config.NewSetConfig()
	s.Id = id
	s.Name = id
	s.Enabled = true
	s.TCP.Duplicate.Enabled = true
	s.Targets.IPs = ips
	s.Targets.IpsToMatch = ips
	return &s
}

func dupTestConfig() *config.Config {
	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = true
	cfg.Queue.IPv6Enabled = true
	cfg.Sets = []*config.SetConfig{
		dupTestSet("wide", "8.8.0.0/16", "2001:4860::/32"),
		dupTestSet("narrow", "8.8.8.0/24", "8.8.8.8", "2001:4860:4860::/48"),
	}
	return &cfg
}

func TestNftDuplicateSetsAreCreatedWithAutoMerge(t *testing.T) {
	argvLog, scriptLog := nftRecordingStub(t)

	if err := NewNFTablesManager(dupTestConfig()).addDuplicateQueueRules("443"); err != nil {
		t.Fatalf("addDuplicateQueueRules: %v", err)
	}

	argv := readLog(t, argvLog)
	for _, want := range []string{
		"add set inet b4_mangle b4_dup_v4 { type ipv4_addr ; flags interval ; auto-merge ; }",
		"add set inet b4_mangle b4_dup_v6 { type ipv6_addr ; flags interval ; auto-merge ; }",
	} {
		if !strings.Contains(argv, want) {
			t.Errorf("missing %q; overlapping prefixes from two duplicate sets make nft reject an interval set without auto-merge, got:\n%s", want, argv)
		}
	}
	if strings.Contains(argv, "flags interval ; }") {
		t.Errorf("an interval set without auto-merge was created:\n%s", argv)
	}
	if !strings.Contains(argv, "ip daddr @b4_dup_v4 tcp dport 443") || !strings.Contains(argv, "ip6 daddr @b4_dup_v6 tcp dport 443") {
		t.Errorf("the queue rules for the duplicate sets are missing:\n%s", argv)
	}

	script := readLog(t, scriptLog)
	for _, want := range []string{
		"add element inet b4_mangle b4_dup_v4 { 8.8.0.0/16, 8.8.8.0/24, 8.8.8.8 }",
		"add element inet b4_mangle b4_dup_v6 { 2001:4860::/32, 2001:4860:4860::/48 }",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("missing %q in the element scripts:\n%s", want, script)
		}
	}
}

func TestNftDuplicateSetsFollowTheEnabledFamilies(t *testing.T) {
	argvLog, _ := nftRecordingStub(t)

	cfg := dupTestConfig()
	cfg.Queue.IPv6Enabled = false
	if err := NewNFTablesManager(cfg).addDuplicateQueueRules("443"); err != nil {
		t.Fatalf("addDuplicateQueueRules: %v", err)
	}
	argv := readLog(t, argvLog)
	if !strings.Contains(argv, "b4_dup_v4") {
		t.Errorf("the ipv4 duplicate set is missing:\n%s", argv)
	}
	if strings.Contains(argv, "b4_dup_v6") {
		t.Errorf("ipv6 is disabled, so no ipv6 duplicate set may be created:\n%s", argv)
	}
}

func TestNftDuplicateSetsNothingWithoutADuplicateSet(t *testing.T) {
	argvLog, scriptLog := nftRecordingStub(t)

	cfg := dupTestConfig()
	for _, s := range cfg.Sets {
		s.TCP.Duplicate.Enabled = false
	}
	if err := NewNFTablesManager(cfg).addDuplicateQueueRules("443"); err != nil {
		t.Fatalf("addDuplicateQueueRules: %v", err)
	}
	if argv := readLog(t, argvLog); argv != "" {
		t.Errorf("no set duplicates, so nothing may reach nft, got:\n%s", argv)
	}
	if script := readLog(t, scriptLog); script != "" {
		t.Errorf("no set duplicates, so no elements may be loaded, got:\n%s", script)
	}
}

func TestNftMSSSetsAreCreatedWithAutoMerge(t *testing.T) {
	argvLog, _ := nftRecordingStub(t)

	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = true
	cfg.Sets = []*config.SetConfig{mssClampSet("s1", 88, []string{"8.8.0.0/16", "8.8.8.0/24"}, nil)}
	if err := NewNFTablesManager(&cfg).ApplyMSSClamp(); err != nil {
		t.Fatalf("ApplyMSSClamp: %v", err)
	}
	if argv := readLog(t, argvLog); !strings.Contains(argv, "add set inet b4_mangle b4_mss_0_v4 { type ipv4_addr ; flags interval ; auto-merge ; }") {
		t.Errorf("the per-set MSS interval set must keep auto-merge, got:\n%s", argv)
	}
}
