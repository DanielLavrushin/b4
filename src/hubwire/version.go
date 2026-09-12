package hubwire

import (
	"strconv"
	"strings"
)

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(v), "v"))
	if v == "" || v == "dev" {
		return out, false
	}
	end := 0
	for end < len(v) && (v[end] == '.' || (v[end] >= '0' && v[end] <= '9')) {
		end++
	}
	parts := strings.Split(v[:end], ".")
	if len(parts) < 2 {
		return out, false
	}
	for i := 0; i < 3 && i < len(parts); i++ {
		if parts[i] == "" {
			break
		}
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

func CompareVersions(a, b string) int {
	va, okA := parseVersion(a)
	vb, okB := parseVersion(b)
	switch {
	case !okA && !okB:
		return 0
	case !okA:
		return 1
	case !okB:
		return -1
	}
	for i := 0; i < 3; i++ {
		if va[i] != vb[i] {
			if va[i] < vb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func MinVersion(projection map[string]interface{}) string {
	min := BaselineVersion
	walkLeaves(projection, "", func(path string, _ interface{}) {
		f, ok := Fields[path]
		if !ok || f.Class != Crosses || f.Since == "" {
			return
		}
		if CompareVersions(f.Since, min) > 0 {
			min = f.Since
		}
	})
	return min
}

func walkLeaves(m map[string]interface{}, prefix string, fn func(path string, v interface{})) {
	for k, v := range m {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		if sub, ok := v.(map[string]interface{}); ok {
			if _, leaf := Fields[path]; !leaf {
				walkLeaves(sub, path, fn)
				continue
			}
		}
		fn(path, v)
	}
}
