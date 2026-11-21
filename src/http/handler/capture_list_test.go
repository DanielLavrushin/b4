package handler

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

func listedPayloadObjects(t *testing.T, mux *http.ServeMux) []map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/capture/list", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var listed []map[string]any
	decodeInto(t, rec, &listed)
	return listed
}

func listedPayloadObject(t *testing.T, mux *http.ServeMux, protocol, name string) map[string]any {
	t.Helper()
	for _, entry := range listedPayloadObjects(t, mux) {
		if entry["domain"] == name && entry["protocol"] == protocol {
			return entry
		}
	}
	t.Fatalf("the %s payload %q is not listed", protocol, name)
	return nil
}

func TestThePayloadListCarriesNoFileBytes(t *testing.T) {
	mux, api := captureTestMux(t)
	manager := capture.GetManager(api.getCfg())
	const name = "heavy.list.example"
	data := bytes.Repeat([]byte{0x16, 0x03, 0x01, 0x42}, 512)
	file, err := manager.SaveImportedPayload("tls", name, data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.DeleteCapture("tls", name) })

	entry := listedPayloadObject(t, mux, "tls", name)
	if hexData, carried := entry["hex_data"]; carried {
		t.Errorf("the list entry carries hex_data (%d characters), so every picker mount downloads every payload's bytes", len(fmt.Sprint(hexData)))
	}
	if entry["filepath"] != file || entry["size"] != float64(len(data)) {
		t.Errorf("the list entry must still name the file and its size, got %v", entry)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/capture/download?file="+filepath.Base(file), nil))
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), data) {
		t.Errorf("the per-payload request must return the file's %d bytes, got %d and %d bytes", len(data), rec.Code, rec.Body.Len())
	}
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
