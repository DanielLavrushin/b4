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
	"reflect"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/capture"
	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sock"
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

func TestAPayloadChangeAfterARestoreLeavesTheRestoredConfigOnDisk(t *testing.T) {
	mux, api := captureTestMux(t)
	api.RegisterBackupApi()
	manager := capture.GetManager(api.getCfg())
	const name = "restored-config.restore.example"
	file, err := manager.SaveImportedPayload("tls", name, payloadTestHello(0x31, 300))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.DeleteCapture("tls", name) })
	set := namedSet("before", "set before the restore", true, "a.example")
	set.Faking.SNI = true
	set.Faking.SNIType = config.FakePayloadCapture
	set.Faking.PayloadFile = file
	useSets(api, set)
	configPath := api.getCfg().ConfigPath
	if err := api.getCfg().SaveToFile(configPath); err != nil {
		t.Fatal(err)
	}

	restoredCfg := config.NewConfig()
	restoredCfg.Sets = []*config.SetConfig{namedSet("restored", "set of the backup", true, "restored.example")}
	scratch := filepath.Join(t.TempDir(), "config.json")
	if err := restoredCfg.SaveToFile(scratch); err != nil {
		t.Fatal(err)
	}
	restoredJSON, err := os.ReadFile(scratch)
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "b4-backup.tar.gz")
	fw.Write(backupArchive(t, map[string][]byte{filepath.Base(configPath): restoredJSON}))
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/backup/restore", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if rec, panicked := serveRecovered(mux, req); panicked != "" || rec.Code != http.StatusOK {
		t.Fatalf("restore: %d %s %s", rec.Code, rec.Body.String(), panicked)
	}

	rec, _ := serveRecovered(mux, httptest.NewRequest(http.MethodDelete, "/api/capture/delete?protocol=tls&domain="+name, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	if got := reloadedBy(t, rec); !reflect.DeepEqual(got, []any{"set before the restore"}) {
		t.Errorf("the delete answered reloaded=%v, want the running set that names the file", got)
	}
	if onDisk, _ := os.ReadFile(configPath); !bytes.Equal(onDisk, restoredJSON) {
		t.Errorf("deleting a payload after a restore rewrote %s from the config held before the restore (restored set kept: %t, earlier set back: %t)",
			filepath.Base(configPath), strings.Contains(string(onDisk), "set of the backup"), strings.Contains(string(onDisk), "set before the restore"))
	}
	if got := sock.GetPayload(&runningSet(api).Faking); !bytes.Equal(got, config.FakeSNI1) {
		t.Errorf("after the delete the running set sends %d bytes, not the Google preset it falls back to", len(got))
	}
}
