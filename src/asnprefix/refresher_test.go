package asnprefix

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func useClock(t *testing.T) *testClock {
	t.Helper()
	clk := &testClock{t: time.Now()}
	prev := nowFn
	nowFn = clk.Now
	t.Cleanup(func() { nowFn = prev })
	return clk
}

func asnConfig(sets ...[]string) *config.Config {
	cfg := config.NewConfig()
	for i, asns := range sets {
		s := config.NewSetConfig()
		s.Id = "set" + string(rune('a'+i))
		s.Name = "Set " + string(rune('A'+i))
		s.Enabled = i%2 == 0
		s.Targets.ASNs = asns
		cfg.Sets = append(cfg.Sets, &s)
	}
	return &cfg
}

type changeLog struct {
	mu    sync.Mutex
	calls [][]string
}

func (c *changeLog) record(ids []string) {
	c.mu.Lock()
	c.calls = append(c.calls, append([]string{}, ids...))
	c.mu.Unlock()
}

func (c *changeLog) all() [][]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([][]string{}, c.calls...)
}

func testRefresher(cfg *config.Config, clk *testClock, changes *changeLog) *refresher {
	r := newRefresher(func() *config.Config { return cfg }, changes.record)
	r.now = clk.Now
	return r
}

func TestReferencedASNsCoversEverySetEnabledOrNot(t *testing.T) {
	cfg := asnConfig([]string{"AS15169", "62041"}, []string{"15169", "AS13335"}, nil)
	got := ReferencedASNs(cfg)
	if want := []string{"13335", "15169", "62041"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if ReferencedASNs(nil) != nil {
		t.Error("nil config")
	}
}

func TestPassResolvesMissingASNsAndReportsOnlyChangedOnes(t *testing.T) {
	s := useStore(t)
	clk := useClock(t)
	f := useFake(t, realRIPE(t))
	changes := &changeLog{}
	r := testRefresher(asnConfig([]string{"62041"}, []string{"AS15169"}), clk, changes)

	r.pass(context.Background())
	if got := changes.all(); len(got) != 1 || !slices.Equal(got[0], []string{"15169", "62041"}) {
		t.Fatalf("the first resolution of both is a change, reported once: %v", got)
	}
	if s.Get("15169") == nil || s.Get("62041") == nil {
		t.Fatal("both are stored")
	}

	r.pass(context.Background())
	if f.count("ris-prefixes", "AS62041") != 1 || len(changes.all()) != 1 {
		t.Fatalf("fresh entries are not fetched again: %v %v", f.hits, changes.all())
	}

	clk.Advance(StaleAfter + time.Minute)
	r.pass(context.Background())
	if f.count("ris-prefixes", "AS62041") != 2 || f.count("ris-prefixes", "AS15169") != 2 {
		t.Fatalf("stale entries are fetched: %v", f.hits)
	}
	if got := changes.all(); len(got) != 1 {
		t.Fatalf("an unchanged prefix list is not a change: %v", got)
	}
	if got := s.Get("62041"); time.Unix(got.UpdatedAt, 0).Before(clk.Now().Add(-time.Minute)) {
		t.Errorf("the refresh stamps updated_at: %d", got.UpdatedAt)
	}
}

func TestPassUpgradesALegacyBrowserEntryWithoutAShrinkWait(t *testing.T) {
	s := useStore(t)
	clk := useClock(t)
	useFake(t, realRIPE(t))
	_ = s.Put(&config.AsnInfo{ID: "62041", Name: "legacy", Prefixes: []string{"91.0.0.0/8"}})
	changes := &changeLog{}
	r := testRefresher(asnConfig([]string{"62041"}), clk, changes)

	r.pass(context.Background())
	got := s.Get("62041")
	if got.Source != config.AsnSourceRIPEstat || len(got.Prefixes) != 6 || got.Name == "legacy" {
		t.Fatalf("a legacy entry is replaced by the server copy at once: %+v", got)
	}
	if len(changes.all()) != 1 {
		t.Fatalf("that is a change: %v", changes.all())
	}
}

func TestPassBacksOffOnFailureAndKeepsItWhenWoken(t *testing.T) {
	s := useStore(t)
	clk := useClock(t)
	var healthy atomic.Bool
	f := useFake(t, func(w http.ResponseWriter, r *http.Request, call, resource string) {
		if !healthy.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		realRIPE(t)(w, r, call, resource)
	})
	old := &config.AsnInfo{ID: "62041", Name: "Telegram", Prefixes: []string{"91.108.4.0/22"}, UpdatedAt: clk.Now().Add(-48 * time.Hour).Unix(), Source: config.AsnSourceRIPEstat}
	_ = s.Put(old)
	changes := &changeLog{}
	r := testRefresher(asnConfig([]string{"62041"}), clk, changes)

	if wait := r.pass(context.Background()); wait != 30*time.Second {
		t.Fatalf("the first retry is in 30s, got %v", wait)
	}
	if f.count("ris-prefixes", "AS62041") != 1 {
		t.Fatalf("one attempt: %v", f.hits)
	}
	if got := s.Get("62041"); !slices.Equal(got.Prefixes, old.Prefixes) {
		t.Fatalf("the last good copy is kept: %+v", got)
	}

	for i := 0; i < 3; i++ {
		r.pass(context.Background())
	}
	if f.count("ris-prefixes", "AS62041") != 1 {
		t.Fatalf("a wake-up inside the backoff does not fetch: %v", f.hits)
	}

	clk.Advance(31 * time.Second)
	if wait := r.pass(context.Background()); wait != time.Minute {
		t.Fatalf("the backoff doubles, got %v", wait)
	}
	clk.Advance(61 * time.Second)
	if wait := r.pass(context.Background()); wait != 2*time.Minute {
		t.Fatalf("and doubles again, got %v", wait)
	}
	if f.count("ris-prefixes", "AS62041") != 3 || len(changes.all()) != 0 {
		t.Fatalf("three attempts, no change: %v %v", f.hits, changes.all())
	}

	healthy.Store(true)
	clk.Advance(2*time.Minute + time.Second)
	r.pass(context.Background())
	if got := changes.all(); len(got) != 1 || got[0][0] != "62041" {
		t.Fatalf("the recovery is a change: %v", got)
	}
	if len(r.state) != 0 || LastError("62041") != "" {
		t.Fatalf("the backoff and the error are cleared: %+v %q", r.state, LastError("62041"))
	}
}

func TestPassDoesNotRefetchWhileTheClockIsBehind(t *testing.T) {
	s := useStore(t)
	clk := useClock(t)
	f := useFake(t, realRIPE(t))
	_ = s.Put(&config.AsnInfo{ID: "62041", Name: "Telegram", Prefixes: []string{"91.108.4.0/22"}, UpdatedAt: clk.Now().Unix(), Source: config.AsnSourceRIPEstat})
	clk.Advance(-50 * 365 * 24 * time.Hour)
	r := testRefresher(asnConfig([]string{"62041"}), clk, &changeLog{})
	if wait := r.pass(context.Background()); wait != passInterval {
		t.Fatalf("the next pass is at most an hour away: %v", wait)
	}
	if len(f.hits) != 0 {
		t.Fatalf("a router booting with its clock at 1970 does not refetch its cache: %v", f.hits)
	}
}

func TestRetryDelayIsCappedAtAnHour(t *testing.T) {
	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 32 * time.Minute, time.Hour, time.Hour}
	for i, w := range want {
		if got := retryDelay(i + 1); got != w {
			t.Errorf("retryDelay(%d) = %v, want %v", i+1, got, w)
		}
	}
	if retryDelay(500) != time.Hour {
		t.Error("capped")
	}
}

func TestPassAcceptsAShrinkOnlyAfterThreeConsecutiveFetches(t *testing.T) {
	s := useStore(t)
	clk := useClock(t)
	var shrunk atomic.Bool
	shrunk.Store(true)
	small := strings.Replace(string(fixture(t, "ris_prefixes_AS62041.json")), `"91.108.4.0/23","149.154.163.0/24"`, `"91.108.4.0/23"],"x":["149.154.163.0/24"`, 1)
	f := useFake(t, func(w http.ResponseWriter, r *http.Request, call, resource string) {
		if call == "ris-prefixes" && shrunk.Load() {
			writeJSON(w, 200, []byte(small))
			return
		}
		realRIPE(t)(w, r, call, resource)
	})
	changes := &changeLog{}
	r := testRefresher(asnConfig([]string{"62041"}), clk, changes)

	r.pass(context.Background())
	shrunk.Store(false)
	full := s.Get("62041")
	if full == nil {
		t.Fatal("seeded")
	}
	_ = s.Put(&config.AsnInfo{ID: "62041", Name: full.Name, Prefixes: config.SanitizeASNPrefixes(fullAS62041(t)), UpdatedAt: clk.Now().Add(-StaleAfter - time.Minute).Unix(), Source: config.AsnSourceRIPEstat})
	known := s.Get("62041").Prefixes
	changes = &changeLog{}
	r.onChange = changes.record
	shrunk.Store(true)

	r.pass(context.Background())
	if got := s.Get("62041"); !slices.Equal(got.Prefixes, known) {
		t.Fatalf("the first shrink is not accepted: %v", got.Prefixes)
	}
	r.pass(context.Background())
	if f.count("ris-prefixes", "AS62041") != 2 {
		t.Fatalf("a wake-up right after does not refetch: %v", f.hits)
	}
	clk.Advance(time.Hour + time.Second)
	r.pass(context.Background())
	if got := s.Get("62041"); !slices.Equal(got.Prefixes, known) || len(changes.all()) != 0 {
		t.Fatalf("the second shrink is not accepted: %v %v", got.Prefixes, changes.all())
	}
	clk.Advance(time.Hour + time.Second)
	r.pass(context.Background())
	got := s.Get("62041")
	if slices.Equal(got.Prefixes, known) {
		t.Fatalf("the third consecutive shrink is accepted: %v", got.Prefixes)
	}
	if c := changes.all(); len(c) != 1 || c[0][0] != "62041" {
		t.Fatalf("and reported as a change: %v", c)
	}
}

func TestPassCountsOnlyTheSameShrunkListAsAConfirmation(t *testing.T) {
	s := useStore(t)
	clk := useClock(t)
	body := string(fixture(t, "ris_prefixes_AS62041.json"))
	cut := func(after, next string) string {
		return strings.Replace(body, `"`+after+`","`+next+`"`, `"`+after+`"],"x":["`+next+`"`, 1)
	}
	lists := []string{
		cut("91.108.4.0/23", "149.154.163.0/24"),
		cut("149.154.163.0/24", "149.154.166.0/24"),
		cut("149.154.166.0/24", "95.161.64.0/21"),
	}
	var current atomic.Value
	current.Store(lists[0])
	useFake(t, func(w http.ResponseWriter, r *http.Request, call, resource string) {
		if call == "ris-prefixes" {
			writeJSON(w, 200, []byte(current.Load().(string)))
			return
		}
		realRIPE(t)(w, r, call, resource)
	})
	_ = s.Put(&config.AsnInfo{ID: "62041", Name: "Telegram", Prefixes: config.SanitizeASNPrefixes(fullAS62041(t)), UpdatedAt: clk.Now().Add(-StaleAfter - time.Minute).Unix(), Source: config.AsnSourceRIPEstat})
	known := s.Get("62041").Prefixes
	changes := &changeLog{}
	r := testRefresher(asnConfig([]string{"62041"}), clk, changes)

	for i, list := range lists {
		current.Store(list)
		r.pass(context.Background())
		if st := r.state["62041"]; st == nil || st.shrinks != 1 {
			t.Fatalf("a different shrunk list %d starts the count over: %+v", i, st)
		}
		if got := s.Get("62041"); !slices.Equal(got.Prefixes, known) || len(changes.all()) != 0 {
			t.Fatalf("three different shrunk lists are not a confirmation: %v %v", got.Prefixes, changes.all())
		}
		clk.Advance(time.Hour + time.Second)
	}

	r.pass(context.Background())
	if st := r.state["62041"]; st == nil || st.shrinks != 2 {
		t.Fatalf("the same list again counts: %+v", st)
	}
	clk.Advance(time.Hour + time.Second)
	r.pass(context.Background())
	got := s.Get("62041")
	if slices.Equal(got.Prefixes, known) {
		t.Fatalf("the third identical shrunk list is accepted: %v", got.Prefixes)
	}
	if want := config.SanitizeASNPrefixes([]string{"91.108.4.0/23", "149.154.163.0/24", "149.154.166.0/24", "2001:67c:4e8::/48"}); !slices.Equal(got.Prefixes, want) {
		t.Fatalf("the accepted list is the repeated one: %v, want %v", got.Prefixes, want)
	}
	if c := changes.all(); len(c) != 1 || c[0][0] != "62041" {
		t.Fatalf("and reported as a change: %v", c)
	}
}

func TestPassResetsTheShrinkCountOnANormalResult(t *testing.T) {
	s := useStore(t)
	clk := useClock(t)
	var shrunk atomic.Bool
	small := strings.Replace(string(fixture(t, "ris_prefixes_AS62041.json")), `"91.108.4.0/23","149.154.163.0/24"`, `"91.108.4.0/23"],"x":["149.154.163.0/24"`, 1)
	useFake(t, func(w http.ResponseWriter, r *http.Request, call, resource string) {
		if call == "ris-prefixes" && shrunk.Load() {
			writeJSON(w, 200, []byte(small))
			return
		}
		realRIPE(t)(w, r, call, resource)
	})
	_ = s.Put(&config.AsnInfo{ID: "62041", Name: "Telegram", Prefixes: config.SanitizeASNPrefixes(fullAS62041(t)), UpdatedAt: clk.Now().Add(-StaleAfter - time.Minute).Unix(), Source: config.AsnSourceRIPEstat})
	changes := &changeLog{}
	r := testRefresher(asnConfig([]string{"62041"}), clk, changes)

	shrunk.Store(true)
	r.pass(context.Background())
	clk.Advance(time.Hour + time.Second)
	r.pass(context.Background())
	shrunk.Store(false)
	clk.Advance(time.Hour + time.Second)
	r.pass(context.Background())
	if len(r.state) != 0 {
		t.Fatalf("a normal result clears the shrink count: %+v", r.state["62041"])
	}
	stale := s.Get("62041")
	stale.UpdatedAt = clk.Now().Add(-StaleAfter - time.Minute).Unix()
	_ = s.Put(stale)
	shrunk.Store(true)
	r.pass(context.Background())
	if st := r.state["62041"]; st == nil || st.shrinks != 1 {
		t.Fatalf("counting starts over: %+v", st)
	}
	if len(changes.all()) != 0 {
		t.Fatalf("nothing changed: %v", changes.all())
	}
}

func TestResolveHoldsAShrinkUntilTheRefresherConfirmsIt(t *testing.T) {
	s := useStore(t)
	clk := useClock(t)
	var shrunk atomic.Bool
	small := strings.Replace(string(fixture(t, "ris_prefixes_AS62041.json")), `"91.108.4.0/23","149.154.163.0/24"`, `"91.108.4.0/23"],"x":["149.154.163.0/24"`, 1)
	useFake(t, func(w http.ResponseWriter, r *http.Request, call, resource string) {
		if call == "ris-prefixes" && shrunk.Load() {
			writeJSON(w, 200, []byte(small))
			return
		}
		realRIPE(t)(w, r, call, resource)
	})
	_ = s.Put(&config.AsnInfo{ID: "62041", Name: "Telegram", Prefixes: config.SanitizeASNPrefixes(fullAS62041(t)), UpdatedAt: clk.Now().Add(-StaleAfter - time.Minute).Unix(), Source: config.AsnSourceRIPEstat})
	known := s.Get("62041").Prefixes
	changes := &changeLog{}
	r := testRefresher(asnConfig([]string{"62041"}), clk, changes)

	shrunk.Store(true)
	r.pass(context.Background())
	if st := r.state["62041"]; st == nil || st.shrinks != 1 {
		t.Fatalf("the refresher holds the first shrink: %+v", st)
	}
	if LastError("62041") == "" {
		t.Fatal("a held shrink is reported as the last error")
	}

	clk.Advance(5 * time.Minute)
	for _, force := range []bool{false, true} {
		info, err := Resolve(context.Background(), "62041", force)
		if !errors.Is(err, ErrCoverageShrunk) || info != nil {
			t.Fatalf("force=%v: a resolve during the hold must refuse the shrink, got %v, %v", force, info, err)
		}
		if got := s.Get("62041"); !slices.Equal(got.Prefixes, known) {
			t.Fatalf("force=%v: the known list is kept: %v", force, got.Prefixes)
		}
		if !strings.Contains(LastError("62041"), "IPv4 addresses") {
			t.Fatalf("force=%v: the refusal is the last error: %q", force, LastError("62041"))
		}
	}

	clk.Advance(time.Hour + time.Second)
	r.pass(context.Background())
	if st := r.state["62041"]; st == nil || st.shrinks != 2 {
		t.Fatalf("the refresher keeps counting: %+v", st)
	}
	clk.Advance(time.Hour + time.Second)
	r.pass(context.Background())
	if got := s.Get("62041"); slices.Equal(got.Prefixes, known) {
		t.Fatalf("the third consecutive shrink is accepted: %v", got.Prefixes)
	}
	if LastError("62041") != "" {
		t.Fatalf("the accepted shrink clears the last error: %q", LastError("62041"))
	}
	if c := changes.all(); len(c) != 1 || c[0][0] != "62041" {
		t.Fatalf("and is reported as a change: %v", c)
	}
}

func TestResolveStoresAGrowingOrFirstList(t *testing.T) {
	s := useStore(t)
	clk := useClock(t)
	useFake(t, realRIPE(t))

	info, err := Resolve(context.Background(), "62041", false)
	if err != nil || len(info.Prefixes) == 0 {
		t.Fatalf("a first resolve stores the list: %v, %v", info, err)
	}
	_ = s.Put(&config.AsnInfo{ID: "62041", Name: "Telegram", Prefixes: []string{"91.108.4.0/23"}, UpdatedAt: clk.Now().Add(-StaleAfter - time.Minute).Unix(), Source: config.AsnSourceRIPEstat})
	info, err = Resolve(context.Background(), "62041", false)
	if err != nil || len(info.Prefixes) <= 1 {
		t.Fatalf("a larger list is stored at once: %v, %v", info, err)
	}
}

func fullAS62041(t *testing.T) []string {
	t.Helper()
	body := string(fixture(t, "ris_prefixes_AS62041.json"))
	start := strings.Index(body, `"v4":{"originating":[`) + len(`"v4":{"originating":[`)
	end := strings.Index(body[start:], "]")
	var out []string
	for _, p := range strings.Split(body[start:start+end], ",") {
		out = append(out, strings.Trim(p, `"`))
	}
	return append(out, "2001:67c:4e8::/48")
}

func TestPassForgetsStateOfASNsNoSetReferences(t *testing.T) {
	useStore(t)
	clk := useClock(t)
	useFake(t, func(w http.ResponseWriter, r *http.Request, call, resource string) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	})
	cfg := asnConfig([]string{"62041"})
	changes := &changeLog{}
	r := newRefresher(func() *config.Config { return cfg }, changes.record)
	r.now = clk.Now
	r.pass(context.Background())
	if r.state["62041"] == nil {
		t.Fatal("failure tracked")
	}
	cfg = asnConfig()
	r.pass(context.Background())
	if len(r.state) != 0 {
		t.Fatalf("state of unreferenced ASNs is dropped: %+v", r.state)
	}
}

func TestRunResolvesAtStartAndOnARefreshRequest(t *testing.T) {
	useStore(t)
	useFake(t, realRIPE(t))
	var cfgPtr atomic.Pointer[config.Config]
	cfgPtr.Store(asnConfig([]string{"62041"}))
	changed := make(chan []string, 4)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	r := newRefresher(cfgPtr.Load, func(ids []string) { changed <- ids })
	go func() {
		defer close(done)
		r.run(ctx)
	}()
	defer func() {
		cancel()
		<-done
	}()

	select {
	case ids := <-changed:
		if !slices.Equal(ids, []string{"62041"}) {
			t.Fatalf("start pass: %v", ids)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the loop resolves at start")
	}

	cfgPtr.Store(asnConfig([]string{"62041", "15169"}))
	config.RequestASNRefresh()
	select {
	case ids := <-changed:
		if !slices.Equal(ids, []string{"15169"}) {
			t.Fatalf("only the new ASN changed: %v", ids)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a refresh request wakes the loop")
	}
}
