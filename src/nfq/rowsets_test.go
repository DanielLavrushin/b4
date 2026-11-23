package nfq

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/log"
)

const rowHost = "rr1.googlevideo.com"

type connectionRow struct{ sniSet, domain, ipSet, tls, meta string }

func newRowSet(id, udpMode string) config.SetConfig {
	set := config.NewSetConfig()
	set.Id = id
	set.Name = id
	set.Enabled = true
	set.UDP.Mode = udpMode
	set.Faking.SNI = false
	set.Fragmentation.Strategy = config.ConfigNone
	set.Fragmentation.StrategyPool = nil
	return set
}

func newNamedRowSet(id, udpMode, domain string) config.SetConfig {
	set := newRowSet(id, udpMode)
	set.Targets.SNIDomains = []string{domain}
	return set
}

func newAddressRowSet(id, udpMode string) config.SetConfig {
	set := newRowSet(id, udpMode)
	set.Targets.IPs = []string{"1.2.3.0/24"}
	return set
}

func subscribeRows(t *testing.T) chan string {
	t.Helper()
	ch, _ := log.GetConnectionHub().Subscribe()
	t.Cleanup(func() { log.GetConnectionHub().Unsubscribe(ch) })
	return ch
}

func connectionRows(ch chan string) []connectionRow {
	var rows []connectionRow
	for {
		select {
		case line := <-ch:
			f := strings.Split(line, ",")
			row := connectionRow{sniSet: f[2], domain: f[3], ipSet: f[5], tls: f[8]}
			if len(f) > 9 {
				row.meta = f[9]
			}
			rows = append(rows, row)
		default:
			return rows
		}
	}
}

func lastConnectionRow(t *testing.T, ch chan string) connectionRow {
	t.Helper()
	rows := connectionRows(ch)
	if len(rows) == 0 {
		t.Fatal("expected a connection log line")
	}
	return rows[len(rows)-1]
}

func rowInitial(t *testing.T, seed byte, host string) []byte {
	t.Helper()
	return buildQUICInitialWithSNI(t, []byte{seed, 1, 2, 3, 4, 5, 6, 7}, host)
}

func TestAQUICClientHelloRowNamesOnlyTheSetThatTookIt(t *testing.T) {
	cases := []struct {
		name  string
		other func() config.SetConfig
		teach string
	}{
		{"over a static address of another set", func() config.SetConfig {
			return newAddressRowSet("other", config.UDPModeDrop)
		}, ""},
		{"over an address another set learned", func() config.SetConfig {
			return newNamedRowSet("other", config.ConfigOff, "learned.example")
		}, "www.learned.example"},
		{"over a port set", func() config.SetConfig {
			set := newRowSet("other", config.UDPModeDrop)
			set.UDP.DPortFilter = "443"
			return set
		}, ""},
	}
	for _, tc := range cases {
		for _, namedFirst := range []bool{true, false} {
			name := tc.name + ", name set below"
			if namedFirst {
				name = tc.name + ", name set above"
			}
			t.Run(name, func(t *testing.T) {
				named := newNamedRowSet("named", config.ConfigOff, "googlevideo.com")
				other := tc.other()
				sets := []*config.SetConfig{&other, &named}
				if namedFirst {
					sets = []*config.SetConfig{&named, &other}
				}
				w := newQUICFlowWorker(t, sets...)
				ch := subscribeRows(t)
				if tc.teach != "" {
					sendQUIC(w, rowInitial(t, 0x10, tc.teach), 56000)
					connectionRows(ch)
				}

				if v := sendQUIC(w, rowInitial(t, 0x11, rowHost), 56001); v != engine.VerdictAccept {
					t.Fatalf("the name set leaves QUIC alone and must decide the ClientHello, got verdict %v", v)
				}
				if row := lastConnectionRow(t, ch); row.sniSet != "named" || row.ipSet != "" || row.domain != rowHost {
					t.Errorf("want only the set that took the ClientHello, got %+v", row)
				}
			})
		}
	}
}

func TestAQUICAndATCPClientHelloToTheSetsOwnAddressCarryTheSameColumns(t *testing.T) {
	both := newNamedRowSet("both", config.ConfigOff, "googlevideo.com")
	both.Targets.IPs = []string{"1.2.3.4/32"}
	w := newQUICFlowWorker(t, &both)
	ch := subscribeRows(t)

	sendQUIC(w, rowInitial(t, 0x12, rowHost), 56002)
	udp := lastConnectionRow(t, ch)
	w.ProcessPacket(makeV4TCPPacket(buildClientHello(rowHost, 16, 0xAB), 1000))
	tcp := lastConnectionRow(t, ch)
	if udp.sniSet != "both" || udp.ipSet != "" || tcp.sniSet != udp.sniSet || tcp.ipSet != udp.ipSet {
		t.Errorf("want the name column alone over both protocols, got UDP %+v and TCP %+v", udp, tcp)
	}
}

func TestAnEscalatedQUICClientHelloRowNamesOnlyTheBackup(t *testing.T) {
	backup := newNamedRowSet("backup", config.ConfigOff, "fallback.example")
	named := newNamedRowSet("named", config.UDPModeDrop, "googlevideo.com")
	named.Escalate.To = backup.Id
	address := newAddressRowSet("address", config.UDPModeDrop)
	w := newQUICFlowWorker(t, &named, &backup, &address)
	w.destState.SetEscalation(rowHost, backup.Id, escalateReasonStall, time.Minute)
	ch := subscribeRows(t)

	if v := sendQUIC(w, rowInitial(t, 0x13, rowHost), 56003); v != engine.VerdictAccept {
		t.Fatalf("the backup leaves QUIC alone, got %v", v)
	}
	if row := lastConnectionRow(t, ch); row.sniSet != "backup" || row.ipSet != "" {
		t.Errorf("want only the backup, got %+v", row)
	}
}

func TestEveryRowOfABlockedQUICClientHelloNamesOnlyTheBlockSet(t *testing.T) {
	block := newNamedRowSet("block", config.ConfigOff, "googlevideo.com")
	block.Routing.Enabled = true
	block.Routing.Mode = config.RoutingModeBlock
	block.Routing.BlockAction = config.BlockActionDrop
	address := newAddressRowSet("address", config.ConfigOff)
	w := newQUICFlowWorker(t, &block, &address)
	ch := subscribeRows(t)

	if v := sendQUIC(w, rowInitial(t, 0x14, rowHost), 56004); v != engine.VerdictDropFlow {
		t.Fatalf("the name is blocked, got %v", v)
	}
	rows := connectionRows(ch)
	if len(rows) == 0 {
		t.Fatal("expected connection log lines")
	}
	for _, row := range rows {
		if row.sniSet != "block" || row.ipSet != "" {
			t.Errorf("want only the block set, got %+v", row)
		}
	}
}

func TestAQUICClientHelloNoNameSetTakesKeepsItsAddressSetInTheRow(t *testing.T) {
	named := newNamedRowSet("named", config.ConfigOff, "googlevideo.com")
	address := newAddressRowSet("address", config.ConfigOff)
	address.Targets.IPPriority = true

	t.Run("a name no set targets", func(t *testing.T) {
		w := newQUICFlowWorker(t, &named, &address)
		ch := subscribeRows(t)
		sendQUIC(w, rowInitial(t, 0x15, "www.unrelated.test"), 56005)
		if row := lastConnectionRow(t, ch); row.sniSet != "" || row.ipSet != "address" {
			t.Errorf("want only the address set, got %+v", row)
		}
	})
	t.Run("a name held by IP priority", func(t *testing.T) {
		w := newQUICFlowWorker(t, &address, &named)
		ch := subscribeRows(t)
		sendQUIC(w, rowInitial(t, 0x16, rowHost), 56006)
		if row := lastConnectionRow(t, ch); row.sniSet != "" || row.ipSet != "address" {
			t.Errorf("want only the holding set, got %+v", row)
		}
	})
}

func TestOtherUDPToALearnedAddressKeepsItsAddressColumn(t *testing.T) {
	learning := newNamedRowSet("learning", config.ConfigOff, "learned.example")
	w := newQUICFlowWorker(t, &learning)
	ch := subscribeRows(t)
	sendQUIC(w, rowInitial(t, 0x17, "www.learned.example"), 56007)
	connectionRows(ch)

	wireguard := make([]byte, 148)
	wireguard[0] = 0x01
	sendQUIC(w, wireguard, 56008)
	if row := lastConnectionRow(t, ch); row.ipSet != "learning" {
		t.Errorf("UDP that follows a learned address is matched by that address, got %+v", row)
	}
}

func TestOtherUDPToALearnedAddressWhoseNameAnotherSetListsShowsThatSetAlone(t *testing.T) {
	learning := newNamedRowSet("learning", config.ConfigOff, "learned.example")
	exact := newNamedRowSet("exact", config.ConfigOff, "www.learned.example")
	w := newQUICFlowWorker(t, &learning, &exact)
	w.getMatcher().LearnIPToDomain(net.ParseIP("1.2.3.4"), "www.learned.example", &learning)
	ch := subscribeRows(t)
	wireguard := make([]byte, 148)
	wireguard[0] = 0x01
	sendQUIC(w, wireguard, 56009)
	if row := lastConnectionRow(t, ch); row.sniSet != "exact" || row.ipSet != "" {
		t.Errorf("want only the set of the name, got %+v", row)
	}
}

func TestATCPClientHelloRowNamesOnlyTheSetThatTookIt(t *testing.T) {
	named := newNamedRowSet("named", config.ConfigOff, "googlevideo.com")

	t.Run("over a port set", func(t *testing.T) {
		ports := newRowSet("other", config.ConfigOff)
		ports.TCP.DPortFilter = "443"
		w := newQUICFlowWorker(t, &named, &ports)
		ch := subscribeRows(t)
		w.ProcessPacket(makeV4TCPPacket(buildClientHello(rowHost, 16, 0xAB), 1000))
		if row := lastConnectionRow(t, ch); row.sniSet != "named" || row.ipSet != "" {
			t.Errorf("want only the name set, got %+v", row)
		}
	})
	t.Run("over a DNS hint of another set", func(t *testing.T) {
		hinted := newNamedRowSet("other", config.ConfigOff, "hinted.example")
		w := newQUICFlowWorker(t, &named, &hinted)
		w.storeHostHint("10.0.0.1", "1.2.3.4", &hinted, "www.hinted.example", "dns", 0, 4)
		ch := subscribeRows(t)
		w.ProcessPacket(makeV4TCPPacket(buildClientHello(rowHost, 16, 0xAB), 1000))
		if row := lastConnectionRow(t, ch); row.sniSet != "named" || row.ipSet != "" {
			t.Errorf("want only the name set, got %+v", row)
		}
	})
	t.Run("over a static address of another set", func(t *testing.T) {
		address := newAddressRowSet("other", config.ConfigOff)
		w := newQUICFlowWorker(t, &named, &address)
		ch := subscribeRows(t)
		w.ProcessPacket(makeV4TCPPacket(buildClientHello(rowHost, 16, 0xAB), 1000))
		if row := lastConnectionRow(t, ch); row.sniSet != "named" || row.ipSet != "" {
			t.Errorf("want only the name set, got %+v", row)
		}
	})
}
