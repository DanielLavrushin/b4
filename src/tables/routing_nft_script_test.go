package tables

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

type nftScriptRecorder struct {
	scripts []string
	calls   []string
	logged  []string
}

func recordNftScripts(t *testing.T, scriptErr error, runErr func(joined string) error) *nftScriptRecorder {
	t.Helper()
	origRun, origStdin, origLogged := run, runNftStdin, runLogged
	t.Cleanup(func() {
		run = origRun
		runNftStdin = origStdin
		runLogged = origLogged
	})
	rec := &nftScriptRecorder{}
	runNftStdin = func(script string) (string, error) {
		rec.scripts = append(rec.scripts, script)
		if scriptErr != nil {
			return scriptErr.Error(), scriptErr
		}
		return "", nil
	}
	run = func(args ...string) (string, error) {
		joined := strings.Join(args, " ")
		rec.calls = append(rec.calls, joined)
		if runErr != nil {
			if err := runErr(joined); err != nil {
				return err.Error(), err
			}
		}
		return "", nil
	}
	runLogged = func(op string, args ...string) bool {
		rec.logged = append(rec.logged, strings.Join(args, " "))
		return true
	}
	return rec
}

func routeScriptTestIPs(n int) []string {
	ips := make([]string, 0, n)
	for i := 0; i < n; i++ {
		ips = append(ips, fmt.Sprintf("10.%d.%d.0/24", i/256, i%256))
	}
	return ips
}

func TestRouteNftElementScript(t *testing.T) {
	got := routeNftElementScript("add", "b4r_x_v4", []string{"8.8.0.0/16", "8.8.8.0/24", "1.1.1.1"})
	if want := "add element inet b4_route b4r_x_v4 { 8.8.0.0/16, 8.8.8.0/24, 1.1.1.1 }\n"; got != want {
		t.Errorf("add script = %q, want %q", got, want)
	}
	got = routeNftElementScript("delete", "b4r_x_v6", []string{"2001:db8::/32"})
	if want := "delete element inet b4_route b4r_x_v6 { 2001:db8::/32 }\n"; got != want {
		t.Errorf("delete script = %q, want %q", got, want)
	}
}

func TestNftStaticEntriesLoadInOneScript(t *testing.T) {
	rec := recordNftScripts(t, nil, nil)

	ips := routeScriptTestIPs(19000)
	(&routeNftBackend{}).addElements("b4r_x_v4", ips, 0)

	if len(rec.scripts) != 1 {
		t.Fatalf("19000 static entries must reach nft in exactly one script, got %d", len(rec.scripts))
	}
	if len(rec.calls) != 0 || len(rec.logged) != 0 {
		t.Errorf("a script that loaded must not be followed by batches: calls=%d logged=%d", len(rec.calls), len(rec.logged))
	}
	if want := routeNftElementScript("add", "b4r_x_v4", ips); rec.scripts[0] != want {
		t.Errorf("unexpected script shape: %.80q", rec.scripts[0])
	}
	if strings.Contains(rec.scripts[0], "_d ") || strings.Contains(rec.scripts[0], "timeout") {
		t.Errorf("static entries belong in the interval set without a timeout: %.120q", rec.scripts[0])
	}
}

func TestNftStaticEntriesScriptExpandsTheDefaultRoute(t *testing.T) {
	rec := recordNftScripts(t, nil, nil)

	(&routeNftBackend{}).addElements("b4r_x_v4", []string{"0.0.0.0/0"}, 0)
	want := []string{"add element inet b4_route b4r_x_v4 { 0.0.0.0/1, 128.0.0.0/1 }\n"}
	if !reflect.DeepEqual(rec.scripts, want) {
		t.Errorf("scripts = %q, want %q", rec.scripts, want)
	}
}

func TestNftStaticEntriesFallBackToBatchesWhenTheScriptFails(t *testing.T) {
	rec := recordNftScripts(t, errors.New("exit status 1"), nil)

	ips := routeScriptTestIPs(300)
	(&routeNftBackend{}).addElements("b4r_x_v4", ips, 0)

	if len(rec.scripts) != 1 {
		t.Fatalf("expected one script attempt, got %d", len(rec.scripts))
	}
	if len(rec.calls) != 3 {
		t.Fatalf("300 entries fall back to 3 batches of at most 128, got %d: %v", len(rec.calls), rec.calls)
	}
	prefix := "nft add element inet b4_route b4r_x_v4 { "
	for i, c := range rec.calls {
		if !strings.HasPrefix(c, prefix) || !strings.HasSuffix(c, " }") {
			t.Errorf("batch %d has an unexpected shape: %.80q", i, c)
		}
	}
	if !strings.Contains(rec.calls[0], ips[0]+" , ") || !strings.Contains(rec.calls[2], ips[len(ips)-1]+" }") {
		t.Error("the batches must cover the first and the last entry")
	}
	if len(rec.logged) != 0 {
		t.Errorf("every batch loaded, so no per-entry add is needed: %v", rec.logged)
	}
}

func TestNftStaticEntriesBadElementDegradesToPerEntryAdds(t *testing.T) {
	rec := recordNftScripts(t, errors.New("exit status 1"), func(joined string) error {
		if strings.Contains(joined, ",") {
			return errors.New("exit status 1")
		}
		return nil
	})

	(&routeNftBackend{}).addElements("b4r_x_v4", []string{"203.0.113.9", "bogus", "203.0.113.10"}, 0)

	want := []string{
		"nft add element inet b4_route b4r_x_v4 { 203.0.113.9 }",
		"nft add element inet b4_route b4r_x_v4 { bogus }",
		"nft add element inet b4_route b4r_x_v4 { 203.0.113.10 }",
	}
	if !reflect.DeepEqual(rec.logged, want) {
		t.Errorf("per-entry fallback = %v, want %v", rec.logged, want)
	}
}

func TestNftLearnedEntriesKeepTheirBatches(t *testing.T) {
	rec := recordNftScripts(t, nil, nil)

	(&routeNftBackend{}).addElements("b4r_x_v4", []string{"203.0.113.9", "203.0.113.10"}, 600)

	if len(rec.scripts) != 0 {
		t.Errorf("learned entries go to the dynamic set with a timeout and keep the batch path, got scripts %q", rec.scripts)
	}
	want := []string{"nft add element inet b4_route b4r_x_v4_d { 203.0.113.9 timeout 600s , 203.0.113.10 timeout 600s }"}
	if !reflect.DeepEqual(rec.calls, want) {
		t.Errorf("calls = %v, want %v", rec.calls, want)
	}
}

func TestNftDelElementsRemovesInOneScript(t *testing.T) {
	rec := recordNftScripts(t, nil, nil)

	ips := routeScriptTestIPs(1000)
	(&routeNftBackend{}).delElements("b4r_x_v4", ips)

	if len(rec.scripts) != 1 || rec.scripts[0] != routeNftElementScript("delete", "b4r_x_v4", ips) {
		t.Fatalf("expected one delete script, got %d", len(rec.scripts))
	}
	if len(rec.calls) != 0 {
		t.Errorf("a script that deleted must not be followed by batches, got %d calls", len(rec.calls))
	}
}

func TestRouteNftScriptErrorKeepsTheFirstLineOnly(t *testing.T) {
	long := "command [nft -f -] failed: exit status 1 (/dev/stdin:1:36-45: Error: conflicting intervals specified\nadd element inet b4_route b4r_x_v4 { " + strings.Repeat("10.0.0.0/24, ", 5000) + "}"
	got := routeNftScriptError(errors.New(long))
	if strings.Contains(got, "\n") || strings.Contains(got, "add element") {
		t.Errorf("the echoed script must not reach the log: %.120q", got)
	}
	if !strings.Contains(got, "conflicting intervals specified") {
		t.Errorf("nft's own reason must survive: %q", got)
	}

	flat := strings.Repeat("x", 1000)
	if got := routeNftScriptError(errors.New(flat)); len(got) > 310 || !strings.HasSuffix(got, "...") {
		t.Errorf("a long single-line error must be cut, got %d bytes", len(got))
	}
}
