package capture

import (
	"sync"

	"github.com/daniellavrushin/b4/config"
)

type Builtin struct {
	ID       string `json:"id"`
	Protocol string `json:"protocol"`
	Size     int    `json:"size"`
	Facts    *Facts `json:"facts"`
}

type builtinPayload struct {
	id       string
	protocol string
	data     []byte
}

var builtinPayloads = []builtinPayload{
	{"google", "tls", config.FakeSNI1},
	{"vk", "tls", config.FakeSNI2},
	{"stun", "tls", config.FakeSTUN},
	{"quic1", "quic", config.FakeQUIC1},
	{"quic2", "quic", config.FakeQUIC2},
}

var builtinRows = sync.OnceValue(func() []Builtin {
	rows := make([]Builtin, 0, len(builtinPayloads))
	for _, p := range builtinPayloads {
		rows = append(rows, Builtin{
			ID:       p.id,
			Protocol: p.protocol,
			Size:     len(p.data),
			Facts:    FactsOf(p.protocol, p.data),
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
