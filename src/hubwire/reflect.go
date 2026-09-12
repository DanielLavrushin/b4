package hubwire

import (
	"reflect"
	"strings"

	"github.com/daniellavrushin/b4/config"
)

type knownPath struct {
	leaf bool
}

var setPaths = collectPaths(reflect.TypeOf(config.SetConfig{}))

func collectPaths(t reflect.Type) map[string]knownPath {
	out := make(map[string]knownPath)
	walkType(t, "", out)
	return out
}

func walkType(t reflect.Type, prefix string, out map[string]knownPath) {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "-" || name == "" {
			continue
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		ft := f.Type
		for ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct {
			before := len(out)
			out[path] = knownPath{leaf: false}
			walkType(ft, path, out)
			if len(out) == before+1 {
				out[path] = knownPath{leaf: true}
			}
			continue
		}
		out[path] = knownPath{leaf: true}
	}
}

func LeafPaths() []string {
	out := make([]string, 0, len(setPaths))
	for p, k := range setPaths {
		if k.leaf {
			out = append(out, p)
		}
	}
	return out
}

func unknownPaths(m map[string]interface{}) []string {
	var unknown []string
	var walk func(node map[string]interface{}, prefix string)
	walk = func(node map[string]interface{}, prefix string) {
		for _, k := range sortedKeys(node) {
			path := k
			if prefix != "" {
				path = prefix + "." + k
			}
			known, ok := setPaths[path]
			if !ok {
				unknown = append(unknown, path)
				continue
			}
			if known.leaf {
				continue
			}
			if sub, ok := node[k].(map[string]interface{}); ok {
				walk(sub, path)
			}
		}
	}
	walk(m, "")
	return unknown
}
