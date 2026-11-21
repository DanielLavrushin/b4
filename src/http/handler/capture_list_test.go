package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/capture"
)

func listedPayloads(t *testing.T, mux *http.ServeMux) []capture.Capture {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/capture/list", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var listed []capture.Capture
	decodeInto(t, rec, &listed)
	return listed
}

func TestThePayloadListKeepsOneSortedOrder(t *testing.T) {
	mux, api := captureTestMux(t)
	manager := capture.GetManager(api.getCfg())
	saved := []struct{ protocol, name string }{{"tls", "order-both.list.example"}, {"quic", "order-both.list.example"}}
	for _, i := range []int{5, 2, 7, 0, 3, 6, 1, 4} {
		saved = append(saved, struct{ protocol, name string }{"tls", fmt.Sprintf("order-%d.list.example", i)})
	}
	for i, s := range saved {
		if _, err := manager.SaveImportedPayload(s.protocol, s.name, []byte{0x16, 0x03, byte(i)}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { manager.DeleteCapture(s.protocol, s.name) })
	}

	order := func() string {
		var keys []string
		for _, c := range listedPayloads(t, mux) {
			if strings.HasPrefix(c.Domain, "order-") && strings.HasSuffix(c.Domain, ".list.example") {
				keys = append(keys, strings.TrimSuffix(strings.TrimPrefix(c.Domain, "order-"), ".list.example")+"/"+c.Protocol)
			}
		}
		return strings.Join(keys, " ")
	}
	const want = "0/tls 1/tls 2/tls 3/tls 4/tls 5/tls 6/tls 7/tls both/quic both/tls"
	seen := map[string]bool{}
	for range 21 {
		seen[order()] = true
	}
	if len(seen) != 1 || !seen[want] {
		var orders []string
		for o := range seen {
			orders = append(orders, o)
		}
		t.Errorf("21 calls to /api/capture/list returned %d orders, want only %q (domain, then protocol); got e.g. %q", len(seen), want, orders[:min(3, len(orders))])
	}
}
