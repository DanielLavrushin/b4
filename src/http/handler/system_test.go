package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/geodat"
	"github.com/daniellavrushin/b4/metrics"
)

func testCfgPtr(cfg *config.Config) *atomic.Pointer[config.Config] {
	p := &atomic.Pointer[config.Config]{}
	p.Store(cfg)
	return p
}

func TestHandleVersion(t *testing.T) {
	cfg := config.NewConfig()
	api := &API{cfgPtr: testCfgPtr(&cfg)}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterSystemApi()

	t.Run("GET returns version info", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}

		var info VersionInfo
		if err := json.NewDecoder(rec.Body).Decode(&info); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if info.Version == "" {
			t.Error("version should not be empty")
		}
	})

	t.Run("POST not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/version", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", rec.Code)
		}
	})
}

func TestHandleSystemInfo(t *testing.T) {
	cfg := config.NewConfig()
	api := &API{cfgPtr: testCfgPtr(&cfg)}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterSystemApi()

	t.Run("GET returns system info", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/system/info", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}

		var info SystemInfo
		if err := json.NewDecoder(rec.Body).Decode(&info); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if info.OS == "" {
			t.Error("OS should not be empty")
		}
		if info.Arch == "" {
			t.Error("Arch should not be empty")
		}
	})

	t.Run("POST not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/system/info", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", rec.Code)
		}
	})
}

func TestHandleCacheStats_NoWorkers(t *testing.T) {
	cfg := config.NewConfig()
	api := &API{cfgPtr: testCfgPtr(&cfg)}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterSystemApi()

	// globalPool is nil
	globalPool = nil

	req := httptest.NewRequest(http.MethodGet, "/api/system/cache", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when no workers, got %d", rec.Code)
	}
}

func TestHandleRestart_Standalone(t *testing.T) {
	cfg := config.NewConfig()
	api := &API{
		cfgPtr:                 testCfgPtr(&cfg),
		overrideServiceManager: func() string { return "standalone" },
	}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterSystemApi()

	req := httptest.NewRequest(http.MethodPost, "/api/system/restart", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	// Should return 400 for standalone
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for standalone, got %d", rec.Code)
	}

	var resp RestartResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.Success {
		t.Error("restart should fail for standalone")
	}
	if resp.ServiceManager != "standalone" {
		t.Errorf("expected standalone service manager, got %s", resp.ServiceManager)
	}
}

func TestHandleUpdate_InvalidBody(t *testing.T) {
	cfg := config.NewConfig()
	api := &API{cfgPtr: testCfgPtr(&cfg)}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterSystemApi()

	req := httptest.NewRequest(http.MethodPost, "/api/system/update", strings.NewReader("invalid json"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", rec.Code)
	}
}

func TestHandleUpdate_MethodNotAllowed(t *testing.T) {
	cfg := config.NewConfig()
	api := &API{cfgPtr: testCfgPtr(&cfg)}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterSystemApi()

	req := httptest.NewRequest(http.MethodGet, "/api/system/update", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestHandleDiagnostics(t *testing.T) {
	cfg := config.NewConfig()
	cfg.ConfigPath = "/etc/b4/b4.json"
	api := &API{
		cfgPtr:                 testCfgPtr(&cfg),
		overrideServiceManager: func() string { return "systemd" },
		geodataManager:         geodat.NewGeodataManager("", ""),
	}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterSystemApi()

	t.Run("GET returns diagnostics", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/system/diagnostics", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}

		var resp DiagnosticsResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if !resp.Success {
			t.Error("expected success=true")
		}
		if resp.Data.System.OS == "" {
			t.Error("OS should not be empty")
		}
		if resp.Data.System.Arch == "" {
			t.Error("Arch should not be empty")
		}
		if resp.Data.System.CPUCores == 0 {
			t.Error("CPU cores should be > 0")
		}
		if resp.Data.B4.Version == "" {
			t.Error("version should not be empty")
		}
		if resp.Data.B4.ServiceManager != "systemd" {
			t.Errorf("expected systemd service manager, got %s", resp.Data.B4.ServiceManager)
		}
		if resp.Data.B4.ConfigPath != "/etc/b4/b4.json" {
			t.Errorf("expected config path /etc/b4/b4.json, got %s", resp.Data.B4.ConfigPath)
		}
		if len(resp.Data.Kernel.Modules) == 0 {
			t.Error("expected at least one kernel module check")
		}
		if len(resp.Data.Tools.Required) == 0 {
			t.Error("expected at least one required tool")
		}
	})

	t.Run("POST not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/system/diagnostics", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", rec.Code)
		}
	})
}

func TestHandleUpdate_ConfigBackup(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := tmpDir + "/b4.json"
	if err := os.WriteFile(configPath, []byte(`{"version":1}`), 0644); err != nil {
		t.Fatalf("failed to create temp config file: %v", err)
	}

	oldVersion := Version
	Version = "1.2.3"
	defer func() { Version = oldVersion }()

	cfg := config.NewConfig()
	cfg.ConfigPath = configPath
	api := &API{
		cfgPtr:                 testCfgPtr(&cfg),
		overrideServiceManager: func() string { return "systemd" },
	}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterSystemApi()

	body := strings.NewReader(`{"version":"v1.3.0"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/system/update", body)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	bakPath := configPath + ".bak.v1.2.3"
	data, err := os.ReadFile(bakPath)
	if err != nil {
		t.Fatalf("backup file not created: %v", err)
	}
	if string(data) != `{"version":1}` {
		t.Errorf("backup content mismatch: %s", string(data))
	}
}

func TestCollectSystemInfo(t *testing.T) {
	info := collectSystemInfo()
	if info.OS == "" {
		t.Error("OS should not be empty")
	}
	if info.Kernel == "" {
		t.Error("Kernel should not be empty")
	}
	if info.CPUCores == 0 {
		t.Error("CPU cores should be > 0")
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input    uint64
		expected string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
		{2684354560, "2.5 GB"},
	}
	for _, tc := range tests {
		result := formatBytes(tc.input)
		if result != tc.expected {
			t.Errorf("formatBytes(%d) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestStoppedWithService(t *testing.T) {
	err := exec.Command("sh", "-c", "kill -TERM $$").Run()
	if err == nil {
		t.Fatal("expected the shell to be killed by SIGTERM")
	}
	if !stoppedWithService(err) {
		t.Errorf("SIGTERM death not recognised: %v", err)
	}

	err = exec.Command("sh", "-c", "kill -KILL $$").Run()
	if err == nil {
		t.Fatal("expected the shell to be killed by SIGKILL")
	}
	if stoppedWithService(err) {
		t.Errorf("SIGKILL death read as a stop alongside the service: %v", err)
	}

	err = exec.Command("sh", "-c", "exit 3").Run()
	if err == nil {
		t.Fatal("expected exit status 3")
	}
	if stoppedWithService(err) {
		t.Errorf("plain exit status read as a stop alongside the service: %v", err)
	}
}

func TestHandleRestart_RestartsInPlaceWithoutAServiceManager(t *testing.T) {
	for _, manager := range []string{"standalone", "docker"} {
		t.Run(manager, func(t *testing.T) {
			restarted := make(chan struct{})
			selfRestartFunc = func() { close(restarted) }
			t.Cleanup(func() { selfRestartFunc = nil })

			cfg := config.NewConfig()
			api := &API{
				cfgPtr:                 testCfgPtr(&cfg),
				overrideServiceManager: func() string { return manager },
			}
			mux := http.NewServeMux()
			api.mux = mux
			api.RegisterSystemApi()

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/system/restart", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
			}
			var resp RestartResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatal(err)
			}
			if !resp.Success || resp.ServiceManager != manager {
				t.Fatalf("unexpected response %+v", resp)
			}
			select {
			case <-restarted:
			case <-time.After(3 * time.Second):
				t.Fatal("the in-place restart was never requested")
			}
		})
	}
}

func TestSystemInfoAndDiagnosticsReportAFailedEngine(t *testing.T) {
	mc := GetMetricsCollector()
	mc.SetEngineFailure(&metrics.EngineFailure{Mode: "nfqueue", Error: "iptables rejected the NFQUEUE target", RetryAt: 1700000000000, RetriesLeft: 2})
	selfRestartFunc = func() {}
	t.Cleanup(func() {
		mc.SetEngineFailure(nil)
		selfRestartFunc = nil
	})

	cfg := config.NewConfig()
	api := &API{
		cfgPtr:                 testCfgPtr(&cfg),
		overrideServiceManager: func() string { return "standalone" },
	}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterSystemApi()

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/system/info", nil))
	var info SystemInfoResponse
	if err := json.NewDecoder(rec.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	if !info.CanRestart {
		t.Error("an in-place restart makes restarting possible without a service manager")
	}
	if info.EngineFailure == nil || info.EngineFailure.Error != "iptables rejected the NFQUEUE target" || info.EngineFailure.RetriesLeft != 2 {
		t.Fatalf("system info must carry the engine failure, got %+v", info.EngineFailure)
	}

	cfg.Queue.Mode = "tun"
	engine := collectEngineInfo(&cfg)
	if engine.StartError != "iptables rejected the NFQUEUE target" {
		t.Fatalf("diagnostics must carry the start error, got %q", engine.StartError)
	}
	if engine.Mode != "nfqueue" || engine.TUN != nil {
		t.Fatalf("with the mode switched but b4 not restarted, diagnostics must name the engine that failed, got mode %q", engine.Mode)
	}
	mc.SetEngineFailure(nil)
	engine = collectEngineInfo(&cfg)
	if engine.StartError != "" || engine.Mode != "tun" {
		t.Fatalf("a running engine reports the configured mode and no start error, got mode %q error %q", engine.Mode, engine.StartError)
	}
}
