package handler

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func newBackupAPI(t *testing.T, configDir string) *http.ServeMux {
	t.Helper()
	cfg := config.NewConfig()
	cfg.ConfigPath = filepath.Join(configDir, "b4.json")
	api := &API{cfgPtr: testCfgPtr(&cfg)}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterBackupApi()
	return mux
}

func readArchive(t *testing.T, body io.Reader) map[string]string {
	t.Helper()
	gr, err := gzip.NewReader(body)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer gr.Close()

	out := map[string]string{}
	tr := tar.NewReader(gr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read %s: %v", header.Name, err)
		}
		out[header.Name] = string(data)
	}
	return out
}

func TestBackupIncludesFilesWithExecutableBit(t *testing.T) {
	dir := t.TempDir()
	writeFileMode(t, filepath.Join(dir, "b4.json"), `{"version":1}`, 0777)
	writeFileMode(t, filepath.Join(dir, "discovery_history.json"), `[]`, 0777)
	if err := os.Mkdir(filepath.Join(dir, "captures"), 0777); err != nil {
		t.Fatal(err)
	}
	writeFileMode(t, filepath.Join(dir, "captures", "quic.bin"), "payload", 0777)

	rec := httptest.NewRecorder()
	newBackupAPI(t, dir).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/backup", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	entries := readArchive(t, rec.Body)
	if entries["b4.json"] != `{"version":1}` {
		t.Errorf("b4.json missing or wrong from backup, entries: %v", keysOf(entries))
	}
	if entries["discovery_history.json"] != `[]` {
		t.Errorf("discovery_history.json missing from backup, entries: %v", keysOf(entries))
	}
	if entries["captures/quic.bin"] != "payload" {
		t.Errorf("captures/quic.bin missing from backup, entries: %v", keysOf(entries))
	}
}

func TestBackupExcludesGeodataAndOui(t *testing.T) {
	dir := t.TempDir()
	writeFileMode(t, filepath.Join(dir, "b4.json"), `{}`, 0644)
	writeFileMode(t, filepath.Join(dir, "geosite.dat"), "geo", 0644)
	writeFileMode(t, filepath.Join(dir, "geoip.dat"), "geo", 0644)
	writeFileMode(t, filepath.Join(dir, "oui.txt"), "oui", 0644)

	rec := httptest.NewRecorder()
	newBackupAPI(t, dir).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/backup", nil))

	entries := readArchive(t, rec.Body)
	for _, name := range []string{"geosite.dat", "geoip.dat", "oui.txt"} {
		if _, ok := entries[name]; ok {
			t.Errorf("%s should be excluded", name)
		}
	}
	if _, ok := entries["b4.json"]; !ok {
		t.Error("b4.json should be included")
	}
}

func TestBackupOmitsWalkRootEntry(t *testing.T) {
	dir := t.TempDir()
	writeFileMode(t, filepath.Join(dir, "b4.json"), `{}`, 0644)

	rec := httptest.NewRecorder()
	newBackupAPI(t, dir).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/backup", nil))

	entries := readArchive(t, rec.Body)
	if _, ok := entries["."]; ok {
		t.Error(`archive should not contain a "." entry`)
	}
}

func TestBackupMarksDirectoriesWithTrailingSlash(t *testing.T) {
	dir := t.TempDir()
	writeFileMode(t, filepath.Join(dir, "b4.json"), `{}`, 0644)
	if err := os.Mkdir(filepath.Join(dir, "captures"), 0755); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	newBackupAPI(t, dir).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/backup", nil))

	entries := readArchive(t, rec.Body)
	if _, ok := entries["captures/"]; !ok {
		t.Errorf("expected captures/ dir entry, got %v", keysOf(entries))
	}
}

func TestBackupFailsWhenNothingToArchive(t *testing.T) {
	dir := t.TempDir()
	writeFileMode(t, filepath.Join(dir, "geosite.dat"), "geo", 0644)
	if err := os.Mkdir(filepath.Join(dir, "captures"), 0755); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	newBackupAPI(t, dir).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/backup", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for an empty backup, got %d body=%q", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("expected a json error, got %q", rec.Body.String())
	}
}

func TestBackupSkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	writeFileMode(t, filepath.Join(dir, "b4.json"), `{}`, 0644)
	if err := os.Symlink(filepath.Join(dir, "b4.json"), filepath.Join(dir, "link.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	rec := httptest.NewRecorder()
	newBackupAPI(t, dir).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/backup", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	entries := readArchive(t, rec.Body)
	if _, ok := entries["link.json"]; ok {
		t.Error("symlink should not be archived as a file")
	}
	if _, ok := entries["b4.json"]; !ok {
		t.Error("b4.json should still be archived")
	}
}

func TestBackupRestoreRoundTrip(t *testing.T) {
	src := t.TempDir()
	writeFileMode(t, filepath.Join(src, "b4.json"), `{"version":2}`, 0777)
	if err := os.Mkdir(filepath.Join(src, "captures"), 0777); err != nil {
		t.Fatal(err)
	}
	writeFileMode(t, filepath.Join(src, "captures", "quic.bin"), "payload", 0777)

	rec := httptest.NewRecorder()
	newBackupAPI(t, src).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/backup", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("backup failed: %d %s", rec.Code, rec.Body.String())
	}
	archive := rec.Body.Bytes()

	dst := t.TempDir()
	restoreRec := httptest.NewRecorder()
	newBackupAPI(t, dst).ServeHTTP(restoreRec, uploadRequest(t, archive))

	if restoreRec.Code != http.StatusOK {
		t.Fatalf("restore failed: %d %s", restoreRec.Code, restoreRec.Body.String())
	}

	got, err := os.ReadFile(filepath.Join(dst, "b4.json"))
	if err != nil || string(got) != `{"version":2}` {
		t.Errorf("b4.json not restored: %v %q", err, string(got))
	}
	got, err = os.ReadFile(filepath.Join(dst, "captures", "quic.bin"))
	if err != nil || string(got) != "payload" {
		t.Errorf("captures/quic.bin not restored: %v %q", err, string(got))
	}
}

func TestRestoreRejectsEmptyArchive(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	tw.WriteHeader(&tar.Header{Name: ".", Typeflag: tar.TypeDir, Mode: 0755})
	tw.WriteHeader(&tar.Header{Name: "captures", Typeflag: tar.TypeDir, Mode: 0755})
	tw.Close()
	gw.Close()

	dst := t.TempDir()
	rec := httptest.NewRecorder()
	newBackupAPI(t, dst).ServeHTTP(rec, uploadRequest(t, buf.Bytes()))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a fileless archive, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRestoreRejectsPathTraversal(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	body := []byte("pwned")
	tw.WriteHeader(&tar.Header{Name: "../escaped.json", Typeflag: tar.TypeReg, Mode: 0644, Size: int64(len(body))})
	tw.Write(body)
	tw.WriteHeader(&tar.Header{Name: "b4.json", Typeflag: tar.TypeReg, Mode: 0644, Size: int64(len(body))})
	tw.Write(body)
	tw.Close()
	gw.Close()

	parent := t.TempDir()
	dst := filepath.Join(parent, "b4")
	if err := os.Mkdir(dst, 0755); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	newBackupAPI(t, dst).ServeHTTP(rec, uploadRequest(t, buf.Bytes()))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(parent, "escaped.json")); err == nil {
		t.Error("path traversal escaped the config directory")
	}
}

func TestSafeRestorePath(t *testing.T) {
	dir := "/opt/etc/b4"
	cases := []struct {
		name string
		ok   bool
	}{
		{"b4.json", true},
		{"captures/quic.bin", true},
		{"..foo.json", true},
		{".", false},
		{"..", false},
		{"../escaped", false},
		{"a/../../escaped", false},
		{"/etc/passwd", false},
	}
	for _, c := range cases {
		if _, ok := safeRestorePath(dir, c.name); ok != c.ok {
			t.Errorf("safeRestorePath(%q) = %v, want %v", c.name, ok, c.ok)
		}
	}
}

func writeFileMode(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func uploadRequest(t *testing.T, archive []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", "backup.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	part.Write(archive)
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/backup/restore", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
