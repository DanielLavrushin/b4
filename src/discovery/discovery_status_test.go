package discovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newProbeOnlySuite(url string) *DiscoverySuite {
	host := strings.TrimPrefix(url, "http://")
	return &DiscoverySuite{
		CheckSuite: &CheckSuite{
			Domains: []DomainInput{{Domain: host, CheckURL: url}},
		},
		ipVersion: "ipv4",
		ctx:       context.Background(),
	}
}

func TestProbeRejectsHTTP400AsCorruptedByTheStrategy(t *testing.T) {
	body := "<html><head><title>400 Bad Request</title></head><body><center><h1>400 Bad Request</h1></center><hr><center>nginx/1.18.0</center></body></html>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	ds := newProbeOnlySuite(srv.URL)
	res := ds.fetchUsingIPForDomain(ds.Domains[0], 5*time.Second, "")

	if res.Status != CheckStatusFailed {
		t.Fatalf("HTTP 400 scored as %q; a Bad Request means the strategy mangled the request, not that it worked", res.Status)
	}
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status code %d recorded, want 400", res.StatusCode)
	}
}

func TestProbeStillAcceptsAHealthyPage(t *testing.T) {
	body := "<html><body>" + string(make([]byte, 2048)) + "</body></html>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	ds := newProbeOnlySuite(srv.URL)
	res := ds.fetchUsingIPForDomain(ds.Domains[0], 5*time.Second, "")

	if res.Status != CheckStatusComplete {
		t.Fatalf("a healthy 200 scored as %q (%s)", res.Status, res.Error)
	}
}
