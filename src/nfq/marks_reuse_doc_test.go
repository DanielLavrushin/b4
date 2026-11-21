package nfq

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/florianl/go-nfqueue"
)

func TestTheMarksPageStatesWhatBsOwnDialsToAReuseSetGet(t *testing.T) {
	saved := wsReuseDiverts
	t.Cleanup(func() { wsReuseDiverts = saved })
	marks := []struct {
		mark uint32
		want int
		set  string
	}{
		{config.SelfDialMark, nfqueue.NfDrop, "Reuse"},
		{config.TelegramSetDialMark, nfqueue.NfDrop, "Telegram"},
		{config.SelfDialRelayMark, nfqueue.NfAccept, ""},
	}
	for _, c := range marks {
		t.Run(fmt.Sprintf("0x%x", c.mark), func(t *testing.T) {
			reuse := newFragmentingYTImagesSet()
			reuse.Id, reuse.Name = "reuse-own", "Reuse"
			reuse.Targets.DomainOnly = false
			reuse.Targets.DomainsToMatch = nil
			reuse.Targets.IpsToMatch = []string{"1.2.3.4"}
			reuse.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 1, Scale: 1}
			reuse.TCP.WSSizeReuse.Enabled = true
			wsReuseDiverts = func(id string) bool { return id == reuse.Id }
			telegram := newTelegramSet()
			w := newInjectingWorker(t, &reuse, &telegram)
			w.getConfig().System.MTProto.UpstreamSet = telegram.Id
			idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
			stubInjectRoute(t, idx["eth7"], true)
			ch, _ := log.GetConnectionHub().Subscribe()
			defer log.GetConnectionHub().Unsubscribe(ch)
			mark, owner := c.mark, uint32(0)
			if got := queuedVerdictFrom(t, w, ytimgHello(), &mark, &owner, idx["eth7"], 0); got != c.want {
				t.Fatalf("b4's own connection with mark 0x%x to an address of a diverted reuse set got verdict %d, want %d", c.mark, got, c.want)
			}
			if c.set != "" {
				if got := loggedSet(t, ch); got != c.set {
					t.Fatalf("b4's own connection with mark 0x%x to an address of a diverted reuse set went out with the strategy of %q, want %q", c.mark, got, c.set)
				}
			}
		})
	}
	for page, phrases := range map[string][2]string{
		"../../docs/docs/guides/marks.md":                                           {"b4's own connections", "always get that strategy"},
		"../../docs/i18n/ru/docusaurus-plugin-content-docs/current/guides/marks.md": {"Собственные соединения b4", "всегда получают эту стратегию"},
	} {
		raw, err := os.ReadFile(page)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		start := strings.Index(text, "{#reuse-wssize-connection}")
		if start < 0 {
			t.Fatalf("%s has no reuse section", page)
		}
		end := strings.Index(text[start:], "\n| ")
		if end < 0 {
			t.Fatalf("%s has no table in its reuse section", page)
		}
		section := strings.Join(strings.Fields(text[start:start+end]), " ")
		if strings.Contains(section, phrases[1]) {
			t.Errorf("%s says b4's own connections to a reuse set %q, though one marked 0x%x gets no strategy", page, phrases[1], config.SelfDialRelayMark)
		}
		at := strings.Index(section, phrases[0])
		if at < 0 {
			t.Errorf("%s has no sentence on b4's own connections to a reuse set's addresses", page)
			continue
		}
		for _, c := range marks {
			if want := fmt.Sprintf("`0x%x`", c.mark); !strings.Contains(section[at:], want) {
				t.Errorf("%s does not say what b4's own connections marked %s get: %s", page, want, section[at:])
			}
		}
	}
}
