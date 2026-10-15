package tables

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

var QueueDirectionExactFunc func(v4, v6 bool)

var (
	queueDirectionMu       sync.Mutex
	queueDirectionV4       bool
	queueDirectionV6       bool
	queueRulesPortSelected map[string]bool
)

func (m Manifest) directionExactFamilies() (v4, v6 bool) {
	return queueDirectionExactFamilies(m.DirectionExact)
}

func queueDirectionExactFamilies(exact map[string]bool) (v4, v6 bool) {
	var seen4, seen6 bool
	v4, v6 = true, true
	for bin, ok := range exact {
		if strings.HasPrefix(bin, "ip6") {
			seen6, v6 = true, v6 && ok
		} else {
			seen4, v4 = true, v4 && ok
		}
	}
	return seen4 && v4, seen6 && v6
}

func publishQueueDirectionExactLocked(v4, v6 bool) {
	queueDirectionV4, queueDirectionV6 = v4, v6
	if QueueDirectionExactFunc != nil {
		QueueDirectionExactFunc(v4, v6)
	}
}

func noteQueueDirectionExact(v4, v6 bool) {
	queueDirectionMu.Lock()
	defer queueDirectionMu.Unlock()
	publishQueueDirectionExactLocked(v4, v6)
}

func narrowQueueDirectionExact(v4, v6 bool) {
	queueDirectionMu.Lock()
	defer queueDirectionMu.Unlock()
	if (queueDirectionV4 && !v4) || (queueDirectionV6 && !v6) {
		publishQueueDirectionExactLocked(queueDirectionV4 && v4, queueDirectionV6 && v6)
	}
}

func noteQueueRulesApplied(exact map[string]bool, applied bool) {
	queueDirectionMu.Lock()
	defer queueDirectionMu.Unlock()
	for bin, ok := range exact {
		if ok {
			continue
		}
		if queueRulesPortSelected == nil {
			queueRulesPortSelected = map[string]bool{}
		}
		queueRulesPortSelected[bin] = true
	}
	var v4, v6 bool
	if applied {
		v4, v6 = queueDirectionExactFamilies(exact)
	}
	publishQueueDirectionExactLocked(v4, v6)
}

func noteQueueRulesCleared() {
	queueDirectionMu.Lock()
	defer queueDirectionMu.Unlock()
	queueRulesPortSelected = nil
	publishQueueDirectionExactLocked(false, false)
}

func queueDirectionMoves(exact map[string]bool) []string {
	queueDirectionMu.Lock()
	defer queueDirectionMu.Unlock()
	var moves []string
	for bin, ok := range exact {
		if ok && queueRulesPortSelected[bin] {
			moves = append(moves, fmt.Sprintf("IPTABLES[%s]: the queue rules in place select TCP packets by port alone and the new ones also by conntrack direction, rebuilding the rules", bin))
		}
	}
	sort.Strings(moves)
	return moves
}
