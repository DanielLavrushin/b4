package hubwire

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

var fingerprintRoots = []string{"tcp", "udp", "fragmentation", "faking", "mss_clamp", "dns"}

var fingerprintExcluded = []string{"tcp.dport_filter", "udp.dport_filter", "dns.pins", "dns.target_dns"}

func Fingerprint(projection map[string]interface{}) string {
	subset := make(map[string]interface{}, len(fingerprintRoots))
	for _, root := range fingerprintRoots {
		if v, ok := projection[root]; ok {
			subset[root] = deepCopy(v)
		}
	}
	for _, path := range fingerprintExcluded {
		deletePath(subset, path)
	}
	for _, root := range fingerprintRoots {
		if sub, ok := subset[root].(map[string]interface{}); ok && len(sub) == 0 {
			delete(subset, root)
		}
	}
	data, _ := json.Marshal(subset)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func deepCopy(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, val := range t {
			out[k] = deepCopy(val)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, val := range t {
			out[i] = deepCopy(val)
		}
		return out
	default:
		return v
	}
}

func splitPath(path string) []string {
	return strings.Split(path, ".")
}

func lookupPath(m map[string]interface{}, path string) (interface{}, bool) {
	parts := splitPath(path)
	var cur interface{} = m
	for _, p := range parts {
		sub, ok := cur.(map[string]interface{})
		if !ok {
			return nil, false
		}
		cur, ok = sub[p]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func deletePath(m map[string]interface{}, path string) bool {
	parts := splitPath(path)
	cur := m
	for i, p := range parts {
		if i == len(parts)-1 {
			if _, ok := cur[p]; !ok {
				return false
			}
			delete(cur, p)
			return true
		}
		sub, ok := cur[p].(map[string]interface{})
		if !ok {
			return false
		}
		cur = sub
	}
	return false
}

func setPath(m map[string]interface{}, path string, v interface{}) {
	parts := splitPath(path)
	cur := m
	for i, p := range parts {
		if i == len(parts)-1 {
			cur[p] = v
			return
		}
		sub, ok := cur[p].(map[string]interface{})
		if !ok {
			sub = make(map[string]interface{})
			cur[p] = sub
		}
		cur = sub
	}
}

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
