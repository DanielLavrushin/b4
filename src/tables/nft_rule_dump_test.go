package tables

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/log"
)

func nftDumpStub(t *testing.T, terseError string) string {
	t.Helper()
	verbose := nftTableVerbose.Load()
	nftTableVerbose.Store(false)
	t.Cleanup(func() { nftTableVerbose.Store(verbose) })
	dir := t.TempDir()
	argvLog := filepath.Join(dir, "argv.log")
	refuse := ""
	if terseError != "" {
		refuse = "if [ \"$1\" = -t ]; then echo \"" + terseError + "\" >&2; exit 1; fi\n"
	}
	stub := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + argvLog + "\n" +
		refuse +
		"case \"$*\" in *'list table inet " + nftTableName + "') echo 'chain dumped';; esac\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "nft"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argvLog
}

func dumpWhileRecording(t *testing.T) string {
	t.Helper()
	level := log.ConfiguredLevel()
	log.Init(io.Discard, log.LevelInfo, true)
	recorded := &bytes.Buffer{}
	log.StartRecording(recorded, log.LevelDebug)
	t.Cleanup(func() {
		log.StopRecording(recorded)
		log.Init(os.Stderr, level, true)
	})
	traceNftRules()
	log.StopRecording(recorded)
	return recorded.String()
}

func TestTheRuleDumpLeavesOutSetContents(t *testing.T) {
	argvLog := nftDumpStub(t, "")
	recorded := dumpWhileRecording(t)

	argv := readLog(t, argvLog)
	if !strings.Contains(argv, "-t list table inet "+nftTableName+"\n") {
		t.Errorf("the dump must ask nft to leave set contents out, got:\n%s", argv)
	}
	if strings.Contains("\n"+argv, "\nlist table inet "+nftTableName+"\n") {
		t.Errorf("a full listing prints every address of every set:\n%s", argv)
	}
	if !strings.Contains(recorded, "Current nftables rules:\nchain dumped") {
		t.Errorf("the recording misses the rules: %q", recorded)
	}
}

func TestTheRuleDumpFallsBackOnAnOldNft(t *testing.T) {
	for _, refusal := range []string{"nft: invalid option -- 't'", "nft: unrecognized option: t"} {
		argvLog := nftDumpStub(t, refusal)
		recorded := dumpWhileRecording(t)

		if argv := readLog(t, argvLog); !strings.Contains(argv, "\nlist table inet "+nftTableName+"\n") {
			t.Errorf("%s: an nft without the terse listing must be asked again without it, got:\n%s", refusal, argv)
		}
		if !strings.Contains(recorded, "Current nftables rules:\nchain dumped") {
			t.Errorf("%s: the recording misses the rules: %q", refusal, recorded)
		}
		if !nftTableVerbose.Load() {
			t.Errorf("%s: an nft that does not know the option must not be asked for it again", refusal)
		}
	}
}

func TestOneFailedTerseListingDoesNotSwitchToFullOnes(t *testing.T) {
	nftDumpStub(t, "Error: Interrupted system call")
	dumpWhileRecording(t)
	if nftTableVerbose.Load() {
		t.Error("a failure that says nothing about the option switched every later listing to the full one")
	}
}

func TestTheRuleDumpNeedsTraceLevel(t *testing.T) {
	argvLog := nftDumpStub(t, "")
	level := log.ConfiguredLevel()
	log.Init(io.Discard, log.LevelInfo, true)
	t.Cleanup(func() { log.Init(os.Stderr, level, true) })
	traceNftRules()
	if argv := readLog(t, argvLog); argv != "" {
		t.Errorf("nothing reads the rules at info, got:\n%s", argv)
	}
}
