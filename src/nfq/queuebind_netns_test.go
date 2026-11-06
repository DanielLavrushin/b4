package nfq

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/florianl/go-nfqueue"
	"github.com/mdlayher/netlink"
)

const (
	netnsBindUDPPort    = 9
	netnsBindCfgCmd     = 1
	netnsBindCfgMask    = 4
	netnsBindCfgFlags   = 5
	netnsBindCmdBind    = 1
	netnsBindSubsysMsg  = 0x03<<8 | 2
	netnsBindLoadPerSec = 30000
)

func netnsBindRule(t *testing.T, args ...string) {
	t.Helper()
	netnsHoldRun(t, append([]string{"iptables", "-w", "-t", "mangle", "-A", "OUTPUT", "-o", netnsHoldLink}, args...)...)
	t.Cleanup(func() {
		_ = exec.Command("iptables", append([]string{"-w", "-t", "mangle", "-D", "OUTPUT", "-o", netnsHoldLink}, args...)...).Run()
	})
}

func netnsBindFlood(t *testing.T, perSecond int) {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP(netnsHoldRouter)})
	if err != nil {
		t.Fatalf("flood socket: %v", err)
	}
	to := &net.UDPAddr{IP: net.ParseIP(netnsHoldServer), Port: netnsBindUDPPort}
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		const tick = 2 * time.Millisecond
		burst := max(perSecond*int(tick/time.Microsecond)/1_000_000, 1)
		payload := make([]byte, 64)
		for {
			select {
			case <-done:
				return
			case <-time.After(tick):
			}
			for range burst {
				_, _ = conn.WriteToUDP(payload, to)
			}
		}
	}()
	t.Cleanup(func() {
		close(done)
		wg.Wait()
		conn.Close()
	})
}

func netnsBindRaw(c *netlink.Conn, qnum uint16, attrs []netlink.Attribute) error {
	body, err := netlink.MarshalAttributes(attrs)
	if err != nil {
		return err
	}
	data := binary.BigEndian.AppendUint16([]byte{syscall.AF_UNSPEC, 0}, qnum)
	_, err = c.Execute(netlink.Message{
		Header: netlink.Header{
			Type:  netlink.HeaderType(netnsBindSubsysMsg),
			Flags: netlink.Request | netlink.Acknowledge,
		},
		Data: append(data, body...),
	})
	return err
}

func netnsBindUnread(t *testing.T, qnum uint16) *netlink.Conn {
	t.Helper()
	c, err := netlink.Dial(syscall.NETLINK_NETFILTER, nil)
	if err != nil {
		t.Fatalf("netlink: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	if err := netnsBindRaw(c, qnum, []netlink.Attribute{{Type: netnsBindCfgCmd, Data: []byte{netnsBindCmdBind, 0, 0, syscall.AF_INET}}}); err != nil {
		t.Fatalf("queue bind: %v", err)
	}
	return c
}

func TestNetnsABindUnderLoadKeepsTheSocketOwner(t *testing.T) {
	netnsHoldRequire(t)
	srv := netnsHoldStartPeer(t)
	netnsHoldRun(t, "ip", "link", "set", "lo", "up")
	netnsHoldRun(t, "nsenter", "-t", strconv.Itoa(srv), "-n", "iptables", "-w", "-A", "INPUT", "-p", "udp", "--dport", strconv.Itoa(netnsBindUDPPort), "-j", "DROP")
	netnsBindRule(t, "-p", "udp", "--dport", strconv.Itoa(netnsBindUDPPort), "-j", "NFQUEUE", "--queue-num", strconv.Itoa(netnsHoldQueue), "--queue-bypass")

	cfg := config.NewConfig()
	cfg.Queue.StartNum, cfg.Queue.Threads = netnsHoldQueue, 1
	w := newTestWorker(t, &cfg)
	w.qnum = netnsHoldQueue

	prevOpen, prevRegister := queueOpen, queueRegister
	t.Cleanup(func() { queueOpen, queueRegister = prevOpen, prevRegister })
	withoutOwner, raced := 0, 0
	queueOpen = func(w *Worker, extra uint32) (*nfqueue.Nfqueue, error) {
		if extra&nfqueue.NfQaCfgFlagUIDGid == 0 {
			withoutOwner++
		}
		return prevOpen(w, extra)
	}
	queueRegister = func(w *Worker, q *nfqueue.Nfqueue, mark uint) error {
		err := prevRegister(w, q, mark)
		if err != nil {
			raced++
		}
		return err
	}

	netnsBindFlood(t, netnsBindLoadPerSec)
	time.Sleep(200 * time.Millisecond)

	const binds = 300
	unbound := 0
	var lastErr error
	for range binds {
		w.ctx, w.cancel = context.WithCancel(context.Background())
		q, err := w.bindQueue(uint(cfg.Queue.Mark))
		if err != nil {
			unbound++
			lastErr = err
		}
		time.Sleep(2 * time.Millisecond)
		w.cancel()
		if q != nil {
			_ = q.Close()
		}
	}
	if raced == 0 {
		t.Fatalf("no queued packet ever beat an acknowledgement in %d binds, so this test proves nothing", binds)
	}
	if unbound != 0 {
		t.Fatalf("%d of %d binds failed under load, last: %v", unbound, binds, lastErr)
	}
	if withoutOwner != 0 {
		t.Fatalf("%d of %d binds gave up the socket owner over a queued packet, and that worker cannot tell the router's own packets apart", withoutOwner, binds)
	}
	t.Logf("%d of %d attempts raced a queued packet", raced, binds+raced)
}

func TestNetnsABoundQueueReportsTheSocketOwner(t *testing.T) {
	netnsHoldRequire(t)
	netnsHoldStartPeer(t)
	netnsHoldRun(t, "ip", "link", "set", "lo", "up")
	netnsBindRule(t, "-p", "udp", "--dport", strconv.Itoa(netnsBindUDPPort), "-j", "NFQUEUE", "--queue-num", strconv.Itoa(netnsHoldQueue), "--queue-bypass")

	cfg := config.NewConfig()
	cfg.Queue.StartNum, cfg.Queue.Threads = netnsHoldQueue, 1
	w := newTestWorker(t, &cfg)
	w.qnum = netnsHoldQueue
	w.ctx, w.cancel = context.WithCancel(context.Background())

	prevRegister := queueRegister
	t.Cleanup(func() { queueRegister = prevRegister })
	owners := make(chan *uint32, 1)
	queueRegister = func(w *Worker, q *nfqueue.Nfqueue, _ uint) error {
		return q.RegisterWithErrorFunc(w.ctx, func(a nfqueue.Attribute) int {
			select {
			case owners <- a.UID:
			default:
			}
			_ = q.SetVerdict(*a.PacketID, nfqueue.NfAccept)
			return 0
		}, func(error) int { return 0 })
	}

	q, err := w.bindQueue(uint(cfg.Queue.Mark))
	if err != nil {
		t.Fatalf("bindQueue: %v", err)
	}
	t.Cleanup(func() {
		w.cancel()
		_ = q.Close()
	})

	netnsBindFlood(t, 500)
	select {
	case uid := <-owners:
		if uid == nil {
			t.Fatal("a packet the router sent itself reached the queue with no socket owner, so the kernel was not asked for it")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no packet reached the queue")
	}
}

func TestNetnsAQueueHeldByAnotherSocketFailsStart(t *testing.T) {
	netnsHoldRequire(t)
	srv := netnsHoldStartPeer(t)
	netnsHoldRun(t, "ip", "link", "set", "lo", "up")
	received := netnsHoldServerIn(t, srv, 16)
	netnsBindRule(t, "-p", "tcp", "--dport", strconv.Itoa(netnsHoldPort), "-j", "NFQUEUE", "--queue-num", strconv.Itoa(netnsHoldQueue), "--queue-bypass")

	squatter := netnsBindUnread(t, netnsHoldQueue)
	start := func() (*Worker, error) {
		cfg := config.NewConfig()
		cfg.Queue.StartNum, cfg.Queue.Threads = netnsHoldQueue, 1
		w := newTestWorker(t, &cfg)
		w.qnum = netnsHoldQueue
		return w, w.Start()
	}
	w, err := start()
	if !errors.Is(err, syscall.EPERM) {
		t.Fatalf("Start returned %v over a queue another socket holds, want the kernel's EPERM", err)
	}
	w.Stop()

	squatter.Close()
	w, err = start()
	if err != nil {
		t.Fatalf("the failed Start left something behind, the next one returned: %v", err)
	}
	t.Cleanup(w.Stop)
	conn, err := netnsHoldDial()
	if err != nil {
		t.Fatalf("dial through the queue: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write(make([]byte, 16)); err != nil {
		t.Fatalf("write through the queue: %v", err)
	}
	select {
	case got := <-received:
		if got == nil {
			t.Fatal("the server read nothing through the queue")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the server read nothing through the queue")
	}
}

func TestNetnsTheKernelsRefusalOfAFlagIsEOPNOTSUPP(t *testing.T) {
	netnsHoldRequire(t)
	c := netnsBindUnread(t, netnsHoldQueue)
	unknown := binary.BigEndian.AppendUint32(nil, uint32(nfqueue.NfQaCfgFlagSecCx)<<1)
	err := netnsBindRaw(c, netnsHoldQueue, []netlink.Attribute{
		{Type: netnsBindCfgFlags, Data: unknown},
		{Type: netnsBindCfgMask, Data: unknown},
	})
	if !errors.Is(err, syscall.EOPNOTSUPP) {
		t.Fatalf("the kernel answered a flag it does not know with %v, and bindQueue falls back only on EOPNOTSUPP", err)
	}
}
