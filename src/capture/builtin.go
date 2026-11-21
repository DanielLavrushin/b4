package capture

import (
	"sync"

	"github.com/daniellavrushin/b4/config"
)

const (
	SourceYoutubeUnblock        = "youtubeunblock"
	SourceYoutubeUnblockDerived = "youtubeunblock_derived"
	SourceZapret                = "zapret"
)

type Builtin struct {
	ID       string `json:"id"`
	Protocol string `json:"protocol"`
	Size     int    `json:"size"`
	Facts    *Facts `json:"facts"`
	Source   string `json:"source"`
}

type builtinPayload struct {
	id       string
	protocol string
	data     []byte
	source   string
}

var builtinPayloads = []builtinPayload{
	{"google", "tls", config.FakeSNI1, SourceYoutubeUnblock},
	{"vk", "tls", config.FakeSNI2, SourceYoutubeUnblockDerived},
	{"stun", "tls", config.FakeSTUN, SourceZapret},
	{"quic1", "quic", config.FakeQUIC1, SourceZapret},
	{"quic2", "quic", config.FakeQUIC2, ""},
}

var builtinRows = sync.OnceValue(func() []Builtin {
	rows := make([]Builtin, 0, len(builtinPayloads))
	for _, p := range builtinPayloads {
		rows = append(rows, Builtin{
			ID:       p.id,
			Protocol: p.protocol,
			Size:     len(p.data),
			Facts:    FactsOf(p.protocol, p.data),
			Source:   p.source,
		})
	}
	return rows
})

func Builtins() []Builtin {
	return append([]Builtin(nil), builtinRows()...)
}

func BuiltinBytes(id string) ([]byte, bool) {
	for _, p := range builtinPayloads {
		if p.id == id {
			return p.data, true
		}
	}
	return nil, false
}
