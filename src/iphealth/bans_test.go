package iphealth

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	testFirstBan  = 24 * time.Hour
	testRepeatBan = 7 * 24 * time.Hour
)

type banClock struct {
	t time.Time
}

func (c *banClock) now() time.Time { return c.t }

func (c *banClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestBans(path string) (*BanRegistry, *banClock) {
	clock := &banClock{t: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)}
	r := NewBanRegistry(path)
	r.now = clock.now
	return r, clock
}

func strike(t *testing.T, r *BanRegistry, ip string, port int) (time.Time, bool) {
	t.Helper()
	until, repeat, ok := r.Strike(net.ParseIP(ip), port, "rr2---sn-ab5l6nr6.googlevideo.com", "1.googlevideo", testFirstBan, testRepeatBan)
	if !ok {
		t.Fatalf("strike on %s:%d was refused", ip, port)
	}
	return until, repeat
}

func testAddr(i int) string {
	return fmt.Sprintf("10.%d.%d.%d", i>>16&0xff, i>>8&0xff, i&0xff)
}

func TestBanFirstStrikeThenRepeatTier(t *testing.T) {
	r, clk := newTestBans("")
	ip := net.ParseIP("74.125.172.167")

	until, repeat := strike(t, r, "74.125.172.167", 443)
	if repeat || !until.Equal(clk.t.Add(testFirstBan)) {
		t.Fatalf("first strike: until %s repeat %v, want %s and the first-ban duration", until, repeat, clk.t.Add(testFirstBan))
	}
	if !r.Banned(ip, 443) || !r.BannedIP(ip) {
		t.Fatal("a struck address must be banned on its port")
	}
	if r.Banned(ip, 80) {
		t.Error("a ban is per port: port 80 of the same address must stay open")
	}

	clk.advance(testFirstBan + time.Minute)
	if r.Banned(ip, 443) || r.BannedIP(ip) {
		t.Fatal("the ban must end at until")
	}
	if got := r.List(); len(got) != 0 {
		t.Fatalf("List shows active bans only, got %+v", got)
	}

	until, repeat = strike(t, r, "74.125.172.167", 443)
	if !repeat || !until.Equal(clk.t.Add(testRepeatBan)) {
		t.Fatalf("a strike within the memory window after a ban ended: until %s repeat %v, want the repeat duration", until, repeat)
	}
	list := r.List()
	if len(list) != 1 || list[0].Strikes != 2 || !list[0].Repeat || !list[0].Since.Equal(clk.t) {
		t.Fatalf("repeat ban record: %+v", list)
	}
	if list[0].Key() != "74.125.172.167:443" || list[0].Host != "rr2---sn-ab5l6nr6.googlevideo.com" || list[0].Set != "1.googlevideo" {
		t.Fatalf("record keeps the address, host and set: %+v", list[0])
	}
}

func TestBanHistoryIsForgottenAfterTheMemoryWindow(t *testing.T) {
	r, clk := newTestBans("")
	strike(t, r, "74.125.172.167", 443)

	clk.advance(testFirstBan + banMemory - time.Minute)
	if n := r.Prune(); n != 0 || r.Len() != 1 {
		t.Fatalf("an ended ban is remembered for %s after it ends: pruned %d, left %d", banMemory, n, r.Len())
	}
	clk.advance(2 * time.Minute)
	if n := r.Prune(); n != 1 || r.Len() != 0 {
		t.Fatalf("past the memory window the record must go: pruned %d, left %d", n, r.Len())
	}

	if _, repeat := strike(t, r, "74.125.172.167", 443); repeat {
		t.Error("a strike after the memory window is a first ban again")
	}
}

func TestBanStrikeWhileActiveExtendsAndResetsPasses(t *testing.T) {
	r, clk := newTestBans("")
	start := clk.t
	strike(t, r, "74.125.172.167", 443)
	key := "74.125.172.167:443"
	r.RecordCheck(key, true)

	clk.advance(time.Hour)
	until, repeat := strike(t, r, "74.125.172.167", 443)
	if repeat || !until.Equal(clk.t.Add(testFirstBan)) {
		t.Fatalf("a strike on an active first ban extends it by the first-ban duration: until %s repeat %v", until, repeat)
	}
	got := r.List()[0]
	if got.Passes != 0 || got.Strikes != 2 || !got.Since.Equal(start) {
		t.Fatalf("extension resets passes, counts the strike and keeps since: %+v", got)
	}

	_, _, ok := r.Strike(net.ParseIP("74.125.172.167"), 443, "", "", time.Minute, time.Minute)
	if !ok || !r.List()[0].Until.Equal(until) {
		t.Fatalf("an extension never shortens a ban: %+v", r.List()[0])
	}
	if r.List()[0].Host != "rr2---sn-ab5l6nr6.googlevideo.com" {
		t.Error("a strike without a host keeps the recorded one")
	}
}

func TestBanLiftsAfterThreeSuccessfulChecksInARow(t *testing.T) {
	r, _ := newTestBans("")
	ip := net.ParseIP("74.125.172.167")
	strike(t, r, "74.125.172.167", 443)
	key := BanKey(ip, 443)

	for i, ok := range []bool{true, true, false, true, true} {
		if r.RecordCheck(key, ok) {
			t.Fatalf("check %d lifted the ban, a failure must reset the count", i)
		}
	}
	if got := r.List()[0].Passes; got != 2 {
		t.Fatalf("passes = %d, want 2 after the reset", got)
	}
	if !r.RecordCheck(key, true) {
		t.Fatal("the third success in a row must lift the ban")
	}
	if r.Banned(ip, 443) || r.Len() != 1 {
		t.Fatalf("a lifted ban stops applying but its history stays: banned %v, records %d", r.Banned(ip, 443), r.Len())
	}
	if _, repeat := strike(t, r, "74.125.172.167", 443); !repeat {
		t.Error("a strike after an early lift counts as a repeat")
	}
	if r.RecordCheck("not-a-key", true) || r.RecordCheck("1.1.1.1:443", true) {
		t.Error("a check for an unknown key changes nothing")
	}
}

func TestBanManualLiftAndClearForgetHistory(t *testing.T) {
	r, clk := newTestBans("")
	strike(t, r, "74.125.172.167", 443)

	if !r.Lift("74.125.172.167:443") {
		t.Fatal("lifting an active ban reports it")
	}
	if r.Len() != 0 {
		t.Fatal("a manual unban deletes the record")
	}
	if _, repeat := strike(t, r, "74.125.172.167", 443); repeat {
		t.Error("after a manual unban the next strike is a first ban")
	}
	if r.Lift("1.1.1.1:443") || r.Lift("garbage") {
		t.Error("lifting an unknown or malformed key reports nothing")
	}

	strike(t, r, "64.233.161.198", 443)
	strike(t, r, "2a00:1450:4010:c05::c6", 443)
	if !r.Banned(net.ParseIP("2a00:1450:4010:c05:0:0:0:c6"), 443) || !r.Lift("[2a00:1450:4010:c05::c6]:443") {
		t.Fatal("IPv6 bans are keyed by the canonical bracketed form")
	}
	strike(t, r, "2a00:1450:4010:c05::c6", 443)
	clk.advance(testFirstBan + time.Second)
	strike(t, r, "64.233.161.198", 80)
	if n := r.Clear(); n != 1 || r.Len() != 0 {
		t.Fatalf("Clear deletes every record and reports the bans in effect: cleared %d, left %d", n, r.Len())
	}
}

func TestBanCapEvictsEndedRecordsFirstThenTheEarliestActive(t *testing.T) {
	r, clk := newTestBans("")
	start := clk.t
	for i := 0; i < maxBans; i++ {
		strike(t, r, testAddr(i), 443)
		clk.advance(20 * time.Second)
	}
	if r.Len() != maxBans {
		t.Fatalf("records = %d, want the cap of %d", r.Len(), maxBans)
	}

	clk.t = start.Add(testFirstBan + 30*time.Second)
	present := func(i int) bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.findLocked(testAddr(i), 443) != nil
	}

	strike(t, r, "192.0.2.1", 443)
	if r.Len() != maxBans || present(0) || !present(1) {
		t.Fatalf("the oldest ended record goes first: records %d, #0 %v, #1 %v", r.Len(), present(0), present(1))
	}
	strike(t, r, "192.0.2.2", 443)
	if present(1) || !present(2) {
		t.Fatal("then the next ended record")
	}
	strike(t, r, "192.0.2.3", 443)
	if present(2) || !present(3) || r.Len() != maxBans {
		t.Fatal("with nothing ended left, the active ban that ends first goes")
	}
	if !r.Banned(net.ParseIP("192.0.2.3"), 443) {
		t.Fatal("the new ban is in place after the eviction")
	}
}

func TestBanBurstGuardRefusesNewBans(t *testing.T) {
	r, clk := newTestBans("")
	for i := 0; i < banBurstLimit; i++ {
		strike(t, r, testAddr(i), 443)
	}
	extra := net.ParseIP("192.0.2.10")
	if _, _, ok := r.Strike(extra, 443, "", "", testFirstBan, testRepeatBan); ok || r.Banned(extra, 443) {
		t.Fatalf("ban %d within %s must be refused", banBurstLimit+1, banBurstWindow)
	}
	if _, _, ok := r.Strike(net.ParseIP(testAddr(0)), 443, "", "", testFirstBan, testRepeatBan); !ok {
		t.Error("extending a ban already in place is not a new ban and is not refused")
	}

	clk.advance(banBurstWindow + time.Second)
	if _, _, ok := r.Strike(extra, 443, "", "", testFirstBan, testRepeatBan); !ok || !r.Banned(extra, 443) {
		t.Fatal("once the window has passed, new bans are taken again")
	}
}

func TestBanDueReturnsTheOldestChecksFirst(t *testing.T) {
	r, clk := newTestBans("")
	strike(t, r, "74.125.172.166", 443)
	clk.advance(time.Minute)
	strike(t, r, "74.125.172.167", 443)

	if due := r.Due(5*time.Minute, 8); len(due) != 0 {
		t.Fatalf("a ban is not due before the interval since its strike: %+v", due)
	}
	clk.advance(5 * time.Minute)
	due := r.Due(5*time.Minute, 8)
	if len(due) != 2 || due[0].Key() != "74.125.172.166:443" {
		t.Fatalf("both are due, the one checked longest ago first: %+v", due)
	}
	if due := r.Due(5*time.Minute, 1); len(due) != 1 || due[0].Key() != "74.125.172.166:443" {
		t.Fatalf("max caps the batch: %+v", due)
	}
	r.RecordCheck("74.125.172.166:443", false)
	if due := r.Due(5*time.Minute, 8); len(due) != 1 || due[0].Key() != "74.125.172.167:443" {
		t.Fatalf("a check moves the ban to the back: %+v", due)
	}
}

func TestBanPersistenceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), BanFileName)
	r, clk := newTestBans(path)
	strike(t, r, "74.125.172.167", 443)
	clk.advance(testFirstBan + time.Hour)
	strike(t, r, "74.125.172.167", 443)
	strike(t, r, "2a00:1450:4010:c05::c6", 443)
	if err := r.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var file struct {
		Version int `json:"version"`
		Bans    []struct {
			IP    string `json:"ip"`
			Since string `json:"since"`
			Until string `json:"until"`
		} `json:"bans"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("the file is not JSON: %v", err)
	}
	if file.Version != 1 || len(file.Bans) != 2 {
		t.Fatalf("file = version %d with %d bans, want version 1 with 2", file.Version, len(file.Bans))
	}
	for _, b := range file.Bans {
		if _, err := time.Parse(time.RFC3339, b.Until); err != nil {
			t.Errorf("until %q is not RFC3339: %v", b.Until, err)
		}
	}

	loaded := NewBanRegistry(path)
	loaded.now = clk.now
	if err := loaded.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	want, got := r.List(), loaded.List()
	if len(got) != len(want) {
		t.Fatalf("loaded %d bans, want %d", len(got), len(want))
	}
	for i := range want {
		w, g := want[i], got[i]
		if g.Key() != w.Key() || g.Host != w.Host || g.Set != w.Set || g.Strikes != w.Strikes || g.Repeat != w.Repeat ||
			!g.Since.Equal(w.Since) || !g.Until.Equal(w.Until) || !g.Checked.Equal(w.Checked) {
			t.Errorf("ban %d after a restart = %+v, want %+v", i, g, w)
		}
	}
	if _, repeat := strike(t, loaded, "74.125.172.167", 443); !repeat {
		t.Error("an active ban survives the restart with its tier")
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := r.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Flush writes only when something changed")
	}
	if err := NewBanRegistry("").Flush(); err != nil {
		t.Errorf("a registry without a path keeps bans in memory only: %v", err)
	}
}

func TestBanLoadMovesACorruptFileAside(t *testing.T) {
	path := filepath.Join(t.TempDir(), BanFileName)
	if err := os.WriteFile(path, []byte("{\"version\":1,\"bans\":["), 0644); err != nil {
		t.Fatal(err)
	}
	r, _ := newTestBans(path)
	if err := r.Load(); err != nil {
		t.Fatalf("a corrupt file must not fail the start: %v", err)
	}
	if r.Len() != 0 {
		t.Fatalf("the registry starts empty, has %d", r.Len())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the corrupt file must be moved away from its name")
	}
	if _, err := os.Stat(path + ".corrupt"); err != nil {
		t.Errorf("the corrupt file must be kept aside: %v", err)
	}

	strike(t, r, "74.125.172.167", 443)
	if err := r.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	fresh := NewBanRegistry(path)
	if err := fresh.Load(); err != nil || fresh.Len() != 1 {
		t.Fatalf("a fresh file is written in its place: err %v, bans %d", err, fresh.Len())
	}
}

func TestBanLoadDropsExpiredAndInvalidRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), BanFileName)
	r, clk := newTestBans(path)
	until := clk.t.Add(time.Hour).Format(time.RFC3339)
	ancient := clk.t.Add(-banMemory - time.Hour).Format(time.RFC3339)
	body := `{"version":1,"bans":[
		{"ip":"74.125.172.167","port":443,"strikes":1,"since":"` + until + `","until":"` + until + `"},
		{"ip":"74.125.172.168","port":443,"strikes":1,"since":"` + ancient + `","until":"` + ancient + `"},
		{"ip":"not-an-ip","port":443,"strikes":1,"until":"` + until + `"},
		{"ip":"74.125.172.169","port":0,"strikes":1,"until":"` + until + `"}
	]}`
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if r.Len() != 1 || !r.Banned(net.ParseIP("74.125.172.167"), 443) {
		t.Fatalf("only the valid active record is kept, have %d", r.Len())
	}
	if err := r.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	fresh := NewBanRegistry(path)
	fresh.now = clk.now
	if err := fresh.Load(); err != nil || fresh.Len() != 1 {
		t.Fatalf("the cleaned list is written back: err %v, bans %d", err, fresh.Len())
	}
}

func TestBanRegistryNilAndEmptyAreSafe(t *testing.T) {
	var r *BanRegistry
	ip := net.ParseIP("1.2.3.4")
	if r.Banned(ip, 443) || r.BannedIP(ip) || r.Len() != 0 || r.List() != nil || r.Due(0, 8) != nil {
		t.Error("a nil registry bans nothing")
	}
	if _, _, ok := r.Strike(ip, 443, "", "", time.Hour, time.Hour); ok {
		t.Error("a nil registry takes no strikes")
	}
	if r.Lift("1.2.3.4:443") || r.Clear() != 0 || r.Prune() != 0 || r.RecordCheck("1.2.3.4:443", true) {
		t.Error("a nil registry changes nothing")
	}
	if err := r.Load(); err != nil {
		t.Error(err)
	}
	if err := r.Flush(); err != nil {
		t.Error(err)
	}

	empty := NewBanRegistry("")
	if empty.Banned(ip, 443) || empty.Banned(nil, 443) {
		t.Error("an empty registry bans nothing")
	}
	if _, _, ok := empty.Strike(ip, 0, "", "", time.Hour, time.Hour); ok {
		t.Error("a strike needs a port")
	}
}

func TestBanKeyRoundTrip(t *testing.T) {
	for _, c := range []struct {
		ip   string
		port int
		key  string
	}{
		{"74.125.172.167", 443, "74.125.172.167:443"},
		{"2a00:1450:4010:0c05:0000:0000:0000:00c6", 8443, "[2a00:1450:4010:c05::c6]:8443"},
	} {
		key := BanKey(net.ParseIP(c.ip), c.port)
		if key != c.key {
			t.Errorf("BanKey(%s, %d) = %q, want %q", c.ip, c.port, key, c.key)
		}
		ip, port, ok := ParseBanKey(key)
		if !ok || !ip.Equal(net.ParseIP(c.ip)) || port != c.port {
			t.Errorf("ParseBanKey(%q) = %v %d %v", key, ip, port, ok)
		}
	}
	for _, bad := range []string{"", "1.2.3.4", "1.2.3.4:0", "1.2.3.4:70000", "example.com:443", "[::1]:x"} {
		if _, _, ok := ParseBanKey(bad); ok {
			t.Errorf("ParseBanKey(%q) accepted a malformed key", bad)
		}
	}
	if got := BanFilePath("/etc/b4/b4.json"); got != "/etc/b4/"+BanFileName {
		t.Errorf("BanFilePath = %q, want the file beside the config", got)
	}
	if BanFilePath("") != "" {
		t.Error("no config path, no ban file")
	}
}
