package nfq

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type serialAnswerSender struct {
	inFlight atomic.Int32
	overlap  atomic.Bool
	entered  chan struct{}
	release  chan struct{}
}

func (s *serialAnswerSender) send() error {
	if s.inFlight.Add(1) > 1 {
		s.overlap.Store(true)
	}
	s.entered <- struct{}{}
	<-s.release
	s.inFlight.Add(-1)
	return nil
}

func (s *serialAnswerSender) SendIPv4([]byte, net.IP) error { return s.send() }

func (s *serialAnswerSender) SendIPv6([]byte, net.IP) error { return s.send() }

func TestDNSAnswersLeaveTheRawSocketOneAtATime(t *testing.T) {
	type answer struct {
		ver          byte
		server, peer net.IP
	}
	v4 := answer{IPv4, net.ParseIP("192.168.1.1"), net.ParseIP("192.168.1.20")}
	v6 := answer{IPv6, net.ParseIP("fd00::1"), net.ParseIP("fd00::20")}
	for _, tc := range []struct {
		name string
		pair [2]answer
	}{
		{"A and AAAA answers to one IPv4 socket", [2]answer{v4, v4}},
		{"answers of both families", [2]answer{v4, v6}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &serialAnswerSender{entered: make(chan struct{}, 2), release: make(chan struct{})}
			prev := dnsAnswerSenderFor
			dnsAnswerSenderFor = func(*Worker) dnsAnswerSender { return fake }
			t.Cleanup(func() { dnsAnswerSenderFor = prev })

			resp := make([]byte, 40)
			var wg sync.WaitGroup
			for i, a := range tc.pair {
				w := &Worker{}
				wg.Add(1)
				go func(w *Worker, a answer, port uint16) {
					defer wg.Done()
					w.sendDNSResponseToClient(a.ver, a.server, a.peer, port, resp)
				}(w, a, uint16(40000+i))
			}

			<-fake.entered
			second := false
			select {
			case <-fake.entered:
				second = true
			case <-time.After(50 * time.Millisecond):
			}
			close(fake.release)
			if !second {
				<-fake.entered
			}
			wg.Wait()
			if second || fake.overlap.Load() {
				t.Errorf("two DNS answers went through netfilter at the same moment; on kernels before 4.18 the second then clashes with the first in conntrack and is dropped")
			}
		})
	}
}
