package engine

const (
	TunSteerMark    = 0x40000000
	ClientMark      = 0x20000000
	ReinjectMarkBit = 0x10000000
	InjectedMarkBit = 0x2000000
)

type PacketVerdict int

const (
	VerdictAccept PacketVerdict = iota
	VerdictDrop
	VerdictDropFlow
)

type Direction uint8

const (
	DirUnknown Direction = iota
	DirOriginal
	DirReply
)

type Engine interface {
	Start() error
	Stop()
}
