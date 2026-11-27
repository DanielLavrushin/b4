package notify

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/store"
)

func (f *fixture) group(t *testing.T, uploader string, brought []string, linked ...string) string {
	t.Helper()
	f.seq++
	id := "01ARZ3NDEKTSV4RRFFQ69G5G" + strconv.Itoa(10+f.seq)
	v := &store.GroupVersion{Title: "YouTube", Status: hubwire.SetStatusPending, UploaderHMAC: uploader, ASNObserved: "64500", CountryObserved: "RU", CreatedAt: f.clock, UpdatedAt: f.clock}
	for _, setID := range brought {
		v.Members = append(v.Members, store.GroupMember{SetID: setID, SetVersion: 1, Brought: true})
	}
	for _, setID := range linked {
		v.Members = append(v.Members, store.GroupMember{SetID: setID, SetVersion: 1})
	}
	err := f.store.Update(context.Background(), func(tx *store.Tx) error {
		return tx.CreateGroup(context.Background(), store.Group{ID: id, AuthorHMAC: uploader, CreatedAt: f.clock, UpdatedAt: f.clock}, v)
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestAGroupIsOneLineInsteadOfOnePerMember(t *testing.T) {
	f := newFixture(t)
	f.save(t, f.update(EventShare, EventReport))
	single, _ := f.share(t, "author", hubwire.SetStatusPending)
	first, _ := f.share(t, "author", hubwire.SetStatusPending)
	second, _ := f.share(t, "author", hubwire.SetStatusPending)
	other, _ := f.share(t, "other", hubwire.SetStatusActive)
	g := f.group(t, "author", []string{first, second}, other)
	linkedOnly := f.group(t, "author", nil, other)

	f.round()
	msg := telegramBody(t, f.tg.last(t)).Text
	for _, want := range []string{
		"b4hub: 1 new set, 2 new groups\n",
		"New sets in the queue (1):\n- Set 1 (" + single + " v1), AS64500 RU",
		"New groups in the queue (2):\n- YouTube (group " + g + " v1, 3 sets), AS64500 RU\n- YouTube (group " + linkedOnly + " v1, 1 set), AS64500 RU",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("telegram text lacks %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "Set 2") || strings.Contains(msg, "Set 3") {
		t.Errorf("the sets a group brought are not announced one by one:\n%s", msg)
	}
	p := verifySignature(t, f.hook.last(t), hookSecret)
	if p.Counts[EventShare] != 1 || p.Counts[EventGroup] != 2 || len(p.Events) != 3 {
		t.Fatalf("webhook counts: %+v", p)
	}
	ev := p.Events[1]
	if ev.Kind != EventGroup || ev.SetID != "" || ev.GroupID != g || ev.Members != 3 || ev.URL != "https://hub.example/admin/groups/"+g+"/v/1" {
		t.Fatalf("group event: %+v", ev)
	}

	f.clock = f.clock.Add(time.Hour)
	f.save(t, f.update(EventReport))
	f.group(t, "author", nil, other)
	f.save(t, f.update(EventShare, EventReport))
	f.round()
	if f.tg.count() != 1 {
		t.Fatal("a group made while shares were off must not be sent once they are on again")
	}
}

func TestHidingAnAnnouncedGroupNeverAnnouncesItsMembers(t *testing.T) {
	for _, later := range []bool{false, true} {
		t.Run(strconv.FormatBool(later), func(t *testing.T) {
			f := newFixture(t)
			f.save(t, f.update(EventShare))
			first, _ := f.share(t, "author", hubwire.SetStatusPending)
			second, _ := f.share(t, "author", hubwire.SetStatusPending)
			g := f.group(t, "author", []string{first, second})
			if later {
				f.share(t, "other", hubwire.SetStatusPending)
			}
			f.round()
			if msg := telegramBody(t, f.tg.last(t)).Text; strings.Contains(msg, "Set 1") || strings.Contains(msg, "Set 2") || !strings.Contains(msg, "1 new group") {
				t.Fatalf("the group is announced once without its members:\n%s", msg)
			}
			sent := f.tg.count()

			f.clock = f.clock.Add(time.Hour)
			if err := f.store.Update(context.Background(), func(tx *store.Tx) error {
				return tx.SetGroupStatus(context.Background(), g, 1, hubwire.SetStatusHidden, "later", f.clock)
			}); err != nil {
				t.Fatal(err)
			}
			f.round()
			if f.tg.count() != sent {
				t.Fatalf("a hidden group's members are not announced as new sets:\n%s", telegramBody(t, f.tg.last(t)).Text)
			}
		})
	}
}

func TestRussianGroupLine(t *testing.T) {
	got := catalogFor(LanguageRU).line(store.NotifyEvent{Kind: EventGroup, SetID: "G1", Version: 2, Title: "YouTube", Members: 3})
	if got != "- YouTube (группа G1 v2, 3 сета)" {
		t.Fatalf("russian group line: %q", got)
	}
}
