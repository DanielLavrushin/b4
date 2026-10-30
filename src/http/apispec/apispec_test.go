package apispec

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const fixture = `{
  "swagger": "2.0",
  "info": {"title": "t", "version": "9.9.9"},
  "basePath": "/api",
  "paths": {
    "/sets": {
      "get": {"summary": "List all sets", "tags": ["Sets"],
        "responses": {"200": {"description": "OK", "schema": {"type": "array", "items": {"$ref": "#/definitions/config.SetConfig"}}}}},
      "post": {"summary": "Create a new set", "tags": ["Sets"],
        "parameters": [{"name": "set", "in": "body", "required": true, "schema": {"$ref": "#/definitions/config.SetConfig"}}],
        "responses": {
          "200": {"description": "OK", "schema": {"$ref": "#/definitions/config.SetConfig"}},
          "400": {"description": "Bad", "schema": {"$ref": "#/definitions/handler.APIError"}}}}
    },
    "/sets/{id}": {
      "get": {"summary": "Get a set by ID", "tags": ["Sets"],
        "parameters": [{"name": "id", "in": "path", "required": true, "type": "string", "description": "Set ID"}],
        "responses": {"200": {"description": "OK", "schema": {"$ref": "#/definitions/config.SetConfig"}}}},
      "delete": {"summary": "Delete a set", "tags": ["Sets"],
        "parameters": [{"name": "id", "in": "path", "required": true, "type": "string"}],
        "responses": {"200": {"description": "OK", "schema": {"type": "object", "additionalProperties": true}}}}
    },
    "/sets/reorder": {
      "get": {"summary": "Show the order", "tags": ["Sets"],
        "responses": {"200": {"description": "OK", "schema": {"type": "object", "additionalProperties": {"type": "string"}}}}}
    },
    "/geodat/upload": {
      "post": {"summary": "Upload geodat file", "tags": ["Geodat"], "consumes": ["multipart/form-data"],
        "parameters": [
          {"name": "file", "in": "formData", "required": true, "type": "file"},
          {"name": "kind", "in": "formData", "type": "string", "enum": ["geosite", "geoip"]}],
        "responses": {"200": {"description": "OK", "schema": {"type": "string"}}}}
    }
  },
  "definitions": {
    "config.SetConfig": {"type": "object", "properties": {
      "id": {"type": "string"},
      "name": {"type": "string", "description": "Shown in the web interface"},
      "tcp": {"$ref": "#/definitions/config.TCPConfig"},
      "children": {"type": "array", "items": {"$ref": "#/definitions/config.SetConfig"}},
      "labels": {"type": "object", "additionalProperties": {"type": "string"}},
      "extra": {"type": "object", "additionalProperties": {}}}},
    "config.TCPConfig": {"type": "object", "properties": {
      "seg2delay": {"type": "integer"},
      "role": {"$ref": "#/definitions/ai.Role"}}},
    "ai.Role": {"type": "string", "enum": ["system", "user"]},
    "hub.SetConfig": {"type": "object", "properties": {"id": {"type": "string"}}},
    "handler.APIError": {"type": "object", "properties": {
      "error": {"type": "string"},
      "fields": {"type": "array", "items": {"$ref": "#/definitions/handler.FieldError"}}}},
    "handler.FieldError": {"type": "object", "properties": {"path": {"type": "string"}}}
  }
}`

func fixtureSpec(t *testing.T) *Spec {
	t.Helper()
	s, err := Parse([]byte(fixture))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return s
}

func refs(ops []Operation) []string {
	out := make([]string, len(ops))
	for i, op := range ops {
		out[i] = op.Method + " " + op.Path
	}
	return out
}

func TestParseSortsOperationsUnderTheBasePath(t *testing.T) {
	s := fixtureSpec(t)
	want := []string{
		"POST /api/geodat/upload",
		"GET /api/sets",
		"POST /api/sets",
		"GET /api/sets/reorder",
		"GET /api/sets/{id}",
		"DELETE /api/sets/{id}",
	}
	if got := refs(s.Operations()); !reflect.DeepEqual(got, want) {
		t.Fatalf("operations = %v, want %v", got, want)
	}
	if s.Version() != "9.9.9" {
		t.Errorf("version = %q", s.Version())
	}
}

func TestFindPrefersLiteralSegmentsAndAcceptsConcretePaths(t *testing.T) {
	s := fixtureSpec(t)
	for ref, want := range map[string]string{
		"GET /api/sets/reorder": "GET /api/sets/reorder",
		"get /sets/abc123":      "GET /api/sets/{id}",
		"DELETE http://192.168.1.1:7000/api/sets/abc?force=1": "DELETE /api/sets/{id}",
		"GET /api/sets/{set_id}/":                             "GET /api/sets/{id}",
		"POST /api/sets":                                      "POST /api/sets",
	} {
		op, err := s.Find(ref)
		if err != nil {
			t.Errorf("Find(%q): %v", ref, err)
			continue
		}
		if got := op.Method + " " + op.Path; got != want {
			t.Errorf("Find(%q) = %s, want %s", ref, got, want)
		}
	}
}

func TestFindExplainsWhatIsMissing(t *testing.T) {
	s := fixtureSpec(t)
	for ref, wants := range map[string][]string{
		"/api/sets/{id}":      {"give the method too", "GET, DELETE /api/sets/{id}"},
		"PUT /api/sets/{id}":  {"/api/sets/{id} has no PUT operation", "it has GET, DELETE"},
		"GET /api/sets/a/b/c": {"no operation at /api/sets/a/b/c", "under /api/sets", "GET /api/sets/reorder"},
		"GET /api/nothing":    {"no operation at /api/nothing"},
		"   ":                 {"give the operation as 'METHOD /path'"},
	} {
		_, err := s.Find(ref)
		if err == nil {
			t.Errorf("Find(%q) succeeded, want an error", ref)
			continue
		}
		for _, want := range wants {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("Find(%q) error %q lacks %q", ref, err, want)
			}
		}
	}
}

func TestOperationsNameTheirTypes(t *testing.T) {
	s := fixtureSpec(t)
	find := func(ref string) Operation {
		t.Helper()
		op, err := s.Find(ref)
		if err != nil {
			t.Fatalf("Find(%q): %v", ref, err)
		}
		return op
	}

	create := find("POST /api/sets")
	if got := create.Parameters[0].Type; got != "config.SetConfig" {
		t.Errorf("body type = %q", got)
	}
	if got := create.Responses["400"].Type; got != "handler.APIError" {
		t.Errorf("400 type = %q", got)
	}
	if got := create.Types(); !reflect.DeepEqual(got, []string{"config.SetConfig", "handler.APIError"}) {
		t.Errorf("types = %v", got)
	}

	for ref, want := range map[string]string{
		"GET /api/sets":         "[]config.SetConfig",
		"DELETE /api/sets/{id}": "map[string]any",
		"GET /api/sets/reorder": "map[string]string",
	} {
		if got := find(ref).Responses["200"].Type; got != want {
			t.Errorf("%s 200 type = %q, want %q", ref, got, want)
		}
	}

	upload := find("POST /api/geodat/upload")
	if got := []string{upload.Parameters[0].Type, upload.Parameters[1].Type}; !reflect.DeepEqual(got, []string{"file", "string (one of: geosite, geoip)"}) {
		t.Errorf("upload parameter types = %v", got)
	}
}

func TestSchemasFollowReferencesWithinTheBudget(t *testing.T) {
	s := fixtureSpec(t)

	all, more := s.Schemas([]string{"config.SetConfig"}, 100000)
	if len(more) != 0 {
		t.Errorf("more = %v, want none under a large budget", more)
	}
	set, ok := all["config.SetConfig"].(map[string]string)
	if !ok {
		t.Fatalf("config.SetConfig rendered as %T", all["config.SetConfig"])
	}
	for field, want := range map[string]string{
		"name":     "string: Shown in the web interface",
		"tcp":      "config.TCPConfig",
		"children": "[]config.SetConfig",
		"labels":   "map[string]string",
		"extra":    "map[string]any",
	} {
		if set[field] != want {
			t.Errorf("field %s = %q, want %q", field, set[field], want)
		}
	}
	if got := all["ai.Role"]; got != "string (one of: system, user)" {
		t.Errorf("ai.Role = %v", got)
	}
	if _, ok := all["handler.APIError"]; ok {
		t.Error("handler.APIError is not reachable from config.SetConfig")
	}

	first, more := s.Schemas([]string{"config.SetConfig"}, 1)
	if _, ok := first["config.SetConfig"]; !ok || len(first) != 1 {
		t.Errorf("a tiny budget should still return the first schema alone, got %v", first)
	}
	if !reflect.DeepEqual(more, []string{"config.TCPConfig"}) {
		t.Errorf("more = %v, want the next type to expand", more)
	}
}

func TestSearchNeedsEveryWord(t *testing.T) {
	s := fixtureSpec(t)
	if got := refs(s.Search("set ORDER")); !reflect.DeepEqual(got, []string{"GET /api/sets/reorder"}) {
		t.Errorf("Search = %v", got)
	}
	if got := refs(s.Search("geodat upload")); !reflect.DeepEqual(got, []string{"POST /api/geodat/upload"}) {
		t.Errorf("Search = %v", got)
	}
	if got := s.Search("  "); got != nil {
		t.Errorf("an empty query matched %v", refs(got))
	}
}

func TestSchemaNameAcceptsTheShortNameWhenItIsUnique(t *testing.T) {
	s := fixtureSpec(t)
	for in, want := range map[string]string{
		"TCPConfig":        "config.TCPConfig",
		"config.setconfig": "config.SetConfig",
		"ai.Role":          "ai.Role",
	} {
		if got, err := s.SchemaName(in); err != nil || got != want {
			t.Errorf("SchemaName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := s.SchemaName("SetConfig"); err == nil || !strings.Contains(err.Error(), "config.SetConfig, hub.SetConfig") {
		t.Errorf("an ambiguous short name should list the candidates, got %v", err)
	}
	if _, err := s.SchemaName("nope"); err == nil {
		t.Error("an unknown name should be refused")
	}
}

const docsSpec = "../../../docs/static/swagger.json"

func checkRealSpec(t *testing.T, s *Spec) {
	t.Helper()
	if s.Version() == "" {
		t.Error("the spec has no version")
	}
	ops := s.Operations()
	if len(ops) < 100 {
		t.Fatalf("only %d operations in the spec", len(ops))
	}
	for _, op := range ops {
		if !strings.HasPrefix(op.Path, "/api/") || op.Summary == "" {
			t.Errorf("%s %s: path outside /api or no summary", op.Method, op.Path)
		}
	}
	if _, err := s.Find("GET /api/sets"); err != nil {
		t.Errorf("Find(GET /api/sets): %v", err)
	}
}

func TestDocsSpecParses(t *testing.T) {
	raw, err := os.ReadFile(docsSpec)
	if err != nil {
		t.Fatalf("read docs/static/swagger.json: %v", err)
	}
	s, err := Parse(raw)
	if err != nil {
		t.Fatalf("parse docs/static/swagger.json: %v", err)
	}
	checkRealSpec(t, s)
}

func TestEmbeddedSpecLoads(t *testing.T) {
	s, err := Load()
	if errors.Is(err, ErrNotBuiltIn) {
		t.Skip("no spec/swagger.json.gz in this tree: make apispec writes it")
	}
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	checkRealSpec(t, s)
}

func TestEmbeddedSpecMatchesTheDocsCopy(t *testing.T) {
	raw, err := unpack()
	if errors.Is(err, ErrNotBuiltIn) {
		t.Skip("no spec/swagger.json.gz in this tree: make apispec writes it")
	}
	if err != nil {
		t.Fatalf("unpack: %v", err)
	}
	docs, err := os.ReadFile(docsSpec)
	if err != nil {
		t.Fatalf("read docs/static/swagger.json: %v", err)
	}
	if !bytes.Equal(raw, docs) {
		t.Fatal("src/http/apispec/spec/swagger.json.gz is older than docs/static/swagger.json: run make apispec")
	}
}

func TestMakeApispecSurvivesParallelRuns(t *testing.T) {
	for _, tool := range []string{"make", "gzip", "mktemp"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "src")
	pkg := filepath.Join(src, "http", "apispec")
	if err := os.MkdirAll(filepath.Join(pkg, "spec"), 0o755); err != nil {
		t.Fatal(err)
	}

	const parallel = 16
	errs := make(chan error, parallel)
	for range parallel {
		go func() {
			out, err := exec.Command("make", "-C", root, "--no-print-directory", "apispec", "SRC_DIR="+src).CombinedOutput()
			if err != nil {
				err = fmt.Errorf("%w: %s", err, bytes.TrimSpace(out))
			}
			errs <- err
		}()
	}
	for range parallel {
		if err := <-errs; err != nil {
			t.Errorf("one of %d parallel make apispec runs failed: %v", parallel, err)
		}
	}

	packed, err := os.ReadFile(filepath.Join(pkg, "spec", "swagger.json.gz"))
	if err != nil {
		t.Fatalf("read the generated spec: %v", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		t.Fatalf("the generated spec is not gzip: %v", err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("the generated spec is truncated: %v", err)
	}
	docs, err := os.ReadFile(docsSpec)
	if err != nil {
		t.Fatalf("read docs/static/swagger.json: %v", err)
	}
	if !bytes.Equal(raw, docs) {
		t.Error("the generated spec is not a copy of docs/static/swagger.json")
	}

	for _, dir := range []string{pkg, filepath.Join(pkg, "spec")} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if name := e.Name(); name != "spec" && name != "swagger.json.gz" {
				t.Errorf("make apispec left %s in %s, where the embed or git would pick it up", name, dir)
			}
		}
	}
}
