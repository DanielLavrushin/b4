package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/geodat"
)

const testIPInfoToken = "s3cr3t+token/with&chars"

func ipinfoAPI(t *testing.T, token string) *http.ServeMux {
	t.Helper()
	cfg := config.NewConfig()
	cfg.System.API.IPInfoToken = token
	api := &API{cfgPtr: testCfgPtr(&cfg), geodataManager: geodat.NewGeodataManager("", "")}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterIntegrationApi()
	return mux
}

func useIPInfo(t *testing.T, base string, client *http.Client) {
	t.Helper()
	prevBase, prevClient := ipinfoBaseURL, ipinfoClient
	ipinfoBaseURL = base
	ipinfoClient = func() *http.Client { return client }
	t.Cleanup(func() { ipinfoBaseURL, ipinfoClient = prevBase, prevClient })
}

func TestIPInfoRelaysTheRecordWithAnEscapedToken(t *testing.T) {
	var gotPath, gotToken atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath.Store(r.URL.Path)
		gotToken.Store(r.URL.Query().Get("token"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ip":"2001:4860:4860::8888","org":"AS15169 Google LLC"}`))
	}))
	defer srv.Close()
	useIPInfo(t, srv.URL, srv.Client())
	mux := ipinfoAPI(t, testIPInfoToken)

	rec := getJSON(t, mux, "/api/integration/ipinfo?ip=%5B2001:4860:4860::8888%5D:443")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "AS15169 Google LLC") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if gotPath.Load() != "/2001:4860:4860::8888" || gotToken.Load() != testIPInfoToken {
		t.Errorf("the address is a clean path segment and the token survives escaping: %v %v", gotPath.Load(), gotToken.Load())
	}
	if strings.Contains(rec.Body.String(), testIPInfoToken) {
		t.Error("the token is never echoed")
	}
}

func TestIPInfoTransportFailureNeverLeaksTheToken(t *testing.T) {
	buf := captureLog(t)
	useIPInfo(t, deadServerURL(t), &http.Client{})
	mux := ipinfoAPI(t, testIPInfoToken)

	rec := getJSON(t, mux, "/api/integration/ipinfo?ip=1.1.1.1")
	expectCode(t, rec, http.StatusBadGateway, "ipinfo_failed")
	for _, leak := range []string{testIPInfoToken, "s3cr3t", "token="} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Errorf("response leaks %q: %s", leak, rec.Body.String())
		}
		if strings.Contains(buf.String(), leak) {
			t.Errorf("log leaks %q: %s", leak, buf.String())
		}
	}
}

func TestIPInfoUpstreamErrorsAreJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad token", http.StatusForbidden)
	}))
	defer srv.Close()
	useIPInfo(t, srv.URL, srv.Client())
	mux := ipinfoAPI(t, testIPInfoToken)

	rec := getJSON(t, mux, "/api/integration/ipinfo?ip=1.1.1.1")
	expectCode(t, rec, http.StatusBadGateway, "ipinfo_failed")
	if !strings.Contains(rec.Body.String(), "token was rejected") {
		t.Errorf("a 403 says the token was rejected: %s", rec.Body.String())
	}
}

func TestIPInfoValidatesTheRequest(t *testing.T) {
	useIPInfo(t, "http://127.0.0.1:1", &http.Client{})
	expectCode(t, getJSON(t, ipinfoAPI(t, ""), "/api/integration/ipinfo?ip=1.1.1.1"), http.StatusBadRequest, "ipinfo_token_missing")
	mux := ipinfoAPI(t, testIPInfoToken)
	expectCode(t, getJSON(t, mux, "/api/integration/ipinfo?ip=../../me"), http.StatusBadRequest, "ip_invalid")
	expectCode(t, getJSON(t, mux, "/api/integration/ipinfo"), http.StatusBadRequest, "ip_invalid")
	if rec := serve(mux, http.MethodPost, "/api/integration/ipinfo?ip=1.1.1.1", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET only: %d", rec.Code)
	}
}

func TestScrubIPInfoErrorRemovesTheTokenInAnyForm(t *testing.T) {
	err := scrubIPInfoError(&testErr{"Get https://ipinfo.io/1.1.1.1?token=s3cr3t%2Btoken%2Fwith%26chars: boom " + testIPInfoToken}, testIPInfoToken)
	if strings.Contains(err.Error(), "s3cr3t") {
		t.Fatalf("scrubbed: %v", err)
	}
}

type testErr struct{ s string }

func (e *testErr) Error() string { return e.s }
