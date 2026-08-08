package convert

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var coreFiles = []string{
	"convert.go", "spec.go", "extract.go", "getopt.go",
	"grammar.go", "ir.go", "parse.go", "emit.go", "tool.go", "detect.go",
}

func TestCore_MentionsNoToolByName(t *testing.T) {
	tools, err := Tools()
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(tools))
	for _, x := range tools {
		names = append(names, x.Tool)
	}

	for _, f := range coreFiles {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		body := string(raw)
		for _, name := range names {
			if f == "extract.go" {
				continue
			}
			if strings.Contains(strings.ToLower(body), name) {
				t.Errorf("%s mentions %q; per-tool behaviour belongs in tool_%s.go "+
					"or in rules/%s.json", f, name, name, name)
			}
		}
	}
}

func TestTools_EachHasItsOwnFileAndRules(t *testing.T) {
	tools, err := Tools()
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) < 2 {
		t.Fatalf("expected at least two tools, got %d", len(tools))
	}
	for _, x := range tools {
		t.Run(x.Tool, func(t *testing.T) {
			for _, f := range []string{
				"tool_" + x.Tool + ".go",
				"tool_" + x.Tool + "_test.go",
				filepath.Join("rules", x.Tool+".json"),
			} {
				if _, err := os.Stat(f); err != nil {
					t.Errorf("missing %s", f)
				}
			}
			if x.Label == "" {
				t.Error("rule file has no label")
			}
			if len(x.Versions) == 0 {
				t.Error("rule file declares no versions")
			}
		})
	}
}

func TestTools_RegisteredGrammarsAreNamespaced(t *testing.T) {
	tools, err := Tools()
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, x := range tools {
		known[x.Tool] = true
	}
	for name := range grammars {
		tool, _, ok := strings.Cut(name, ".")
		if !ok {
			continue
		}
		if !known[tool] {
			t.Errorf("grammar %q is namespaced under an unknown tool", name)
		}
	}
}

var grammarRefRe = regexp.MustCompile(`"grammar":\s*"([a-zA-Z0-9._]+)"`)

func TestRules_ReferenceOnlyRegisteredGrammars(t *testing.T) {
	entries, err := os.ReadDir("rules")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join("rules", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range grammarRefRe.FindAllStringSubmatch(string(raw), -1) {
			if _, ok := grammars[m[1]]; !ok {
				t.Errorf("%s references unregistered grammar %q", e.Name(), m[1])
			}
		}
	}
}

func TestRules_NormalizersAndEmittersResolve(t *testing.T) {
	all, err := loadSpecs()
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range all {
		if spec.Normalize != "" {
			if _, ok := normalizers[spec.Normalize]; !ok {
				t.Errorf("%s names normalizer %q, which is not registered",
					spec.Tool, spec.Normalize)
			}
		}
	}
}
