package nfq

import (
	"sync"
	"time"
)

const (
	dnsPinNoteTTL      = 10 * time.Second
	dnsPinNoteMax      = 4096
	dnsPinNoteSweepGap = time.Second
)

type dnsPinNoteKey struct {
	ver   byte
	port  uint16
	txid  uint16
	qtype uint16
	name  string
}

type dnsPinNote struct {
	setID   string
	expires time.Time
}

var (
	dnsPinNotesMu    sync.Mutex
	dnsPinNotes      = map[dnsPinNoteKey]dnsPinNote{}
	dnsPinNotesSwept time.Time
)

func noteDNSPinSet(key dnsPinNoteKey, setID string) {
	now := time.Now()
	dnsPinNotesMu.Lock()
	defer dnsPinNotesMu.Unlock()
	if len(dnsPinNotes) >= dnsPinNoteMax {
		if now.Sub(dnsPinNotesSwept) < dnsPinNoteSweepGap {
			return
		}
		dnsPinNotesSwept = now
		for k, n := range dnsPinNotes {
			if now.After(n.expires) {
				delete(dnsPinNotes, k)
			}
		}
		if len(dnsPinNotes) >= dnsPinNoteMax {
			return
		}
	}
	dnsPinNotes[key] = dnsPinNote{setID: setID, expires: now.Add(dnsPinNoteTTL)}
}

func takeDNSPinSet(key dnsPinNoteKey) (string, bool) {
	dnsPinNotesMu.Lock()
	defer dnsPinNotesMu.Unlock()
	n, ok := dnsPinNotes[key]
	if !ok {
		return "", false
	}
	delete(dnsPinNotes, key)
	if time.Now().After(n.expires) {
		return "", false
	}
	return n.setID, true
}
