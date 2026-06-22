package engine

const (
	TunSteerMark    = 0x40000000
	ClientMark      = 0x20000000
	ReinjectMarkBit = 0x10000000
)

type PacketVerdict int

const (
	VerdictAccept PacketVerdict = iota
	VerdictDrop
)

type Engine interface {
	Start() error
	Stop()
}
