package apispec

import (
	"bytes"
	"compress/gzip"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"sort"
	"strings"
)

//go:embed all:spec
var files embed.FS

const embeddedSpec = "spec/swagger.json.gz"

var ErrNotBuiltIn = errors.New("this b4 was built without its API reference, which only a build through make embeds")

var methodOrder = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}

type Spec struct {
	version     string
	base        string
	operations  []Operation
	definitions map[string]*schema
}

type Operation struct {
	Method      string              `json:"method"`
	Path        string              `json:"path"`
	Summary     string              `json:"summary,omitempty"`
	Description string              `json:"description,omitempty"`
	Tags        []string            `json:"tags,omitempty"`
	Consumes    []string            `json:"consumes,omitempty"`
	Produces    []string            `json:"produces,omitempty"`
	Parameters  []Parameter         `json:"parameters,omitempty"`
	Responses   map[string]Response `json:"responses,omitempty"`
	types       []string
}

type Parameter struct {
	Name        string `json:"name"`
	In          string `json:"in"`
	Type        string `json:"type"`
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description,omitempty"`
}

type Response struct {
	Description string `json:"description,omitempty"`
	Type        string `json:"type,omitempty"`
}

type document struct {
	Info struct {
		Version string `json:"version"`
	} `json:"info"`
	BasePath    string                                `json:"basePath"`
	Paths       map[string]map[string]json.RawMessage `json:"paths"`
	Definitions map[string]*schema                    `json:"definitions"`
}

type rawOperation struct {
	Summary     string                 `json:"summary"`
	Description string                 `json:"description"`
	Tags        []string               `json:"tags"`
	Consumes    []string               `json:"consumes"`
	Produces    []string               `json:"produces"`
	Parameters  []rawParameter         `json:"parameters"`
	Responses   map[string]rawResponse `json:"responses"`
}

type rawParameter struct {
	Name        string  `json:"name"`
	In          string  `json:"in"`
	Description string  `json:"description"`
	Required    bool    `json:"required"`
	Type        string  `json:"type"`
	Items       *schema `json:"items"`
	Enum        []any   `json:"enum"`
	Schema      *schema `json:"schema"`
}

type rawResponse struct {
	Description string  `json:"description"`
	Schema      *schema `json:"schema"`
}

type schema struct {
	Ref                  string             `json:"$ref"`
	Type                 string             `json:"type"`
	Description          string             `json:"description"`
	Enum                 []any              `json:"enum"`
	Items                *schema            `json:"items"`
	Properties           map[string]*schema `json:"properties"`
	AdditionalProperties json.RawMessage    `json:"additionalProperties"`
}

func Load() (*Spec, error) {
	raw, err := unpack()
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}

func unpack() ([]byte, error) {
	packed, err := files.ReadFile(embeddedSpec)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotBuiltIn
	}
	if err != nil {
		return nil, fmt.Errorf("open the built-in API spec: %w", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		return nil, fmt.Errorf("open the built-in API spec: %w", err)
	}
	defer zr.Close()
	raw, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("read the built-in API spec: %w", err)
	}
	return raw, nil
}

func Parse(raw []byte) (*Spec, error) {
	var doc document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("decode the API spec: %w", err)
	}
	s := &Spec{
		version:     doc.Info.Version,
		base:        strings.TrimRight("/"+strings.Trim(doc.BasePath, "/"), "/"),
		definitions: doc.Definitions,
	}
	if s.definitions == nil {
		s.definitions = map[string]*schema{}
	}
	for path, item := range doc.Paths {
		for method, body := range item {
			m := strings.ToUpper(method)
			if methodRank(m) < 0 {
				continue
			}
			var op rawOperation
			if err := json.Unmarshal(body, &op); err != nil {
				return nil, fmt.Errorf("decode %s %s: %w", m, path, err)
			}
			s.operations = append(s.operations, build(m, s.base+path, op))
		}
	}
	sort.Slice(s.operations, func(i, j int) bool {
		a, b := s.operations[i], s.operations[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return methodRank(a.Method) < methodRank(b.Method)
	})
	return s, nil
}

func (s *Spec) Version() string { return s.version }

func (s *Spec) Operations() []Operation {
	return append([]Operation(nil), s.operations...)
}

func (o Operation) Types() []string { return o.types }

func (s *Spec) Search(query string) []Operation {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return nil
	}
	var hits []Operation
	for _, op := range s.operations {
		hay := strings.ToLower(strings.Join([]string{op.Method, op.Path, op.Summary, op.Description, strings.Join(op.Tags, " ")}, " "))
		matched := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				matched = false
				break
			}
		}
		if matched {
			hits = append(hits, op)
		}
	}
	return hits
}

func (s *Spec) Find(ref string) (Operation, error) {
	method, path := s.splitRef(ref)
	if path == "" {
		return Operation{}, errors.New("give the operation as 'METHOD /path', e.g. 'GET " + s.base + "/sets'")
	}
	want := pathSegments(path)

	best, bestScore := -1, -1
	var methods []string
	for i, op := range s.operations {
		score, ok := matchSegments(pathSegments(op.Path), want)
		if !ok {
			continue
		}
		if score > bestScore {
			bestScore, best, methods = score, -1, nil
		}
		if score < bestScore {
			continue
		}
		methods = append(methods, op.Method)
		if op.Method == method {
			best = i
		}
	}

	switch {
	case best >= 0:
		return s.operations[best], nil
	case len(methods) == 0:
		return Operation{}, s.notFound(path)
	case method == "":
		return Operation{}, fmt.Errorf("give the method too: %s %s", strings.Join(methods, ", "), s.pathOf(want))
	default:
		return Operation{}, fmt.Errorf("%s has no %s operation; it has %s", s.pathOf(want), method, strings.Join(methods, ", "))
	}
}

func (s *Spec) pathOf(want []string) string {
	best, bestScore := "", -1
	for _, op := range s.operations {
		if score, ok := matchSegments(pathSegments(op.Path), want); ok && score > bestScore {
			best, bestScore = op.Path, score
		}
	}
	return best
}

func (s *Spec) notFound(path string) error {
	segs := pathSegments(path)
	prefix := s.base
	if len(segs) > len(pathSegments(s.base)) {
		prefix = "/" + strings.Join(segs[:len(pathSegments(s.base))+1], "/")
	}
	var near []string
	for _, op := range s.operations {
		if op.Path == prefix || strings.HasPrefix(op.Path, prefix+"/") {
			near = append(near, op.Method+" "+op.Path)
		}
	}
	if len(near) == 0 {
		return fmt.Errorf("no operation at %s", path)
	}
	const shown = 10
	if len(near) > shown {
		near = append(near[:shown], fmt.Sprintf("and %d more", len(near)-shown))
	}
	return fmt.Errorf("no operation at %s; under %s there are: %s", path, prefix, strings.Join(near, ", "))
}

func (s *Spec) splitRef(ref string) (string, string) {
	fields := strings.Fields(ref)
	if len(fields) == 0 {
		return "", ""
	}
	method := ""
	if methodRank(strings.ToUpper(fields[0])) >= 0 {
		method = strings.ToUpper(fields[0])
		fields = fields[1:]
	}
	return method, s.normalizePath(strings.Join(fields, ""))
}

func (s *Spec) normalizePath(p string) string {
	if strings.Contains(p, "://") {
		if u, err := url.Parse(p); err == nil {
			p = u.Path
		}
	}
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	p = "/" + p
	if s.base != "" && p != s.base && !strings.HasPrefix(p, s.base+"/") {
		p = s.base + p
	}
	return p
}

func pathSegments(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

func isParam(seg string) bool {
	return strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}")
}

func matchSegments(pattern, path []string) (int, bool) {
	if len(pattern) != len(path) {
		return 0, false
	}
	score := 0
	for i, seg := range pattern {
		switch {
		case strings.EqualFold(seg, path[i]):
			score++
		case isParam(seg):
		default:
			return 0, false
		}
	}
	return score, true
}

func (s *Spec) SchemaName(name string) (string, error) {
	want := strings.TrimSpace(name)
	if _, ok := s.definitions[want]; ok {
		return want, nil
	}
	var hits []string
	for n := range s.definitions {
		short := n[strings.LastIndex(n, ".")+1:]
		if strings.EqualFold(n, want) || strings.EqualFold(short, want) {
			hits = append(hits, n)
		}
	}
	sort.Strings(hits)
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return "", fmt.Errorf("no schema named %q", want)
	default:
		return "", fmt.Errorf("%q could be %s: give the full name", want, strings.Join(hits, ", "))
	}
}

func (s *Spec) Schemas(roots []string, budget int) (map[string]any, []string) {
	out := map[string]any{}
	var more []string
	seen := map[string]bool{}
	queue := append([]string(nil), roots...)
	used := 0
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if seen[name] {
			continue
		}
		seen[name] = true
		sc := s.definitions[name]
		if sc == nil {
			continue
		}
		rendered := render(sc)
		size := renderedSize(name, rendered)
		if len(out) > 0 && used+size > budget {
			more = append(more, name)
			continue
		}
		out[name] = rendered
		used += size
		queue = append(queue, appendRefs(nil, sc)...)
	}
	return out, more
}

func build(method, path string, op rawOperation) Operation {
	out := Operation{
		Method:      method,
		Path:        path,
		Summary:     op.Summary,
		Description: op.Description,
		Tags:        op.Tags,
		Consumes:    op.Consumes,
		Produces:    op.Produces,
	}
	var types []string
	for _, p := range op.Parameters {
		t := p.Type
		switch {
		case p.Schema != nil:
			t = typeName(p.Schema)
			types = appendRefs(types, p.Schema)
		case p.Type == "array":
			t = "[]" + typeName(p.Items)
		}
		if len(p.Enum) > 0 {
			t += oneOf(p.Enum)
		}
		out.Parameters = append(out.Parameters, Parameter{
			Name:        p.Name,
			In:          p.In,
			Type:        t,
			Required:    p.Required,
			Description: p.Description,
		})
	}
	codes := make([]string, 0, len(op.Responses))
	for code := range op.Responses {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		r := op.Responses[code]
		resp := Response{Description: r.Description}
		if r.Schema != nil {
			resp.Type = typeName(r.Schema)
			types = appendRefs(types, r.Schema)
		}
		if out.Responses == nil {
			out.Responses = make(map[string]Response, len(codes))
		}
		out.Responses[code] = resp
	}
	out.types = types
	return out
}

func render(sc *schema) any {
	if len(sc.Properties) == 0 {
		return typeName(sc)
	}
	fields := make(map[string]string, len(sc.Properties))
	for name, prop := range sc.Properties {
		t := typeName(prop)
		if d := strings.TrimSpace(prop.Description); d != "" {
			t += ": " + d
		}
		fields[name] = t
	}
	return fields
}

func renderedSize(name string, rendered any) int {
	raw, err := json.Marshal(rendered)
	if err != nil {
		return 0
	}
	return len(name) + len(raw) + 4
}

func typeName(sc *schema) string {
	switch {
	case sc == nil:
		return "any"
	case sc.Ref != "":
		return refName(sc.Ref)
	case sc.Type == "array":
		return "[]" + typeName(sc.Items)
	case len(sc.Properties) > 0:
		names := make([]string, 0, len(sc.Properties))
		for name := range sc.Properties {
			names = append(names, name)
		}
		sort.Strings(names)
		parts := make([]string, 0, len(names))
		for _, name := range names {
			parts = append(parts, name+": "+typeName(sc.Properties[name]))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	if value, ok := sc.mapValue(); ok {
		return "map[string]" + typeName(value)
	}
	if sc.Type == "" {
		return "any"
	}
	if len(sc.Enum) > 0 {
		return sc.Type + oneOf(sc.Enum)
	}
	return sc.Type
}

func (sc *schema) mapValue() (*schema, bool) {
	raw := bytes.TrimSpace(sc.AdditionalProperties)
	switch {
	case len(raw) == 0, bytes.Equal(raw, []byte("false")):
		return nil, false
	case bytes.Equal(raw, []byte("true")):
		return nil, true
	}
	var value schema
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, true
	}
	if value.Ref == "" && value.Type == "" && len(value.Properties) == 0 && len(value.AdditionalProperties) == 0 {
		return nil, true
	}
	return &value, true
}

func appendRefs(names []string, sc *schema) []string {
	if sc == nil {
		return names
	}
	if sc.Ref != "" {
		name := refName(sc.Ref)
		for _, n := range names {
			if n == name {
				return names
			}
		}
		return append(names, name)
	}
	names = appendRefs(names, sc.Items)
	props := make([]string, 0, len(sc.Properties))
	for name := range sc.Properties {
		props = append(props, name)
	}
	sort.Strings(props)
	for _, name := range props {
		names = appendRefs(names, sc.Properties[name])
	}
	if value, ok := sc.mapValue(); ok {
		names = appendRefs(names, value)
	}
	return names
}

func refName(ref string) string {
	return strings.TrimPrefix(ref, "#/definitions/")
}

func oneOf(values []any) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = fmt.Sprint(v)
	}
	return " (one of: " + strings.Join(parts, ", ") + ")"
}

func methodRank(method string) int {
	for i, m := range methodOrder {
		if m == method {
			return i
		}
	}
	return -1
}
