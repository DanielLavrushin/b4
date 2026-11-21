package handler

import (
	"encoding/json"
	"github.com/daniellavrushin/b4/nfq"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCollectedEndpointsMethodsValidationAndEmptyPool(t *testing.T) {
	saved := globalPool
	t.Cleanup(func() { globalPool = saved })
	api := &API{}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterCollectedIPsApi()
	for _, pool := range []*nfq.Pool{nil, {}} {
		globalPool = pool
		rec := serveIPBans(mux, http.MethodGet, "/api/collected-ips", "")
		var response CollectedIPsResponse
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &response) != nil || response.Items == nil || len(response.Items) != 0 {
			t.Fatal("missing items array", rec.Body.String())
		}
		for _, body := range []string{"", "{}", `{"ip":"1.2.3.4"}`, `{"ip":"2001:4860:4860::8888"}`} {
			rec = serveIPBans(mux, http.MethodPost, "/api/collected-ips/clear", body)
			if rec.Code != 200 {
				t.Fatal(body, rec.Code, rec.Body.String())
			}
		}
	}
	for _, test := range []struct {
		method, path, body string
		status             int
	}{
		{"POST", "/api/collected-ips", "", 405}, {"GET", "/api/collected-ips/clear", "", 405},
		{"POST", "/api/collected-ips/clear", `{"ip":"bad"}`, 400},
		{"POST", "/api/collected-ips/clear", `{"ip":"1.2.3.4:443"}`, 400},
		{"POST", "/api/collected-ips/clear", `{"ip":"1.2.3.0/24"}`, 400},
		{"POST", "/api/collected-ips/clear", `{"ip":"fe80::1%eth0"}`, 400},
		{"POST", "/api/collected-ips/clear", `{"unexpected":true}`, 400},
		{"POST", "/api/collected-ips/clear", `{} {}`, 400},
		{"POST", "/api/collected-ips/clear", `null`, 400},
		{"POST", "/api/collected-ips/clear", `{"ip":null}`, 400},
		{"POST", "/api/collected-ips/clear", `{"ip":"` + strings.Repeat("a", 5000) + `"}`, 400},
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(test.method, test.path, strings.NewReader(test.body)))
		if rec.Code != test.status {
			t.Fatal(test.method, test.path, test.body, rec.Code)
		}
	}
}
