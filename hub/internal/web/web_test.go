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
	"github.com/daniellavrushin/b4hub/internal/geo"
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
		Geo:       geo.NewIndex(layout.Geo() + "/geosite.dat"),
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
