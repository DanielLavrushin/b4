package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/nfq"
)

func newIPBansTestMux(t *testing.T) *http.ServeMux {
	t.Helper()
	cfg := config.NewConfig()
	api := &API{cfgPtr: testCfgPtr(&cfg)}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterIPBansApi()
	return mux
}

func serveIPBans(mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	mux.ServeHTTP(rec, req)
	return rec
}

func TestIPBansEndpointsRejectOtherMethods(t *testing.T) {
	mux := newIPBansTestMux(t)
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/ipbans"},
		{http.MethodGet, "/api/ipbans/clear"},
		{http.MethodDelete, "/api/ipbans/clear"},
	} {
		if rec := serveIPBans(mux, c.method, c.path, ""); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: got %d, want 405", c.method, c.path, rec.Code)
		}
	}
}

func TestGetIPBansAlwaysReturnsAnItemsArray(t *testing.T) {
	mux := newIPBansTestMux(t)
	saved := globalPool
	t.Cleanup(func() { globalPool = saved })

	for _, pool := range []*nfq.Pool{nil, {}} {
		globalPool = pool
		rec := serveIPBans(mux, http.MethodGet, "/api/ipbans", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("pool %v: got %d", pool != nil, rec.Code)
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(string(raw["items"])); got != "[]" {
			t.Errorf("pool %v: items = %s, want an empty array, never null", pool != nil, got)
		}
	}
}

func TestClearIPBansValidatesTheKey(t *testing.T) {
	mux := newIPBansTestMux(t)
	saved := globalPool
	t.Cleanup(func() { globalPool = saved })

	for _, pool := range []*nfq.Pool{nil, {}} {
		globalPool = pool
		for _, body := range []string{"", "{}", `{"key":""}`, `{"key":"74.125.172.167:443"}`, `{"key":" [2001:db8::1]:443 "}`} {
			rec := serveIPBans(mux, http.MethodPost, "/api/ipbans/clear", body)
			if rec.Code != http.StatusOK {
				t.Fatalf("pool %v body %q: got %d %s", pool != nil, body, rec.Code, rec.Body.String())
			}
			var resp IPBansClearResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatal(err)
			}
			if !resp.Success || resp.Cleared != 0 {
				t.Errorf("pool %v body %q: response %+v", pool != nil, body, resp)
			}
		}
	}

	for body, code := range map[string]string{
		`{"key":"rr2---sn-ab5l6nr6.googlevideo.com:443"}`: "bad_request",
		`{"key":"74.125.172.167"}`:                        "bad_request",
		`{"key":"74.125.172.167:0"}`:                      "bad_request",
		`{"key":`:                                         "invalid_json",
		`{"key":"` + strings.Repeat("1", 5000) + `"}`:     "invalid_json",
	} {
		rec := serveIPBans(mux, http.MethodPost, "/api/ipbans/clear", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %.40q: got %d, want 400", body, rec.Code)
			continue
		}
		var apiErr APIError
		if err := json.NewDecoder(rec.Body).Decode(&apiErr); err != nil {
			t.Fatal(err)
		}
		if apiErr.Code != code {
			t.Errorf("body %.40q: code %q, want %q", body, apiErr.Code, code)
		}
	}
}
