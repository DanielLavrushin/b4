package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/api"
	"github.com/daniellavrushin/b4hub/internal/asn"
	"github.com/daniellavrushin/b4hub/internal/catalogue"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/ingest"
	"github.com/daniellavrushin/b4hub/internal/ratelimit"
	"github.com/daniellavrushin/b4hub/internal/score"
	"github.com/daniellavrushin/b4hub/internal/store"
	"github.com/daniellavrushin/b4hub/internal/testkit"
)

const (
	authorAddress = "203.0.113.5"
	voterAddress  = "203.0.113.9"
	otherAddress  = "198.51.100.7"
	password      = "moderator-secret"
)

var origins = map[string]testkit.Origin{
	authorAddress: {ASN: "64500", Country: "RU", Name: "EXAMPLE-A ISP A"},
	voterAddress:  {ASN: "64500", Country: "RU", Name: "EXAMPLE-A ISP A"},
	otherAddress:  {ASN: "64501", Country: "DE", Name: "EXAMPLE-B ISP B"},
}

type fixture struct {
	t        *testing.T
	server   *httptest.Server
	store    *store.Store
	builder  *catalogue.Builder
	ingest   *ingest.Service
	web      *Server
	clock    time.Time
	rebuilds int
}

func newFixture(t *testing.T, adminPassword string) *fixture {
	t.Helper()
	layout := hubdata.Layout{Root: t.TempDir()}
	if err := layout.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	hubID, err := hubwire.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	secret, err := layout.LoadOrCreateSecret()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(layout.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	f := &fixture{t: t, store: st, clock: time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)}
	now := func() time.Time { return f.clock }
	f.builder = &catalogue.Builder{Store: st, Identity: hubID, PublicDir: layout.Public(), PublicURL: "https://hub.example", Now: now}
	resolver := asn.New(testkit.CymruLookup(origins), st, now)
	f.ingest = &ingest.Service{Store: st, Blobs: layout.Blobs(), Secret: secret, Limiter: ratelimit.New(now), ASN: resolver, Now: now}
	apiServer := &api.Server{
		Store:     st,
		Blobs:     layout.Blobs(),
		PublicDir: layout.Public(),
		Ingest:    f.ingest,
		Catalogue: f.builder,
	}
	f.web = &Server{
		Store:         st,
		Blobs:         layout.Blobs(),
		Catalogue:     f.builder,
		ASN:           resolver,
		Secret:        secret,
		AdminPassword: adminPassword,
		Version:       "test",
		KeyID:         hubID.KeyID(),
		Now:           now,
		Rebuild: func() error {
			f.rebuilds++
			_, _, err := f.builder.BuildIfNeeded(context.Background())
			return err
		},
	}
	mux := apiServer.Router()
	f.web.Mount(mux)
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fixture) share(name string, address string, domains ...string) (string, *hubwire.Envelope) {
	f.t.Helper()
	author := testkit.Identity(f.t)
	set := testkit.SampleSet(name, domains...)
	env := testkit.BuildEnvelope(f.t, &set)
	resp := f.ingest.Handle(context.Background(), testkit.SignShare(f.t, author, env, f.clock), parseIP(address))
	if resp.Status != http.StatusAccepted {
		f.t.Fatalf("share %s: %d %v", name, resp.Status, resp.Body)
	}
	return resp.Body["set_id"].(string), env
}

func (f *fixture) vote(setID string, version int, fp string, address string) {
	f.t.Helper()
	voter := testkit.Identity(f.t)
	body := hubwire.VoteBody{SetID: setID, Version: version, FP: fp, Kind: hubwire.VoteWorks}
	resp := f.ingest.Handle(context.Background(), testkit.Sign(f.t, voter, hubwire.RecordVote, body, f.clock), parseIP(address))
	if resp.Status != http.StatusAccepted {
		f.t.Fatalf("vote: %d %v", resp.Status, resp.Body)
	}
}

func (f *fixture) approve(setID string) {
	f.t.Helper()
	if err := f.store.Approve(context.Background(), setID, 1, f.clock); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) build() {
	f.t.Helper()
	f.clock = f.clock.Add(time.Minute)
	if _, err := f.builder.Build(context.Background()); err != nil {
		f.t.Fatal(err)
	}
}

type response struct {
	status   int
	body     string
	headers  http.Header
	location string
}

func (r response) decode(t *testing.T, into interface{}) {
	t.Helper()
	if err := json.Unmarshal([]byte(r.body), into); err != nil {
		t.Fatalf("decode %q: %v", r.body, err)
	}
}

func (f *fixture) request(method, path string, body interface{}, mutate func(*http.Request)) response {
	f.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			f.t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, f.server.URL+path, reader)
	if err != nil {
		f.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if mutate != nil {
		mutate(req)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		f.t.Fatal(err)
	}
	return response{status: resp.StatusCode, body: string(raw), headers: resp.Header, location: resp.Header.Get("Location")}
}

func (f *fixture) get(path string) response {
	f.t.Helper()
	return f.request(http.MethodGet, path, nil, nil)
}

func sameSite(req *http.Request) {
	req.Header.Set("Origin", "http://"+req.Host)
}

func asAdmin(req *http.Request) {
	req.SetBasicAuth("admin", password)
	if req.Method != http.MethodGet {
		sameSite(req)
	}
}

func (f *fixture) admin(method, path string, body interface{}) response {
	f.t.Helper()
	return f.request(method, path, body, asAdmin)
}

func (f *fixture) signIn(pass string) response {
	f.t.Helper()
	return f.request(http.MethodPost, PathAPI+"/login", map[string]string{"password": pass}, sameSite)
}

func sessionCookieOf(t *testing.T, resp response) *http.Cookie {
	t.Helper()
	parsed := (&http.Response{Header: resp.headers}).Cookies()
	for _, c := range parsed {
		if c.Name == sessionCookie {
			return c
		}
	}
	t.Fatalf("no %s cookie in %v", sessionCookie, resp.headers)
	return nil
}

func withCookie(c *http.Cookie) func(*http.Request) {
	return func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
		if req.Method == http.MethodPost {
			sameSite(req)
		}
	}
}

func parseIP(s string) net.IP {
	return net.ParseIP(s)
}

func setPath(id string, version int, action string) string {
	return PathAPI + "/sets/" + id + "/" + itoa(version) + "/" + action
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

func TestRootRedirectsToAdmin(t *testing.T) {
	f := newFixture(t, password)
	resp := f.get("/")
	if resp.status != http.StatusFound || resp.location != PathAdmin+"/" {
		t.Fatalf("root must redirect to the admin console, got %d %q", resp.status, resp.location)
	}
	if page := f.get(PathAdmin + "/"); page.status != http.StatusOK && page.status != http.StatusServiceUnavailable {
		t.Fatalf("console shell: %d", page.status)
	}
	if page := f.get(PathAdmin + "/sets/anything"); page.status != http.StatusOK && page.status != http.StatusServiceUnavailable {
		t.Fatalf("deep links must fall back to the shell, got %d", page.status)
	}
}

func TestSessionLifecycle(t *testing.T) {
	unset := newFixture(t, "")
	var state sessionState
	unset.get(PathAPI+"/session").decode(t, &state)
	if state.Configured || state.Authenticated {
		t.Fatalf("unconfigured hub must report so: %+v", state)
	}
	if resp := unset.signIn("anything"); resp.status != http.StatusServiceUnavailable {
		t.Fatalf("sign-in without a configured password must answer 503, got %d", resp.status)
	}
	if resp := unset.admin(http.MethodGet, PathAPI+"/sets", nil); resp.status != http.StatusServiceUnavailable {
		t.Fatalf("reads without a configured password must answer 503, got %d", resp.status)
	}

	f := newFixture(t, password)
	f.get(PathAPI+"/session").decode(t, &state)
	if !state.Configured || state.Authenticated {
		t.Fatalf("fresh visitor: %+v", state)
	}
	if resp := f.get(PathAPI + "/sets"); resp.status != http.StatusUnauthorized {
		t.Fatalf("anonymous read must be refused, got %d", resp.status)
	}
	if resp := f.request(http.MethodPost, PathAPI+"/login", map[string]string{"password": password}, nil); resp.status != http.StatusForbidden {
		t.Fatalf("sign-in without origin evidence must be refused, got %d", resp.status)
	}
	if resp := f.signIn("wrong"); resp.status != http.StatusUnauthorized {
		t.Fatalf("wrong password must be refused, got %d", resp.status)
	}
	resp := f.signIn(password)
	if resp.status != http.StatusOK {
		t.Fatalf("sign-in: %d %s", resp.status, resp.body)
	}
	cookie := sessionCookieOf(t, resp)
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != PathAdmin {
		t.Fatalf("session cookie flags: %+v", cookie)
	}
	if resp := f.request(http.MethodGet, PathAPI+"/session", nil, withCookie(cookie)); resp.status != http.StatusOK || !strings.Contains(resp.body, `"authenticated":true`) {
		t.Fatalf("cookie must authenticate: %d %s", resp.status, resp.body)
	}
	if resp := f.request(http.MethodGet, PathAPI+"/sets", nil, withCookie(cookie)); resp.status != http.StatusOK {
		t.Fatalf("cookie read: %d %s", resp.status, resp.body)
	}
	if resp := f.request(http.MethodGet, PathAPI+"/sets", nil, withCookie(&http.Cookie{Name: sessionCookie, Value: cookie.Value + "x"})); resp.status != http.StatusUnauthorized {
		t.Fatalf("tampered cookie must be refused, got %d", resp.status)
	}

	f.clock = f.clock.Add(sessionTTL + time.Minute)
	if resp := f.request(http.MethodGet, PathAPI+"/sets", nil, withCookie(cookie)); resp.status != http.StatusUnauthorized {
		t.Fatalf("expired session must be refused, got %d", resp.status)
	}
	f.clock = f.clock.Add(-sessionTTL)

	f.web.AdminPassword = "rotated"
	if resp := f.request(http.MethodGet, PathAPI+"/sets", nil, withCookie(cookie)); resp.status != http.StatusUnauthorized {
		t.Fatalf("a password change must end every session, got %d", resp.status)
	}
	f.web.AdminPassword = password

	out := f.request(http.MethodPost, PathAPI+"/logout", nil, withCookie(cookie))
	if out.status != http.StatusNoContent || sessionCookieOf(t, out).MaxAge >= 0 {
		t.Fatalf("logout must clear the cookie: %d %v", out.status, out.headers)
	}

	for i := 0; i < loginAttempts; i++ {
		f.signIn("wrong")
	}
	if resp := f.signIn(password); resp.status != http.StatusTooManyRequests {
		t.Fatalf("brute force must be throttled, got %d", resp.status)
	}
}

func TestQueueAndActions(t *testing.T) {
	f := newFixture(t, password)
	pendingID, env := f.share("Queue <script>alert(1)</script>", authorAddress, "youtube.com")
	f.build()

	resp := f.admin(http.MethodGet, PathAPI+"/sets", nil)
	if resp.status != http.StatusOK {
		t.Fatalf("sets: %d %s", resp.status, resp.body)
	}
	var sets SetsView
	resp.decode(t, &sets)
	if len(sets.Pending) != 1 || len(sets.Listed) != 0 {
		t.Fatalf("one pending set expected: %+v", sets)
	}
	p := sets.Pending[0]
	if p.SetID != pendingID || p.Version != 1 || p.Title != "Queue <script>alert(1)</script>" || p.ASNObserved != "64500" || p.CountryObserved != "RU" {
		t.Fatalf("pending entry: %+v", p)
	}
	if p.Lineage == nil || p.Lineage.Kind != LineageFirst {
		t.Fatalf("first version lineage: %+v", p.Lineage)
	}
	if len(p.Emitted) != 1 || p.Emitted[0].Name != "www.google.com" || p.Emitted[0].Source != SourceCapture {
		t.Fatalf("emitted names: %+v", p.Emitted)
	}
	if len(p.Payloads) != 1 || p.Payloads[0].SHA256 != env.Payloads[0].SHA256 {
		t.Fatalf("payloads: %+v", p.Payloads)
	}
	if !strings.Contains(strings.Join(p.Flags, ","), "needs_payload") {
		t.Fatalf("flags: %v", p.Flags)
	}
	if _, ok := p.Projection["faking"]; !ok {
		t.Fatalf("projection must be carried raw: %v", p.Projection)
	}

	if resp = f.admin(http.MethodPost, setPath(pendingID, 1, ActionReject), map[string]string{}); resp.status != http.StatusBadRequest {
		t.Fatalf("reject without a reason must be refused, got %d", resp.status)
	}
	resp = f.request(http.MethodPost, setPath(pendingID, 1, ActionApprove), nil, func(req *http.Request) {
		asAdmin(req)
		req.Header.Set("Sec-Fetch-Site", "cross-site")
	})
	if resp.status != http.StatusForbidden {
		t.Fatalf("cross-site posts must be refused, got %d", resp.status)
	}
	resp = f.request(http.MethodPost, setPath(pendingID, 1, ActionApprove), nil, func(req *http.Request) {
		req.SetBasicAuth("admin", password)
	})
	if resp.status != http.StatusForbidden {
		t.Fatalf("a post with no origin evidence at all must be refused, got %d", resp.status)
	}

	resp = f.admin(http.MethodPost, setPath(pendingID, 1, ActionApprove), nil)
	if resp.status != http.StatusOK || !strings.Contains(resp.body, "approved "+pendingID+"/1") {
		t.Fatalf("approve: %d %s", resp.status, resp.body)
	}
	if f.rebuilds != 1 {
		t.Fatalf("approve must rebuild the catalogue, rebuilds %d", f.rebuilds)
	}
	v, err := f.store.GetVersion(context.Background(), pendingID, 1)
	if err != nil || v.Status != hubwire.SetStatusActive {
		t.Fatalf("approved version: %v %+v", err, v)
	}
	if latest := f.builder.Latest(); latest == nil || latest.ByID[pendingID] == nil {
		t.Fatalf("the approved set must be in the rebuilt catalogue")
	}
	f.admin(http.MethodGet, PathAPI+"/sets", nil).decode(t, &sets)
	if len(sets.Pending) != 0 || len(sets.Listed) != 1 || len(sets.Listed[0].Versions) != 1 {
		t.Fatalf("after approve: %+v", sets)
	}

	f.vote(pendingID, 1, v.FP, voterAddress)
	var detail SetDetailView
	resp = f.admin(http.MethodGet, PathAPI+"/sets/"+pendingID, nil)
	if resp.status != http.StatusOK {
		t.Fatalf("detail: %d %s", resp.status, resp.body)
	}
	resp.decode(t, &detail)
	if detail.ID != pendingID || len(detail.Versions) != 1 || len(detail.Votes) != 2 || detail.Versions[0].Votes.Works != 2 {
		t.Fatalf("detail: %+v", detail)
	}
	if resp = f.admin(http.MethodGet, PathAPI+"/sets/01ARZ3NDEKTSV4RRFFQ69G5FAV", nil); resp.status != http.StatusNotFound {
		t.Fatalf("unknown set must be 404, got %d", resp.status)
	}

	resp = f.admin(http.MethodPost, setPath(pendingID, 1, ActionHide), map[string]string{"reason": "breaks video on ISP A"})
	if resp.status != http.StatusOK {
		t.Fatalf("hide: %d %s", resp.status, resp.body)
	}
	v, _ = f.store.GetVersion(context.Background(), pendingID, 1)
	if v.Status != hubwire.SetStatusHidden || v.StatusReason != "breaks video on ISP A" {
		t.Fatalf("hidden version: %+v", v)
	}
	if latest := f.builder.Latest(); latest.ByID[pendingID] != nil {
		t.Fatalf("a hidden set must leave the catalogue")
	}
	f.admin(http.MethodGet, PathAPI+"/sets", nil).decode(t, &sets)
	if len(sets.Hidden) != 1 || sets.Hidden[0].StatusReason != "breaks video on ISP A" {
		t.Fatalf("hidden: %+v", sets.Hidden)
	}

	rejectedID, _ := f.share("Second", otherAddress, "example.net")
	resp = f.admin(http.MethodPost, setPath(rejectedID, 1, ActionReject), map[string]string{"reason": "private domain"})
	if resp.status != http.StatusOK {
		t.Fatalf("reject: %d %s", resp.status, resp.body)
	}
	v, _ = f.store.GetVersion(context.Background(), rejectedID, 1)
	if v.Status != hubwire.SetStatusRejected || v.StatusReason != "private domain" {
		t.Fatalf("rejected version: %+v", v)
	}
	f.admin(http.MethodGet, PathAPI+"/sets", nil).decode(t, &sets)
	if len(sets.Rejected) != 1 {
		t.Fatalf("rejected: %+v", sets.Rejected)
	}

	if resp = f.admin(http.MethodPost, setPath(rejectedID, 9, ActionApprove), nil); resp.status != http.StatusNotFound {
		t.Fatalf("unknown version must be 404, got %d", resp.status)
	}
	if resp = f.admin(http.MethodPost, setPath(rejectedID, 1, "explode"), nil); resp.status != http.StatusNotFound {
		t.Fatalf("unknown action must be 404, got %d", resp.status)
	}
	if resp = f.admin(http.MethodPost, setPath(rejectedID, 1, ActionApprove), map[string]string{"bogus": "field"}); resp.status != http.StatusBadRequest {
		t.Fatalf("unknown body fields must be refused, got %d", resp.status)
	}

	var overview OverviewView
	resp = f.admin(http.MethodGet, PathAPI+"/overview", nil)
	if resp.status != http.StatusOK {
		t.Fatalf("overview: %d %s", resp.status, resp.body)
	}
	resp.decode(t, &overview)
	if overview.Counts.Hidden != 1 || overview.Counts.Rejected != 1 || overview.Counts.Keys != 3 || overview.Counts.Votes != 3 || !overview.Catalogue.Published || overview.Catalogue.Sets != 0 || overview.Version != "test" {
		t.Fatalf("overview: %+v", overview)
	}

	var feedback FeedbackView
	f.admin(http.MethodGet, PathAPI+"/feedback?limit=5", nil).decode(t, &feedback)
	if len(feedback.Votes) != 3 || feedback.Votes[0].SetID != rejectedID || feedback.Votes[1].Kind != score.KindManualWorks {
		t.Fatalf("feedback: %+v", feedback)
	}
	if resp = f.admin(http.MethodGet, PathAPI+"/feedback?limit=zero", nil); resp.status != http.StatusBadRequest {
		t.Fatalf("bad limit must be refused, got %d", resp.status)
	}
}

func TestKeys(t *testing.T) {
	f := newFixture(t, password)
	setID, _ := f.share("Keyed", authorAddress, "youtube.com")
	v, err := f.store.GetVersion(context.Background(), setID, 1)
	if err != nil {
		t.Fatal(err)
	}
	var keys []KeyView
	resp := f.admin(http.MethodGet, PathAPI+"/keys", nil)
	if resp.status != http.StatusOK {
		t.Fatalf("keys: %d", resp.status)
	}
	resp.decode(t, &keys)
	if len(keys) != 1 || keys[0].KeyHMAC != v.UploaderHMAC || keys[0].Sets != 1 || keys[0].Banned {
		t.Fatalf("keys: %+v", keys)
	}

	resp = f.admin(http.MethodPost, PathAPI+"/keys/"+v.UploaderHMAC+"/ban", map[string]string{"reason": "spam"})
	if resp.status != http.StatusOK {
		t.Fatalf("ban: %d %s", resp.status, resp.body)
	}
	key, err := f.store.GetKey(context.Background(), v.UploaderHMAC)
	if err != nil || !key.Banned || key.BanReason != "spam" {
		t.Fatalf("banned key: %v %+v", err, key)
	}
	if f.rebuilds != 1 {
		t.Fatalf("a ban must rebuild the catalogue, rebuilds %d", f.rebuilds)
	}
	f.admin(http.MethodGet, PathAPI+"/keys", nil).decode(t, &keys)
	if !keys[0].Banned || keys[0].BanReason != "spam" || keys[0].BannedAt == nil {
		t.Fatalf("banned view: %+v", keys[0])
	}

	if resp = f.admin(http.MethodPost, PathAPI+"/keys/"+v.UploaderHMAC+"/unban", nil); resp.status != http.StatusOK {
		t.Fatalf("unban: %d", resp.status)
	}
	if key, _ = f.store.GetKey(context.Background(), v.UploaderHMAC); key.Banned {
		t.Fatalf("key must be unbanned")
	}
	if resp = f.admin(http.MethodPost, PathAPI+"/keys/not-a-key/ban", nil); resp.status != http.StatusNotFound {
		t.Fatalf("malformed key must be 404, got %d", resp.status)
	}

	rebuilds := f.rebuilds
	if resp = f.admin(http.MethodPost, PathAPI+"/keys/"+v.UploaderHMAC+"/trust", nil); resp.status != http.StatusOK {
		t.Fatalf("trust: %d %s", resp.status, resp.body)
	}
	if f.rebuilds != rebuilds {
		t.Fatalf("trusting a key does not touch the catalogue")
	}
	f.admin(http.MethodGet, PathAPI+"/keys", nil).decode(t, &keys)
	if !keys[0].Trusted || keys[0].TrustedAt == nil {
		t.Fatalf("trusted view: %+v", keys[0])
	}
	if resp = f.admin(http.MethodPost, PathAPI+"/keys/"+v.UploaderHMAC+"/untrust", nil); resp.status != http.StatusOK {
		t.Fatalf("untrust: %d", resp.status)
	}
	if key, _ = f.store.GetKey(context.Background(), v.UploaderHMAC); key.Trusted {
		t.Fatalf("key must be untrusted")
	}
}

func TestSettings(t *testing.T) {
	f := newFixture(t, password)
	var view SettingsView
	resp := f.admin(http.MethodGet, PathAPI+"/settings", nil)
	if resp.status != http.StatusOK {
		t.Fatalf("settings: %d %s", resp.status, resp.body)
	}
	resp.decode(t, &view)
	if view.Limits != view.Defaults || view.Limits.SharesPerDay != ratelimit.SharesPerDay {
		t.Fatalf("fresh hub answers the defaults: %+v", view)
	}
	view.Limits.SharesPerDay = 50
	resp = f.admin(http.MethodPut, PathAPI+"/settings", map[string]interface{}{"limits": view.Limits})
	if resp.status != http.StatusOK {
		t.Fatalf("save: %d %s", resp.status, resp.body)
	}
	resp.decode(t, &view)
	if view.Limits.SharesPerDay != 50 || view.Defaults.SharesPerDay != ratelimit.SharesPerDay {
		t.Fatalf("saved view: %+v", view)
	}
	if saved, _ := f.store.Settings(context.Background()); saved.SharesPerDay != 50 {
		t.Fatalf("settings not stored: %+v", saved)
	}
	view.Limits.VotesPerDay = 0
	if resp = f.admin(http.MethodPut, PathAPI+"/settings", map[string]interface{}{"limits": view.Limits}); resp.status != http.StatusBadRequest {
		t.Fatalf("a zero limit must be refused, got %d %s", resp.status, resp.body)
	}
	if resp = f.request(http.MethodPut, PathAPI+"/settings", map[string]interface{}{"limits": view.Limits}, func(req *http.Request) { req.SetBasicAuth("admin", password) }); resp.status != http.StatusForbidden {
		t.Fatalf("cross-site save must be refused, got %d", resp.status)
	}
}

func TestDeleteSet(t *testing.T) {
	f := newFixture(t, password)
	author := testkit.Identity(t)
	set := testkit.SampleSet("Doomed", "doomed.example")
	env := testkit.BuildEnvelope(t, &set)
	raw := testkit.SignShare(t, author, env, f.clock)
	first := f.ingest.Handle(context.Background(), raw, parseIP(authorAddress))
	if first.Status != http.StatusAccepted {
		t.Fatalf("share: %d %v", first.Status, first.Body)
	}
	doomed := first.Body["set_id"].(string)
	keeper, _ := f.share("Keeper", otherAddress, "keeper.example")
	f.approve(doomed)
	f.build()
	f.vote(doomed, 1, env.Fingerprint, voterAddress)
	blob := env.Payloads[0].SHA256
	if !f.web.Blobs.Exists(blob) {
		t.Fatalf("the sample payload must be stored")
	}

	resp := f.admin(http.MethodPost, PathAPI+"/sets/"+doomed+"/delete", map[string]string{"confirm": "nope"})
	if resp.status != http.StatusBadRequest {
		t.Fatalf("a wrong confirmation must be refused, got %d %s", resp.status, resp.body)
	}
	rebuilds := f.rebuilds
	resp = f.admin(http.MethodPost, PathAPI+"/sets/"+doomed+"/delete", map[string]string{"confirm": doomed})
	if resp.status != http.StatusOK {
		t.Fatalf("delete: %d %s", resp.status, resp.body)
	}
	if f.rebuilds != rebuilds+1 {
		t.Fatalf("a delete must rebuild the catalogue")
	}
	ctx := context.Background()
	if _, _, err := f.store.GetSet(ctx, doomed); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("set must be gone, got %v", err)
	}
	votes, _ := f.store.RecentVotes(ctx, 10)
	for _, v := range votes {
		if v.SetID == doomed {
			t.Fatalf("votes of a deleted set must be gone")
		}
	}
	if !f.web.Blobs.Exists(blob) {
		t.Fatalf("the payload is still referenced by %s and must stay", keeper)
	}
	replay := f.ingest.Handle(ctx, raw, parseIP(authorAddress))
	if replay.Status != http.StatusOK || replay.Body["duplicate"] != true || replay.Body["set_id"] != nil {
		t.Fatalf("a replay of the deleted record must not resurrect it, got %d %v", replay.Status, replay.Body)
	}
	if resp = f.admin(http.MethodPost, PathAPI+"/sets/"+doomed+"/delete", map[string]string{"confirm": doomed}); resp.status != http.StatusNotFound {
		t.Fatalf("deleting twice must be 404, got %d", resp.status)
	}

	if resp = f.admin(http.MethodPost, PathAPI+"/sets/"+keeper+"/delete", map[string]string{"confirm": keeper}); resp.status != http.StatusOK {
		t.Fatalf("delete keeper: %d %s", resp.status, resp.body)
	}
	if f.web.Blobs.Exists(blob) {
		t.Fatalf("an orphaned payload must be removed")
	}
	if resp = f.request(http.MethodPost, PathAPI+"/sets/"+keeper+"/delete", map[string]string{"confirm": keeper}, func(req *http.Request) { req.SetBasicAuth("admin", password) }); resp.status != http.StatusForbidden {
		t.Fatalf("cross-site delete must be refused, got %d", resp.status)
	}
}

func TestCatalogueOperations(t *testing.T) {
	f := newFixture(t, password)
	setID, _ := f.share("Ops", authorAddress, "youtube.com")
	f.approve(setID)

	resp := f.admin(http.MethodPost, PathAPI+"/catalogue/build", nil)
	if resp.status != http.StatusOK || !strings.Contains(resp.body, "with 1 sets") {
		t.Fatalf("build: %d %s", resp.status, resp.body)
	}
	var overview OverviewView
	f.admin(http.MethodGet, PathAPI+"/overview", nil).decode(t, &overview)
	if !overview.Catalogue.Published || overview.Catalogue.Sets != 1 || overview.Catalogue.Dirty {
		t.Fatalf("after build: %+v", overview.Catalogue)
	}
	epoch := overview.Catalogue.Epoch

	f.clock = f.clock.Add(time.Minute)
	if resp = f.admin(http.MethodPost, PathAPI+"/catalogue/epoch", nil); resp.status != http.StatusOK {
		t.Fatalf("epoch: %d %s", resp.status, resp.body)
	}
	f.admin(http.MethodGet, PathAPI+"/overview", nil).decode(t, &overview)
	if overview.Catalogue.Epoch <= epoch || overview.Catalogue.Seq != 1 {
		t.Fatalf("a new epoch must restart the sequence: %+v", overview.Catalogue)
	}

	other := testkit.Identity(t)
	if resp = f.admin(http.MethodPost, PathAPI+"/catalogue/revoke", map[string]string{"key_id": "not-a-key"}); resp.status != http.StatusBadRequest {
		t.Fatalf("malformed key id must be refused, got %d", resp.status)
	}
	if resp = f.admin(http.MethodPost, PathAPI+"/catalogue/revoke", map[string]string{"key_id": f.web.KeyID}); resp.status != http.StatusBadRequest {
		t.Fatalf("revoking the hub's own key must be refused, got %d", resp.status)
	}
	if resp = f.admin(http.MethodPost, PathAPI+"/catalogue/revoke", map[string]string{"key_id": other.KeyID()}); resp.status != http.StatusOK {
		t.Fatalf("revoke: %d %s", resp.status, resp.body)
	}
	f.admin(http.MethodGet, PathAPI+"/overview", nil).decode(t, &overview)
	if len(overview.Catalogue.RevokedKeys) != 1 || overview.Catalogue.RevokedKeys[0] != other.KeyID() {
		t.Fatalf("revoked keys: %+v", overview.Catalogue.RevokedKeys)
	}
	if latest := f.builder.Latest(); len(latest.Manifest.RevokedKeys) != 1 {
		t.Fatalf("the manifest must carry the revocation: %+v", latest.Manifest.RevokedKeys)
	}
}

func TestTargetFiltersDescribeTargetsNotStrategy(t *testing.T) {
	set := testkit.SampleSet("Filtered", "ntc.party")
	set.Targets.TLSVersion = "1.3"
	set.Targets.IPVersion = "4"
	set.Targets.DomainOnly = true
	env := testkit.BuildEnvelope(t, &set)
	imp, err := hubwire.Open(env, hubwire.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	projection, _, err := hubwire.Scrub(&imp.Set)
	if err != nil {
		t.Fatal(err)
	}
	summary := TargetsOf(projection).Summary()
	for _, want := range []string{"ntc.party", "TLS 1.3 only", "IPv4 only", "domain-only matching"} {
		if !strings.Contains(summary, want) {
			t.Errorf("targets summary %q lacks %q", summary, want)
		}
	}
	for _, word := range StrategyWords(&imp.Set, nil) {
		if strings.Contains(word, "only") || strings.Contains(word, "domain-only") {
			t.Errorf("strategy words must not carry a target filter, got %q", word)
		}
	}
}

func projectionCopy(t *testing.T, projection map[string]interface{}) map[string]interface{} {
	t.Helper()
	raw, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func withDomains(t *testing.T, projection map[string]interface{}, domains []string) map[string]interface{} {
	t.Helper()
	out := projectionCopy(t, projection)
	targets, _ := out["targets"].(map[string]interface{})
	if targets == nil {
		targets = map[string]interface{}{}
		out["targets"] = targets
	}
	targets["sni_domains"] = domains
	return out
}

func withFakeTTL(t *testing.T, projection map[string]interface{}, ttl int) map[string]interface{} {
	t.Helper()
	out := projectionCopy(t, projection)
	faking, _ := out["faking"].(map[string]interface{})
	if faking == nil {
		faking = map[string]interface{}{}
		out["faking"] = faking
	}
	faking["ttl"] = ttl
	return out
}

func TestEditPendingVersion(t *testing.T) {
	f := newFixture(t, password)
	ctx := context.Background()
	id, env := f.share("Discord", authorAddress, "cdn.discordapp.com", "discordapp.com", "www.discord.com", "*.discord.com", "*.discord.gg", "discord.gg")
	before, err := f.store.GetVersion(ctx, id, 1)
	if err != nil {
		t.Fatal(err)
	}

	unchanged := EditRequest{Title: before.Title, Description: before.Description, Projection: before.Projection}
	resp := f.admin(http.MethodPost, setPath(id, 1, "preview"), unchanged)
	if resp.status != http.StatusOK {
		t.Fatalf("preview: %d %s", resp.status, resp.body)
	}
	var preview EditPreview
	resp.decode(t, &preview)
	if preview.Changed || preview.FPChanged || preview.Duplicate != nil || preview.FP != before.FP || len(preview.Payloads) != 1 {
		t.Fatalf("an untouched set must preview as unchanged: %+v", preview)
	}
	if preview.Tidy == nil || len(preview.Tidy.Suggestions) != 4 {
		t.Fatalf("tidy suggestions: %+v", preview.Tidy)
	}
	if got := strings.Join(preview.Tidy.Domains, ","); got != "discordapp.com,discord.com,discord.gg" {
		t.Fatalf("tidied domains: %s", got)
	}
	if resp = f.admin(http.MethodPost, setPath(id, 1, "edit"), unchanged); resp.status != http.StatusBadRequest || !strings.Contains(resp.body, codeUnchanged) {
		t.Fatalf("an unchanged edit must be refused: %d %s", resp.status, resp.body)
	}
	if resp = f.admin(http.MethodPost, setPath(id, 1, "edit"), map[string]interface{}{"title": "x", "projection": map[string]interface{}{"tcp": "nonsense"}}); resp.status != http.StatusBadRequest || !strings.Contains(resp.body, codeInvalidSet) {
		t.Fatalf("a set that does not decode must be refused: %d %s", resp.status, resp.body)
	}
	if resp = f.admin(http.MethodPost, setPath(id, 1, "edit"), EditRequest{Title: "x", Projection: map[string]interface{}{"faking": map[string]interface{}{"ttl": 3}}}); resp.status != http.StatusBadRequest || !strings.Contains(resp.body, "no targets") {
		t.Fatalf("a set without targets must be refused: %d %s", resp.status, resp.body)
	}

	f.clock = f.clock.Add(time.Minute)
	edited := EditRequest{Title: "Discord (tidy)", Description: "Cleaned by a moderator", Projection: withDomains(t, before.Projection, preview.Tidy.Domains), Note: "dead wildcards removed"}
	resp = f.admin(http.MethodPost, setPath(id, 1, "edit"), edited)
	if resp.status != http.StatusOK || !strings.Contains(resp.body, "edited "+id+"/1") {
		t.Fatalf("edit: %d %s", resp.status, resp.body)
	}
	if f.rebuilds != 0 {
		t.Fatalf("editing a pending version must not rebuild the catalogue, rebuilds %d", f.rebuilds)
	}
	after, err := f.store.GetVersion(ctx, id, 1)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != hubwire.SetStatusPending || after.Title != "Discord (tidy)" || after.Description != "Cleaned by a moderator" || after.EditNote != "dead wildcards removed" {
		t.Fatalf("edited version: %+v", after)
	}
	if !after.EditedAt.Equal(f.clock) || !after.UpdatedAt.Equal(f.clock) {
		t.Fatalf("edit stamps: %v %v, clock %v", after.EditedAt, after.UpdatedAt, f.clock)
	}
	if got := strings.Join(store.TargetList(after.Projection, "sni_domains"), ","); got != "discordapp.com,discord.com,discord.gg" {
		t.Fatalf("domains after edit: %s", got)
	}
	if name, _ := after.Projection["name"].(string); name != "Discord (tidy)" {
		t.Fatalf("the projection must carry the new title: %v", after.Projection["name"])
	}
	if after.FP != before.FP || after.TargetsKey == before.TargetsKey {
		t.Fatalf("a targets-only edit keeps the fingerprint and changes the targets key: %+v", after)
	}
	if !sameJSON(after.OriginalProjection, before.Projection) {
		t.Fatalf("the received projection must be kept: %v", after.OriginalProjection)
	}
	if len(after.Payloads) != 1 || after.Payloads[0].SHA256 != env.Payloads[0].SHA256 {
		t.Fatalf("the attached payload must survive an edit: %+v", after.Payloads)
	}
	if file, _ := after.Projection["faking"].(map[string]interface{})["payload_file"].(string); file != hubwire.RefPrefix+env.Payloads[0].SHA256 {
		t.Fatalf("payload reference: %q", file)
	}

	var sets SetsView
	f.admin(http.MethodGet, PathAPI+"/sets", nil).decode(t, &sets)
	if len(sets.Pending) != 1 {
		t.Fatalf("one pending set expected: %+v", sets)
	}
	p := sets.Pending[0]
	if p.EditedAt == nil || p.EditNote != "dead wildcards removed" || p.OriginalProjection == nil || p.OriginalTitle != "Discord" || p.Title != "Discord (tidy)" || len(p.Targets.Domains) != 3 {
		t.Fatalf("queue entry after edit: %+v", p)
	}

	f.clock = f.clock.Add(time.Minute)
	strategy := EditRequest{Title: after.Title, Description: after.Description, Projection: withFakeTTL(t, after.Projection, 3)}
	f.admin(http.MethodPost, setPath(id, 1, "preview"), strategy).decode(t, &preview)
	if !preview.Changed || !preview.FPChanged || preview.FP == after.FP {
		t.Fatalf("a strategy edit must change the fingerprint: %+v", preview)
	}
	if resp = f.admin(http.MethodPost, setPath(id, 1, "edit"), strategy); resp.status != http.StatusOK {
		t.Fatalf("strategy edit: %d %s", resp.status, resp.body)
	}
	third, _ := f.store.GetVersion(ctx, id, 1)
	if third.FP != preview.FP || third.FP == after.FP || !sameJSON(third.OriginalProjection, before.Projection) {
		t.Fatalf("after the strategy edit: %+v", third)
	}
	votes, err := f.store.VotesForVersion(ctx, id, 1)
	if err != nil || len(votes) != 0 {
		t.Fatalf("votes for a strategy nobody evaluated must be dropped: %v %+v", err, votes)
	}
	if third.OriginalTitle != "Discord" || third.OriginalDescription != "" {
		t.Fatalf("the received title and description must be kept: %+v", third)
	}

	otherID, _ := f.share("Other", otherAddress, "example.org")
	other, _ := f.store.GetVersion(ctx, otherID, 1)
	duplicate := EditRequest{Title: "Other", Projection: withFakeTTL(t, withDomains(t, other.Projection, store.TargetList(third.Projection, "sni_domains")), 3)}
	f.admin(http.MethodPost, setPath(otherID, 1, "preview"), duplicate).decode(t, &preview)
	if preview.Duplicate == nil || preview.Duplicate.SetID != id || preview.Duplicate.Version != 1 {
		t.Fatalf("preview must name the duplicate: %+v", preview.Duplicate)
	}
	if resp = f.admin(http.MethodPost, setPath(otherID, 1, "edit"), duplicate); resp.status != http.StatusConflict || !strings.Contains(resp.body, codeDuplicate) {
		t.Fatalf("an edit that duplicates a set must be refused: %d %s", resp.status, resp.body)
	}

	approved := EditRequest{Title: "Other (approved)", Projection: withDomains(t, other.Projection, []string{"example.org", "example.com"}), Approve: true}
	resp = f.admin(http.MethodPost, setPath(otherID, 1, "edit"), approved)
	if resp.status != http.StatusOK || !strings.Contains(resp.body, "edited and approved "+otherID+"/1") {
		t.Fatalf("edit and approve: %d %s", resp.status, resp.body)
	}
	if f.rebuilds != 1 {
		t.Fatalf("approving must rebuild the catalogue, rebuilds %d", f.rebuilds)
	}
	latest := f.builder.Latest()
	if latest == nil || latest.ByID[otherID] == nil || latest.ByID[otherID].Title != "Other (approved)" {
		t.Fatalf("the edited set must be published: %+v", latest)
	}
	if got := strings.Join(store.TargetList(latest.ByID[otherID].Set, "sni_domains"), ","); got != "example.org,example.com" {
		t.Fatalf("published domains: %s", got)
	}
	if resp = f.admin(http.MethodPost, setPath(otherID, 1, "edit"), approved); resp.status != http.StatusConflict || !strings.Contains(resp.body, codeNotPending) {
		t.Fatalf("a listed version must not be editable: %d %s", resp.status, resp.body)
	}
	if resp = f.admin(http.MethodPost, setPath(otherID, 1, "preview"), approved); resp.status != http.StatusOK {
		t.Fatalf("preview of a listed version stays available: %d %s", resp.status, resp.body)
	}
	if resp = f.admin(http.MethodPost, setPath(otherID, 9, "edit"), approved); resp.status != http.StatusNotFound {
		t.Fatalf("unknown version must be 404, got %d", resp.status)
	}
}

func TestEditKeepsVotesOnlyWhileStrategyStands(t *testing.T) {
	f := newFixture(t, password)
	ctx := context.Background()
	id, _ := f.share("Votes", authorAddress, "votes.example")
	v, err := f.store.GetVersion(ctx, id, 1)
	if err != nil {
		t.Fatal(err)
	}
	targets := EditRequest{Title: v.Title, Projection: withDomains(t, v.Projection, []string{"votes.example", "more.example"})}
	if resp := f.admin(http.MethodPost, setPath(id, 1, "edit"), targets); resp.status != http.StatusOK {
		t.Fatalf("targets edit: %d %s", resp.status, resp.body)
	}
	if votes, _ := f.store.VotesForVersion(ctx, id, 1); len(votes) != 1 || votes[0].FP != v.FP {
		t.Fatalf("a targets-only edit keeps the upload vote: %+v", votes)
	}
	edited, _ := f.store.GetVersion(ctx, id, 1)
	strategy := EditRequest{Title: v.Title, Projection: withFakeTTL(t, edited.Projection, 3)}
	if resp := f.admin(http.MethodPost, setPath(id, 1, "edit"), strategy); resp.status != http.StatusOK {
		t.Fatalf("strategy edit: %d %s", resp.status, resp.body)
	}
	if votes, _ := f.store.VotesForVersion(ctx, id, 1); len(votes) != 0 {
		t.Fatalf("a strategy edit drops the votes: %+v", votes)
	}
	all, _ := f.store.AllVotes(ctx)
	if len(all) != 0 {
		t.Fatalf("no vote may survive with the obsolete fingerprint: %+v", all)
	}
}

func TestEditRefusesWhenPayloadUnreadable(t *testing.T) {
	f := newFixture(t, password)
	ctx := context.Background()
	id, env := f.share("Capture", authorAddress, "capture.example")
	before, err := f.store.GetVersion(ctx, id, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.web.Blobs.Remove(env.Payloads[0].SHA256); err != nil {
		t.Fatal(err)
	}
	req := EditRequest{Title: "Capture edited", Projection: before.Projection}
	resp := f.admin(http.MethodPost, setPath(id, 1, "edit"), req)
	if resp.status != http.StatusInternalServerError || !strings.Contains(resp.body, env.Payloads[0].SHA256) {
		t.Fatalf("an unreadable payload must refuse the edit: %d %s", resp.status, resp.body)
	}
	after, _ := f.store.GetVersion(ctx, id, 1)
	if after.Title != before.Title || len(after.Payloads) != 1 || !sameJSON(after.Projection, before.Projection) {
		t.Fatalf("the version must be untouched: %+v", after)
	}
}

func TestPreviewReportsStrippedFields(t *testing.T) {
	f := newFixture(t, password)
	ctx := context.Background()
	id, _ := f.share("Routing", authorAddress, "routing.example")
	v, err := f.store.GetVersion(ctx, id, 1)
	if err != nil {
		t.Fatal(err)
	}
	projection := projectionCopy(t, v.Projection)
	projection["routing"] = map[string]interface{}{"enabled": true, "mode": "proxy", "upstream": "socks5://10.0.0.1:1080"}
	projection["targets"].(map[string]interface{})["source_devices"] = []string{"aa:bb:cc:dd:ee:ff"}
	projection["tcp"] = map[string]interface{}{"seg2delay": 0, "made_up": 1}
	var preview EditPreview
	resp := f.admin(http.MethodPost, setPath(id, 1, "preview"), EditRequest{Title: v.Title, Projection: projection})
	if resp.status != http.StatusOK {
		t.Fatalf("preview: %d %s", resp.status, resp.body)
	}
	resp.decode(t, &preview)
	got := map[string]string{}
	for _, s := range preview.Stripped {
		got[s.Path] = s.Reason
	}
	if got["routing.upstream"] != strippedPrivate || got["targets.source_devices"] != strippedPrivate || got["routing.mode"] != strippedNotShareable || got["routing.enabled"] != strippedNotShareable {
		t.Fatalf("stripped: %+v", preview.Stripped)
	}
	if _, ok := got["tcp.seg2delay"]; ok {
		t.Fatalf("a value equal to the default is not a strip: %+v", preview.Stripped)
	}
	if _, ok := got["tcp.made_up"]; ok {
		t.Fatalf("an unknown field is reported as a warning, not a strip: %+v", preview.Stripped)
	}
	if _, ok := preview.Projection["routing"]; ok {
		t.Fatalf("routing must not survive: %v", preview.Projection["routing"])
	}
}

func TestEditRefusesStaleRevision(t *testing.T) {
	f := newFixture(t, password)
	ctx := context.Background()
	id, _ := f.share("Stale", authorAddress, "stale.example")
	loaded, err := f.store.GetVersion(ctx, id, 1)
	if err != nil {
		t.Fatal(err)
	}
	opened := loaded.UpdatedAt
	f.clock = f.clock.Add(time.Minute)
	first := EditRequest{Title: "First moderator", Projection: loaded.Projection, Expect: &opened}
	if resp := f.admin(http.MethodPost, setPath(id, 1, "edit"), first); resp.status != http.StatusOK {
		t.Fatalf("first edit: %d %s", resp.status, resp.body)
	}
	second := EditRequest{Title: "Second moderator", Projection: loaded.Projection, Expect: &opened}
	resp := f.admin(http.MethodPost, setPath(id, 1, "edit"), second)
	if resp.status != http.StatusConflict || !strings.Contains(resp.body, codeStale) {
		t.Fatalf("an edit from a stale dialog must be refused: %d %s", resp.status, resp.body)
	}
	v, _ := f.store.GetVersion(ctx, id, 1)
	if v.Title != "First moderator" {
		t.Fatalf("the first edit must stand: %+v", v)
	}
	fresh := EditRequest{Title: "Second moderator", Projection: loaded.Projection, Expect: &v.UpdatedAt}
	if resp := f.admin(http.MethodPost, setPath(id, 1, "edit"), fresh); resp.status != http.StatusOK {
		t.Fatalf("an edit with the current revision must pass: %d %s", resp.status, resp.body)
	}
}
