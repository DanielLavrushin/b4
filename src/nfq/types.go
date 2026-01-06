package nfq

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/daniellavrushin/b4/dhcp"
	"github.com/daniellavrushin/b4/sock"
	"github.com/florianl/go-nfqueue"
)

type Segment struct {
	Data []byte
	Seq  uint32
}

type Pool struct {
	Workers  []*Worker
	configMu sync.Mutex
	Dhcp     *dhcp.Manager
}

type PacketInfo struct {
	IPHdrLen     int
	TCPHdrLen    int
	PayloadStart int
	PayloadLen   int
	Payload      []byte
	Seq0         uint32
	ID0          uint16
	IsIPv6       bool
}

type Worker struct {
	packetsProcessed uint64
	lastOverflowLog  int64
	cfg              atomic.Value
	qnum             uint16
	ctx              context.Context
	cancel           context.CancelFunc
	q                *nfqueue.Nfqueue
	wg               sync.WaitGroup
	matcher          atomic.Value
	sock             *sock.Sender
	ipToMac          atomic.Value
	connState        sync.Map
}
