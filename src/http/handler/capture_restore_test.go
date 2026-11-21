package handler

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/capture"
)

func backupArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	tw := tar.NewWriter(zw)
	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return gz.Bytes()
}

func TestARestoredPayloadListSurvivesTheNextPayloadSave(t *testing.T) {
	mux, api := captureTestMux(t)
	api.RegisterBackupApi()
	manager := capture.GetManager(api.getCfg())
	const restored, later = "restored.restore.example", "after-restore.restore.example"
	const restoredFile = "tls_restored_restore_example.bin"
	t.Cleanup(func() {
		manager.DeleteCapture("tls", restored)
		manager.DeleteCapture("tls", later)
		os.Remove(filepath.Join(manager.GetOutputPath(), restoredFile))
	})

	meta := map[string]map[string]any{restored: {"tls": map[string]any{"timestamp": "2026-11-01T00:00:00Z", "size": 3, "filepath": restoredFile}}}
	for _, c := range manager.ListCaptures() {
		meta[c.Domain] = map[string]any{c.Protocol: map[string]any{"timestamp": c.Timestamp, "size": c.Size, "filepath": filepath.Base(c.Filepath)}}
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	archive := backupArchive(t, map[string][]byte{
		"captures/payloads.json":   metaJSON,
		"captures/" + restoredFile: {0x16, 0x03, 0x01},
	})

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "b4-backup.tar.gz")
	fw.Write(archive)
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/backup/restore", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec, panicked := serveRecovered(mux, req)
	if panicked != "" || rec.Code != http.StatusOK {
		t.Fatalf("restore: %d %s %s", rec.Code, rec.Body.String(), panicked)
	}
	if _, listed := manager.GetCapture("tls", restored); !listed {
		t.Errorf("the restore wrote captures/payloads.json with %s, but the Payloads page does not list it until a restart", restored)
	}

	if up := uploadCapturePayload(t, mux, later, "tls", []byte{0x16, 0x03, 0x03}); up.Code != http.StatusOK {
		t.Fatalf("upload after restore: %d %s", up.Code, up.Body.String())
	}
	onDisk, err := os.ReadFile(filepath.Join(manager.GetOutputPath(), "payloads.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(onDisk), restored) {
		t.Errorf("the next upload after the restore rewrote payloads.json from the list held before it: %s is gone (%d bytes left) though its file %s is on disk",
			restored, len(onDisk), restoredFile)
	}
}
