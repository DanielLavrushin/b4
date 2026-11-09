package nfq

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"golang.org/x/net/dns/dnsmessage"
)

const (
	pinAliasName   = "www.instagram.com"
	pinAliasTarget = "z-p42-instagram.c10r.instagram.com"
)

func pinAliasSet(id string, pinned bool) *config.SetConfig {
	set := echTestSet(id, false, "instagram.com")
	if pinned {
		set.DNS.Pins = map[string][]string{pinAliasName: {"57.144.248.34", "157.240.0.174"}}
	}
	return set
}

func pinAliasMXReply(t *testing.T, txid uint16) []byte {
	t.Helper()
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: txid, Response: true, RecursionDesired: true, RecursionAvailable: true})
	b.EnableCompression()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(b.StartQuestions())
	must(b.Question(dnsmessage.Question{Name: dnsmessage.MustNewName(pinAliasName + "."), Type: dnsmessage.TypeMX, Class: dnsmessage.ClassINET}))
	must(b.StartAnswers())
	must(b.CNAMEResource(
		dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName(pinAliasName + "."), Type: dnsmessage.TypeCNAME, Class: dnsmessage.ClassINET, TTL: 2442},
		dnsmessage.CNAMEResource{CNAME: dnsmessage.MustNewName(pinAliasTarget + ".")},
	))
	must(b.StartAuthorities())
	must(b.SOAResource(
		dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName("instagram.com."), Type: dnsmessage.TypeSOA, Class: dnsmessage.ClassINET, TTL: 1390},
		dnsmessage.SOAResource{NS: dnsmessage.MustNewName("a.ns.instagram.com."), MBox: dnsmessage.MustNewName("dns.facebook.com."), Serial: 1, Refresh: 14400, Retry: 1800, Expire: 604800, MinTTL: 3600},
	))
	msg, err := b.Finish()
	must(err)
	return msg
}

func carriesTheAlias(resp []byte) bool {
	var p dnsmessage.Parser
	if _, err := p.Start(resp); err != nil {
		return false
	}
	if err := p.SkipAllQuestions(); err != nil {
		return false
	}
	answers, err := p.AllAnswers()
	if err != nil {
		return false
	}
	for _, rr := range answers {
		if rr.Header.Type == dnsmessage.TypeCNAME {
			return true
		}
	}
	return false
}

func emptyPinAnswer(t *testing.T, resp []byte, why string) {
	t.Helper()
	if carriesTheAlias(resp) {
		t.Fatalf("%s: the answer still carries %s's alias, a caching resolver on the network follows it once the pinned answer expires", why, pinAliasName)
	}
	if len(resp) < 12 || resp[2]&0x80 == 0 || resp[3]&0x0F != 0 || binary.BigEndian.Uint16(resp[6:8]) != 0 {
		t.Fatalf("%s: want an empty NOERROR answer, got %x", why, resp[:min(len(resp), 12)])
	}
}

func TestAReplyCarryingAPinnedNamesAliasIsRewrittenInPlace(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{pinAliasSet("instagram", true)}
	w := passiveDNSWorker(t, &cfg)
	sent := captureSends(t)
	q := newFakeModQueue()
	raw := masqueradedReply(pinAliasMXReply(t, 0x5151))

	handleReply(t, w, &verdictCtx{id: 1, q: q}, raw)

	emptyPinAnswer(t, keepsItsPacket(t, q.now(t), raw), "the MX reply")
	sent.none(t, 30*time.Millisecond, "b4 sent a packet of its own although it rewrote the reply in place")
}

func TestACopiedRequestsReplyLosesAPinnedNamesAlias(t *testing.T) {
	injectAfter(t, time.Minute)
	stubRouteAwait(t)
	r := newCopyRig(t, nil, pinAliasSet("instagram", true))
	sent := captureSends(t)

	r.read(copyOf(nfHookForward, rigDevice, rigOutside, 40000, dns.BuildQuery(pinAliasName, 0x5151, uint16(dnsmessage.TypeMX))))
	if live := r.l.claims.live.Load(); live != 0 {
		t.Fatalf("%d claims for an MX query the pins do not answer, want none", live)
	}
	q := newFakeModQueue()
	raw := masqueradedReply(pinAliasMXReply(t, 0x5151))
	r.reply(q, raw)

	emptyPinAnswer(t, keepsItsPacket(t, q.now(t), raw), "the reply to a copied request")
	sent.none(t, 30*time.Millisecond, "b4 sent a packet of its own although it rewrote the reply in place")
}

func TestAReplyForAnUnpinnedNameKeepsItsAlias(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{pinAliasSet("instagram", false)}
	w := passiveDNSWorker(t, &cfg)
	q := newFakeModQueue()

	handleReply(t, w, &verdictCtx{id: 1, q: q}, masqueradedReply(pinAliasMXReply(t, 0x5151)))

	passedUnchanged(t, q.now(t), "a set without pins leaves the resolver's answer alone")
}

func TestAReplyLosesTheAliasTheEscalatedSetPins(t *testing.T) {
	primary := pinAliasSet("primary", false)
	primary.Escalate.To = "backup"
	backup := pinAliasSet("backup", true)
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{primary, backup}
	w := passiveDNSWorker(t, &cfg)
	if !w.destState.SetEscalation(pinAliasName, backup.Id, escalateReasonDNS, time.Minute) {
		t.Fatal("the escalation was not recorded")
	}
	q := newFakeModQueue()
	raw := masqueradedReply(pinAliasMXReply(t, 0x5151))

	handleReply(t, w, &verdictCtx{id: 1, q: q}, raw)

	emptyPinAnswer(t, keepsItsPacket(t, q.now(t), raw), "the escalated set answers the name from its pins")
}

func TestFilterDNSAnswerDropsAPinnedNamesAlias(t *testing.T) {
	set := pinAliasSet("instagram", true)
	w, cfg := echTestWorker(t, set)

	out, action, actor := w.filterDNSAnswer(cfg, set, pinAliasName, "", pinAliasMXReply(t, 7), false)
	if out == nil || action != dnsActionAliasStripped || actor != set {
		t.Fatalf("action %q by %v, want %q by the pinning set: the answers of the set's own resolver lose the alias too", action, actor, dnsActionAliasStripped)
	}
	emptyPinAnswer(t, out, "the set resolver's MX answer")

	if out, _, _ := w.filterDNSAnswer(cfg, set, pinAliasName, "", buildDNSResponse(7, pinAliasName, []net.IP{net.ParseIP("157.240.0.174")}), false); out != nil {
		t.Fatal("an answer for the pinned name that carries no alias was rewritten")
	}
}

func pinAliasTCPUpstream(t *testing.T, reply []byte) {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			if query, err := readDNSTCPMessage(conn); err == nil {
				out := append([]byte(nil), reply...)
				copy(out[:2], query[:2])
				_ = writeDNSTCPMessage(conn, out, time.Second)
				_ = conn.SetReadDeadline(time.Now().Add(time.Second))
				_, _ = io.Copy(io.Discard, conn)
			}
			conn.Close()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		<-done
	})

	prevDst, prevDial := dnsTCPOriginalDst, dialDNSTCPUpstream
	dnsTCPOriginalDst = func(net.Conn) (net.IP, int, error) { return addr.IP, addr.Port, nil }
	dialDNSTCPUpstream = func(ctx context.Context, _ *config.Config, a string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", a)
	}
	t.Cleanup(func() { dnsTCPOriginalDst, dialDNSTCPUpstream = prevDst, prevDial })
}

func mxOverTCP(t *testing.T, w *Worker) []byte {
	t.Helper()
	s := newDNSTCPServer(w, 0)
	t.Cleanup(s.cancel)
	clientEnd, serverEnd := net.Pipe()
	handled := make(chan struct{})
	go func() {
		defer close(handled)
		s.handle(remoteAddrConn{Conn: serverEnd, remote: &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 40000}})
	}()

	_ = clientEnd.SetDeadline(time.Now().Add(5 * time.Second))
	if err := writeDNSTCPMessage(clientEnd, dns.BuildQuery(pinAliasName, 0x6262, uint16(dnsmessage.TypeMX)), time.Second); err != nil {
		t.Fatal(err)
	}
	resp, err := readDNSTCPMessage(clientEnd)
	if err != nil {
		t.Fatalf("no answer over TCP: %v", err)
	}
	clientEnd.Close()
	select {
	case <-handled:
	case <-time.After(5 * time.Second):
		t.Fatal("the connection handler never returned")
	}
	return resp
}

func TestDNSOverTCPDropsAPinnedNamesAlias(t *testing.T) {
	w, _ := echTestWorker(t, pinAliasSet("instagram", true))
	pinAliasTCPUpstream(t, pinAliasMXReply(t, 0))

	emptyPinAnswer(t, mxOverTCP(t, w), "the MX answer relayed over TCP")
}

func TestDNSOverTCPRelaysAnUnpinnedAnswerUnchanged(t *testing.T) {
	w, _ := echTestWorker(t, pinAliasSet("instagram", false))
	pinAliasTCPUpstream(t, pinAliasMXReply(t, 0))

	if !carriesTheAlias(mxOverTCP(t, w)) {
		t.Fatal("an answer for a name no set pins lost its alias")
	}
}
