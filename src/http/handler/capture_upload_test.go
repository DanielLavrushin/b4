package handler

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func uploadCapturePayload(t *testing.T, mux *http.ServeMux, name, protocol string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "upload.bin")
	if err != nil {
		t.Fatal(err)
	}
	fw.Write(data)
	mw.WriteField("domain", name)
	mw.WriteField("protocol", protocol)
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/capture/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec, panicked := serveRecovered(mux, req)
	if panicked != "" {
		t.Fatalf("upload of %d bytes as %q panicked: %s", len(data), name, panicked)
	}
	return rec
}

func TestPayloadUploadSaysWhyTheFormFailed(t *testing.T) {
	mux, _ := captureTestMux(t)

	req := httptest.NewRequest(http.MethodPost, "/api/capture/upload", strings.NewReader(`{"domain":"x.example"}`))
	req.Header.Set("Content-Type", "application/json")
	rec, _ := serveRecovered(mux, req)
	text := strings.TrimSpace(rec.Body.String())
	if rec.Code != http.StatusBadRequest || strings.Contains(strings.ToLower(text), "too large") || !strings.Contains(strings.ToLower(text), "form") {
		t.Errorf("a 22-byte request that is not multipart was answered %d %q, want 400 naming the upload form, not the size", rec.Code, text)
	}

	rec = uploadCapturePayload(t, mux, "oversized.upload.example", "tls", bytes.Repeat([]byte{0x16}, 200<<10))
	text = strings.TrimSpace(rec.Body.String())
	if rec.Code != http.StatusBadRequest || !strings.Contains(strings.ToLower(text), "too large") {
		t.Errorf("a 200 KiB upload was answered %d %q, want 400 saying the file is too large", rec.Code, text)
	}
}
