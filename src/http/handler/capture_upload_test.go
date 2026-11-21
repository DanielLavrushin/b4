package handler

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/capture"
	"github.com/daniellavrushin/b4/config"
)

func uploadCapturePayload(t *testing.T, mux *http.ServeMux, name, protocol string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	return postCapturePayload(t, mux, name, protocol, data, false)
}

func replaceCapturePayload(t *testing.T, mux *http.ServeMux, name, protocol string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	return postCapturePayload(t, mux, name, protocol, data, true)
}

func postCapturePayload(t *testing.T, mux *http.ServeMux, name, protocol string, data []byte, replace bool) *httptest.ResponseRecorder {
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
	if replace {
		mw.WriteField("replace", "1")
	}
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/capture/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec, panicked := serveRecovered(mux, req)
	if panicked != "" {
		t.Fatalf("upload of %d bytes as %q panicked: %s", len(data), name, panicked)
	}
	return rec
}

func TestPayloadUploadOfABlankNameIsRefused(t *testing.T) {
	mux, api := captureTestMux(t)
	manager := capture.GetManager(api.getCfg())
	t.Cleanup(func() { manager.DeleteCapture("tls", "") })
	for _, name := range []string{"   ", "\t\n"} {
		rec := uploadCapturePayload(t, mux, name, "tls", []byte{0x16, 0x03, 0x01})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("an upload named %q was answered %d %q, want 400", name, rec.Code, strings.TrimSpace(rec.Body.String()))
		}
	}
	if _, saved := manager.GetCapture("tls", ""); saved {
		t.Errorf("a payload was saved under the empty name; Delete refuses an empty name, so only Clear All removes it")
	}
}

func TestPayloadUploadNamesTheFormAcceptsGetTheirOwnFile(t *testing.T) {
	mux, api := captureTestMux(t)
	manager := capture.GetManager(api.getCfg())
	uploads := map[string][]byte{"пример.рф": bytes.Repeat([]byte{0x41}, 150), "тест.рф": bytes.Repeat([]byte{0x42}, 250)}
	for name, data := range uploads {
		t.Cleanup(func() { manager.DeleteCapture("tls", name) })
		if rec := uploadCapturePayload(t, mux, name, "tls", data); rec.Code != http.StatusOK {
			t.Fatalf("upload %q: %d %s", name, rec.Code, rec.Body.String())
		}
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/capture/list", nil))
	var listed []capture.Capture
	decodeInto(t, rec, &listed)
	files := map[string]string{}
	for _, c := range listed {
		if _, ours := uploads[c.Domain]; !ours {
			continue
		}
		files[c.Domain] = c.Filepath
		data, err := manager.LoadCaptureData(&c)
		if err != nil || !bytes.Equal(data, uploads[c.Domain]) {
			t.Errorf("the %q card points at %s, which holds %d bytes (%v), not its own %d-byte upload", c.Domain, c.Filepath, len(data), err, len(uploads[c.Domain]))
		}
	}
	if len(files) != 2 || files["пример.рф"] == files["тест.рф"] {
		t.Errorf("the cards пример.рф and тест.рф must point at files of their own, got %v", files)
	}
}

func TestPayloadUploadTakesAFileOfTheLargestPayloadSize(t *testing.T) {
	mux, api := captureTestMux(t)
	manager := capture.GetManager(api.getCfg())
	const name = "largest.upload.example"
	t.Cleanup(func() { manager.DeleteCapture("tls", name) })
	limit := strconv.Itoa(config.MaxCapturePayloadBytes)

	rec := uploadCapturePayload(t, mux, name, "tls", bytes.Repeat([]byte{0x16}, config.MaxCapturePayloadBytes))
	if rec.Code != http.StatusOK {
		t.Errorf("a %d-byte file, the largest a payload may have, was answered %d %q", config.MaxCapturePayloadBytes, rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	rec = uploadCapturePayload(t, mux, name+".over", "tls", bytes.Repeat([]byte{0x16}, config.MaxCapturePayloadBytes+1))
	if text := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusBadRequest || !strings.Contains(text, limit) {
		t.Errorf("a %d-byte file was answered %d %q, want 400 naming the %s-byte limit", config.MaxCapturePayloadBytes+1, rec.Code, text, limit)
	}
	if _, saved := manager.GetCapture("tls", name+".over"); saved {
		manager.DeleteCapture("tls", name+".over")
		t.Errorf("a file over the limit was saved; loading refuses it")
	}
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
