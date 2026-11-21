package nfq

import (
	"context"
	"net"
	"net/netip"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/ctnl"
	"github.com/daniellavrushin/b4/iphealth"
	"github.com/daniellavrushin/b4/tables"
	"golang.org/x/sys/unix"
)

type synackSnapshot struct {
	arrived      int
	ctPackets    uint64
	ctBytes      uint64
	queuePackets uint64
	queueBytes   uint64
	helloAcked   bool
	watched      bool
}

var synackCounter = regexp.MustCompile(`^\s*(\d+)\s+(\d+)\s+tcp\b.*b4hh0`)

func synackArrived(t *testing.T) int {
	t.Helper()
	out, err := exec.Command("iptables", "-w", "-t", "raw", "-L", "PREROUTING", "-v", "-x", "-n").CombinedOutput()
	if err != nil {
		t.Fatalf("read the SYN-ACK counter: %v (%s)", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if m := synackCounter.FindStringSubmatch(line); m != nil && strings.Contains(line, "spt:443") {
			n, _ := strconv.Atoi(m[1])
			return n
		}
	}
	t.Fatalf("the SYN-ACK counter is gone:\n%s", out)
	return 0
}

func TestNetnsARetransmittedSynAckAfterTheHello(t *testing.T) {
	netnsHoldRequire(t)
	srv := netnsHoldStartPeer(t)
	client := netnsWSStartLANClient(t)
	netnsConntrackSysctl(t, "nf_conntrack_acct", "1")

	var ln net.Listener
	netnsHoldInside(t, srv, func() error {
		var err error
		ln, err = net.Listen("tcp4", net.JoinHostPort(netnsHoldServer, strconv.Itoa(netnsHoldPort)))
		return err
	})
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	inServer := func(args ...string) error {
		return exec.Command("nsenter", append([]string{"-t", strconv.Itoa(srv), "-n"}, args...)...).Run()
	}
	drop := []string{"iptables", "-w", "-I", "INPUT", "-p", "tcp", "--dport", strconv.Itoa(netnsHoldPort), "--tcp-flags", "SYN", "NONE", "-j", "DROP"}
	if err := inServer(drop...); err != nil {
		t.Skipf("cannot drop the client's packets in the server namespace: %v", err)
	}
	undrop := append([]string{"iptables", "-w", "-D"}, drop[3:]...)
	t.Cleanup(func() { _ = inServer(undrop...) })
	count := []string{"PREROUTING", "-i", netnsHoldLink, "-p", "tcp", "--sport", strconv.Itoa(netnsHoldPort), "--tcp-flags", "SYN,ACK", "SYN,ACK"}
	netnsHoldRun(t, append([]string{"iptables", "-w", "-t", "raw", "-I"}, count...)...)
	t.Cleanup(func() { _ = exec.Command("iptables", append([]string{"-w", "-t", "raw", "-D"}, count...)...).Run() })

	producer := silenceSet()
	producer.Id, producer.Name = "collect", "collect"
	producer.TCP.IPBlockDetect.Action = "collect"
	producer.TCP.IPBlockDetect.TimeoutMs = 8000
	producer.Targets.IpsToMatch = []string{netnsHoldServer}
	consumer := readyConsumer(false)
	cfg := config.NewConfig()
	cfg.Queue.StartNum, cfg.Queue.Threads = netnsHoldQueue, 1
	cfg.Queue.IPv6Enabled = false
	cfg.System.Tables.Engine = "iptables"
	cfg.Sets = []*config.SetConfig{&producer, consumer}
	view := cfg.WithCollectedIPs(nil)
	if err := tables.AddRules(view); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	t.Cleanup(func() { _ = tables.ClearRules(view) })

	w := newTestWorker(t, view)
	w.qnum = netnsHoldQueue
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	p := &Pool{Workers: []*Worker{w}, state: &runtimeState{tlsCache: w.tlsCache, destState: w.destState}, collectedBase: view, collectedCtx: ctx, collectedCancel: cancel,
		collectedInstall: oneStepInstaller(func(context.Context, *config.Config) error { return nil })}
	w.pool = p
	if err := w.Start(); err != nil {
		t.Fatalf("bind queue %d: %v", netnsHoldQueue, err)
	}
	t.Cleanup(w.Stop)

	hello := buildClientHello(netnsStallHost, 16, 0xAB)
	var conn net.Conn
	netnsHoldInside(t, client, func() error {
		var err error
		conn, err = net.DialTimeout("tcp4", net.JoinHostPort(netnsHoldServer, strconv.Itoa(netnsHoldPort)), 3*time.Second)
		if err == nil {
			_, err = conn.Write(hello)
		}
		return err
	})
	t.Cleanup(func() { _ = conn.Close() })
	start := time.Now()
	local := conn.LocalAddr().(*net.TCPAddr).AddrPort()
	tuple, err := ctnl.NewTuple(unix.IPPROTO_TCP, netip.AddrPortFrom(local.Addr().Unmap(), local.Port()), netip.MustParseAddrPort(net.JoinHostPort(netnsHoldServer, strconv.Itoa(netnsHoldPort))))
	if err != nil {
		t.Fatal(err)
	}
	snap := func() synackSnapshot {
		s := synackSnapshot{arrived: synackArrived(t)}
		if f, found, err := ctnl.Lookup(tuple, ctnl.Original); err == nil && found {
			s.ctPackets, s.ctBytes = f.ReplyPackets, f.ReplyBytes
		}
		w.destState.watchMu.RLock()
		for _, h := range w.destState.watches {
			if h.collect && h.cport == local.Port() {
				s.queuePackets, s.queueBytes, s.helloAcked, s.watched = h.observedReplyPackets, h.observedReplyBytes, h.clientHelloAcked, true
			}
		}
		w.destState.watchMu.RUnlock()
		return s
	}
	for deadline := time.Now().Add(3 * time.Second); !snap().watched; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the ClientHello was never watched, so this test measures nothing")
		}
	}
	before := snap()
	time.Sleep(time.Until(start.Add(1500 * time.Millisecond)))
	_ = inServer(undrop...)
	time.Sleep(time.Until(start.Add(6 * time.Second)))
	after := snap()
	p.collectedPass(time.Now().Add(time.Minute))
	w.destState.watchMu.RLock()
	run := w.destState.collectRuns[iphealth.BanKey(net.ParseIP(netnsHoldServer), netnsHoldPort)]
	w.destState.watchMu.RUnlock()
	t.Logf("SYN-ACKs that reached the router: %d at the watch, %d at the end", before.arrived, after.arrived)
	t.Logf("conntrack reply counters: %d packets %d bytes at the watch, %d packets %d bytes at the end", before.ctPackets, before.ctBytes, after.ctPackets, after.ctBytes)
	t.Logf("queue-observed reply counters: %d packets %d bytes at the watch, %d packets %d bytes at the end; ClientHello acknowledged=%v", before.queuePackets, before.queueBytes, after.queuePackets, after.queueBytes, after.helloAcked)
	t.Logf("Collect's conclusion at the deadline: counted as silent=%v (count %d)", run.count == 1, run.count)
	if after.arrived <= before.arrived || !after.helloAcked {
		t.Skipf("the server retransmitted no SYN-ACK after the watch began (%d->%d) or never acknowledged the ClientHello (%v), so this run measures nothing", before.arrived, after.arrived, after.helloAcked)
	}
	if after.queuePackets != after.ctPackets || after.queueBytes != after.ctBytes || run.count != 1 {
		t.Errorf("the server acknowledged the ClientHello and sent nothing else, yet the queue's reply counters (%d packets, %d bytes) differ from conntrack's (%d, %d) and the connection counted=%v; a retransmitted SYN-ACK that conntrack leaves out must stay out of the queue's counters too", after.queuePackets, after.queueBytes, after.ctPackets, after.ctBytes, run.count == 1)
	}
}
