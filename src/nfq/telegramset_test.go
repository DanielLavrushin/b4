package nfq

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/florianl/go-nfqueue"
)

func newTelegramSet() config.SetConfig {
	set := newFragmentingYTImagesSet()
	set.Id, set.Name = "telegram", "Telegram"
	set.Targets.DomainOnly = false
	set.Targets.DomainsToMatch = nil
	return set
}

func telegramSetVerdict(t *testing.T, w *Worker, mark uint32) int {
	t.Helper()
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
	stubInjectRoute(t, idx["eth7"], true)
	return queuedVerdict(t, w, ytimgHello(), &mark, idx["eth7"], 0)
}

func TestTheTelegramSetTakesBsOwnTelegramConnections(t *testing.T) {
	for _, c := range []struct {
		name     string
		chosen   string
		disabled bool
		mark     uint32
		want     int
		wantSet  string
		why      string
	}{
		{"a marked connection another set targets", "telegram", false, config.TelegramSetDialMark, nfqueue.NfDrop, "Telegram", "the Telegram set's strategy applies whatever the other sets target"},
		{"the self-dial mark alone", "telegram", false, config.SelfDialMark, nfqueue.NfAccept, "YT images", "a connection without the Telegram-set bit is matched as before"},
		{"a disabled Telegram set", "telegram", true, config.TelegramSetDialMark, nfqueue.NfAccept, "YT images", "a disabled set handles nothing, so ordinary matching takes over"},
		{"no Telegram set chosen", "", false, config.TelegramSetDialMark, nfqueue.NfAccept, "YT images", "without the setting the bit means nothing"},
		{"the chosen set no longer exists", "gone", false, config.TelegramSetDialMark, nfqueue.NfAccept, "YT images", "a missing set handles nothing"},
	} {
		t.Run(c.name, func(t *testing.T) {
			other := newPassiveSet()
			telegram := newTelegramSet()
			telegram.Enabled = !c.disabled
			w := newInjectingWorker(t, &other, &telegram)
			w.getConfig().System.MTProto.UpstreamSet = c.chosen

			ch, _ := log.GetConnectionHub().Subscribe()
			defer log.GetConnectionHub().Unsubscribe(ch)

			if got := telegramSetVerdict(t, w, c.mark); got != c.want {
				t.Fatalf("verdict %d, want %d: %s", got, c.want, c.why)
			}
			if got := loggedSet(t, ch); got != c.wantSet {
				t.Fatalf("connection logged for set %q, want %q: %s", got, c.wantSet, c.why)
			}
			replyTo := "no set"
			if reply := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443); reply != nil {
				replyTo = reply.Name
			}
			if replyTo != c.wantSet {
				t.Fatalf("replies on the connection go to %q, want %q: the server's packets carry no mark, so they must follow the set the connection went out with", replyTo, c.wantSet)
			}
		})
	}
}

func TestTheTelegramSetNeedsNoTargets(t *testing.T) {
	telegram := newTelegramSet()
	w := newInjectingWorker(t, &telegram)
	w.getConfig().System.MTProto.UpstreamSet = telegram.Id

	if got := telegramSetVerdict(t, w, config.TelegramSetDialMark); got != nfqueue.NfDrop {
		t.Fatalf("verdict %d: a set with no targets must still take the connections b4 marks for it", got)
	}
	if got := telegramSetVerdict(t, w, config.SelfDialMark); got != nfqueue.NfAccept {
		t.Fatalf("verdict %d: an unmarked connection must not reach a set that targets nothing", got)
	}
}

func TestTheTelegramSetKeepsTheSetItEscalatesTo(t *testing.T) {
	telegram := newTelegramSet()
	telegram.Escalate.To = "yt-images"
	backup := newPassiveSet()
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{&backup, &telegram}

	keep := telegramSetScope(&cfg, &telegram)
	if !keep(&telegram) {
		t.Fatal("the Telegram set must stay a candidate for its own connections")
	}
	if keep(&backup) != escalationReaches(&cfg, &telegram, backup.Id) {
		t.Fatal("the set the Telegram set escalates to must stay a candidate exactly when escalation reaches it")
	}
	stranger := newPassiveSet()
	stranger.Id = "stranger"
	if keep(&stranger) {
		t.Fatal("a set the Telegram set does not escalate to must not take its connections")
	}
}
