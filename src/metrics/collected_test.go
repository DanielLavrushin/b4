package metrics

import "testing"

func TestCollectedMetricsRevisionAndIndependentSnapshots(t *testing.T) {
	m := &MetricsCollector{}
	if list := m.collected.list(); list.Items == nil || len(list.Items) != 0 {
		t.Fatal("empty list is null")
	}
	a := CollectedEntry{IP: "1.2.3.4", Host: "first.example.org", Since: 1000}
	b := CollectedEntry{IP: "8.8.8.8", Host: "second.example.org", Since: 2000}
	m.UpdateCollectedIPs([]CollectedEntry{a, b})
	list := m.collected.list()
	if list.Rev != 1 || list.Items[0] != b {
		t.Fatal("bad list", list)
	}
	m.UpdateCollectedIPs([]CollectedEntry{b, a})
	if m.collected.currentRev() != 1 {
		t.Fatal("same entries advanced revision")
	}
	list.Items[0].Host = "changed"
	if m.collected.list().Items[0].Host == "changed" {
		t.Fatal("list aliases storage")
	}
	full := m.Hello()
	if full.Collected == nil || len(full.Collected.Items) != 2 {
		t.Fatal("hello missing collected entries")
	}
	sent := &SentRevs{}
	if tick := m.Tick(sent); tick.Collected == nil {
		t.Fatal("first tick missed update")
	}
	if tick := m.Tick(sent); tick.Collected != nil {
		t.Fatal("unchanged list repeated")
	}
	m.UpdateCollectedIPs(nil)
	if tick := m.Tick(sent); tick.Collected == nil || tick.Collected.Items == nil || len(tick.Collected.Items) != 0 {
		t.Fatal("clear not transmitted")
	}
}

func TestCollectedRefusalsBumpTheRevision(t *testing.T) {
	m := &MetricsCollector{}
	m.UpdateCollectedRefused(2)
	sent := &SentRevs{}
	if tick := m.Tick(sent); tick.Collected == nil || tick.Collected.Refused != 2 {
		t.Fatalf("a refusal count must reach the next frame, got %+v", tick.Collected)
	}
	m.UpdateCollectedRefused(2)
	if tick := m.Tick(sent); tick.Collected != nil {
		t.Fatal("an unchanged refusal count sent the list again")
	}
	m.UpdateCollectedRefused(3)
	if tick := m.Tick(sent); tick.Collected == nil || tick.Collected.Refused != 3 {
		t.Fatal("a new refusal did not reach the frame")
	}
}
