package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/daniellavrushin/b4/capture"
	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sni"
)

type CaptureRequest struct {
	Domain   string `json:"domain"`
	Protocol string `json:"protocol"`
}

func (api *API) RegisterCaptureApi() {
	api.mux.HandleFunc("/api/capture/generate", api.handleGenerateCapture)
	api.mux.HandleFunc("/api/capture/regenerate", api.handleRegenerateCapture)
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

	host, ok := normalizeHostName(req.Domain)
	if !ok {
		http.Error(w, fmt.Sprintf("Not a host name: %q", strings.TrimSpace(req.Domain)), http.StatusBadRequest)
		return
	}
	req.Domain = host

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
				"domain":           req.Domain,
			})
			return
		}

		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	size := 0
	var facts *capture.Facts
	if generated, ok := manager.GetCapture(req.Protocol, req.Domain); ok {
		size = generated.Size
		facts = generated.Facts
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": generatedPayloadMessage(req.Domain, size),
		"method":  "generated",
		"size":    size,
		"domain":  req.Domain,
		"facts":   facts,
	})
}

func normalizeHostName(raw string) (string, bool) {
	host := strings.ToLower(strings.TrimSpace(raw))
	if strings.Contains(host, "://") {
		u, err := url.Parse(host)
		if err != nil {
			return "", false
		}
		host = u.Host
	} else if i := strings.IndexByte(host, '/'); i >= 0 {
		host = host[:i]
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host, err := sni.HostToASCII(strings.TrimSuffix(host, "."))
	if err != nil || !sni.IsValidSNI([]byte(host)) {
		return "", false
	}
	return host, true
}

type RegenerateRequest struct {
	Domain string `json:"domain"`
}

// @Summary Regenerate a generated payload
// @Description Rebuilds a payload made by Generate with the current generator, under the same name and file, and reloads the sets that name it.
// @Tags Capture
// @Accept json
// @Produce json
// @Param body body RegenerateRequest true "Name of a generated TLS payload"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {string} string
// @Failure 404 {string} string
// @Failure 409 {string} string
// @Security BearerAuth
// @Router /capture/regenerate [post]
func (api *API) handleRegenerateCapture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req RegenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	domain := strings.ToLower(strings.TrimSpace(req.Domain))

	manager := capture.GetManager(api.getCfg())
	regenerated, err := manager.RegenerateCapture(domain)
	switch {
	case errors.Is(err, capture.ErrPayloadNotFound):
		http.Error(w, fmt.Sprintf("No TLS payload named %q", domain), http.StatusNotFound)
		return
	case errors.Is(err, capture.ErrNotGenerated):
		http.Error(w, fmt.Sprintf("The TLS payload %q was not made by Generate, so it cannot be regenerated", domain), http.StatusConflict)
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Regenerated the %d-byte TLS ClientHello of %s", regenerated.Size, domain),
		"size":    regenerated.Size,
		"domain":  domain,
		"facts":   regenerated.Facts,
	}
	api.reloadSetsNaming([]string{regenerated.Filepath}, response)
	setJsonHeader(w)
	json.NewEncoder(w).Encode(response)
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

func (api *API) reloadSetsNaming(files []string, response map[string]interface{}) {
	cfg := api.getCfg()
	var names []string
	seen := map[string]bool{}
	for _, file := range files {
		for _, name := range setsNamingCapture(cfg, file) {
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	response["reloaded"] = []string{}
	if len(names) == 0 {
		return
	}
	if _, _, err := api.editConfig(func(*config.Config) error { return nil }); err != nil {
		log.Errorf("The payload change did not reach sets %s: %v", strings.Join(names, ", "), err)
		response["reload_error"] = err.Error()
		return
	}
	response["reloaded"] = names
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
	var files []string
	if c, ok := manager.GetCapture(protocol, domain); ok {
		files = append(files, c.Filepath)
	}
	if err := manager.DeleteCapture(protocol, domain); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	response := map[string]interface{}{
		"success": true,
		"message": "Capture deleted",
	}
	api.reloadSetsNaming(files, response)
	setJsonHeader(w)
	json.NewEncoder(w).Encode(response)
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
	var files []string
	for _, c := range manager.ListCaptures() {
		files = append(files, c.Filepath)
	}
	if err := manager.ClearAll(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"success": true,
		"message": "All captures cleared",
	}
	api.reloadSetsNaming(files, response)
	setJsonHeader(w)
	json.NewEncoder(w).Encode(response)
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

const uploadFormOverhead = 16 << 10

func uploadTooLargeText() string {
	return fmt.Sprintf("File too large: a payload may have at most %d bytes", config.MaxCapturePayloadBytes)
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

	const bodyLimit = config.MaxCapturePayloadBytes + uploadFormOverhead
	r.Body = http.MaxBytesReader(w, r.Body, bodyLimit)

	if err := r.ParseMultipartForm(bodyLimit); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, uploadTooLargeText(), http.StatusBadRequest)
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

	data, err := io.ReadAll(io.LimitReader(file, config.MaxCapturePayloadBytes+1))
	if err != nil {
		http.Error(w, "Failed to read file", http.StatusInternalServerError)
		return
	}
	if len(data) > config.MaxCapturePayloadBytes {
		http.Error(w, uploadTooLargeText(), http.StatusBadRequest)
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

	response := map[string]interface{}{
		"success":  true,
		"message":  fmt.Sprintf("Uploaded %s payload for %s", protocol, domain),
		"size":     len(data),
		"domain":   domain,
		"protocol": protocol,
		"facts":    nil,
	}
	var files []string
	if c, ok := manager.GetCapture(protocol, domain); ok {
		files = append(files, c.Filepath)
		response["facts"] = c.Facts
	}
	api.reloadSetsNaming(files, response)
	setJsonHeader(w)
	json.NewEncoder(w).Encode(response)
}
