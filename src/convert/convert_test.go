package convert

import (
	"errors"
	"strings"
	"testing"
)

func analyze(t *testing.T, line string) *Result {
	t.Helper()
	res, err := Analyze(line, Options{})
	if err != nil {
		t.Fatalf("Analyze(%q): %v", line, err)
	}
	return res
}

func noteFor(t *testing.T, res *Result, token string) Note {
	t.Helper()
	for _, n := range res.Notes {
		if n.Token == token {
			return n
		}
	}
	t.Fatalf("no note for token %q; notes: %+v", token, res.Notes)
	return Note{}
}

func hasField(n Note, field string) bool {
	for _, f := range n.Fields {
		if f == field || strings.HasPrefix(f, field+"=") {
			return true
		}
	}
	return false
}

func TestAnalyze_PerProfileDomains(t *testing.T) {
	res, err := Analyze("-An -s1+s -An -d0+sm", Options{
		Domains:        []string{"fallback.example"},
		ProfileDomains: map[int][]string{1: {"custom.example"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Sets[0].Targets.SNIDomains; len(got) != 1 || got[0] != "fallback.example" {
		t.Fatalf("profile 0 should use the shared domains, got %v", got)
	}
	if got := res.Sets[1].Targets.SNIDomains; len(got) != 1 || got[0] != "custom.example" {
		t.Fatalf("profile 1 should use its override, got %v", got)
	}
}

func TestAnalyze_PlanDescribesRoles(t *testing.T) {
	res := analyze(t, "-H:example.com -s1+s -At -d0+sm")
	if len(res.Plan) != 2 {
		t.Fatalf("expected a plan entry per set, got %d", len(res.Plan))
	}
	if res.Plan[0].Role != "entry" || !res.Plan[0].AcceptsTargets {
		t.Fatalf("plan[0]: got %+v", res.Plan[0])
	}
	if res.Plan[1].Role != "fallback" || res.Plan[1].AcceptsTargets {
		t.Fatalf("plan[1]: got %+v", res.Plan[1])
	}
	if res.Plan[1].FallbackFor != 0 {
		t.Fatalf("plan[1] should be the fallback for set 0, got %d", res.Plan[1].FallbackFor)
	}
}

func TestAnalyze_DomainsOption(t *testing.T) {
	res, err := Analyze("-s1+s -At -d0+sm", Options{Domains: []string{"youtube.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Sets[0].Targets.SNIDomains) != 1 {
		t.Fatalf("head set should get the supplied domains, got %v", res.Sets[0].Targets.SNIDomains)
	}
	if len(res.Sets[1].Targets.SNIDomains) != 0 {
		t.Fatalf("escalation set must stay target-less, got %v", res.Sets[1].Targets.SNIDomains)
	}
}

func TestAnalyze_Rejects(t *testing.T) {
	if _, err := Analyze("   ", Options{}); !errors.Is(err, ErrNothingToParse) {
		t.Fatalf("got %v, want ErrNothingToParse", err)
	}
	if _, err := Analyze("-s1", Options{Tool: "nosuchtool"}); err == nil {
		t.Fatal("expected an error for an unknown tool")
	}
	mixed := "--dpi-desync=fake --tlsrec=1 --mod-http=h"
	if _, err := Analyze(mixed, Options{}); !errors.Is(err, ErrUnsupportedTool) {
		t.Fatalf("got %v, want ErrUnsupportedTool for a line no tool owns", err)
	}
}

func TestAnalyze_ToolDetection(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{"byedpiShort", "-Ku -a1 -An -s1+s", "byedpi"},
		{"byedpiLong", "--proto=t --split=1+s", "byedpi"},
		{"zapret", "--filter-tcp=443 --dpi-desync=fake,multidisorder --new", "zapret"},
		{"zapretEnvVar", `NFQWS_OPT="--dpi-desync=fake --dpi-desync-repeats=6"`, "zapret"},
		{"zapretBinary", "nfqws --qnum=200 --dpi-desync=fake", "zapret"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := analyze(t, tt.line)
			if res.Tool != tt.want {
				t.Fatalf("tool: got %q, want %q", res.Tool, tt.want)
			}
		})
	}
}
