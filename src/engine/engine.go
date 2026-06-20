package engine

const (
	TunSteerMark    = 0x80000
	TunClientMark   = 0x100000
	ReinjectMarkBit = 0x800000
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
