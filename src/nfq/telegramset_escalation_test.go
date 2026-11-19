package nfq

import (
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/florianl/go-nfqueue"
)

func TestTheSetTheTelegramSetEscalatesToWaitsForTheEscalation(t *testing.T) {
	for _, c := range []struct {
		name       string
		escalated  bool
		ipPriority bool
		ipVersion  string
		want       int
		wantSet    string
		why        string
	}{
		{"before the escalation", false, false, "", nfqueue.NfDrop, "Telegram", "the set the Telegram set escalates to names the host, but only an escalation may hand it the connection"},
		{"before the escalation, with IP priority on", false, true, "", nfqueue.NfDrop, "Telegram", "the order of the sets must not decide it either"},
		{"before the escalation, with an IPv6 filter", false, false, "6", nfqueue.NfDrop, "Telegram", "the Telegram set takes its connections whatever their family, so its family filter must not hand them on"},
		{"after the escalation", true, false, "", nfqueue.NfAccept, "YT images", "once the host escalated, the set it escalated to takes the connection"},
	} {
		t.Run(c.name, func(t *testing.T) {
			backup := newPassiveSet()
			telegram := newTelegramSet()
			telegram.Escalate.To = backup.Id
			telegram.Targets.IPPriority = c.ipPriority
			telegram.Targets.IPVersion = c.ipVersion
			w := newInjectingWorker(t, &backup, &telegram)
			w.getConfig().System.MTProto.UpstreamSet = telegram.Id
			if c.escalated {
				w.destState.SetEscalation("i.ytimg.com", backup.Id, escalateReasonStall, time.Hour)
			}

			ch, _ := log.GetConnectionHub().Subscribe()
			defer log.GetConnectionHub().Unsubscribe(ch)

			if got := telegramSetVerdict(t, w, config.TelegramSetDialMark); got != c.want {
				t.Fatalf("verdict %d, want %d: %s", got, c.want, c.why)
			}
			if got := loggedSet(t, ch); got != c.wantSet {
				t.Fatalf("connection logged for set %q, want %q: %s", got, c.wantSet, c.why)
			}
		})
	}
}

func TestAnAddressTheEscalationTargetLearnedStaysWithTheTelegramSet(t *testing.T) {
	backup := newPassiveSet()
	backup.Targets.DomainOnly = false
	telegram := newTelegramSet()
	telegram.Escalate.To = backup.Id
	w := newInjectingWorker(t, &backup, &telegram)
	w.getConfig().System.MTProto.UpstreamSet = telegram.Id
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
	stubInjectRoute(t, idx["eth7"], true)

	ch, _ := log.GetConnectionHub().Subscribe()
	defer log.GetConnectionHub().Unsubscribe(ch)

	unmarked := config.SelfDialMark
	lan := makeV4TCPPacketFromPort(buildClientHello("i.ytimg.com", 16, 0xAB), 1000, 2000, 0x18)
	if got := queuedVerdict(t, w, lan, &unmarked, idx["eth7"], 0); got != nfqueue.NfAccept {
		t.Fatalf("verdict %d for the unmarked connection, want the passive set's accept", got)
	}
	if got := loggedSet(t, ch); got != backup.Name {
		t.Fatalf("the unmarked connection went to %q, want %q, which learns the address", got, backup.Name)
	}

	marked := config.TelegramSetDialMark
	hello := makeV4TCPPacketFromPort(buildClientHello("kws2.web.telegram.org", 16, 0xAB), 1000, 3000, 0x18)
	if got := queuedVerdict(t, w, hello, &marked, idx["eth7"], 0); got != nfqueue.NfDrop {
		t.Fatalf("verdict %d: an address another connection taught the set the Telegram set escalates to must not take the Telegram set's own connection", got)
	}
	if got := loggedSet(t, ch); got != telegram.Name {
		t.Fatalf("connection logged for set %q, want %q", got, telegram.Name)
	}
}
