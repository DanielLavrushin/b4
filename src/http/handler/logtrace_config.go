package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/maphash"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const (
	traceSampleEntries = 8
	traceListEntries   = 64
	traceTextBytes     = 512
	traceValueBytes    = 160
	traceLineBytes     = 4 << 10
	traceNoteBytes     = 16 << 10
	traceNone          = "none"
	traceTextCut       = "... [cut]"
	tracePins          = "dns.pins"
)

var (
	traceCountedLists = []string{
		"queue.devices.devices",
		"system.socks5.allowed_sources",
		"targets.source_devices",
	}
	traceSampledLists = []string{
		tracePins,
		"discovery.urls",
		"system.checker.watchdog.domains",
		"targets.ip",
		"targets.sni_domains",
	}

	traceDefaults = sync.OnceValues(func() (global, set map[string]any) {
		cfg, fresh := config.NewConfig(), config.NewSetConfig()
		return flatTraceDoc(&cfg), flatTraceDoc(&fresh)
	})

	traceSeed      = maphash.MakeSeed()
	recordedConfig atomic.Pointer[traceConfigLog]
)

type traceCount int

type traceLeaf struct {
	sum   uint64
	value any
	shown bool
}

type traceValues struct {
	leaves   map[string]traceLeaf
	defaults map[string]any
}

type traceSet struct {
	id     string
	name   string
	values traceValues
}

type traceConfig struct {
	global traceValues
	sets   []traceSet
}

type traceConfigLog struct {
	load func() *config.Config

	mu    sync.Mutex
	shown *config.Config
	last  traceConfig
}

func NoteTraceConfig() {
	if l := recordedConfig.Load(); l != nil {
		l.note()
	}
}

func (l *traceConfigLog) note() {
	l.mu.Lock()
	defer l.mu.Unlock()
	cfg := l.load()
	if cfg == l.shown {
		return
	}
	l.shown = cfg
	now, _, err := readTraceConfig(cfg)
	if err != nil {
		log.Debugf("Configuration changed, it cannot be shown: %v", err)
		return
	}
	lines := boundTraceLines(now.changesSince(l.last))
	l.last = now
	if recordedConfig.Load() != l {
		return
	}
	for _, line := range lines {
		log.Debugf("Configuration changed: %s", line)
	}
}

func readTraceConfig(cfg *config.Config) (traceConfig, map[string]any, error) {
	copied, err := cfg.RedactedCopy()
	if err != nil {
		return traceConfig{}, nil, err
	}
	safe, err := config.SparseMap(copied)
	if err != nil {
		return traceConfig{}, nil, err
	}
	raw, err := config.SparseMap(cfg)
	if err != nil {
		return traceConfig{}, nil, err
	}

	safeSets, rawSets := takeTraceSets(safe, cfg), takeTraceSets(raw, cfg)
	globalDefaults, setDefaults := traceDefaults()
	countTraceLists("", safe)
	out, doc := traceConfig{global: traceValuesOf(raw, safe, globalDefaults)}, cutTraceDoc("", safe, globalDefaults)
	docs := make([]any, 0, len(safeSets))
	for i, set := range safeSets {
		countTraceLists("", set)
		docs = append(docs, cutTraceDoc("", set, setDefaults))
		id, _ := set["id"].(string)
		name, _ := set["name"].(string)
		delete(set, "id")
		delete(rawSets[i], "id")
		out.sets = append(out.sets, traceSet{id: id, name: name, values: traceValuesOf(rawSets[i], set, setDefaults)})
	}
	doc["sets"] = docs
	return out, doc, nil
}

func takeTraceSets(doc map[string]any, cfg *config.Config) []map[string]any {
	entries, _ := doc["sets"].([]any)
	delete(doc, "sets")
	delete(doc, "ui")
	sets := make([]map[string]any, 0, len(entries))
	for i, entry := range entries {
		set, ok := entry.(map[string]any)
		if !ok || i >= len(cfg.Sets) || cfg.Sets[i] == nil {
			continue
		}
		if hub, ok := set["hub"].(map[string]any); ok {
			maps.DeleteFunc(hub, func(key string, _ any) bool { return key != "applied_at" })
			if len(hub) == 0 {
				delete(set, "hub")
			}
		}
		if cfg.Sets[i].Enabled {
			addResolvedTargets(set, cfg.Sets[i].Targets)
		}
		sets = append(sets, set)
	}
	return sets
}

func addResolvedTargets(set map[string]any, t config.TargetsConfig) {
	targets, ok := set["targets"].(map[string]any)
	if !ok {
		targets = map[string]any{}
		set["targets"] = targets
	}
	targets["resolved_domains"] = len(t.DomainsToMatch)
	targets["resolved_ips"] = len(t.IpsToMatch)
	if missing := unresolvedASNs(t.ASNs); len(missing) > 0 {
		list := make([]any, len(missing))
		for i, id := range missing {
			list[i] = id
		}
		targets["asns_unresolved"] = list
	}
}

func tracePath(base, key string) string {
	if base == "" {
		return key
	}
	return base + "." + key
}

func countTraceLists(base string, node map[string]any) {
	for key, value := range node {
		path := tracePath(base, key)
		switch v := value.(type) {
		case map[string]any:
			countTraceLists(path, v)
		case []any:
			if slices.Contains(traceCountedLists, path) {
				node[key] = traceCount(len(v))
			}
		}
	}
}

func cutTraceDoc(base string, node, defaults map[string]any) map[string]any {
	out := make(map[string]any, len(node)+1)
	for key, value := range node {
		path := tracePath(base, key)
		switch v := value.(type) {
		case traceCount:
			out[key+"_total"] = int(v)
			continue
		case map[string]any:
			if most := traceSampleEntries; path == tracePins && len(v) > most {
				out[key+"_total"] = len(v)
				sample := make(map[string]any, most)
				for _, name := range slices.Sorted(maps.Keys(v))[:most] {
					sample[name] = v[name]
				}
				value = sample
			}
		case []any:
			most := traceListEntries
			if slices.Contains(traceSampledLists, path) {
				most = traceSampleEntries
			}
			if len(v) > most {
				out[key+"_total"] = len(v)
				value = v[:most]
			}
		}
		out[key] = cutTraceValue(path, value, defaults)
	}
	if _, told := out["enabled"]; !told {
		if enabled, known := defaults[tracePath(base, "enabled")]; known {
			out["enabled"] = enabled
		}
	}
	return out
}

func cutTraceValue(path string, value any, defaults map[string]any) any {
	switch v := value.(type) {
	case map[string]any:
		return cutTraceDoc(path, v, defaults)
	case []any:
		list := make([]any, len(v))
		for i, entry := range v {
			list[i] = cutTraceValue(path+"[]", entry, defaults)
		}
		return list
	case string:
		return cutTraceText(v, traceTextBytes)
	}
	return value
}

func cutTraceText(text string, most int) string {
	if len(text) <= most {
		return text
	}
	return strings.ToValidUTF8(text[:most-len(traceTextCut)], "") + traceTextCut
}

func traceValuesOf(raw, safe, defaults map[string]any) traceValues {
	leaves := map[string]traceLeaf{}
	walkTraceDoc("", raw, func(path string, value any) {
		leaves[path] = traceLeaf{sum: traceSum(value)}
	})
	walkTraceDoc("", safe, func(path string, value any) {
		leaf, ok := leaves[path]
		if !ok {
			leaf.sum = traceSum(value)
		}
		leaf.value, leaf.shown = value, true
		leaves[path] = leaf
	})
	return traceValues{leaves: leaves, defaults: defaults}
}

func traceSum(value any) uint64 {
	data, _ := json.Marshal(value)
	return maphash.Bytes(traceSeed, data)
}

func flatTraceDoc(v any) map[string]any {
	data, _ := json.Marshal(v)
	var doc map[string]any
	_ = json.Unmarshal(data, &doc)
	flat := map[string]any{}
	walkTraceDoc("", doc, func(path string, value any) { flat[path] = value })
	return flat
}

func walkTraceDoc(base string, node map[string]any, leaf func(path string, value any)) {
	for key, value := range node {
		path := tracePath(base, key)
		child, ok := value.(map[string]any)
		switch {
		case !ok:
			leaf(path, value)
		case path == tracePins:
			leaf(path, tracePinList(child))
		default:
			walkTraceDoc(path, child, leaf)
		}
	}
}

func tracePinList(pins map[string]any) []any {
	list := make([]any, 0, len(pins))
	for _, name := range slices.Sorted(maps.Keys(pins)) {
		entry := name
		addresses, _ := pins[name].([]any)
		for _, address := range addresses {
			entry += " " + fmt.Sprint(address)
		}
		list = append(list, entry)
	}
	return list
}

func (now traceConfig) changesSince(was traceConfig) []string {
	var lines []string
	if changes := now.global.changesSince(was.global); len(changes) > 0 {
		lines = append(lines, strings.Join(changes, "; "))
	}

	label := traceLabels(was.sets, now.sets)
	before := make(map[string]traceSet, len(was.sets))
	for _, set := range was.sets {
		before[set.id] = set
	}
	after := make(map[string]bool, len(now.sets))
	for i, set := range now.sets {
		after[set.id] = true
		old, known := before[set.id]
		if !known {
			lines = append(lines, fmt.Sprintf("%s added as set %d of %d: id %q; %s", label(set), i+1, len(now.sets), set.id, strings.Join(set.values.texts(), "; ")))
			continue
		}
		if changes := set.values.changesSince(old.values); len(changes) > 0 {
			lines = append(lines, label(set)+": "+strings.Join(changes, "; "))
		}
	}
	for _, set := range was.sets {
		if !after[set.id] {
			lines = append(lines, label(set)+" removed")
		}
	}
	if traceOrderChanged(was.sets, now.sets) {
		labels := make([]string, 0, len(now.sets))
		for _, set := range now.sets {
			labels = append(labels, label(set))
		}
		lines = append(lines, "the order of the sets: "+strings.Join(labels, ", "))
	}
	return lines
}

func traceLabels(was, now []traceSet) func(traceSet) string {
	owner, shared := map[string]string{}, map[string]bool{}
	for _, set := range slices.Concat(was, now) {
		if id, taken := owner[set.name]; taken && id != set.id {
			shared[set.name] = true
		}
		owner[set.name] = set.id
	}
	return func(set traceSet) string {
		switch {
		case set.name == "":
			return fmt.Sprintf("set %q", set.id)
		case shared[set.name]:
			return fmt.Sprintf("set %q (id %q)", set.name, set.id)
		}
		return fmt.Sprintf("set %q", set.name)
	}
}

func traceOrderChanged(was, now []traceSet) bool {
	kept := func(sets, others []traceSet) []traceSet {
		known := make(map[string]bool, len(others))
		for _, set := range others {
			known[set.id] = true
		}
		return slices.DeleteFunc(slices.Clone(sets), func(set traceSet) bool { return !known[set.id] })
	}
	same := func(a, b traceSet) bool { return a.id == b.id }
	return !slices.EqualFunc(kept(was, now), kept(now, was), same)
}

func (v traceValues) at(path string) traceLeaf {
	if leaf, ok := v.leaves[path]; ok {
		return leaf
	}
	return traceLeaf{value: v.defaults[path], shown: true}
}

func (v traceValues) texts() []string {
	texts := make([]string, 0, len(v.leaves))
	for _, path := range slices.Sorted(maps.Keys(v.leaves)) {
		texts = append(texts, path+" "+v.leaves[path].text())
	}
	return texts
}

func (v traceValues) changesSince(was traceValues) []string {
	paths := slices.Collect(maps.Keys(v.leaves))
	for path := range was.leaves {
		if _, ok := v.leaves[path]; !ok {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)

	var changes []string
	for _, path := range paths {
		old, had := was.leaves[path]
		leaf, has := v.leaves[path]
		if had && has && old.sum == leaf.sum {
			continue
		}
		changes = append(changes, path+" "+traceChange(path, was.at(path), v.at(path)))
	}
	return changes
}

func traceChange(path string, was, now traceLeaf) string {
	oldList, _ := was.value.([]any)
	list, _ := now.value.([]any)
	if slices.Contains(traceSampledLists, path) || max(len(oldList), len(list)) > traceSampleEntries {
		return traceListChange(oldList, list)
	}
	before, after := was.text(), now.text()
	if before == after {
		return "changed"
	}
	return before + " -> " + after
}

func (l traceLeaf) text() string {
	if !l.shown {
		return config.RedactedMarker
	}
	switch v := l.value.(type) {
	case nil:
		return traceNone
	case traceCount:
		return fmt.Sprintf("%d entries", int(v))
	case []any:
		switch {
		case len(v) == 0:
			return traceNone
		case len(v) > traceSampleEntries:
			return fmt.Sprintf("%s of %d entries", traceSample(v), len(v))
		}
		return traceSample(v)
	}
	return cutTraceText(traceJSON(l.value), traceValueBytes)
}

func traceListChange(was, now []any) string {
	added, removed := traceListDiff(was, now), traceListDiff(now, was)
	var parts []string
	for _, side := range []struct {
		verb    string
		entries []any
	}{{"added", added}, {"removed", removed}} {
		if n := len(side.entries); n > traceSampleEntries {
			parts = append(parts, fmt.Sprintf("%s %s of %d entries", side.verb, traceSample(side.entries), n))
		} else if n > 0 {
			parts = append(parts, side.verb+" "+traceSample(side.entries))
		}
	}
	switch {
	case len(parts) > 0:
		return fmt.Sprintf("%s (%d -> %d entries)", strings.Join(parts, ", "), len(was), len(now))
	case slices.EqualFunc(was, now, func(a, b any) bool { return traceEntryKey(a) == traceEntryKey(b) }):
		return "changed"
	}
	return "reordered"
}

func traceListDiff(was, now []any) []any {
	left := make(map[string]int, len(was))
	for _, entry := range was {
		left[traceEntryKey(entry)]++
	}
	var added []any
	for _, entry := range now {
		if key := traceEntryKey(entry); left[key] > 0 {
			left[key]--
			continue
		}
		added = append(added, entry)
	}
	return added
}

func traceEntryKey(entry any) string {
	if text, ok := entry.(string); ok {
		return text
	}
	return traceJSON(entry)
}

func traceSample(entries []any) string {
	texts := make([]string, 0, traceSampleEntries)
	for _, entry := range entries[:min(len(entries), traceSampleEntries)] {
		texts = append(texts, cutTraceText(traceJSON(entry), traceValueBytes))
	}
	return "[" + strings.Join(texts, ", ") + "]"
}

func traceJSON(value any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return err.Error()
	}
	return strings.TrimSuffix(buf.String(), "\n")
}

func traceLineRune(r rune) rune {
	if unicode.IsControl(r) {
		return ' '
	}
	return r
}

func boundTraceLines(lines []string) []string {
	bounded := make([]string, 0, len(lines))
	total := 0
	for i, line := range lines {
		line = cutTraceText(strings.Map(traceLineRune, line), traceLineBytes)
		if total += len(line); total > traceNoteBytes {
			return append(bounded, fmt.Sprintf("%d more changes", len(lines)-i))
		}
		bounded = append(bounded, line)
	}
	return bounded
}
