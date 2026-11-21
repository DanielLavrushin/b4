package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/daniellavrushin/b4/capture"
	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

type CaptureRequest struct {
	Domain   string `json:"domain"`
	Protocol string `json:"protocol"`
}

func (api *API) RegisterCaptureApi() {
	api.mux.HandleFunc("/api/capture/generate", api.handleGenerateCapture)
	api.mux.HandleFunc("/api/capture/list", api.handleListCaptures)
	api.mux.HandleFunc("/api/capture/delete", api.handleDeleteCapture)
	api.mux.HandleFunc("/api/capture/clear", api.handleClearCaptures)
	api.mux.HandleFunc("/api/capture/download", api.handleDownloadCapture)
	api.mux.HandleFunc("/api/capture/upload", api.handleUploadCapture)
}

// @Summary Generate capture payload
// @Tags Capture
// @Accept json
// @Produce json
// @Param body body CaptureRequest true "Capture request"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @Router /capture/generate [post]
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

	manager := capture.GetManager(api.getCfg())

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

	size := 0
	if generated, ok := manager.GetCapture(req.Protocol, req.Domain); ok {
		size = generated.Size
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": generatedPayloadMessage(req.Domain, size),
		"method":  "generated",
		"size":    size,
	})
}

func generatedPayloadMessage(domain string, size int) string {
	if size <= 0 {
		return fmt.Sprintf("Generated a TLS ClientHello for %s", domain)
	}
	return fmt.Sprintf("Generated a %d-byte TLS ClientHello for %s", size, domain)
}

// @Summary List all captures
// @Tags Capture
// @Produce json
// @Success 200 {array} object
// @Security BearerAuth
// @Router /capture/list [get]
func (api *API) handleListCaptures(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	manager := capture.GetManager(api.getCfg())
	captures := manager.ListCaptures()

	if captures == nil {
		captures = make([]*capture.Capture, 0)
	}
	cfg := api.getCfg()
	for _, c := range captures {
		c.UsedBy = setsNamingCapture(cfg, c.Filepath)
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(captures)
}

func setsNamingCapture(cfg *config.Config, file string) []string {
	var names []string
	for _, set := range cfg.Sets {
		if set != nil && set.NamesCapture(file) {
			names = append(names, set.Name)
		}
	}
	return names
}

// @Summary Delete a capture
// @Tags Capture
// @Produce json
// @Param protocol query string true "Protocol (tls or quic)"
// @Param domain query string true "Domain name"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {string} string
// @Security BearerAuth
// @Router /capture/delete [delete]
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

	manager := capture.GetManager(api.getCfg())
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

// @Summary Clear all captures
// @Tags Capture
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @Router /capture/clear [post]
func (api *API) handleClearCaptures(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	manager := capture.GetManager(api.getCfg())
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

// @Summary Download a capture file
// @Tags Capture
// @Produce application/octet-stream
// @Param file query string true "Filename"
// @Success 200 {file} binary
// @Security BearerAuth
// @Router /capture/download [get]
func (api *API) handleDownloadCapture(w http.ResponseWriter, r *http.Request) {
	fileParam := r.URL.Query().Get("file")
	if fileParam == "" {
		http.Error(w, "Filename required", http.StatusBadRequest)
		return
	}

	// Get the captures directory from manager
	manager := capture.GetManager(api.getCfg())
	capturesDir := manager.GetOutputPath()

	absCapturesDir, err := filepath.Abs(capturesDir)
	if err != nil {
		http.Error(w, "Server error", http.StatusInternalServerError)
		return
	}

	// Resolve the file path: use only the base filename to prevent directory traversal
	filename := filepath.Base(fileParam)
	absPath := filepath.Join(absCapturesDir, filename)

	info, err := os.Stat(absPath)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	if info.IsDir() || !strings.HasSuffix(absPath, ".bin") {
		http.Error(w, "Invalid file type", http.StatusBadRequest)
		return
	}

	// Serve the file
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))

	http.ServeFile(w, r, absPath)
	log.Tracef("Served capture file: %s", filename)
}

// @Summary Upload a capture file
// @Tags Capture
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "Capture binary file"
// @Param domain formData string true "Domain name"
// @Param protocol formData string false "Protocol (tls or quic)" default(tls)
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @Router /capture/upload [post]
func (api *API) handleUploadCapture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, config.MaxCapturePayloadBytes)

	if err := r.ParseMultipartForm(config.MaxCapturePayloadBytes); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "File too large (max 64KB)", http.StatusBadRequest)
			return
		}
		http.Error(w, "Invalid upload form: "+err.Error(), http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "File required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	domain := strings.ToLower(strings.TrimSpace(r.FormValue("domain")))
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

	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Failed to read file", http.StatusInternalServerError)
		return
	}

	if len(data) == 0 {
		http.Error(w, "Empty file", http.StatusBadRequest)
		return
	}

	manager := capture.GetManager(api.getCfg())
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
