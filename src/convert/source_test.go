package convert

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSource_ReadsConfigFileShapes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			"multiLineQuotedValue",
			"NFQWS_OPT=\"\n--filter-tcp=80 --dpi-desync=fake --new\n--filter-tcp=443 --dpi-desync=fake\n\"",
			[]string{"--filter-tcp=80", "--dpi-desync=fake", "--new", "--filter-tcp=443", "--dpi-desync=fake"},
		},
		{
			"variableReferencesAreExpandedOnce",
			"MODE_LIST=\"--hostlist=/opt/user.list\"\nNFQWS_OPT=\"--dpi-desync=fake $MODE_LIST\"",
			[]string{"--dpi-desync=fake", "--hostlist=/opt/user.list"},
		},
		{
			"nonOptionVariablesAreIgnored",
			"USER=nobody\nNFQUEUE_NUM=200\nTCP_PORTS=443,2053\nNFQWS_OPT=\"--dpi-desync=fake\"",
			[]string{"--dpi-desync=fake"},
		},
		{
			"apostropheInACommentDoesNotOpenAQuote",
			"# normally it's needed only for stateless DPI\nNFQWS_OPT=\"--dpi-desync=fake\"",
			[]string{"--dpi-desync=fake"},
		},
		{
			"caretContinuation",
			"NFQWS_OPT=\"--dpi-desync=fake --new ^\n--filter-tcp=443 --dpi-desync=rst\"",
			[]string{"--dpi-desync=fake", "--new", "--filter-tcp=443", "--dpi-desync=rst"},
		},
		{
			"batchCommentsAndBinaryAreStripped",
			"@echo off\nrem a comment\n:: another\nwinws.exe --dpi-desync=fake",
			[]string{"--dpi-desync=fake"},
		},
		{
			"windowsPathKeepsItsBackslashes",
			`winws.exe --dpi-desync-fake-tls=C:\zapret\files\tls.bin`,
			[]string{`--dpi-desync-fake-tls=C:\zapret\files\tls.bin`},
		},
		{
			"percentVariableIsExpanded",
			"set BIN=C:\\zapret\\bin\\\n%BIN%winws.exe --dpi-desync=fake",
			[]string{"--dpi-desync=fake"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractArgv(tt.in)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSource_CommentedAssignmentsBecomeAlternatives(t *testing.T) {
	src := scanSource("#NFQWS_ARGS=\"--dpi-desync=rst\"\nNFQWS_ARGS=\"--dpi-desync=fake\"")
	if len(src.Alternatives) != 1 || src.Alternatives[0].Name != "NFQWS_ARGS" {
		t.Fatalf("alternatives: %+v", src.Alternatives)
	}
	if got := src.Vars["NFQWS_ARGS"]; got != "--dpi-desync=fake" {
		t.Fatalf("the active assignment must win, got %q", got)
	}
}

func loadFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func assertNoLostOptions(t *testing.T, res *Result) {
	t.Helper()
	for _, n := range res.Notes {
		switch {
		case n.Status == StatusUnknown, n.Status == StatusInvalid:
			t.Errorf("%q was not understood: %s/%s", n.Token, n.Status, n.Reason)
		case n.Reason == "unaccountedOption":
			t.Errorf("%q fell through every emit rule", n.Token)
		}
	}
}

func TestCorpus_ZapretReferenceConfig(t *testing.T) {
	res := analyze(t, loadFixture(t, "zapret-config"))

	if res.Tool != "zapret" {
		t.Fatalf("tool: got %q", res.Tool)
	}
	if len(res.Sets) != 3 {
		t.Fatalf("NFQWS_OPT holds three profiles, got %d sets", len(res.Sets))
	}
	if !res.Applicable {
		t.Fatal("the reference zapret config must be importable")
	}
	if res.Fidelity.Score < 70 {
		t.Errorf("fidelity %d%% is too low for a config b4 fully understands", res.Fidelity.Score)
	}
	assertNoLostOptions(t, res)

	if got := res.Sets[0].TCP.DPortFilter; got != "80" {
		t.Errorf("set 0 port filter: got %q", got)
	}
	if got := res.Sets[1].Fragmentation.Strategy; got != "disorder" {
		t.Errorf("multidisorder must become a disorder strategy, got %q", got)
	}
	if got := res.Sets[2].UDP.DPortFilter; got != "443" {
		t.Errorf("set 2 UDP port filter: got %q", got)
	}
	if !hasWarning(res, "foreignDaemonVars") {
		t.Error("TPWS_OPT belongs to another daemon and must be reported, not converted")
	}
	for _, n := range res.Notes {
		if strings.Contains(n.Token, "--methodeol") || strings.Contains(n.Token, "--split-pos=") {
			t.Errorf("a tpws option reached the nfqws conversion: %q", n.Token)
		}
		if strings.Contains(n.Token, "--prefix-length") {
			t.Errorf("IPSET_OPT is not an nfqws variable: %q", n.Token)
		}
	}
}

func TestCorpus_NfqwsKeeneticConfig(t *testing.T) {
	res := analyze(t, loadFixture(t, "nfqws-keenetic.conf"))

	if len(res.Sets) != 8 {
		t.Fatalf("the keenetic launcher builds eight profiles from this file, got %d", len(res.Sets))
	}
	if res.Fidelity.Score < 55 {
		t.Errorf("fidelity %d%% is too low", res.Fidelity.Score)
	}
	assertNoLostOptions(t, res)

	if !hasWarning(res, "sourceLayout") {
		t.Error("the keenetic layout must be reported so the profile order is explainable")
	}
	if !hasWarning(res, "alternativeStrategies") {
		t.Error("the two commented-out NFQWS_ARGS lines must be reported")
	}

	tcp := 0
	for _, s := range res.Sets {
		if s.Fragmentation.Strategy != "none" && s.Faking.SNI {
			tcp++
		}
	}
	if tcp == 0 {
		t.Fatal("the TCP strategy from NFQWS_ARGS was lost")
	}

	last := res.Sets[len(res.Sets)-1]
	if !last.Faking.SNI || last.Fragmentation.Strategy != "tcp" {
		t.Errorf("the final group is NFQWS_ARGS and must carry its TLS strategy, got %+v", last.Fragmentation.Strategy)
	}
	if last.Faking.CustomPayload != "" {
		t.Errorf("a hex --dpi-desync-fake-tls must not become an ASCII custom payload, got %q", last.Faking.CustomPayload)
	}
	if last.Faking.PayloadDomain != "www.google.com" {
		t.Errorf("the fake SNI from --dpi-desync-fake-tls-mod was lost, got %q", last.Faking.PayloadDomain)
	}
}

func TestCorpus_WinwsBatchFile(t *testing.T) {
	res := analyze(t, loadFixture(t, "winws.bat"))

	if res.Tool != "zapret" {
		t.Fatalf("tool: got %q", res.Tool)
	}
	if len(res.Sets) != 3 {
		t.Fatalf("the batch file holds three profiles, got %d", len(res.Sets))
	}
	assertNoLostOptions(t, res)

	if got := res.Sets[1].Fragmentation.Strategy; got != "disorder" {
		t.Errorf("set 1 strategy: got %q", got)
	}
}

func hasWarning(res *Result, code string) bool {
	for _, w := range res.Warnings {
		if w.Code == code {
			return true
		}
	}
	return false
}

func TestSource_ResolvesReferencesWithoutLosingTheAssignment(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			"bareReferenceAtTheStartOfAValue",
			"NFQWS_OPT=$COMMON_OPT --dpi-desync=fake\n",
			[]string{"--dpi-desync=fake"},
		},
		{
			"referenceIsTheWholeValue",
			"A=\"--dpi-desync=fake --dpi-desync-ttl=1\"\nNFQWS_OPT=$A\n",
			[]string{"--dpi-desync=fake", "--dpi-desync-ttl=1"},
		},
		{
			"statementStartingWithAReference",
			"$NFQWS_BIN --dpi-desync=fake\n",
			[]string{"--dpi-desync=fake"},
		},
		{
			"selfAppendKeepsTheAssignment",
			"NFQWS_OPT=\"--dpi-desync=fake\"\nNFQWS_OPT=\"$NFQWS_OPT --dpi-desync-ttl=1\"\n",
			[]string{"--dpi-desync=fake", "--dpi-desync-ttl=1"},
		},
		{
			"lastAssignmentWins",
			"NFQWS_OPT=\"--dpi-desync=fake\"\nNFQWS_OPT=\"--dpi-desync=split2\"\n",
			[]string{"--dpi-desync=split2"},
		},
		{
			"apostropheInATrailingCommentDoesNotSwallowTheFile",
			"MODE=nfqws # user's choice\nNFQWS_OPT=\"--dpi-desync=fake\"\n",
			[]string{"--dpi-desync=fake"},
		},
		{
			"trailingCommentIsNotPartOfTheValue",
			"NFQWS_OPT=\"--dpi-desync=fake\" # keep this off for now\n",
			[]string{"--dpi-desync=fake"},
		},
		{
			"quotedPathInsideAValueSurvives",
			"NFQWS_OPT=\"--dpi-desync=fake --hostlist='/opt/my lists/hosts.txt'\"\n",
			[]string{"--dpi-desync=fake", "--hostlist=/opt/my lists/hosts.txt"},
		},
		{
			"posixShellPreambleIsNotAnOption",
			"#!/bin/sh\nset -e\nNFQWS_OPT=\"--dpi-desync=fake\"\n",
			[]string{"--dpi-desync=fake"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractArgv(tt.in)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSource_AScalarVariableCannotSelectALayout(t *testing.T) {
	res := analyze(t, "NFQWS_ARGS=1\nNFQWS_OPT=\"--filter-tcp=80 --dpi-desync=fake --new --filter-tcp=443 --dpi-desync=rst\"\n")
	if len(res.Sets) != 2 {
		t.Fatalf("NFQWS_OPT holds two profiles, got %d sets (argv %q)", len(res.Sets), res.Argv)
	}
}

func TestSource_OptionCarryingVariablesAreNeverDroppedSilently(t *testing.T) {
	res := analyze(t, "NFQWS_OPT=\"--dpi-desync=fake\"\nNFQWS_OPT_QUIC=\"--filter-udp=443 --dpi-desync=fake\"\n")
	if !hasWarning(res, "unusedVars") {
		t.Fatalf("NFQWS_OPT_QUIC carries options and was skipped, which must be reported: %+v", res.Warnings)
	}
}

func TestAnalyze_AValueThatCannotBeParsedStaysInvalid(t *testing.T) {
	res := analyze(t, "nfqws --dpi-desync=fake --dpi-desync-ttl=bad")
	n := noteFor(t, res, "--dpi-desync-ttl=bad")
	if n.Status != StatusInvalid || n.Reason != "badValue" {
		t.Fatalf("got %+v", n)
	}
}

func TestAnalyze_ARepeatedFlagKeepsItsOwnVerdict(t *testing.T) {
	res := analyze(t, "nfqws --dpi-desync=fake --hostcase --hostcase")
	for _, n := range res.Notes {
		if n.Token == "--hostcase" && n.Status != StatusUnsupported {
			t.Fatalf("a repeated no-argument flag must keep its own verdict, got %+v", n)
		}
	}
}

func TestSource_SystemdEnvironmentCarriesOptions(t *testing.T) {
	got := extractArgv("[Service]\nEnvironment=NFQWS_OPT=\"--dpi-desync=fake --dpi-desync-ttl=3\"\nExecStart=/opt/zapret/nfqws --qnum=200\n")
	want := []string{"--qnum=200", "--dpi-desync=fake", "--dpi-desync-ttl=3"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAnalyze_WildcardPortFilterIsAResult(t *testing.T) {
	res := analyze(t, "nfqws --filter-tcp=* --dpi-desync=fake,multisplit --dpi-desync-split-pos=1,midsld")
	n := noteFor(t, res, "--filter-tcp=*")
	if n.Status != StatusMapped || n.Reason != "everyPortMatched" {
		t.Fatalf("got %+v", n)
	}
	if res.Sets[0].TCP.DPortFilter != "" {
		t.Fatalf("every port means no port filter, got %q", res.Sets[0].TCP.DPortFilter)
	}
}

func TestSource_BackslashContinuationVsWindowsPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			"continuationWithNoSpaceBeforeTheBackslash",
			"-s1\\\n-f-1",
			[]string{"-s1", "-f-1"},
		},
		{
			"continuationWithASpaceBeforeTheBackslash",
			"-s1 \\\n-f-1",
			[]string{"-s1", "-f-1"},
		},
		{
			"aWindowsPathEndingInABackslashIsNotAContinuation",
			"set BIN=C:\\zapret\\bin\\\n%BIN%winws.exe --dpi-desync=fake",
			[]string{"--dpi-desync=fake"},
		},
		{
			"aValueEndingInAWindowsPathIsNotAContinuation",
			"NFQWS_OPT=\"--dpi-desync-fake-tls=C:\\zapret\\files\\\"\nNFQWS_ARGS_QUIC=\"--filter-udp=443 --dpi-desync=fake\"",
			[]string{"--dpi-desync-fake-tls=C:\\zapret\\files\\", "--filter-udp=443", "--dpi-desync=fake"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractArgv(tt.in)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
