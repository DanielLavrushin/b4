package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/daniellavrushin/b4/capture"
	"github.com/daniellavrushin/b4/log"
)

type CaptureRequest struct {
	Domain   string `json:"domain"`
	Protocol string `json:"protocol"` // "tls", "quic", or "both"
}

func (api *API) RegisterCaptureApi() {
	api.mux.HandleFunc("/api/capture/probe", api.handleProbeCapture)
	api.mux.HandleFunc("/api/capture/generate", api.handleGenerateCapture)
	api.mux.HandleFunc("/api/capture/list", api.handleListCaptures)
	api.mux.HandleFunc("/api/capture/delete", api.handleDeleteCapture)
	api.mux.HandleFunc("/api/capture/clear", api.handleClearCaptures)
	api.mux.HandleFunc("/api/capture/download", api.handleDownloadCapture)
	api.mux.HandleFunc("/api/capture/upload", api.handleUploadCapture)
}

func (api *API) handleGenerateCapture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req CaptureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.Domain == "" {
		http.Error(w, "Domain required", http.StatusBadRequest)
		return
	}

	req.Domain = strings.ToLower(strings.TrimSpace(req.Domain))
	req.Domain = strings.TrimPrefix(req.Domain, "http://")
	req.Domain = strings.TrimPrefix(req.Domain, "https://")
	req.Domain = strings.Split(req.Domain, "/")[0]

	if req.Protocol == "" {
		req.Protocol = "tls"
	}

	if req.Protocol != "tls" {
		http.Error(w, "Only 'tls' protocol supported for generation", http.StatusBadRequest)
		return
	}

	manager := capture.GetManager(api.cfg)

	if err := manager.GenerateCapture(req.Domain, req.Protocol); err != nil {
		if strings.Contains(err.Error(), "already captured") {
			setJsonHeader(w)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success":          true,
				"message":          fmt.Sprintf("Payload for %s already exists", req.Domain),
				"already_captured": true,
			})
			return
		}

		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Generated optimized %s payload for %s (SNI-first for DPI bypass)", req.Protocol, req.Domain),
		"method":  "generated",
	})
}

// Trigger traffic to capture payload
func (api *API) handleProbeCapture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req CaptureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.Domain == "" {
		http.Error(w, "Domain required", http.StatusBadRequest)
		return
	}

	// Normalize domain (lowercase, no protocol)
	req.Domain = strings.ToLower(strings.TrimSpace(req.Domain))
	req.Domain = strings.TrimPrefix(req.Domain, "http://")
	req.Domain = strings.TrimPrefix(req.Domain, "https://")
	req.Domain = strings.Split(req.Domain, "/")[0] // Remove path if any

	if req.Protocol == "" {
		req.Protocol = "both"
	}

	manager := capture.GetManager(api.cfg)

	var errors []string

	// Probe for the requested protocol(s)
	if req.Protocol == "both" || req.Protocol == "tls" {
		if err := manager.ProbeCapture(req.Domain, "tls"); err != nil {
			errors = append(errors, fmt.Sprintf("TLS: %v", err))
			log.Tracef("TLS probe error for %s: %v", req.Domain, err)
		}
	}

	if req.Protocol == "both" || req.Protocol == "quic" {
		if err := manager.ProbeCapture(req.Domain, "quic"); err != nil {
			errors = append(errors, fmt.Sprintf("QUIC: %v", err))
			log.Tracef("QUIC probe error for %s: %v", req.Domain, err)
		}
	}

	// If all probes failed with "already captured", that's actually fine
	if len(errors) > 0 {
		allAlreadyCaptured := true
		for _, err := range errors {
			if !strings.Contains(err, "already captured") {
				allAlreadyCaptured = false
				break
			}
		}

		if allAlreadyCaptured {
			setJsonHeader(w)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success":          true,
				"message":          fmt.Sprintf("Payload for %s already captured", req.Domain),
				"already_captured": true,
			})
			return
		}
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Probing %s for %s payload", req.Domain, req.Protocol),
		"errors":  errors,
	})
}

// List all captures
func (api *API) handleListCaptures(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	manager := capture.GetManager(api.cfg)
	captures := manager.ListCaptures()

	if captures == nil {
		captures = make([]*capture.Capture, 0)
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(captures)
}

// Delete specific capture
func (api *API) handleDeleteCapture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	protocol := r.URL.Query().Get("protocol")
	domain := r.URL.Query().Get("domain")

	if protocol == "" || domain == "" {
		http.Error(w, "Protocol and domain required", http.StatusBadRequest)
		return
	}

	manager := capture.GetManager(api.cfg)
	if err := manager.DeleteCapture(protocol, domain); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Capture deleted",
	})
}

// Clear all captures
func (api *API) handleClearCaptures(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	manager := capture.GetManager(api.cfg)
	if err := manager.ClearAll(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "All captures cleared",
	})
}

// Download capture file
func (api *API) handleDownloadCapture(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Query().Get("file")
	if filePath == "" {
		http.Error(w, "File path required", http.StatusBadRequest)
		return
	}

	// Get the captures directory from manager
	manager := capture.GetManager(api.cfg)
	capturesDir := manager.GetOutputPath()

	// Security check - ensure the requested file is in the captures directory
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		http.Error(w, "Invalid file path", http.StatusBadRequest)
		return
	}

	absCapturesDir, err := filepath.Abs(capturesDir)
	if err != nil {
		http.Error(w, "Server error", http.StatusInternalServerError)
		return
	}

	// Check that the file is within the captures directory
	if !strings.HasPrefix(absPath, absCapturesDir) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Check file exists and is a .bin file
	info, err := os.Stat(absPath)
	if os.IsNotExist(err) {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	if info.IsDir() || !strings.HasSuffix(absPath, ".bin") {
		http.Error(w, "Invalid file type", http.StatusBadRequest)
		return
	}

	// Serve the file
	filename := filepath.Base(absPath)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))

	http.ServeFile(w, r, absPath)
	log.Tracef("Served capture file: %s", filename)
}

func (api *API) handleUploadCapture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Max 64KB for a ClientHello
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)

	if err := r.ParseMultipartForm(64 * 1024); err != nil {
		http.Error(w, "File too large (max 64KB)", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "File required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	domain := r.FormValue("domain")
	protocol := r.FormValue("protocol")

	if domain == "" {
		http.Error(w, "Domain required", http.StatusBadRequest)
		return
	}
	if protocol == "" {
		protocol = "tls"
	}
	if protocol != "tls" && protocol != "quic" {
		http.Error(w, "Protocol must be 'tls' or 'quic'", http.StatusBadRequest)
		return
	}

	domain = strings.ToLower(strings.TrimSpace(domain))

	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Failed to read file", http.StatusInternalServerError)
		return
	}

	if len(data) == 0 {
		http.Error(w, "Empty file", http.StatusBadRequest)
		return
	}

	manager := capture.GetManager(api.cfg)
	if err := manager.SaveUploadedCapture(protocol, domain, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Infof("Uploaded %s capture for %s: %s (%d bytes)", protocol, domain, header.Filename, len(data))

	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"message":  fmt.Sprintf("Uploaded %s payload for %s", protocol, domain),
		"size":     len(data),
		"domain":   domain,
		"protocol": protocol,
	})
}
