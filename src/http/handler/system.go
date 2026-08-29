package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

func (api *API) RegisterSystemApi() {
	api.mux.HandleFunc("/api/system/restart", api.handleRestart)
	api.mux.HandleFunc("/api/system/info", api.handleSystemInfo)
	api.mux.HandleFunc("/api/version", api.handleVersion)
	api.mux.HandleFunc("/api/system/update", api.handleUpdate)
	api.mux.HandleFunc("/api/system/update/upload", api.handleUpdateUpload)
	api.mux.HandleFunc("/api/system/releases", api.handleReleases)
	api.mux.HandleFunc("/api/system/changelog", api.handleChangelog)
	api.mux.HandleFunc("/api/system/cache", api.handleCacheStats)
	api.mux.HandleFunc("/api/system/diagnostics", api.handleDiagnostics)
}

func (api *API) backupConfig(logPath string) {
	configPath := api.getCfg().ConfigPath
	if configPath == "" {
		return
	}

	bakPath := configPath + ".bak.v" + Version
	fi, err := os.Stat(configPath)
	if err != nil {
		return
	}

	src, err := os.ReadFile(configPath)
	if err != nil {
		log.Warnf("Failed to read config for backup: %v", err)
		return
	}

	if err := os.WriteFile(bakPath, src, fi.Mode().Perm()); err != nil {
		log.Warnf("Failed to create config backup at %s: %v", bakPath, err)
		writeUpdateLog(logPath, "WARN: failed to back up config to %s: %v", bakPath, err)
		return
	}

	log.Infof("Config backed up to %s", bakPath)
	writeUpdateLog(logPath, "Config backed up to %s", bakPath)
}

func (api *API) getServiceManager() string {
	if api.overrideServiceManager != nil {
		return api.overrideServiceManager()
	}
	return detectServiceManager()
}

func detectServiceManager() string {
	if _, err := os.Stat("/etc/systemd/system/b4.service"); err == nil {
		if _, err := exec.LookPath("systemctl"); err == nil {
			return "systemd"
		}
	}

	if _, err := os.Stat("/opt/etc/init.d/S99b4"); err == nil {
		return "entware"
	}

	if _, err := os.Stat("/etc/init.d/b4"); err == nil {
		return "init"
	}

	if isDockerEnvironment() {
		return "docker"
	}

	return "standalone"
}

func isDockerEnvironment() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	if _, err := os.Stat("/run/.containerenv"); err == nil {
		return true
	}
	if os.Getenv("container") != "" {
		return true
	}
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		s := string(data)
		if strings.Contains(s, "docker") || strings.Contains(s, "containerd") ||
			strings.Contains(s, "lxc") || strings.Contains(s, "kubepods") {
			return true
		}
	}
	return false
}

func stoppedWithService(err error) bool {
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return false
	}
	ws, ok := ee.Sys().(syscall.WaitStatus)
	return ok && ws.Signaled() && ws.Signal() == syscall.SIGTERM
}

func (api *API) updateLogPath() string {
	if cfg := api.getCfg(); cfg != nil {
		return cfg.System.Logging.UpdateLogPath()
	}
	return ""
}

const maxUpdateLogSize = 1 << 20

func openUpdateLog(path string) (*os.File, error) {
	if path == "" {
		return nil, os.ErrInvalid
	}
	if st, err := os.Stat(path); err == nil && st.Size() > maxUpdateLogSize {
		_ = os.Rename(path, path+".1")
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
}

func writeUpdateLog(path, format string, args ...interface{}) {
	if path == "" {
		return
	}
	f, err := openUpdateLog(path)
	if err != nil {
		return
	}
	defer f.Close()
	ts := time.Now().Format("2006-01-02 15:04:05")
	fmt.Fprintf(f, "%s [HANDLER] %s\n", ts, fmt.Sprintf(format, args...))
}

type SystemInfoResponse struct {
	SystemInfo
	HostHasGlobalIPv6 bool `json:"host_has_global_ipv6"`
	IPv6BypassesSets  bool `json:"ipv6_bypasses_sets"`
}

// @Summary Get system information
// @Tags System
// @Produce json
// @Success 200 {object} SystemInfoResponse
// @Security BearerAuth
// @Router /system/info [get]
func (api *API) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	serviceManager := api.getServiceManager()
	isDocker := serviceManager == "docker"
	canRestart := serviceManager != "standalone" && !isDocker

	cfg := api.getCfg()
	hostIPv6 := config.HostHasGlobalIPv6()

	info := SystemInfoResponse{
		SystemInfo: SystemInfo{
			ServiceManager: serviceManager,
			OS:             runtime.GOOS,
			Arch:           runtime.GOARCH,
			CanRestart:     canRestart,
			IsDocker:       isDocker,
		},
		HostHasGlobalIPv6: hostIPv6,
		IPv6BypassesSets:  hostIPv6 && cfg != nil && !cfg.Queue.IPv6Enabled,
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(info)
}

// @Summary Restart the service
// @Tags System
// @Produce json
// @Success 200 {object} RestartResponse
// @Failure 400 {object} RestartResponse
// @Security BearerAuth
// @Router /system/restart [post]
func (api *API) handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	serviceManager := api.getServiceManager()
	log.Infof("Restart requested via web UI (service manager: %s)", serviceManager)

	var response RestartResponse
	response.ServiceManager = serviceManager

	switch serviceManager {
	case "systemd":
		response.Success = true
		response.Message = "Restart initiated via systemd"
		response.RestartCommand = "systemctl restart b4"

	case "entware":
		response.Success = true
		response.Message = "Restart initiated via Entware init script"
		response.RestartCommand = "/opt/etc/init.d/S99b4 restart"

	case "init":
		response.Success = true
		response.Message = "Restart initiated via init script"
		response.RestartCommand = "/etc/init.d/b4 restart"

	case "standalone":
		response.Success = false
		response.Message = "Cannot restart: B4 is not running as a service. Please restart manually."
		setJsonHeader(w)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return

	default:
		response.Success = false
		response.Message = "Unknown service manager"
		setJsonHeader(w)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(response)

	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	go func() {
		time.Sleep(500 * time.Millisecond)
		log.Infof("Executing restart command: %s", response.RestartCommand)

		var cmd *exec.Cmd
		switch serviceManager {
		case "systemd":
			cmd = exec.Command("systemctl", "restart", "--no-block", "b4")
		case "entware", "init":
			cmd = exec.Command("sh", "-c", response.RestartCommand+" > /dev/null 2>&1 &")
			cmd.SysProcAttr = &syscall.SysProcAttr{
				Setsid: true,
			}
		}

		if cmd != nil {
			if serviceManager == "systemd" {
				output, err := cmd.CombinedOutput()
				switch {
				case err == nil:
					log.Infof("Restart job queued with systemd")
				case stoppedWithService(err):
					log.Infof("Restart job queued with systemd, which stopped systemctl along with the service")
				default:
					log.Errorf("Restart command failed: %v\nOutput: %s", err, string(output))
				}
			} else {
				if err := cmd.Start(); err != nil {
					log.Errorf("Failed to start restart command: %v", err)
				} else {
					log.Infof("Restart command initiated")
				}
			}
		}
	}()
}

// @Summary Get version information
// @Tags System
// @Produce json
// @Success 200 {object} VersionInfo
// @Router /version [get]
func (api *API) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	versionInfo := VersionInfo{
		Version:   Version,
		Commit:    Commit,
		BuildDate: Date,
	}
	setJsonHeader(w)
	enc := json.NewEncoder(w)
	_ = enc.Encode(versionInfo)
}

// @Summary Start update process
// @Tags System
// @Accept json
// @Produce json
// @Param body body UpdateRequest true "Update request"
// @Success 200 {object} UpdateResponse
// @Failure 400 {object} UpdateResponse
// @Security BearerAuth
// @Router /system/update [post]
func (api *API) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req UpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	serviceManager := api.getServiceManager()
	log.Infof("Update requested via web UI (service manager: %s, version: %s)", serviceManager, req.Version)

	logPath := api.updateLogPath()
	if logPath != "" {
		if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
			log.Warnf("Update log disabled: cannot create %s: %v", filepath.Dir(logPath), err)
			logPath = ""
		} else if err := os.WriteFile(logPath, []byte{}, 0644); err != nil {
			log.Warnf("Update log disabled: cannot reset %s: %v", logPath, err)
			logPath = ""
		}
	}
	writeUpdateLog(logPath, "=== Update session started ===")
	writeUpdateLog(logPath, "Service manager: %s | requested version: %q | os/arch: %s/%s",
		serviceManager, req.Version, runtime.GOOS, runtime.GOARCH)

	if serviceManager == "docker" {
		writeUpdateLog(logPath, "ABORTED: running inside Docker — update via image pull")
		response := UpdateResponse{
			Success:        false,
			Message:        "Cannot update: B4 is running inside Docker. Pull the latest image and recreate your container to update.",
			ServiceManager: serviceManager,
		}
		setJsonHeader(w)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	if serviceManager == "standalone" {
		writeUpdateLog(logPath, "ABORTED: B4 is not running as a service (standalone) — manual update required")
		response := UpdateResponse{
			Success:        false,
			Message:        "Cannot update: B4 is not running as a service. Please update manually.",
			ServiceManager: serviceManager,
		}
		setJsonHeader(w)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	api.backupConfig(logPath)

	response := UpdateResponse{
		Success:        true,
		Message:        "Update initiated. The service will restart automatically.",
		ServiceManager: serviceManager,
	}

	if err := api.launchInstaller(installerRun{
		serviceManager: serviceManager,
		logPath:        logPath,
		version:        req.Version,
		mirrors:        api.updateMirrors(),
		cachePath:      api.installerCachePath(),
	}); err != nil {
		setJsonHeader(w)
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(UpdateResponse{
			Success:        false,
			Message:        "Could not fetch the installer: " + err.Error(),
			ServiceManager: serviceManager,
		})
		return
	}

	sendResponse(w, response)
}

// @Summary Get cache statistics
// @Tags System
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 503 {string} string
// @Security BearerAuth
// @Router /system/cache [get]
func (api *API) handleCacheStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if globalPool == nil || len(globalPool.Workers) == 0 {
		http.Error(w, "No workers available", http.StatusServiceUnavailable)
		return
	}

	stats := globalPool.Workers[0].GetCacheStats()

	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"cache":   stats,
	})
}
