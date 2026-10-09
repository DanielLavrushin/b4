package metrics

type topRow[K any] struct {
	key   K
	count uint64
	last  int64
	seq   uint64
	sets  [TopSetsKept]string
}

func (r topRow[K]) ranksAbove(o topRow[K]) bool {
	if r.count != o.count {
		return r.count > o.count
	}
	return r.seq > o.seq
}

func insertTop[K any](top []topRow[K], r topRow[K], n int) []topRow[K] {
	i := len(top)
	for i > 0 && r.ranksAbove(top[i-1]) {
		i--
	}
	if i >= n {
		return top
	}
	if len(top) < n {
		top = append(top, topRow[K]{})
	}
	copy(top[i+1:], top[i:len(top)-1])
	top[i] = r
	return top
}

func topEntries[K any](rows []topRow[K], name func(K) string, off int64) []TopEntry {
	out := make([]TopEntry, len(rows))
	for i, r := range rows {
		out[i] = TopEntry{Key: name(r.key), Count: r.count, Last: wallMs(r.last, off), Sets: setIDs(r.sets)}
	}
	return out
}

func withSet(sets [TopSetsKept]string, id string) [TopSetsKept]string {
	if sets[0] == id {
		return sets
	}
	carry := id
	for i := range sets {
		sets[i], carry = carry, sets[i]
		if carry == id || carry == "" {
			break
		}
	}
	return sets
}

func setIDs(sets [TopSetsKept]string) []string {
	var out []string
	for _, id := range sets {
		if id == "" {
			break
		}
		out = append(out, id)
	}
	return out
}
