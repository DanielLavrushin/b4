package nfq

import (
	"container/list"

	"github.com/daniellavrushin/b4/ctnl"
	"github.com/daniellavrushin/b4/log"
)

type collectedHistory struct {
	flows map[string]*list.Element
	order list.List
}

func (t *destStateTracker) rememberCollectedLocked(h *helloWatch) bool {
	if !h.collect || h.flowID == 0 {
		return true
	}
	record := &helloWatch{key: h.key, flowID: h.flowID, client: h.client, dst: h.dst, wire: h.wire, cport: h.cport, port: h.port, tun: h.tun}
	if previous := t.collectHistory.flows[h.key]; previous != nil {
		if previous.Value.(*helloWatch).flowID == h.flowID {
			return false
		}
		previous.Value = record
		t.collectHistory.order.MoveToBack(previous)
		return true
	}
	if len(t.collectHistory.flows) >= maxHelloWatches {
		if !t.historyFullNoted {
			t.historyFullNoted = true
			log.Warnf("Collected IPs: %d connections of Collect sets are remembered, the maximum; new connections of Collect sets are not watched until older ones close", maxHelloWatches)
		}
		return false
	}
	t.historyFullNoted = false
	if t.collectHistory.flows == nil {
		t.collectHistory.flows = make(map[string]*list.Element)
	}
	t.collectHistory.flows[h.key] = t.collectHistory.order.PushBack(record)
	t.collectRecorded.Store(int32(len(t.collectHistory.flows)))
	return true
}

func (t *destStateTracker) forgetCollectedHistoryLocked(h *helloWatch) {
	if !h.collect || h.flowID == 0 {
		return
	}
	entry := t.collectHistory.flows[h.key]
	if entry == nil || entry.Value.(*helloWatch).flowID != h.flowID {
		return
	}
	t.collectHistory.order.Remove(entry)
	delete(t.collectHistory.flows, h.key)
	t.collectRecorded.Store(int32(len(t.collectHistory.flows)))
}

func (t *destStateTracker) cleanupCollectedHistory() {
	type pending struct {
		original *helloWatch
		watch    helloWatch
	}
	if t == nil || t.collectRecorded.Load() == 0 {
		return
	}
	t.watchMu.Lock()
	limit := min(t.collectHistory.order.Len(), maxSilenceLookups)
	batch := make([]pending, 0, limit)
	for range limit {
		entry := t.collectHistory.order.Front()
		t.collectHistory.order.MoveToBack(entry)
		h := entry.Value.(*helloWatch)
		if t.watches[h.key] == nil {
			batch = append(batch, pending{h, *h})
		}
	}
	t.watchMu.Unlock()
	for _, item := range batch {
		flow, found, err := helloFlow(item.watch)
		if err != nil {
			return
		}
		if found && (flow.ID == 0 || flow.ID == item.watch.flowID && (!flow.HasState || flow.TCPState == ctnl.TCPEstablished)) {
			continue
		}
		t.watchMu.Lock()
		entry := t.collectHistory.flows[item.watch.key]
		if entry != nil && entry.Value == item.original && t.watches[item.watch.key] == nil {
			t.collectHistory.order.Remove(entry)
			delete(t.collectHistory.flows, item.watch.key)
			t.collectRecorded.Store(int32(len(t.collectHistory.flows)))
		}
		t.watchMu.Unlock()
	}
}
