package nfq

import (
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sock"
	"github.com/florianl/go-nfqueue"
)

type verdictSetter interface {
	SetVerdict(id uint32, verdict int) error
}

type verdictModifier interface {
	SetVerdictModPacket(id uint32, verdict int, packet []byte) error
}

type verdictCtx struct {
	id            uint32
	q             verdictSetter
	verdict       engine.PacketVerdict
	copy          *dnsCopyDecision
	ifaceFiltered bool
	hook          uint8
	hooked        bool
	dir           engine.Direction
	flowDrop      bool
	tunnel        string
}

type dnsCopyDecision struct {
	claim *dnsClaim
	kind  dnsClaimKind
	waits []<-chan struct{}
}

func (vc *verdictCtx) accept() int {
	vc.verdict = engine.VerdictAccept
	if vc.q != nil {
		if err := vc.q.SetVerdict(vc.id, nfqueue.NfAccept); err != nil {
			log.Tracef("failed to set verdict on packet %d: %v", vc.id, err)
		}
	}
	return 0
}

func (vc *verdictCtx) drop() bool {
	vc.verdict = engine.VerdictDrop
	if vc.copy != nil {
		if vc.copy.kind != claimDrop {
			vc.copy.kind = claimAnswer
		}
		return true
	}
	if vc.q != nil {
		if err := vc.q.SetVerdict(vc.id, nfqueue.NfDrop); err != nil {
			log.Tracef("failed to set drop verdict on packet %d: %v", vc.id, err)
			return false
		}
	}
	return true
}

func (vc *verdictCtx) dropFlow() {
	vc.flowDrop = true
}

func (vc *verdictCtx) dropsFlow() bool {
	return vc.flowDrop && vc.verdict == engine.VerdictDrop
}

func (vc *verdictCtx) dropUnanswered() bool {
	if vc.copy == nil {
		return vc.drop()
	}
	vc.verdict = engine.VerdictDrop
	vc.copy.kind = claimDrop
	return true
}

func (vc *verdictCtx) replace(raw []byte) bool {
	m, ok := vc.q.(verdictModifier)
	if !ok {
		return false
	}
	if err := m.SetVerdictModPacket(vc.id, nfqueue.NfAccept, raw); err != nil {
		log.Tracef("failed to replace packet %d: %v", vc.id, err)
		return false
	}
	vc.verdict = engine.VerdictAccept
	return true
}

func (w *Worker) InitSender() error {
	if w.sock != nil {
		return nil
	}
	cfg := w.getConfig()
	reinjectMark := int(cfg.Queue.Mark)
	tun := cfg.Queue.Mode == "tun"
	if tun {
		reinjectMark |= engine.ReinjectMarkBit
	}
	s, err := sock.NewSenderWithMark(reinjectMark)
	if err != nil {
		return err
	}
	w.sock = s
	if tun {
		cs, err := sock.NewSenderWithMark(engine.ClientMark)
		if err != nil {
			w.sock.Close()
			w.sock = nil
			return err
		}
		w.clientSock = cs
	} else {
		cs, err := sock.NewSenderWithMark(reinjectMark | engine.ClientMark)
		if err != nil {
			w.sock.Close()
			w.sock = nil
			return err
		}
		w.clientSock = cs
	}
	return nil
}

func (w *Worker) clientSender() *sock.Sender {
	if w.clientSock != nil {
		return w.clientSock
	}
	return w.sock
}

func (w *Worker) lookupSender() *sock.Sender {
	if w.lookupSock != nil {
		return w.lookupSock
	}
	return w.sock
}

func (w *Worker) ProcessPacket(raw []byte) engine.PacketVerdict {
	return w.ProcessPacketDir(raw, engine.DirUnknown)
}

func (w *Worker) ProcessPacketDir(raw []byte, dir engine.Direction) engine.PacketVerdict {
	if len(raw) == 0 {
		return engine.VerdictAccept
	}
	vc := &verdictCtx{verdict: engine.VerdictAccept, dir: dir}
	w.dispatch(vc, raw)
	if vc.dropsFlow() {
		return engine.VerdictDropFlow
	}
	return vc.verdict
}
