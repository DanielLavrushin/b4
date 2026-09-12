package web

import (
	"context"
	"encoding/json"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
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
		Search:        apiServer,
		ASN:           resolver,
		AdminPassword: adminPassword,
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

func (f *fixture) request(method, path string, form url.Values, mutate func(*http.Request)) response {
	f.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, f.server.URL+path, body)
	if err != nil {
		f.t.Fatal(err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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

func asAdmin(req *http.Request) {
	req.SetBasicAuth("admin", password)
}

func (f *fixture) admin(method, path string, form url.Values) response {
	f.t.Helper()
	return f.request(method, path, form, asAdmin)
}

func parseIP(s string) net.IP {
	return net.ParseIP(s)
}

func mustContain(t *testing.T, body string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

func mustNotContain(t *testing.T, body string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if strings.Contains(body, want) {
			t.Errorf("page must not contain %q", want)
		}
	}
}

var envelopeBlock = regexp.MustCompile(`(?s)<pre id="envelope" class="json">(.*?)</pre>`)

func TestIndexAndSetPages(t *testing.T) {
	f := newFixture(t, password)
	listedID, env := f.share("YouTube <b>fast</b>", authorAddress, "youtube.com", "googlevideo.com")
	f.approve(listedID)
	pendingID, _ := f.share("Pending one", otherAddress, "example.net")
	f.build()

	page := f.get("/")
	if page.status != http.StatusOK || !strings.HasPrefix(page.headers.Get("Content-Type"), "text/html") {
		t.Fatalf("index: %d %s", page.status, page.headers.Get("Content-Type"))
	}
	mustContain(t, page.body, "YouTube &lt;b&gt;fast&lt;/b&gt;", "/s/"+listedID, "youtube.com, googlevideo.com", "fake ClientHello", "attached capture for www.google.com", "tls fragmentation", "not enough reports to rate", "1 listed set")
	mustNotContain(t, page.body, "<b>fast</b>", "Pending one", pendingID)

	page = f.get("/?domain=WWW.YouTube.com")
	if page.status != http.StatusOK {
		t.Fatalf("search: %d", page.status)
	}
	mustContain(t, page.body, "Sets covering www.youtube.com", "matches youtube.com (covered)", "/s/"+listedID)

	page = f.get("/?domain=example.org")
	mustContain(t, page.body, "No listed set covers this domain.")
	mustNotContain(t, page.body, "/s/"+listedID)

	page = f.get("/?domain=")
	mustContain(t, page.body, "Enter a domain name to search.")

	page = f.get("/s/" + listedID)
	if page.status != http.StatusOK {
		t.Fatalf("set page: %d", page.status)
	}
	mustContain(t, page.body, "Copy JSON", "YouTube &lt;b&gt;fast&lt;/b&gt;", "tls www.google.com", "<li>youtube.com</li>", "<li>googlevideo.com</li>", "Import into b4")
	match := envelopeBlock.FindStringSubmatch(page.body)
	if match == nil {
		t.Fatalf("set page carries no envelope block")
	}
	var shared hubwire.Envelope
	if err := json.Unmarshal([]byte(html.UnescapeString(match[1])), &shared); err != nil {
		t.Fatalf("envelope on the page does not decode: %v", err)
	}
	if shared.Format != hubwire.Format || shared.Fingerprint != env.Fingerprint || shared.DerivedFrom == nil || shared.DerivedFrom.ID != listedID || shared.DerivedFrom.Version != 1 {
		t.Fatalf("unexpected envelope %+v", shared)
	}
	if len(shared.Payloads) != 1 || hubwire.BlobHash(shared.Payloads[0].Data) != env.Payloads[0].SHA256 || shared.Payloads[0].Domain != "www.google.com" {
		t.Fatalf("the payload must be inlined from the blob store: %+v", shared.Payloads)
	}
	imp, err := hubwire.Open(&shared, hubwire.OpenOptions{})
	if err != nil {
		t.Fatalf("the pasted envelope must import: %v", err)
	}
	if imp.Set.Hub == nil || imp.Set.Hub.ID != listedID || len(imp.Payloads) != 1 {
		t.Fatalf("imported set lost its hub origin or payload: %+v", imp.Set.Hub)
	}

	page = f.get("/s/" + pendingID)
	if page.status != http.StatusNotFound {
		t.Fatalf("a pending set must not be served, got %d", page.status)
	}
	mustContain(t, page.body, "waiting for moderation")
	if page = f.get("/s/01ARZ3NDEKTSV4RRFFQ69G5FAV"); page.status != http.StatusNotFound {
		t.Fatalf("unknown set must be 404, got %d", page.status)
	}
	if page = f.get("/s/not-an-id"); page.status != http.StatusNotFound {
		t.Fatalf("malformed id must be 404, got %d", page.status)
	}
}

func TestIndexShowsViewerNetworkRating(t *testing.T) {
	f := newFixture(t, password)
	listedID, env := f.share("YouTube", authorAddress, "youtube.com")
	f.approve(listedID)
	f.build()
	f.vote(listedID, 1, env.Fingerprint, voterAddress)
	f.clock = f.clock.Add(8 * 24 * time.Hour)
	f.build()

	page := f.request(http.MethodGet, "/", nil, func(req *http.Request) { req.Header.Set("X-Forwarded-For", authorAddress) })
	mustContain(t, page.body, "Ratings are shown for EXAMPLE-A ISP A", "positive on EXAMPLE-A ISP A", "from 2 devices")

	page = f.request(http.MethodGet, "/", nil, func(req *http.Request) { req.Header.Set("X-Forwarded-For", otherAddress) })
	mustContain(t, page.body, "positive on all networks", "from 2 devices")
	mustNotContain(t, page.body, "positive on EXAMPLE-A ISP A")
}

func TestAdminAuthentication(t *testing.T) {
	unset := newFixture(t, "")
	if page := unset.get("/admin"); page.status != http.StatusServiceUnavailable {
		t.Fatalf("no password configured must answer 503, got %d", page.status)
	}
	if page := unset.request(http.MethodPost, "/admin/sets/01ARZ3NDEKTSV4RRFFQ69G5FAV/1/approve", url.Values{}, asAdmin); page.status != http.StatusServiceUnavailable {
		t.Fatalf("actions without a configured password must answer 503, got %d", page.status)
	}

	f := newFixture(t, password)
	page := f.get("/admin")
	if page.status != http.StatusUnauthorized || !strings.HasPrefix(page.headers.Get("WWW-Authenticate"), "Basic") {
		t.Fatalf("missing credentials: %d %q", page.status, page.headers.Get("WWW-Authenticate"))
	}
	page = f.request(http.MethodGet, "/admin", nil, func(req *http.Request) { req.SetBasicAuth("admin", "wrong") })
	if page.status != http.StatusUnauthorized {
		t.Fatalf("wrong password must be refused, got %d", page.status)
	}
	if page = f.get("/admin/keys"); page.status != http.StatusUnauthorized {
		t.Fatalf("keys page must be guarded, got %d", page.status)
	}
	if page = f.admin(http.MethodGet, "/admin", nil); page.status != http.StatusOK {
		t.Fatalf("valid credentials: %d", page.status)
	}
	mustContain(t, page.body, "Nothing is waiting.")
}

func TestAdminQueueAndActions(t *testing.T) {
	f := newFixture(t, password)
	pendingID, env := f.share("Queue <script>alert(1)</script>", authorAddress, "youtube.com")
	f.build()

	page := f.admin(http.MethodGet, "/admin", nil)
	if page.status != http.StatusOK {
		t.Fatalf("queue: %d", page.status)
	}
	mustContain(t, page.body, "Pending (1)", "Queue &lt;script&gt;alert(1)&lt;/script&gt;", pendingID+"/1", "seen from AS64500 RU",
		"www.google.com <span class=\"muted\">(attached TLS capture)</span>", "needs_payload", "/b4/hub/blob/"+env.Payloads[0].SHA256,
		"/admin/sets/"+pendingID+"/1/approve", "/admin/sets/"+pendingID+"/1/reject", "/admin/sets/"+pendingID+"/1/hide", "&#34;payload_file&#34;: &#34;sha256:")
	mustNotContain(t, page.body, "<script>alert(1)</script>")

	if page = f.get("/"); strings.Contains(page.body, pendingID) {
		t.Fatalf("a pending set must not be public")
	}

	page = f.admin(http.MethodPost, "/admin/sets/"+pendingID+"/1/reject", url.Values{})
	if page.status != http.StatusBadRequest {
		t.Fatalf("reject without a reason must be refused, got %d", page.status)
	}
	page = f.request(http.MethodPost, "/admin/sets/"+pendingID+"/1/approve", url.Values{}, func(req *http.Request) {
		asAdmin(req)
		req.Header.Set("Sec-Fetch-Site", "cross-site")
	})
	if page.status != http.StatusForbidden {
		t.Fatalf("cross-site posts must be refused, got %d", page.status)
	}

	page = f.admin(http.MethodPost, "/admin/sets/"+pendingID+"/1/approve", url.Values{})
	if page.status != http.StatusSeeOther || !strings.HasPrefix(page.location, "/admin?notice=") {
		t.Fatalf("approve: %d %q", page.status, page.location)
	}
	afterApprove := page.location
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
	if page = f.get("/s/" + pendingID); page.status != http.StatusOK {
		t.Fatalf("approved set must be public, got %d", page.status)
	}
	page = f.admin(http.MethodGet, afterApprove, nil)
	mustContain(t, page.body, "Pending (0)", "Listed (1)", "approved "+pendingID+"/1")

	page = f.admin(http.MethodPost, "/admin/sets/"+pendingID+"/1/hide", url.Values{"reason": {"breaks video on ISP A"}})
	if page.status != http.StatusSeeOther {
		t.Fatalf("hide: %d", page.status)
	}
	v, _ = f.store.GetVersion(context.Background(), pendingID, 1)
	if v.Status != hubwire.SetStatusHidden || v.StatusReason != "breaks video on ISP A" {
		t.Fatalf("hidden version: %+v", v)
	}
	if latest := f.builder.Latest(); latest.ByID[pendingID] != nil {
		t.Fatalf("a hidden set must leave the catalogue")
	}
	page = f.admin(http.MethodGet, "/admin", nil)
	mustContain(t, page.body, "Hidden (1)", "breaks video on ISP A")

	rejectedID, _ := f.share("Second", otherAddress, "example.net")
	page = f.admin(http.MethodPost, "/admin/sets/"+rejectedID+"/1/reject", url.Values{"reason": {"private domain"}})
	if page.status != http.StatusSeeOther {
		t.Fatalf("reject: %d", page.status)
	}
	v, _ = f.store.GetVersion(context.Background(), rejectedID, 1)
	if v.Status != hubwire.SetStatusRejected || v.StatusReason != "private domain" {
		t.Fatalf("rejected version: %+v", v)
	}
	page = f.admin(http.MethodGet, "/admin", nil)
	mustContain(t, page.body, "Rejected (1)", "private domain")

	if page = f.admin(http.MethodPost, "/admin/sets/"+rejectedID+"/9/approve", url.Values{}); page.status != http.StatusNotFound {
		t.Fatalf("unknown version must be 404, got %d", page.status)
	}
	if page = f.admin(http.MethodPost, "/admin/sets/"+rejectedID+"/1/explode", url.Values{}); page.status != http.StatusNotFound {
		t.Fatalf("unknown action must be 404, got %d", page.status)
	}
}

func TestAdminKeys(t *testing.T) {
	f := newFixture(t, password)
	setID, _ := f.share("Keyed", authorAddress, "youtube.com")
	v, err := f.store.GetVersion(context.Background(), setID, 1)
	if err != nil {
		t.Fatal(err)
	}
	page := f.admin(http.MethodGet, "/admin/keys", nil)
	if page.status != http.StatusOK {
		t.Fatalf("keys: %d", page.status)
	}
	mustContain(t, page.body, "Keys (1)", v.UploaderHMAC, "/admin/keys/"+v.UploaderHMAC+"/ban", "<td>1</td>")

	page = f.admin(http.MethodPost, "/admin/keys/"+v.UploaderHMAC+"/ban", url.Values{"reason": {"spam"}})
	if page.status != http.StatusSeeOther || !strings.HasPrefix(page.location, "/admin/keys?notice=") {
		t.Fatalf("ban: %d %q", page.status, page.location)
	}
	key, err := f.store.GetKey(context.Background(), v.UploaderHMAC)
	if err != nil || !key.Banned || key.BanReason != "spam" {
		t.Fatalf("banned key: %v %+v", err, key)
	}
	if f.rebuilds != 1 {
		t.Fatalf("a ban must rebuild the catalogue, rebuilds %d", f.rebuilds)
	}
	page = f.admin(http.MethodGet, "/admin/keys", nil)
	mustContain(t, page.body, "banned", "spam", "/admin/keys/"+v.UploaderHMAC+"/unban")

	if page = f.admin(http.MethodPost, "/admin/keys/"+v.UploaderHMAC+"/unban", url.Values{}); page.status != http.StatusSeeOther {
		t.Fatalf("unban: %d", page.status)
	}
	if key, _ = f.store.GetKey(context.Background(), v.UploaderHMAC); key.Banned {
		t.Fatalf("key must be unbanned")
	}
	if page = f.admin(http.MethodPost, "/admin/keys/not-a-key/ban", url.Values{}); page.status != http.StatusNotFound {
		t.Fatalf("malformed key must be 404, got %d", page.status)
	}
}
