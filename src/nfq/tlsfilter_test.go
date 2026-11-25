package nfq

import (
	"encoding/binary"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/sni"
)

func newTLSFilteredSet(tls string, mutate func(set *config.SetConfig)) config.SetConfig {
	set := newFragmentingYTImagesSet()
	set.Id, set.Name = "filtered", "Filtered"
	set.Targets.DomainOnly = false
	set.Targets.DomainsToMatch = nil
	set.Targets.TLSVersion = tls
	set.TCP.RSTProtection.Enabled = false
	if mutate != nil {
		mutate(&set)
	}
	return set
}

func targetsByAddress(set *config.SetConfig) {
	set.Targets.IpsToMatch = []string{"1.2.3.0/24"}
}

func targetsByPort(set *config.SetConfig) {
	set.TCP.DPortFilter = "443"
	set.TCPPortRanges = config.ParsePortRanges("443")
}

func targetsByName(set *config.SetConfig) {
	set.Targets.DomainsToMatch = []string{"ytimg.com"}
}

func taughtAddress(w *Worker, set *config.SetConfig) {
	w.getMatcher().LearnIPToDomain(net.ParseIP("1.2.3.4"), "i.ytimg.com", set)
}

func taughtHint(w *Worker, set *config.SetConfig) {
	w.storeHostHint("10.0.0.1", "1.2.3.4", set, "i.ytimg.com", "dns", 0, 0)
}

func helloNamedBeforeItsVersions(host string) []byte {
	return helloWithExtensions(buildSNIExtension(host), buildExtension(51, make([]byte, 1400)), buildExtension(43, []byte{0x02, 0x03, 0x04}))
}

type filterStep struct {
	what string
	raw  []byte
}

func expectFilteredOut(t *testing.T, w *Worker, ch chan string, steps ...filterStep) {
	t.Helper()
	for _, step := range steps {
		if v := w.ProcessPacket(step.raw); v != engine.VerdictAccept {
			t.Errorf("%s: the match the filter dropped came back, got %v", step.what, v)
		}
		if row := lastConnectionRow(t, ch); row.sniSet != "" || row.ipSet != "" || row.meta != "" {
			t.Errorf("%s: want a row of no set and no reason, got %+v", step.what, row)
		}
	}
}

func TestATCPMatchTheTLSFilterDropsStaysDropped(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(set *config.SetConfig)
		teach  func(w *Worker, set *config.SetConfig)
	}{
		{"a listed address", targetsByAddress, nil},
		{"a port filter", targetsByPort, nil},
		{"a learned address", targetsByName, taughtAddress},
		{"a DNS answer", targetsByName, taughtHint},
		{"a listed address, SACK stripped", func(set *config.SetConfig) {
			targetsByAddress(set)
			set.TCP.DropSACK = true
		}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := newTLSFilteredSet("1.2", tc.mutate)
			w := newInjectingWorker(t, &set)
			if tc.teach != nil {
				tc.teach(w, &set)
			}
			ch := subscribeRows(t)

			hello := buildClientHello("www.example.org", 16, 0xAB)
			next := 1000 + uint32(len(hello))
			w.ProcessPacket(makeV4TCPPacket(hello, 1000))
			if row := lastConnectionRow(t, ch); row.sniSet != "" || row.ipSet != "" {
				t.Fatalf("a TLS 1.3 ClientHello does not fit the filter, got %+v", row)
			}
			expectFilteredOut(t, w, ch,
				filterStep{"later data", makeV4TCPPacket(tlsAppData(64), next)},
				filterStep{"more data", makeV4TCPPacket(tlsAppData(64), next+69)},
				filterStep{"a bare ACK", makeV4TCPPacketFlags(nil, next+138, 0x10)},
			)
			w.ProcessPacket(makeV4TCPPacket(hello, 1000))
			if row := lastConnectionRow(t, ch); row.sniSet != "" || row.ipSet != "" {
				t.Errorf("the ClientHello resent does not fit the filter either, got %+v", row)
			}
			expectFilteredOut(t, w, ch, filterStep{"data after the resend", makeV4TCPPacket(tlsAppData(64), next+138)})
		})
	}
}

func TestATLS12ConnectionATLS13SetDropsStaysDropped(t *testing.T) {
	set := newTLSFilteredSet("1.3", func(set *config.SetConfig) {
		targetsByAddress(set)
		set.TCP.Incoming.Mode = "fake"
	})
	w := newInjectingWorker(t, &set)
	ch := subscribeRows(t)
	w.ProcessPacket(makeV4TCPPacketFlags(nil, 999, 0x02))
	hello := setOrderTLS12Hello("www.example.org")
	next := 1000 + uint32(len(hello))
	w.ProcessPacket(makeV4TCPPacket(hello, 1000))
	connectionRows(ch)
	expectFilteredOut(t, w, ch,
		filterStep{"later data", makeV4TCPPacket(tlsAppData(64), next)},
		filterStep{"a bare ACK", makeV4TCPPacketFlags(nil, next+69, 0x10)},
	)
	if bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443); bound != nil {
		t.Errorf("the replies are still handled by %s", bound.Name)
	}
}

func TestAMatchTheTLSFilterDropsOnASplitClientHelloStaysDropped(t *testing.T) {
	set := newTLSFilteredSet("1.2", func(set *config.SetConfig) {
		targetsByAddress(set)
		set.Fragmentation.Strategy = config.ConfigNone
	})
	other := newFragmentingYTImagesSet()
	w := newInjectingWorker(t, &set, &other)
	ch := subscribeRows(t)
	full, seg1, seg2 := splitHello("www.example.org")

	w.ProcessPacket(makeV4TCPPacket(seg1, splitSeq))
	w.ProcessPacket(makeV4TCPPacket(seg2, splitSeq+splitCut))
	if row := lastConnectionRow(t, ch); row.ipSet != "" || row.domain != "www.example.org" {
		t.Fatalf("the part with the name completes a TLS 1.3 ClientHello, got %+v", row)
	}
	next := splitSeq + uint32(len(full))
	expectFilteredOut(t, w, ch, filterStep{"later data", makeV4TCPPacket(tlsAppData(64), next)})
	w.ProcessPacket(makeV4TCPPacket(seg1, splitSeq))
	connectionRows(ch)
	expectFilteredOut(t, w, ch,
		filterStep{"the second part resent", makeV4TCPPacket(seg2, splitSeq+splitCut)},
		filterStep{"data after the resend", makeV4TCPPacket(tlsAppData(64), next)},
	)
}

func TestAClientHelloCutBeforeItsVersionsDropsTheMatchForThatPartOnly(t *testing.T) {
	versions := buildExtension(43, []byte{0x02, 0x03, 0x04})
	for _, tc := range []struct {
		name  string
		hello []byte
		cuts  []int
	}{
		{"the name in the first part", helloNamedBeforeItsVersions("www.example.org"), []int{splitCut}},
		{"the name in the second of three parts", helloWithExtensions(
			buildExtension(51, make([]byte, splitCut)), buildSNIExtension("www.example.org"), buildExtension(21, make([]byte, splitCut)), versions,
		), []int{splitCut, 2 * splitCut}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := newTLSFilteredSet("1.3", func(set *config.SetConfig) {
				targetsByAddress(set)
				set.TCP.RSTProtection.Enabled = true
			})
			w := newInjectingWorker(t, &set)
			ch := subscribeRows(t)
			w.ProcessPacket(makeV4TCPPacketFlags(nil, 999, 0x02))
			from := 0
			for _, cut := range tc.cuts {
				w.ProcessPacket(makeV4TCPPacket(tc.hello[from:cut], 1000+uint32(from)))
				w.wg.Wait()
				from = cut
			}
			connectionRows(ch)
			w.ProcessPacket(makeV4TCPPacket(tc.hello[from:], 1000+uint32(from)))
			if row := lastConnectionRow(t, ch); row.ipSet != set.Name {
				t.Errorf("the last part of a TLS 1.3 ClientHello fits the filter, got %+v", row)
			}
			w.ProcessPacket(makeV4TCPPacket(tlsAppData(64), 1000+uint32(len(tc.hello))))
			if row := lastConnectionRow(t, ch); row.ipSet != set.Name {
				t.Errorf("later data of a TLS 1.3 connection fits the filter, got %+v", row)
			}
			if bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443); bound == nil || bound.Id != set.Id {
				t.Errorf("the replies of a TLS 1.3 connection stay with the set, got %v", bound)
			}
		})
	}
}

func TestAMatchTheTLSFilterDropsLeavesTheIncomingSideAlone(t *testing.T) {
	for _, version := range []string{"1.2", "1.3"} {
		t.Run("TLS "+version, func(t *testing.T) {
			set := newTLSFilteredSet(version, func(set *config.SetConfig) {
				targetsByAddress(set)
				set.TCP.Incoming.Mode = "fake"
			})
			w := newInjectingWorker(t, &set)
			w.ProcessPacket(makeV4TCPPacketFlags(nil, 999, 0x02))
			if bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443); bound == nil {
				t.Fatal("the SYN binds the address set to the replies")
			}
			hello := buildClientHello("www.example.org", 16, 0xAB)
			w.ProcessPacket(makeV4TCPPacket(hello, 1000))
			w.ProcessPacket(makeV4TCPPacket(tlsAppData(64), 1000+uint32(len(hello))))
			bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443)
			if fits := version == "1.3"; (bound != nil) != fits {
				t.Errorf("replies bound to a set: %v, want %v", bound != nil, fits)
			}
		})
	}
}

func TestAFilterDropLeavesTheReplySideOfAnotherSet(t *testing.T) {
	watching := newTLSFilteredSet("", targetsByAddress)
	watching.Id, watching.Name = "watching", "Watching"
	watching.TCP.Incoming.Mode = "fake"
	filtered := newTLSFilteredSet("1.2", targetsByName)
	w := newInjectingWorker(t, &watching, &filtered)
	w.ProcessPacket(makeV4TCPPacketFlags(nil, 999, 0x02))
	taughtAddress(w, &filtered)
	w.ProcessPacket(makeV4TCPPacket(buildClientHello("www.example.org", 16, 0xAB), 1000))
	if bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443); bound == nil || bound.Id != watching.Id {
		t.Errorf("the filter of one set ended the reply handling of another, got %v", bound)
	}
}

func TestReleaseForgetsOnlyItsOwnSetAndKeepsTheConnection(t *testing.T) {
	tracker := newRuntimeState().connState
	kept := newBystanderSet(nil)
	const key = "10.0.0.1:12345->1.2.3.4:443"
	tracker.RegisterOutgoing(key, &kept, false)
	tracker.Release(key, "another")
	if tracker.conns[key].set == nil {
		t.Fatal("the reply side of a set another one was released for is gone")
	}
	tracker.Release(key, kept.Id)
	tracker.Release(key, kept.Id)
	tracker.Release("10.0.0.1:1->1.2.3.4:443", kept.Id)
	if info := tracker.conns[key]; info == nil || info.set != nil {
		t.Fatalf("want the connection known and bound to no set, got %+v", info)
	}

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for range 200 {
				tracker.RegisterOutgoing(key, &kept, false)
			}
		}()
		go func() {
			defer wg.Done()
			for range 200 {
				tracker.Release(key, kept.Id)
			}
		}()
	}
	wg.Wait()
}

func TestATCPMatchTheTLSFilterFitsKeepsItsLaterPackets(t *testing.T) {
	for name, mutate := range map[string]func(set *config.SetConfig){"a listed address": targetsByAddress, "a port filter": targetsByPort} {
		t.Run(name, func(t *testing.T) {
			set := newTLSFilteredSet("1.3", mutate)
			w := newInjectingWorker(t, &set)
			ch := subscribeRows(t)
			hello := buildClientHello("www.example.org", 16, 0xAB)
			if v := w.ProcessPacket(makeV4TCPPacket(hello, 1000)); v != engine.VerdictDrop {
				t.Fatalf("a TLS 1.3 ClientHello fits the filter, got %v", v)
			}
			w.wg.Wait()
			connectionRows(ch)
			if v := w.ProcessPacket(makeV4TCPPacket(tlsAppData(64), 1000+uint32(len(hello)))); v != engine.VerdictDrop {
				t.Errorf("later data of a connection that fits belongs to the set, got %v", v)
			}
			if row := lastConnectionRow(t, ch); row.ipSet != set.Name {
				t.Errorf("later data of a connection that fits shows the set, got %+v", row)
			}
		})
	}
}

func TestASetThatTookTheNameKeepsLaterPacketsWhateverItsTLSFilter(t *testing.T) {
	for name, mutate := range map[string]func(set *config.SetConfig){
		"learning":                targetsByName,
		"learning and by address": func(set *config.SetConfig) { targetsByName(set); targetsByAddress(set) },
		"domain only":             func(set *config.SetConfig) { targetsByName(set); set.Targets.DomainOnly = true },
		"domain only and by address": func(set *config.SetConfig) {
			targetsByName(set)
			targetsByAddress(set)
			set.Targets.DomainOnly = true
		},
		"domain only and by address, SACK stripped": func(set *config.SetConfig) {
			targetsByName(set)
			targetsByAddress(set)
			set.Targets.DomainOnly = true
			set.Fragmentation.Strategy = config.ConfigNone
			set.TCP.DropSACK = true
		},
	} {
		t.Run(name, func(t *testing.T) {
			set := newTLSFilteredSet("1.2", mutate)
			w := newInjectingWorker(t, &set)
			ch := subscribeRows(t)
			hello := buildClientHello("i.ytimg.com", 16, 0xAB)
			if v := w.ProcessPacket(makeV4TCPPacket(hello, 1000)); v != engine.VerdictDrop {
				t.Fatalf("the filter is a preference for a name, the only set of the name takes it, got %v", v)
			}
			w.wg.Wait()
			connectionRows(ch)
			if v := w.ProcessPacket(makeV4TCPPacket(tlsAppData(64), 1000+uint32(len(hello)))); v != engine.VerdictDrop {
				t.Errorf("later data must stay with the set of the name, got %v", v)
			}
			if row := lastConnectionRow(t, ch); row.sniSet != set.Name && row.ipSet != set.Name {
				t.Errorf("later data must show the set of the name, got %+v", row)
			}
		})
	}
}

func TestAnAddressSetThatDoesNotFitLeavesTheWholeConnectionToADomainOnlyName(t *testing.T) {
	named := newFragmentingYTImagesSet()
	named.TCP.RSTProtection.Enabled = false
	address := newTLSFilteredSet("1.2", func(set *config.SetConfig) {
		targetsByAddress(set)
		set.TCP.Incoming.Mode = "fake"
	})
	for name, sets := range map[string][]*config.SetConfig{"address set above": {&address, &named}, "address set below": {&named, &address}} {
		t.Run(name, func(t *testing.T) {
			w := newInjectingWorker(t, sets...)
			ch := subscribeRows(t)
			w.ProcessPacket(makeV4TCPPacketFlags(nil, 999, 0x02))
			hello := buildClientHello("i.ytimg.com", 16, 0xAB)
			w.ProcessPacket(makeV4TCPPacket(hello, 1000))
			if row := lastConnectionRow(t, ch); row.sniSet != named.Name || row.ipSet != "" {
				t.Fatalf("the set of the name takes the ClientHello, got %+v", row)
			}
			w.wg.Wait()
			w.ProcessPacket(makeV4TCPPacket(tlsAppData(64), 1000+uint32(len(hello))))
			if row := lastConnectionRow(t, ch); row.sniSet != named.Name || row.ipSet != "" {
				t.Errorf("later data belongs to the set of the name, got %+v", row)
			}
			if bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443); bound != nil {
				t.Errorf("the replies are still handled by %s", bound.Name)
			}
		})
	}
}

func TestABackupThatTookTheClientHelloKeepsLaterPackets(t *testing.T) {
	backupNamed := func(tls string) config.SetConfig {
		backup := newTLSFilteredSet(tls, func(set *config.SetConfig) { set.Targets.DomainsToMatch = []string{"backup.example"} })
		backup.Id, backup.Name = "backup", "Backup"
		return backup
	}
	for _, tc := range []struct {
		name, host, setTLS, backupTLS string
		domainOnly                    bool
	}{
		{"of the name in the ClientHello", "i.ytimg.com", "", "1.2", false},
		{"of the name in the ClientHello, domain only", "i.ytimg.com", "", "1.2", true},
		{"of the domain its address was learned for", "www.example.org", "1.2", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			primary := newTLSFilteredSet(tc.setTLS, targetsByName)
			primary.Targets.DomainOnly = tc.domainOnly
			primary.Escalate.To = "backup"
			backup := backupNamed(tc.backupTLS)
			w := newInjectingWorker(t, &primary, &backup)
			if !tc.domainOnly {
				taughtAddress(w, &primary)
			}
			w.destState.SetEscalation("i.ytimg.com", backup.Id, escalateReasonStall, time.Minute)
			ch := subscribeRows(t)

			hello := buildClientHello(tc.host, 16, 0xAB)
			w.ProcessPacket(makeV4TCPPacket(hello, 1000))
			if row := lastConnectionRow(t, ch); row.sniSet != backup.Name && row.ipSet != backup.Name {
				t.Fatalf("the backup takes the ClientHello, got %+v", row)
			}
			w.wg.Wait()
			w.ProcessPacket(makeV4TCPPacket(tlsAppData(64), 1000+uint32(len(hello))))
			if row := lastConnectionRow(t, ch); row.sniSet != backup.Name && row.ipSet != backup.Name {
				t.Errorf("later data stays with the backup, got %+v", row)
			}
		})
	}
}

func TestABackupTheTLSFilterDropsStaysDropped(t *testing.T) {
	primary := newTLSFilteredSet("", targetsByName)
	primary.Escalate.To = "backup"
	backup := newTLSFilteredSet("1.2", func(set *config.SetConfig) {
		set.Targets.DomainsToMatch = []string{"backup.example"}
		set.TCP.Incoming.Mode = "fake"
	})
	backup.Id, backup.Name = "backup", "Backup"
	w := newInjectingWorker(t, &primary, &backup)
	taughtAddress(w, &primary)
	w.destState.SetEscalation("i.ytimg.com", backup.Id, escalateReasonStall, time.Minute)
	ch := subscribeRows(t)
	w.ProcessPacket(makeV4TCPPacketFlags(nil, 999, 0x02))
	if bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443); bound == nil || bound.Id != backup.Id {
		t.Fatalf("the SYN binds the backup to the replies, got %v", bound)
	}
	hello := buildClientHello("www.example.org", 16, 0xAB)
	w.ProcessPacket(makeV4TCPPacket(hello, 1000))
	if row := lastConnectionRow(t, ch); row.sniSet != "" || row.ipSet != "" {
		t.Fatalf("the backup's filter drops the match, got %+v", row)
	}
	expectFilteredOut(t, w, ch, filterStep{"later data", makeV4TCPPacket(tlsAppData(64), 1000+uint32(len(hello)))})
	if bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443); bound != nil {
		t.Errorf("the replies are still handled by %s", bound.Name)
	}
}

func TestARefusedSetBoundAtTheSynIsReleasedWhenItsBackupWasJudged(t *testing.T) {
	primary := newTLSFilteredSet("", func(set *config.SetConfig) {
		targetsByName(set)
		set.Escalate.To = "backup"
		set.TCP.Incoming.Mode = "fake"
	})
	backup := newTLSFilteredSet("1.2", func(set *config.SetConfig) {
		set.Id, set.Name = "backup", "Backup"
		set.Targets.DomainsToMatch = []string{"backup.example"}
	})
	w := newInjectingWorker(t, &primary, &backup)
	taughtAddress(w, &primary)
	w.ProcessPacket(makeV4TCPPacketFlags(nil, 999, 0x02))
	if bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443); bound == nil || bound.Id != primary.Id {
		t.Fatalf("the SYN binds the set of the address to the replies, got %v", bound)
	}
	w.destState.SetEscalation("i.ytimg.com", backup.Id, escalateReasonStall, time.Minute)
	w.ProcessPacket(makeV4TCPPacket(buildClientHello("www.example.org", 16, 0xAB), 1000))
	if bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443); bound != nil {
		t.Errorf("the replies are still handled by %s", bound.Name)
	}
}

func TestABackupWhoseOwnNameTheClientHelloCarriesKeepsLaterPackets(t *testing.T) {
	primary := newTLSFilteredSet("", targetsByName)
	primary.Escalate.To = "backup"
	backup := newTLSFilteredSet("1.2", func(set *config.SetConfig) {
		set.Id, set.Name = "backup", "Backup"
		set.Targets.DomainsToMatch = []string{"backup.example"}
		set.Targets.DomainOnly = true
		set.TCP.Incoming.Mode = "fake"
	})
	w := newInjectingWorker(t, &primary, &backup)
	taughtAddress(w, &primary)
	w.destState.SetEscalation("i.ytimg.com", backup.Id, escalateReasonStall, time.Minute)
	ch := subscribeRows(t)
	hello := buildClientHello("www.backup.example", 16, 0xAB)
	w.ProcessPacket(makeV4TCPPacket(hello, 1000))
	if row := lastConnectionRow(t, ch); row.sniSet != backup.Name {
		t.Fatalf("the backup takes its own name, got %+v", row)
	}
	w.wg.Wait()
	if v := w.ProcessPacket(makeV4TCPPacket(tlsAppData(64), 1000+uint32(len(hello)))); v != engine.VerdictDrop {
		t.Errorf("later data stays with the backup, got %v", v)
	}
	if row := lastConnectionRow(t, ch); row.sniSet != backup.Name && row.ipSet != backup.Name {
		t.Errorf("later data stays with the backup, got %+v", row)
	}
	if bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443); bound == nil || bound.Id != backup.Id {
		t.Errorf("the replies stay with the backup, got %v", bound)
	}
}

func TestALearnedSetThatTookTheHelloInPlaceOfADroppedHintKeepsIt(t *testing.T) {
	const hintedName = "www.hinted.example"
	for _, tc := range []struct {
		name, ports, escalated, want string
	}{
		{"the learned set", "", "", "Learned"},
		{"the hinted name is switched to a backup meanwhile", "", hintedName, "Learned"},
		{"the learned name is switched to a backup", "", "i.ytimg.com", "Backup"},
		{"the learned set leaves the port alone", "8443", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hinted := newTLSFilteredSet("1.2", func(set *config.SetConfig) { set.Targets.DomainsToMatch = []string{"hinted.example"} })
			learned := newTLSFilteredSet("", func(set *config.SetConfig) {
				targetsByName(set)
				set.TCP.DPortFilter = tc.ports
				set.TCPPortRanges = config.ParsePortRanges(tc.ports)
			})
			learned.Id, learned.Name, learned.Escalate.To = "learned", "Learned", "backup"
			backup := newTLSFilteredSet("", func(set *config.SetConfig) { set.Targets.DomainsToMatch = []string{"backup.example"} })
			backup.Id, backup.Name = "backup", "Backup"
			hinted.Escalate.To = "backup"
			w := newInjectingWorker(t, &hinted, &learned, &backup)
			taughtAddress(w, &learned)
			w.storeHostHint("10.0.0.1", "1.2.3.4", &hinted, hintedName, "dns", 0, 0)
			if tc.escalated == "i.ytimg.com" {
				w.destState.SetEscalation(tc.escalated, "backup", escalateReasonStall, time.Minute)
			}
			ch := subscribeRows(t)
			hello := buildClientHello("www.example.org", 16, 0xAB)
			w.ProcessPacket(makeV4TCPPacket(hello, 1000))
			if row := lastConnectionRow(t, ch); row.ipSet != tc.want {
				t.Fatalf("the ClientHello the hinted set does not fit, want %q, got %+v", tc.want, row)
			}
			w.wg.Wait()
			if tc.escalated == hintedName {
				w.destState.SetEscalation(tc.escalated, "backup", escalateReasonStall, time.Minute)
			}
			w.ProcessPacket(makeV4TCPPacket(tlsAppData(64), 1000+uint32(len(hello))))
			if row := lastConnectionRow(t, ch); row.ipSet != tc.want || row.meta != "" {
				t.Errorf("later data, want %q, got %+v", tc.want, row)
			}
		})
	}
}

func TestARefusedDNSAnswerMatchIsNotReplacedByALearnedSetThatTookNoClientHello(t *testing.T) {
	passive := func(set *config.SetConfig) { set.Fragmentation.Strategy = config.ConfigNone }
	hinted := newTLSFilteredSet("1.2", func(set *config.SetConfig) {
		set.Targets.DomainsToMatch = []string{"hinted.example"}
		passive(set)
	})
	named := newTLSFilteredSet("", func(set *config.SetConfig) {
		set.Targets.DomainsToMatch = []string{"named.example"}
		set.Targets.DomainOnly = true
		passive(set)
	})
	named.Id, named.Name = "named", "Named"
	learnedFor := func(tls string) config.SetConfig {
		learned := newTLSFilteredSet(tls, func(set *config.SetConfig) {
			targetsByName(set)
			set.TCP.Incoming.Mode = "fake"
		})
		learned.Id, learned.Name = "learned", "Learned"
		return learned
	}
	alsoFiltered, unfiltered := learnedFor("1.2"), learnedFor("")
	for _, tc := range []struct {
		name, host    string
		hint, learned *config.SetConfig
	}{
		{"the learned set does not fit either", "www.example.org", &hinted, &alsoFiltered},
		{"the address was learned for the refused set itself", "www.example.org", &alsoFiltered, &alsoFiltered},
		{"a domain-only set took the name", "www.named.example", &hinted, &unfiltered},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newInjectingWorker(t, &hinted, &named, tc.learned)
			taughtAddress(w, tc.learned)
			w.storeHostHint("10.0.0.1", "1.2.3.4", tc.hint, "www.hinted.example", "dns", 0, 0)
			ch := subscribeRows(t)
			hello := buildClientHello(tc.host, 16, 0xAB)
			next := 1000 + uint32(len(hello))
			w.ProcessPacket(makeV4TCPPacket(hello, 1000))
			if row := lastConnectionRow(t, ch); row.ipSet != "" {
				t.Fatalf("no set takes the ClientHello by its address, got %+v", row)
			}
			expectFilteredOut(t, w, ch, filterStep{"later data", makeV4TCPPacket(tlsAppData(64), next)})
			w.ProcessPacket(makeV4TCPPacketFlags(nil, next+69, 0x10))
			if row := lastConnectionRow(t, ch); row.ipSet != "" {
				t.Errorf("a bare ACK went to %s", row.ipSet)
			}
			if bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443); bound != nil {
				t.Errorf("the replies are handled by %s", bound.Name)
			}
		})
	}
}

func TestAClientHelloCutAfterItsVersionsDropsTheMatchForGood(t *testing.T) {
	hello := helloWithExtensions(buildSNIExtension("i.ytimg.com"), buildExtension(43, []byte{0x02, 0x03, 0x04}), buildExtension(51, make([]byte, 1400)))
	named := newFragmentingYTImagesSet()
	named.TCP.RSTProtection.Enabled = false
	address := newTLSFilteredSet("1.2", func(set *config.SetConfig) {
		targetsByAddress(set)
		set.TCP.Incoming.Mode = "fake"
	})
	for name, sets := range map[string][]*config.SetConfig{
		"a name of no set":               {&address},
		"a name of another set":          {&address, &named},
		"a name of another set above it": {&named, &address},
	} {
		t.Run(name, func(t *testing.T) {
			w := newInjectingWorker(t, sets...)
			ch := subscribeRows(t)
			w.ProcessPacket(makeV4TCPPacketFlags(nil, 999, 0x02))
			for _, step := range []filterStep{
				{"the part with the name", makeV4TCPPacket(hello[:splitCut], 1000)},
				{"the second part", makeV4TCPPacket(hello[splitCut:], 1000+splitCut)},
				{"later data", makeV4TCPPacket(tlsAppData(64), 1000+uint32(len(hello)))},
				{"a bare ACK", makeV4TCPPacketFlags(nil, 1069+uint32(len(hello)), 0x10)},
			} {
				w.ProcessPacket(step.raw)
				w.wg.Wait()
				if row := lastConnectionRow(t, ch); row.ipSet != "" || (len(sets) > 1 && row.sniSet != named.Name) {
					t.Errorf("%s: got %+v", step.what, row)
				}
			}
			if bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443); bound != nil {
				t.Errorf("the replies are still handled by %s", bound.Name)
			}
		})
	}
}

func inTwoRecords(hello []byte, cut int) []byte {
	msg := hello[5:]
	out := binary.BigEndian.AppendUint16([]byte{0x16, 0x03, 0x01}, uint16(cut))
	out = append(append(out, msg[:cut]...), 0x16, 0x03, 0x01)
	out = binary.BigEndian.AppendUint16(out, uint16(len(msg)-cut))
	return append(out, msg[cut:]...)
}

func behindChangeCipherSpec(hello []byte) []byte {
	return append(append([]byte(nil), changeCipherSpecRecord...), hello...)
}

func TestAVersionReadFromAPartOfTheClientHelloRefusesNothing(t *testing.T) {
	hello := helloNamedBeforeItsVersions("www.example.org")
	cut := len(hello) - 5 - 20
	two := inTwoRecords(hello, cut)
	second := behindChangeCipherSpec(hello)
	for _, tc := range []struct {
		name  string
		parts [][]byte
	}{
		{"two records in one segment", [][]byte{two}},
		{"two records in two segments", [][]byte{two[:5+cut], two[5+cut:]}},
		{"a second ClientHello behind a ChangeCipherSpec, cut after its name", [][]byte{buildClientHello("www.example.org", 16, 0xAB), second[:splitCut], second[splitCut:]}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := newTLSFilteredSet("1.3", func(set *config.SetConfig) {
				targetsByAddress(set)
				set.TCP.RSTProtection.Enabled = true
			})
			w := newInjectingWorker(t, &set)
			ch := subscribeRows(t)
			w.ProcessPacket(makeV4TCPPacketFlags(nil, 999, 0x02))
			seq := uint32(1000)
			for _, part := range tc.parts {
				w.ProcessPacket(makeV4TCPPacket(part, seq))
				w.wg.Wait()
				seq += uint32(len(part))
			}
			connectionRows(ch)
			w.ProcessPacket(makeV4TCPPacket(tlsAppData(64), seq))
			if row := lastConnectionRow(t, ch); row.ipSet != set.Name {
				t.Errorf("later data of a TLS 1.3 connection fits the filter, got %+v", row)
			}
			if bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443); bound == nil || bound.Id != set.Id {
				t.Errorf("the replies of a TLS 1.3 connection stay with the set, got %v", bound)
			}
		})
	}
}

func TestASecondClientHelloOfTheSameVersionKeepsTheRefusal(t *testing.T) {
	named := newFragmentingYTImagesSet()
	named.Targets.DomainsToMatch = []string{"named.example"}
	for _, tc := range []struct {
		name, host, want string
		mutate           func(set *config.SetConfig)
		teach            func(w *Worker, set *config.SetConfig)
	}{
		{"a listed address", "www.example.org", "", targetsByAddress, nil},
		{"a port filter", "www.example.org", "", targetsByPort, nil},
		{"a learned address", "www.example.org", "", targetsByName, taughtAddress},
		{"a DNS answer", "www.example.org", "", targetsByName, taughtHint},
		{"a listed address, the name of a domain-only set", "www.named.example", "YT images", targetsByAddress, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := newTLSFilteredSet("1.2", tc.mutate)
			w := newInjectingWorker(t, &named, &set)
			if tc.teach != nil {
				tc.teach(w, &set)
			}
			ch := subscribeRows(t)
			seq := uint32(1000)
			for _, part := range [][]byte{
				buildClientHello(tc.host, 16, 0xAB),
				behindChangeCipherSpec(buildClientHello(tc.host, 48, 0xAC)),
				tlsAppData(64),
				nil,
			} {
				w.ProcessPacket(makeV4TCPPacketFlags(part, seq, 0x10))
				w.wg.Wait()
				seq += uint32(len(part))
				if row := lastConnectionRow(t, ch); row.ipSet != "" || row.sniSet != tc.want {
					t.Errorf("after %d bytes: want set %q, got %+v", seq-1000, tc.want, row)
				}
			}
		})
	}
}

func TestAClientHelloResentAfterTheEscalationEndsKeepsTheSetThatTakesIt(t *testing.T) {
	for _, tc := range []struct {
		name string
		cut  int
	}{
		{"whole", 0},
		{"split", splitCut},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := newTLSFilteredSet("", func(set *config.SetConfig) {
				targetsByName(set)
				set.Escalate.To = "backup"
			})
			backup := newTLSFilteredSet("1.2", func(set *config.SetConfig) {
				set.Id, set.Name = "backup", "Backup"
				set.Targets.DomainsToMatch = []string{"backup.example"}
			})
			w := newInjectingWorker(t, &set, &backup)
			taughtAddress(w, &set)
			w.destState.SetEscalation("i.ytimg.com", backup.Id, escalateReasonStall, time.Minute)
			ch := subscribeRows(t)

			hello := buildClientHello("www.example.org", 16, 0xAB)
			parts := []filterStep{{"the ClientHello", makeV4TCPPacket(hello, splitSeq)}}
			if tc.cut > 0 {
				hello, _, _ = splitHello("www.example.org")
				parts = []filterStep{
					{"the first part", makeV4TCPPacket(hello[:tc.cut], splitSeq)},
					{"the second part", makeV4TCPPacket(hello[tc.cut:], splitSeq+uint32(tc.cut))},
				}
			}
			data := makeV4TCPPacket(tlsAppData(64), splitSeq+uint32(len(hello)))
			for _, step := range parts {
				w.ProcessPacket(step.raw)
			}
			w.wg.Wait()
			connectionRows(ch)
			expectFilteredOut(t, w, ch, filterStep{"data while the backup is in use", data})
			w.destState.ClearEscalation("i.ytimg.com")

			for _, step := range append(parts, filterStep{"later data", data}) {
				if v := w.ProcessPacket(step.raw); v != engine.VerdictDrop {
					t.Errorf("%s: want the strategy of the set that takes it, got %v", step.what, v)
				}
				w.wg.Wait()
				if row := lastConnectionRow(t, ch); row.ipSet != set.Name {
					t.Errorf("%s: want the set that takes it, got %+v", step.what, row)
				}
			}
		})
	}
}

func TestAResentClientHelloCutBeforeItsVersionsLeavesTheRefusal(t *testing.T) {
	set := newTLSFilteredSet("1.2", targetsByAddress)
	w := newInjectingWorker(t, &set)
	ch := subscribeRows(t)
	hello := helloNamedBeforeItsVersions("www.example.org")
	next := 1000 + uint32(len(hello))
	w.ProcessPacket(makeV4TCPPacket(hello, 1000))
	connectionRows(ch)
	expectFilteredOut(t, w, ch, filterStep{"later data", makeV4TCPPacket(tlsAppData(64), next)})
	w.ProcessPacket(makeV4TCPPacket(hello[:600], 1000))
	w.ProcessPacket(makeV4TCPPacket(hello[600:], 1600))
	w.wg.Wait()
	connectionRows(ch)
	expectFilteredOut(t, w, ch, filterStep{"data after the resend", makeV4TCPPacket(tlsAppData(64), next)})
}

func TestABackupThatDoesNotFitLeavesTheConnectionToADomainOnlyName(t *testing.T) {
	for _, tc := range []struct {
		name, setTLS, backupTLS string
	}{
		{"the backup does not fit", "", "1.2"},
		{"the set itself does not fit", "1.2", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := newTLSFilteredSet(tc.setTLS, func(set *config.SetConfig) {
				targetsByName(set)
				set.Escalate.To = "backup"
			})
			backup := newTLSFilteredSet(tc.backupTLS, func(set *config.SetConfig) {
				set.Id, set.Name = "backup", "Backup"
				set.Targets.DomainsToMatch = []string{"backup.example"}
				set.TCP.Incoming.Mode = "fake"
			})
			named := newPassiveSet()
			named.Id, named.Name = "named", "Named"
			named.Targets.DomainsToMatch = []string{"named.example"}
			named.TCP.RSTProtection.Enabled = false
			w := newInjectingWorker(t, &set, &backup, &named)
			taughtAddress(w, &set)
			w.destState.SetEscalation("i.ytimg.com", backup.Id, escalateReasonStall, time.Minute)
			ch := subscribeRows(t)
			w.ProcessPacket(makeV4TCPPacketFlags(nil, 999, 0x02))
			if bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443); bound == nil || bound.Id != backup.Id {
				t.Fatalf("the SYN binds the backup to the replies, got %v", bound)
			}
			hello := buildClientHello("www.named.example", 16, 0xAB)
			w.ProcessPacket(makeV4TCPPacket(hello, 1000))
			if row := lastConnectionRow(t, ch); row.sniSet != named.Name || row.ipSet != "" {
				t.Fatalf("the set of the name takes the ClientHello, got %+v", row)
			}
			if v := w.ProcessPacket(makeV4TCPPacket(tlsAppData(64), 1000+uint32(len(hello)))); v != engine.VerdictAccept {
				t.Errorf("later data went through a strategy, got %v", v)
			}
			if row := lastConnectionRow(t, ch); row.ipSet != "" {
				t.Errorf("later data went to %s", row.ipSet)
			}
			if bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443); bound != nil {
				t.Errorf("the replies are still handled by %s", bound.Name)
			}
		})
	}
}

func TestAConnectionIsJudgedByItsOwnClientHello(t *testing.T) {
	tls12 := setOrderTLS12Hello("www.example.org")
	first := buildClientHello("www.example.org", 16, 0xAB)
	after := 1000 + uint32(len(first))

	t.Run("a SYN resent after the ClientHello", func(t *testing.T) {
		set := newTLSFilteredSet("1.2", targetsByAddress)
		w := newInjectingWorker(t, &set)
		ch := subscribeRows(t)
		w.ProcessPacket(makeV4TCPPacketFlags(nil, 999, 0x02))
		w.ProcessPacket(makeV4TCPPacket(first, 1000))
		w.ProcessPacket(makeV4TCPPacketFlags(nil, 999, 0x02))
		if row := lastConnectionRow(t, ch); row.ipSet != set.Name {
			t.Errorf("the resent SYN is handled like the first one, got %+v", row)
		}
	})
	t.Run("a new connection on the tuple", func(t *testing.T) {
		set := newTLSFilteredSet("1.2", targetsByAddress)
		w := newInjectingWorker(t, &set)
		ch := subscribeRows(t)
		w.ProcessPacket(makeV4TCPPacket(first, 1000))
		connectionRows(ch)
		w.ProcessPacket(makeV4TCPPacketFlags(nil, 90000, 0x02))
		if row := lastConnectionRow(t, ch); row.ipSet != set.Name {
			t.Errorf("the TLS version of a new connection is not known at its SYN, got %+v", row)
		}
		if v := w.ProcessPacket(makeV4TCPPacket(tls12, 90001)); v != engine.VerdictDrop {
			t.Errorf("a TLS 1.2 ClientHello fits the filter, got %v", v)
		}
		w.wg.Wait()
		connectionRows(ch)
		w.ProcessPacket(makeV4TCPPacket(tlsAppData(64), 90001+uint32(len(tls12))))
		if row := lastConnectionRow(t, ch); row.ipSet != set.Name {
			t.Errorf("later data of a connection that fits belongs to the set, got %+v", row)
		}
	})
	t.Run("a second ClientHello on the connection", func(t *testing.T) {
		set := newTLSFilteredSet("1.2", targetsByAddress)
		w := newInjectingWorker(t, &set)
		ch := subscribeRows(t)
		w.ProcessPacket(makeV4TCPPacket(first, 1000))
		expectFilteredOut(t, w, ch, filterStep{"later data", makeV4TCPPacket(tlsAppData(64), after)})
		if v := w.ProcessPacket(makeV4TCPPacket(tls12, after+69)); v != engine.VerdictDrop {
			t.Errorf("the second ClientHello fits the filter, got %v", v)
		}
		if row := lastConnectionRow(t, ch); row.ipSet != set.Name {
			t.Errorf("the second ClientHello belongs to the set, got %+v", row)
		}
		w.wg.Wait()
		w.ProcessPacket(makeV4TCPPacket(tlsAppData(64), after+69+uint32(len(tls12))))
		if row := lastConnectionRow(t, ch); row.ipSet != set.Name {
			t.Errorf("data after the second ClientHello belongs to the set, got %+v", row)
		}
	})
}

func TestB4sOwnTelegramConnectionIgnoresTheTLSFilter(t *testing.T) {
	telegram := newTLSFilteredSet("1.2", nil)
	telegram.Escalate.To = "backup"
	backup := newTLSFilteredSet("1.2", nil)
	backup.Id, backup.Name = "backup", "Backup"
	hello := buildClientHello("www.example.org", 16, 0xAB)
	later := makeV4TCPPacket(tlsAppData(64), 1000+uint32(len(hello)))
	for _, escalated := range []bool{false, true} {
		want := telegram.Name
		if escalated {
			want = backup.Name
		}
		t.Run("later data belongs to "+want, func(t *testing.T) {
			w := newInjectingWorker(t, &telegram, &backup)
			ch := subscribeRows(t)
			send := func(raw []byte) {
				w.dispatch(&verdictCtx{telegramSet: &telegram, selfDial: true}, raw)
				w.wg.Wait()
			}
			send(makeV4TCPPacket(hello, 1000))
			if row := lastConnectionRow(t, ch); row.ipSet != telegram.Name {
				t.Fatalf("the Telegram set keeps b4's own ClientHello, got %+v", row)
			}
			if escalated {
				w.destState.SetEscalation("www.example.org", backup.Id, escalateReasonStall, time.Minute)
				send(makeV4TCPPacket(hello, 1000))
				connectionRows(ch)
			}
			send(later)
			if row := lastConnectionRow(t, ch); row.ipSet != want {
				t.Errorf("want b4's own later data in %s, got %+v", want, row)
			}
		})
	}
}

func TestB4sOwnTelegramConnectionKeepsItsBackupAfterAHelloItsFilterDoesNotFit(t *testing.T) {
	telegram := newTLSFilteredSet("", targetsByName)
	telegram.Id, telegram.Name, telegram.Escalate.To = "telegram", "Telegram", "backup"
	backup := newTLSFilteredSet("1.2", func(set *config.SetConfig) { set.Targets.DomainsToMatch = []string{"backup.example"} })
	backup.Id, backup.Name = "backup", "Backup"
	w := newInjectingWorker(t, &telegram, &backup)
	taughtAddress(w, &telegram)
	w.destState.SetEscalation("i.ytimg.com", backup.Id, escalateReasonStall, time.Minute)
	ch := subscribeRows(t)
	send := func(raw []byte) {
		w.dispatch(&verdictCtx{telegramSet: &telegram, selfDial: true}, raw)
		w.wg.Wait()
	}
	hello := buildClientHello("www.outside.example", 16, 0xAB)
	send(makeV4TCPPacket(hello, 1000))
	connectionRows(ch)
	send(makeV4TCPPacket(tlsAppData(64), 1000+uint32(len(hello))))
	if row := lastConnectionRow(t, ch); row.ipSet != backup.Name {
		t.Errorf("later data of b4's own connection stays with the backup, got %+v", row)
	}
}

func TestAClientHelloInFlightDuringAReloadIsRefusedOnlyByAFilterThatStands(t *testing.T) {
	filtered := func(tls string, change func(set *config.SetConfig)) *config.SetConfig {
		set := newTLSFilteredSet(tls, targetsByAddress)
		if change != nil {
			change(&set)
		}
		return &set
	}
	learned := func(tls string) *config.SetConfig {
		set := newTLSFilteredSet(tls, targetsByName)
		set.Id, set.Name, set.Escalate.To = "learned", "Learned", "backup"
		return &set
	}
	named := func() *config.SetConfig {
		set := newPassiveSet()
		set.Id, set.Name, set.Targets.DomainOnly = "named", "Named", true
		set.Targets.DomainsToMatch = []string{"named.example"}
		set.TCP.RSTProtection.Enabled = false
		return &set
	}
	backup := func(tls string) *config.SetConfig {
		set := newTLSFilteredSet(tls, func(set *config.SetConfig) { set.Targets.DomainsToMatch = []string{"backup.example"} })
		set.Id, set.Name = "backup", "Backup"
		return &set
	}
	off := func(set *config.SetConfig) *config.SetConfig {
		set.Enabled = false
		return set
	}
	extra := newNamedRowSet("extra", config.ConfigOff, "extra.example")
	for _, tc := range []struct {
		name, takes string
		old, sets   []*config.SetConfig
		want        string
	}{
		{"nothing changes", "", []*config.SetConfig{filtered("1.2", nil)}, nil, ""},
		{"only the settings change", "", []*config.SetConfig{filtered("1.2", nil)}, []*config.SetConfig{filtered("1.2", nil)}, ""},
		{"an unrelated set is added", "", []*config.SetConfig{filtered("1.2", nil)}, []*config.SetConfig{filtered("1.2", nil), &extra}, ""},
		{"another field of the set changes", "", []*config.SetConfig{filtered("1.2", nil)}, []*config.SetConfig{filtered("1.2", func(set *config.SetConfig) { set.TCP.DropSACK = !set.TCP.DropSACK })}, ""},
		{"the filter is turned off", "", []*config.SetConfig{filtered("1.2", nil)}, []*config.SetConfig{filtered("", nil)}, "Filtered"},
		{"the filter becomes TLS 1.3", "", []*config.SetConfig{filtered("1.2", nil)}, []*config.SetConfig{filtered("1.3", nil)}, "Filtered"},
		{"the filter becomes TLS 1.1", "", []*config.SetConfig{filtered("1.2", nil)}, []*config.SetConfig{filtered("1.1", nil)}, ""},
		{"the backup's filter stays", "", []*config.SetConfig{learned(""), backup("1.2")}, []*config.SetConfig{learned(""), backup("1.2"), &extra}, ""},
		{"the backup's filter is turned off", "", []*config.SetConfig{learned(""), backup("1.2")}, []*config.SetConfig{learned(""), backup("")}, "Backup"},
		{"the backup is turned off", "", []*config.SetConfig{learned(""), backup("1.2")}, []*config.SetConfig{learned(""), off(backup("1.2"))}, "Learned"},
		{"the backup is deleted", "", []*config.SetConfig{learned(""), backup("1.2")}, []*config.SetConfig{learned("")}, "Learned"},
		{"the backup is turned off, the set does not fit either", "", []*config.SetConfig{learned("1.2"), backup("1.2")}, []*config.SetConfig{learned("1.2"), off(backup("1.2"))}, ""},
		{"the backup is deleted, the set does not fit either", "", []*config.SetConfig{learned("1.2"), backup("1.2")}, []*config.SetConfig{learned("1.2")}, ""},
		{"the backup's filter is turned off, the set does not fit", "", []*config.SetConfig{learned("1.2"), backup("1.2")}, []*config.SetConfig{learned("1.2"), backup("")}, "Backup"},
		{"the set and its backup are deleted", "", []*config.SetConfig{learned(""), backup("1.2")}, []*config.SetConfig{&extra}, ""},
		{"the set's filter is turned off, its backup refused", "", []*config.SetConfig{learned("1.2"), backup("1.2")}, []*config.SetConfig{learned(""), backup("1.2")}, ""},
		{"the set that fits loses its filter, its backup refused", "", []*config.SetConfig{learned("1.3"), backup("1.2")}, []*config.SetConfig{learned(""), backup("1.2")}, ""},
		{"the set that fits gets a filter, its backup refused", "", []*config.SetConfig{learned(""), backup("1.2")}, []*config.SetConfig{learned("1.3"), backup("1.2")}, ""},
		{"another set takes the name, the filter stays", "named", []*config.SetConfig{learned("1.2"), backup(""), named()}, []*config.SetConfig{learned("1.2"), backup(""), named(), &extra}, ""},
		{"another set takes the name, the filter is turned off", "named", []*config.SetConfig{learned("1.2"), backup(""), named()}, []*config.SetConfig{learned(""), backup(""), named()}, "Backup"},
		{"another set takes the name, both refused, the set's filter is turned off", "named", []*config.SetConfig{learned("1.2"), backup("1.2"), named()}, []*config.SetConfig{learned(""), backup("1.2"), named()}, ""},
		{"another set takes the name, both refused, the backup's filter is turned off", "named", []*config.SetConfig{learned("1.2"), backup("1.2"), named()}, []*config.SetConfig{learned("1.2"), backup(""), named()}, ""},
		{"another set takes the name, both refused, both filters are turned off", "named", []*config.SetConfig{learned("1.2"), backup("1.2"), named()}, []*config.SetConfig{learned(""), backup(""), named()}, "Backup"},
		{"another set takes the name, the filter moves to the set", "named", []*config.SetConfig{learned(""), backup("1.2"), named()}, []*config.SetConfig{learned("1.2"), backup(""), named()}, ""},
		{"the backup takes the name, the filter stays", "backup", []*config.SetConfig{learned("1.2"), backup("1.2")}, []*config.SetConfig{learned("1.2"), backup("1.2"), &extra}, ""},
		{"the backup takes the name, the set's filter is turned off", "backup", []*config.SetConfig{learned("1.2"), backup("1.2")}, []*config.SetConfig{learned(""), backup("1.2")}, "Learned"},
		{"another set takes the name, the filter of a backup that fits changes", "named", []*config.SetConfig{learned("1.2"), backup("1.3"), named()}, []*config.SetConfig{learned("1.2"), backup(""), named()}, ""},
	} {
		run := func(name string, newMatcher bool) {
			t.Run(name, func(t *testing.T) {
				w := newInjectingWorker(t, tc.old...)
				escalate := func() {
					if len(tc.old) > 1 {
						w.destState.SetEscalation("i.ytimg.com", "backup", escalateReasonStall, time.Minute)
					}
				}
				if len(tc.old) > 1 {
					taughtAddress(w, tc.old[0])
				}
				escalate()
				pool := &Pool{Workers: []*Worker{w}, state: &runtimeState{hostHints: w.hostHints, destState: w.destState, tlsCache: w.tlsCache}}
				ch := subscribeRows(t)
				cfg, matcher := w.getConfig(), w.getMatcher()
				if tc.sets != nil {
					next := cfg.Clone()
					next.Sets = tc.sets
					if err := pool.UpdateConfig(next); err != nil {
						t.Fatal(err)
					}
					escalate()
				}
				if newMatcher {
					matcher = w.getMatcher()
				}
				host, taker := "www.example.org", ""
				switch tc.takes {
				case "named":
					host, taker = "www.named.example", "Named"
				case "backup":
					host, taker = "www.backup.example", "Backup"
				}
				hello := buildClientHello(host, 16, 0xAB)
				vc := &verdictCtx{verdict: engine.VerdictAccept, tunEntry: true}
				pkt, _ := w.parseIPHeaders(makeV4TCPPacket(hello, 1000))
				matched, set, prefix := w.addressSet(vc, pkt, matcher)
				w.handleTCPPacket(vc, pkt, cfg, matcher, matched, set, set, prefix)
				w.wg.Wait()
				if row := lastConnectionRow(t, ch); !newMatcher && (row.ipSet != "" || row.sniSet != taker) {
					t.Fatalf("the ClientHello is judged by the sets it was read with, got %+v", row)
				}
				if refused := w.tlsCache.Refuses("10.0.0.1:12345->1.2.3.4:443", tc.old[0].Id); tc.takes == "backup" && refused != (tc.want == "") {
					t.Errorf("the set of the address is refused: %v, want %v", refused, tc.want == "")
				}
				w.ProcessPacket(makeV4TCPPacket(tlsAppData(64), 1000+uint32(len(hello))))
				w.wg.Wait()
				if row := lastConnectionRow(t, ch); tc.takes != "backup" && row.ipSet != tc.want {
					t.Errorf("later data is in %q, want %q", row.ipSet, tc.want)
				}
			})
		}
		run(tc.name, false)
		run(tc.name+", read with the old config and the new matcher", true)
	}
}

func TestARefusalIsStoredBeforeTheSetsInUseAreRead(t *testing.T) {
	const key = "10.0.0.1:12345->1.2.3.4:443"
	set, open := newTLSFilteredSet("1.2", targetsByAddress), newTLSFilteredSet("", targetsByAddress)
	w := newInjectingWorker(t, &set)
	gen := w.tlsCache.Store(key, "www.example.org", 0x0304, 1000, 7, 500)
	next := w.getConfig().Clone()
	next.Sets = []*config.SetConfig{&open}

	w.tlsCache.mu.Lock()
	var refused sync.WaitGroup
	refused.Go(func() { w.refuseTLS(key, gen, &set, []*config.SetConfig{&set}, 0x0304) })
	time.Sleep(20 * time.Millisecond)
	w.cfg.Store(next)
	w.tlsCache.mu.Unlock()
	refused.Wait()

	if w.tlsCache.Refuses(key, set.Id) {
		t.Error("a reload while the refusal was being stored must drop it")
	}
}

func TestARefusedSetIsForgottenWithTheClientHelloItWasRefusedFor(t *testing.T) {
	const key = "10.0.0.1:12345->1.2.3.4:443"
	cache := func() (*tlsInfoCache, uint64) {
		c := newRuntimeState().tlsCache
		gen := c.Store(key, "www.example.org", 0x0304, 1000, 7, 500)
		c.Refuse(key, gen, "filtered")
		return c, gen
	}
	c := newRuntimeState().tlsCache
	if c.Store(key, "www.example.org", 0x0304, 1000, 7, 500); c.Refuses(key, "") {
		t.Fatal("nothing is refused before a verdict")
	}
	c, gen := cache()
	if !c.Refuses(key, "filtered") || c.Refuses(key, "another") || c.Refuses(key, "") || c.Refuses("10.0.0.1:1->1.2.3.4:443", "filtered") {
		t.Fatal("exactly the refused set of exactly this connection is refused")
	}
	if c.Store(key, "www.example.org", 0x0304, 1000, 7, 500); !c.Refuses(key, "filtered") {
		t.Error("the same ClientHello resent keeps the refusal")
	}
	c.Refuse(key, gen, "stale")
	if c.Refuses(key, "stale") {
		t.Error("a verdict on an older ClientHello must not land on a newer one")
	}
	if c.Store(key, "www.example.org", 0x0303, 1500, 9, 500); c.Refuses(key, "filtered") {
		t.Error("another ClientHello starts without a refusal")
	}
	c, _ = cache()
	if c.Store(key, "www.example.org", 0x0303, 1000, 9, 500); c.Refuses(key, "filtered") {
		t.Error("another ClientHello at the same place starts without a refusal")
	}
	c, _ = cache()
	if c.ForgetDecisions(sni.NewSuffixSet(nil)); c.Refuses(key, "filtered") {
		t.Error("a change of the sets forgets the refusal with the other decisions")
	}
}
