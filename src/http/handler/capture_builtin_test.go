package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestTheBuiltInPayloadsAreListedWithTheirSource(t *testing.T) {
	mux, _ := captureTestMux(t)
	rec, _ := serveRecovered(mux, httptest.NewRequest(http.MethodGet, "/api/capture/builtin", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/capture/builtin answered %d %q, want 200", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	var rows []map[string]any
	decodeInto(t, rec, &rows)

	want := []struct {
		id, protocol string
		data         []byte
		kind, sni    string
		source       string
	}{
		{"google", "tls", config.FakeSNI1, "tls_client_hello", "www.google.com", "youtubeunblock"},
		{"vk", "tls", config.FakeSNI2, "tls_client_hello", "vk.com", "youtubeunblock_derived"},
		{"stun", "tls", config.FakeSTUN, "stun", "", "zapret"},
		{"quic1", "quic", config.FakeQUIC1, "quic_initial", "www.google.com", "zapret"},
		{"quic2", "quic", config.FakeQUIC2, "quic_initial", "dbankcloud.ru", ""},
	}
	if len(rows) != len(want) {
		t.Fatalf("the built-in list holds %d rows, want %d: %v", len(rows), len(want), rows)
	}
	for i, w := range want {
		row := rows[i]
		requireKeys(t, "a built-in row", row, "id", "protocol", "size", "facts", "source")
		facts := objectAt(t, "a built-in row", row, "facts")
		if row["id"] != w.id || row["protocol"] != w.protocol || row["size"] != float64(len(w.data)) || row["source"] != w.source {
			t.Errorf("built-in row %d is %v, want id %s, protocol %s, %d bytes, source %q", i, row, w.id, w.protocol, len(w.data), w.source)
		}
		if facts["kind"] != w.kind || facts["sni"] != w.sni {
			t.Errorf("built-in %s has facts %v, want kind %s and SNI %q", w.id, facts, w.kind, w.sni)
		}

		rec, _ := serveRecovered(mux, httptest.NewRequest(http.MethodGet, "/api/capture/builtin/download?id="+w.id, nil))
		if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), w.data) {
			t.Errorf("downloading built-in %s answered %d with %d bytes, want 200 with its %d bytes", w.id, rec.Code, rec.Body.Len(), len(w.data))
		}
		if got := rec.Header().Get("Content-Type"); got != "application/octet-stream" {
			t.Errorf("downloading built-in %s answered Content-Type %q", w.id, got)
		}
	}

	for _, id := range []string{"", "nope", "../quic1"} {
		rec, _ := serveRecovered(mux, httptest.NewRequest(http.MethodGet, "/api/capture/builtin/download?id="+id, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("downloading built-in %q answered %d, want 404", id, rec.Code)
		}
	}
}
