package handler

import (
	"bytes"
	"encoding/json"
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

func payloadTestHello(fill byte, size int) []byte {
	p := bytes.Repeat([]byte{fill}, size)
	p[0], p[1], p[2], p[5] = 0x16, 0x03, 0x01, 0x01
	p[3], p[4] = byte((size-5)>>8), byte(size-5)
	return p
}

func reloadedBy(t *testing.T, rec *httptest.ResponseRecorder) any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("answer %q: %v", rec.Body.String(), err)
	}
	return out["reloaded"]
}

func runningSet(api *API) *config.SetConfig {
	return api.getCfg().Sets[0]
}

func useSets(api *API, sets ...*config.SetConfig) {
	cfg := api.getCfg()
	cfg.Sets = sets
	cfg.LoadCapturePayloads()
}

func TestADeletedPayloadStopsGoingOutAtOnce(t *testing.T) {
	mux, api := captureTestMux(t)
	manager := capture.GetManager(api.getCfg())
	const name = "deleted.reload.example"
	tcpBytes, udpBytes := payloadTestHello(0xab, 300), bytes.Repeat([]byte{0xcd}, 200)
	tcpFile, err := manager.SaveImportedPayload("tls", name, tcpBytes)
	if err != nil {
		t.Fatal(err)
	}
	udpFile, err := manager.SaveImportedPayload("quic", name, udpBytes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		manager.DeleteCapture("tls", name)
		manager.DeleteCapture("quic", name)
	})
	set := namedSet("del", "uses the file", true, "a.example")
	set.Faking.SNI = true
	set.Faking.SNIType = config.FakePayloadCapture
	set.Faking.PayloadFile = tcpFile
	set.UDP.Mode = config.UDPModeFake
	set.UDP.FakePayloadFile = udpFile
	set.UDP.FakeLen = 64
	useSets(api, set)
	if !bytes.Equal(sock.GetPayload(&runningSet(api).Faking), tcpBytes) {
		t.Fatal("setup: the set does not send the TCP payload")
	}

	for _, p := range []string{"tls", "quic"} {
		rec, _ := serveRecovered(mux, httptest.NewRequest(http.MethodDelete, "/api/capture/delete?protocol="+p+"&domain="+name, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("delete %s: %d %s", p, rec.Code, rec.Body.String())
		}
		if got := reloadedBy(t, rec); !reflect.DeepEqual(got, []any{"uses the file"}) {
			t.Errorf("delete %s answered reloaded=%v, want the set that names the file", p, got)
		}
	}
	if got := sock.GetPayload(&runningSet(api).Faking); !bytes.Equal(got, config.FakeSNI1) {
		t.Errorf("after the delete the TCP fake carries %d bytes (the deleted file: %t), the dialog promises Preset: Google (classic), %d bytes",
			len(got), bytes.Equal(got, tcpBytes), len(config.FakeSNI1))
	}
	if got := runningSet(api).UDP.FakePayloadData; len(got) != 0 {
		t.Errorf("after the delete the UDP fake carries %d bytes of the deleted file, the dialog promises zeros", len(got))
	}
}

func TestAReuploadedPayloadReachesTheSetsThatUseIt(t *testing.T) {
	mux, api := captureTestMux(t)
	manager := capture.GetManager(api.getCfg())
	const name = "reupload.reload.example"
	oldBytes, newBytes := payloadTestHello(0x11, 400), payloadTestHello(0x22, 517)
	file, err := manager.SaveImportedPayload("tls", name, oldBytes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.DeleteCapture("tls", name) })
	set := namedSet("re", "reupload", true, "a.example")
	set.Faking.SNI = true
	set.Faking.SNIType = config.FakePayloadCapture
	set.Faking.PayloadFile = file
	useSets(api, set)

	rec := replaceCapturePayload(t, mux, name, "tls", newBytes)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}
	if got := reloadedBy(t, rec); !reflect.DeepEqual(got, []any{"reupload"}) {
		t.Errorf("the re-upload answered reloaded=%v, want the set that names the file", got)
	}
	if got := sock.GetPayload(&runningSet(api).Faking); !bytes.Equal(got, newBytes) {
		t.Errorf("the Payloads page lists %d bytes for %s, the set still sends %d bytes (the old upload: %t)",
			len(newBytes), file, len(got), bytes.Equal(got, oldBytes))
	}

	del, _ := serveRecovered(mux, httptest.NewRequest(http.MethodDelete, "/api/capture/delete?protocol=tls&domain="+name, nil))
	if del.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", del.Code, del.Body.String())
	}
	if got := sock.GetPayload(&runningSet(api).Faking); !bytes.Equal(got, config.FakeSNI1) {
		t.Errorf("after deleting %s the set still sends %d bytes, not the Google preset the delete dialog promises", file, len(got))
	}
}

func TestAnUploadReachesTheSetsThatNameItsFile(t *testing.T) {
	mux, api := captureTestMux(t)
	manager := capture.GetManager(api.getCfg())
	const name = "running.reload.example"
	t.Cleanup(func() { manager.DeleteCapture("tls", name) })
	first, second := payloadTestHello(0x31, 96), payloadTestHello(0x32, 120)

	if rec := uploadCapturePayload(t, mux, name, "tls", first); rec.Code != http.StatusOK {
		t.Fatalf("first upload: %d %s", rec.Code, rec.Body.String())
	} else if got := reloadedBy(t, rec); !reflect.DeepEqual(got, []any{}) {
		t.Errorf("an upload no set names answered reloaded=%v, want []", got)
	}
	saved, ok := manager.GetCapture("tls", name)
	if !ok {
		t.Fatal("the first upload is not listed")
	}
	set := namedSet("run", "running", true, "a.example")
	set.Faking.SNI = true
	set.Faking.SNIType = config.FakePayloadCapture
	set.Faking.PayloadFile = saved.Filepath
	useSets(api, set)
	if !bytes.Equal(runningSet(api).Faking.PayloadData, first) {
		t.Fatal("setup: the set did not load the first upload")
	}

	if rec := replaceCapturePayload(t, mux, name, "tls", second); rec.Code != http.StatusOK {
		t.Fatalf("second upload: %d %s", rec.Code, rec.Body.String())
	}
	if got := runningSet(api).Faking.PayloadData; !bytes.Equal(got, second) {
		t.Errorf("the upload replaced %s, but set %q still sends the earlier %d-byte payload until the next config apply or restart", saved.Filepath, set.Name, len(got))
	}
}

func TestAGeneratedPayloadReachesTheSetsThatNameItsFile(t *testing.T) {
	mux, api := captureTestMux(t)
	manager := capture.GetManager(api.getCfg())
	const name = "named-before.generate.example"
	const file = "captures/tls_named-before_generate_example.bin"
	manager.DeleteCapture("tls", name)
	t.Cleanup(func() { manager.DeleteCapture("tls", name) })
	set := namedSet("gen", "names the file before it exists", true, "a.example")
	set.Faking.SNI = true
	set.Faking.SNIType = config.FakePayloadCapture
	set.Faking.PayloadFile = file
	useSets(api, set)
	if got := sock.GetPayload(&runningSet(api).Faking); !bytes.Equal(got, config.FakeSNI1) {
		t.Fatalf("setup: the set must fall back to the Google preset, it sends %d bytes", len(got))
	}

	rec, _ := serveRecovered(mux, httptest.NewRequest(http.MethodPost, "/api/capture/generate", strings.NewReader(`{"domain":"`+name+`"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("generate: %d %s", rec.Code, rec.Body.String())
	}
	saved, ok := manager.GetCapture("tls", name)
	if !ok || saved.Filepath != file {
		t.Fatalf("generate saved %v, want the file %s", saved, file)
	}
	data, err := manager.LoadCaptureData(saved)
	if err != nil {
		t.Fatal(err)
	}
	got := sock.GetPayload(&runningSet(api).Faking)
	if reloaded := reloadedBy(t, rec); !reflect.DeepEqual(reloaded, []any{set.Name}) || !bytes.Equal(got, data) {
		t.Errorf("generate wrote %s (%d bytes), which set %q names, but answered reloaded=%v and the running set sends %d bytes (the Google preset: %t)",
			file, len(data), set.Name, reloaded, len(got), bytes.Equal(got, config.FakeSNI1))
	}
}

func TestUploadAndGenerateRepairAnUnlistedFileASetNames(t *testing.T) {
	mux, api := captureTestMux(t)
	manager := capture.GetManager(api.getCfg())
	for _, c := range []struct {
		action, name, file string
		send               func(name string) *httptest.ResponseRecorder
	}{
		{"upload", "unlisted-upload.repair.example", "captures/tls_unlisted-upload_repair_example.bin", func(name string) *httptest.ResponseRecorder {
			return uploadCapturePayload(t, mux, name, "tls", payloadTestHello(0x77, 444))
		}},
		{"generate", "unlisted-generate.repair.example", "captures/tls_unlisted-generate_repair_example.bin", func(name string) *httptest.ResponseRecorder {
			rec, _ := serveRecovered(mux, httptest.NewRequest(http.MethodPost, "/api/capture/generate", strings.NewReader(`{"domain":"`+name+`"}`)))
			return rec
		}},
	} {
		full := filepath.Join(filepath.Dir(api.getCfg().ConfigPath), c.file)
		stale := payloadTestHello(0x66, 222)
		if err := os.WriteFile(full, stale, 0644); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			manager.DeleteCapture("tls", c.name)
			os.Remove(full)
		})
		set := namedSet("repair", "names an unlisted file", true, "a.example")
		set.Faking.SNI = true
		set.Faking.SNIType = config.FakePayloadCapture
		set.Faking.PayloadFile = c.file
		useSets(api, set)

		rec := c.send(c.name)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", c.action, rec.Code, rec.Body.String())
		}
		storedAt := "nothing"
		if saved, ok := manager.GetCapture("tls", c.name); ok {
			storedAt = saved.Filepath
		}
		got := sock.GetPayload(&runningSet(api).Faking)
		if storedAt != c.file || bytes.Equal(got, stale) || !reflect.DeepEqual(reloadedBy(t, rec), []any{set.Name}) {
			t.Errorf("the %s of %s, whose file %s a set names but no entry lists, stored %s and answered reloaded=%v; the set still sends the stale file: %t",
				c.action, c.name, c.file, storedAt, reloadedBy(t, rec), bytes.Equal(got, stale))
		}
	}
}

func TestClearAllReachesTheSetsThatUsedItsFiles(t *testing.T) {
	mux, api := captureTestMux(t)
	manager := capture.GetManager(api.getCfg())
	tcpFile, err := manager.SaveImportedPayload("tls", "tcp.clear.example", payloadTestHello(0x41, 200))
	if err != nil {
		t.Fatal(err)
	}
	udpFile, err := manager.SaveImportedPayload("quic", "udp.clear.example", bytes.Repeat([]byte{0x42}, 100))
	if err != nil {
		t.Fatal(err)
	}
	tcpSet := namedSet("ctcp", "clear tcp", true, "a.example")
	tcpSet.Faking.SNI = true
	tcpSet.Faking.SNIType = config.FakePayloadCapture
	tcpSet.Faking.PayloadFile = tcpFile
	udpSet := namedSet("cudp", "clear udp", true, "b.example")
	udpSet.UDP.Mode = config.UDPModeFake
	udpSet.UDP.FakePayloadFile = udpFile
	udpSet.UDP.FakeLen = 64
	useSets(api, tcpSet, udpSet)

	rec, _ := serveRecovered(mux, httptest.NewRequest(http.MethodPost, "/api/capture/clear", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("clear: %d %s", rec.Code, rec.Body.String())
	}
	if got := reloadedBy(t, rec); !reflect.DeepEqual(got, []any{"clear tcp", "clear udp"}) {
		t.Errorf("Clear All answered reloaded=%v, want both sets", got)
	}
	sets := api.getCfg().Sets
	if got := sock.GetPayload(&sets[0].Faking); !bytes.Equal(got, config.FakeSNI1) {
		t.Errorf("after Clear All the TCP fake carries %d bytes of a cleared file, not Preset: Google (classic)", len(got))
	}
	if got := sets[1].UDP.FakePayloadData; len(got) != 0 {
		t.Errorf("after Clear All the UDP fake carries %d bytes of a cleared file, not zeros", len(got))
	}
}

func TestAPayloadChangeWhoseReloadFailsSaysSo(t *testing.T) {
	mux, api := captureTestMux(t)
	manager := capture.GetManager(api.getCfg())
	const name = "failing.reload.example"
	file, err := manager.SaveImportedPayload("tls", name, payloadTestHello(0x51, 90))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.DeleteCapture("tls", name) })
	set := namedSet("fail", "cannot reload", true, "a.example")
	set.Faking.SNI = true
	set.Faking.SNIType = config.FakePayloadCapture
	set.Faking.PayloadFile = file
	useSets(api, set)
	api.getCfg().System.MemoryLimit = "not a size"

	rec := replaceCapturePayload(t, mux, name, "tls", payloadTestHello(0x52, 110))
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("upload answered %d %q: %v", rec.Code, rec.Body.String(), err)
	}
	reloadErr, _ := out["reload_error"].(string)
	if rec.Code != http.StatusOK || out["success"] != true || reloadErr == "" || !reflect.DeepEqual(out["reloaded"], []any{}) {
		t.Errorf("an upload whose set reload fails must answer 200 success with reloaded=[] and the reload_error, got %d %s", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
}
