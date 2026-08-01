package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/geodat"
)

func TestHandleGeodatSources(t *testing.T) {
	cfg := config.NewConfig()
	api := &API{
		cfgPtr:         testCfgPtr(&cfg),
		geodataManager: geodat.NewGeodataManager("", ""),
	}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterGeodatApi()

	t.Run("GET returns sources", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/geodat/sources", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}

		var sources []GeodatSource
		if err := json.NewDecoder(rec.Body).Decode(&sources); err != nil {
			t.Fatalf("failed to decode: %v", err)
		}

		if len(sources) == 0 {
			t.Error("expected at least one source")
		}

		// Check first source has required fields
		if sources[0].Name == "" || sources[0].GeositeURL == "" || sources[0].GeoipURL == "" {
			t.Error("source missing required fields")
		}
	})

	t.Run("POST not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/geodat/sources", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", rec.Code)
		}
	})
}

func TestHandleFileInfo(t *testing.T) {
	cfg := config.NewConfig()
	api := &API{
		cfgPtr:         testCfgPtr(&cfg),
		geodataManager: geodat.NewGeodataManager("", ""),
	}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterGeodatApi()

	t.Run("missing path parameter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/geodat/info", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("nonexistent file", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/geodat/info?path=/nonexistent/file.dat", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}

		var resp map[string]interface{}
		json.NewDecoder(rec.Body).Decode(&resp)

		if resp["exists"] != false {
			t.Error("expected exists=false for nonexistent file")
		}
	})

	t.Run("existing file", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "test.dat")
		os.WriteFile(tmpFile, []byte("test"), 0644)

		req := httptest.NewRequest(http.MethodGet, "/api/geodat/info?path="+tmpFile, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}

		var resp map[string]interface{}
		json.NewDecoder(rec.Body).Decode(&resp)

		if resp["exists"] != true {
			t.Error("expected exists=true")
		}
		if resp["size"].(float64) != 4 {
			t.Errorf("expected size=4, got %v", resp["size"])
		}
	})

	t.Run("POST not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/geodat/info?path=/tmp/test", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", rec.Code)
		}
	})
}

func TestHandleGeodatRemove_Validation(t *testing.T) {
	cfg := config.NewConfig()
	api := &API{
		cfgPtr:         testCfgPtr(&cfg),
		geodataManager: geodat.NewGeodataManager("", ""),
	}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterGeodatApi()

	t.Run("GET not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/geodat/remove", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", rec.Code)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/geodat/remove", strings.NewReader("not json"))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("unknown type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/geodat/remove", strings.NewReader(`{"type":"geodns"}`))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rec.Code)
		}
	})
}

func TestDeleteGeodatFile(t *testing.T) {
	t.Run("empty path is a no-op", func(t *testing.T) {
		removed, err := deleteGeodatFile("", "geosite.dat")
		if err != nil || removed != "" {
			t.Errorf("expected no-op, got removed=%q err=%v", removed, err)
		}
	})

	t.Run("missing file is not an error", func(t *testing.T) {
		removed, err := deleteGeodatFile(filepath.Join(t.TempDir(), "geosite.dat"), "geosite.dat")
		if err != nil || removed != "" {
			t.Errorf("expected no-op, got removed=%q err=%v", removed, err)
		}
	})

	t.Run("deletes managed file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "geosite.dat")
		os.WriteFile(path, []byte("data"), 0644)

		removed, err := deleteGeodatFile(path, "geosite.dat")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if removed != path {
			t.Errorf("expected removed=%s, got %s", path, removed)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("file still on disk")
		}
	})

	t.Run("keeps file b4 did not write", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "geosite_ru.dat")
		os.WriteFile(path, []byte("data"), 0644)

		if _, err := deleteGeodatFile(path, "geosite.dat"); !errors.Is(err, errUnmanagedGeodat) {
			t.Errorf("expected errUnmanagedGeodat, got %v", err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Error("file should be untouched")
		}
	})

	t.Run("keeps file under the wrong name for its type", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "geosite.dat")
		os.WriteFile(path, []byte("data"), 0644)

		if _, err := deleteGeodatFile(path, "geoip.dat"); !errors.Is(err, errUnmanagedGeodat) {
			t.Errorf("expected errUnmanagedGeodat, got %v", err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Error("file should be untouched")
		}
	})

	t.Run("keeps denied prefix", func(t *testing.T) {
		if _, err := deleteGeodatFile("/proc/geosite.dat", "geosite.dat"); !errors.Is(err, errUnmanagedGeodat) {
			t.Errorf("expected errUnmanagedGeodat for /proc path, got %v", err)
		}
	})

	t.Run("keeps relative path", func(t *testing.T) {
		if _, err := deleteGeodatFile("geosite.dat", "geosite.dat"); !errors.Is(err, errUnmanagedGeodat) {
			t.Errorf("expected errUnmanagedGeodat for relative path, got %v", err)
		}
	})
}

func TestRemoveRelocatedGeodat(t *testing.T) {
	t.Run("removes old copy after relocation", func(t *testing.T) {
		oldDir := t.TempDir()
		oldPath := filepath.Join(oldDir, "geosite.dat")
		os.WriteFile(oldPath, []byte("old"), 0644)
		newPath := filepath.Join(t.TempDir(), "geosite.dat")

		if got := removeRelocatedGeodat(oldPath, newPath, "geosite.dat"); got != oldPath {
			t.Errorf("expected %s removed, got %q", oldPath, got)
		}
		if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
			t.Error("old file still on disk")
		}
	})

	t.Run("same path is a no-op", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "geosite.dat")
		os.WriteFile(path, []byte("data"), 0644)

		if got := removeRelocatedGeodat(path, path, "geosite.dat"); got != "" {
			t.Errorf("expected no removal, got %q", got)
		}
		if _, err := os.Stat(path); err != nil {
			t.Error("file should be untouched")
		}
	})

	t.Run("keeps files b4 did not write", func(t *testing.T) {
		oldPath := filepath.Join(t.TempDir(), "custom-geosite.dat")
		os.WriteFile(oldPath, []byte("data"), 0644)
		newPath := filepath.Join(t.TempDir(), "geosite.dat")

		if got := removeRelocatedGeodat(oldPath, newPath, "geosite.dat"); got != "" {
			t.Errorf("expected no removal, got %q", got)
		}
		if _, err := os.Stat(oldPath); err != nil {
			t.Error("custom file should be untouched")
		}
	})
}

func TestHandleGeodatDownload_Validation(t *testing.T) {
	cfg := config.NewConfig()
	api := &API{
		cfgPtr:         testCfgPtr(&cfg),
		geodataManager: geodat.NewGeodataManager("", ""),
	}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterGeodatApi()

	t.Run("missing fields", func(t *testing.T) {
		body := `{"geosite_url": "http://example.com/geosite.dat"}`
		req := httptest.NewRequest(http.MethodPost, "/api/geodat/download", strings.NewReader(body))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for missing fields, got %d", rec.Code)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/geodat/download", strings.NewReader("not json"))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for invalid JSON, got %d", rec.Code)
		}
	})

	t.Run("GET not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/geodat/download", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", rec.Code)
		}
	})
}
