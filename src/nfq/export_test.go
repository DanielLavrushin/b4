package nfq

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

type InjectedPiece struct {
	Offset int
	Data   []byte
}

func InjectedRealPieces(t *testing.T, set *config.SetConfig, ipv6 bool, payload []byte) []InjectedPiece {
	t.Helper()
	family := injectFamilies()[0]
	if ipv6 {
		family = injectFamilies()[1]
	}
	cfg := config.NewConfig()
	w := newTestWorker(t, &cfg)
	sent := captureInjected(t, w)
	openComboStream(t, w, family.packet, family.parse, disorderTestSeq)
	family.inject(w, set, family.packet(payload, disorderTestSeq), family.dst, true)
	var pieces []InjectedPiece
	for _, p := range realPieces(t, family, sent()) {
		pieces = append(pieces, InjectedPiece{Offset: p.offset, Data: p.data})
	}
	return pieces
}
