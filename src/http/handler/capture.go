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
	"unicode/utf8"

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
	api.mux.HandleFunc("/api/capture/resolve", api.handleResolveCapture)
	api.mux.HandleFunc("/api/capture/upload", api.handleUploadCapture)
	api.mux.HandleFunc("/api/capture/inspect", api.handleInspectCapture)
	api.mux.HandleFunc("/api/capture/builtin", api.handleListBuiltinPayloads)
	api.mux.HandleFunc("/api/capture/builtin/download", api.handleDownloadBuiltinPayload)
}

// @Summary Generate capture payload
// @Description Generates a TLS ClientHello for a host name and reloads the sets that already name its file.
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
	var files []string
	if generated, ok := manager.GetCapture(req.Protocol, req.Domain); ok {
		size = generated.Size
		facts = generated.Facts
		files = append(files, generated.Filepath)
	}

	response := map[string]interface{}{
		"success": true,
		"message": generatedPayloadMessage(req.Domain, size),
		"method":  "generated",
		"size":    size,
		"domain":  req.Domain,
		"facts":   facts,
	}
	api.reloadSetsNaming(files, response)
	setJsonHeader(w)
	json.NewEncoder(w).Encode(response)
}

const (
	maxHostNameLength  = 253
	maxHostLabelLength = 63
)

var fullStops = strings.NewReplacer("。", ".", "．", ".", "｡", ".")

func normalizeHostName(raw string) (string, bool) {
	host := fullStops.Replace(strings.ToLower(strings.TrimSpace(raw)))
	if scheme, _, found := strings.Cut(host, "://"); found && isURLScheme(scheme) {
		u, err := url.Parse(host)
		if err != nil {
			return "", false
		}
		host = u.Host
	} else if i := strings.IndexAny(host, "/?#"); i >= 0 {
		host = host[:i]
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(host, ".")
	if net.ParseIP(strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")) != nil {
		return "", false
	}
	host, err := sni.HostToASCII(host)
	if err != nil || len(host) > maxHostNameLength || !sni.IsValidSNI([]byte(host)) {
		return "", false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > maxHostLabelLength {
			return "", false
		}
	}
	return host, true
}

func isURLScheme(s string) bool {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '+' && c != '-' && c != '.' {
			return false
		}
	}
	return true
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
	domain := strings.TrimSpace(req.Domain)

	manager := capture.GetManager(api.getCfg())
	regenerated, err := manager.RegenerateCapture(domain)
	switch {
	case errors.Is(err, capture.ErrPayloadNotFound):
		http.Error(w, fmt.Sprintf("No TLS payload named %q", domain), http.StatusNotFound)
		return
	case errors.Is(err, capture.ErrNotGenerated):
		http.Error(w, fmt.Sprintf("The TLS payload %q was not made by Generate, so it cannot be regenerated", domain), http.StatusConflict)
		return
	case errors.Is(err, capture.ErrNoHostName):
		http.Error(w, fmt.Sprintf("Neither the server name of the TLS payload %q nor its name is a host name, so it cannot be regenerated", domain), http.StatusConflict)
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

func setsNamingCaptures(cfg *config.Config, files []string) []string {
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
	return names
}

func (api *API) reloadSetsNaming(files []string, response map[string]interface{}) {
	response["reloaded"] = []string{}
	var named []string
	names, err := api.reloadRunningSets(func(running *config.Config) []string {
		named = setsNamingCaptures(running, files)
		return named
	})
	if err != nil {
		log.Errorf("The payload change did not reach sets %s: %v", strings.Join(named, ", "), err)
		response["reload_error"] = err.Error()
		return
	}
	if len(names) > 0 {
		response["reloaded"] = names
	}
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

type PayloadFileResolution struct {
	File   string         `json:"file"`
	Listed bool           `json:"listed"`
	Exists bool           `json:"exists"`
	Size   int64          `json:"size"`
	Facts  *capture.Facts `json:"facts,omitempty"`
}

// @Summary Resolve a payload file as a set names it
// @Description Cleans the path the way the payload loader does and answers it in the form the payload list uses, whether a list entry names that file, whether the file exists, its size and its facts. Facts are judged for the protocol of the list entry, or the protocol the file suits when no entry names it, and are omitted when the file cannot be read, is empty or is larger than a payload may be. A path that leaves captures/ or does not end in .bin is refused.
// @Tags Capture
// @Produce json
// @Param file query string true "Payload file as the set names it, e.g. captures/tls_example_com.bin"
// @Success 200 {object} PayloadFileResolution
// @Failure 400 {string} string
// @Security BearerAuth
// @Router /capture/resolve [get]
func (api *API) handleResolveCapture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	cfg := api.getCfg()
	path, file, err := cfg.CapturePayloadPath(r.URL.Query().Get("file"))
	if err != nil {
		http.Error(w, "Not a payload file: "+err.Error(), http.StatusBadRequest)
		return
	}

	answer := PayloadFileResolution{File: file}
	protocol := ""
	for _, c := range capture.GetManager(cfg).ListCaptures() {
		if filepath.ToSlash(filepath.Clean(c.Filepath)) == file {
			answer.Listed = true
			protocol = c.Protocol
			break
		}
	}

	if capturesDir, err := filepath.Abs(filepath.Join(filepath.Dir(cfg.ConfigPath), "captures")); err == nil {
		if real, info, ok := captureFileInside(capturesDir, path); ok {
			answer.Exists = true
			answer.Size = info.Size()
			if data, ok := readCaptureBytes(real, info.Size()); ok {
				if protocol == "" {
					answer.Facts = suggestedFacts(data)
				} else {
					answer.Facts = capture.FactsOf(protocol, data)
				}
			}
		}
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(answer)
}

func captureFileInside(capturesDir, path string) (string, os.FileInfo, bool) {
	root, err := filepath.EvalSymlinks(capturesDir)
	if err != nil {
		return "", nil, false
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", nil, false
	}
	rel, err := filepath.Rel(root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", nil, false
	}
	info, err := os.Stat(real)
	if err != nil || !info.Mode().IsRegular() {
		return "", nil, false
	}
	return real, info, true
}

func readCaptureBytes(path string, size int64) ([]byte, bool) {
	if size <= 0 || size > config.MaxCapturePayloadBytes {
		return nil, false
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, config.MaxCapturePayloadBytes+1))
	if err != nil || len(data) == 0 || len(data) > config.MaxCapturePayloadBytes {
		return nil, false
	}
	return data, true
}

func suggestedFacts(data []byte) *capture.Facts {
	facts := capture.FactsOf("tls", data)
	if capture.SuggestedProtocol(facts.Kind) == "quic" {
		facts.FitsOnePacket = capture.FitsOnePacket("quic", len(data))
	}
	return facts
}

// @Summary List the built-in payloads
// @Description The payloads compiled into b4 (google, vk, stun, quic1, quic2), each with its protocol, size, facts and source.
// @Tags Capture
// @Produce json
// @Success 200 {array} capture.Builtin
// @Security BearerAuth
// @Router /capture/builtin [get]
func (api *API) handleListBuiltinPayloads(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	setJsonHeader(w)
	json.NewEncoder(w).Encode(capture.Builtins())
}

// @Summary Download a built-in payload
// @Tags Capture
// @Produce application/octet-stream
// @Param id query string true "Built-in payload id (google, vk, stun, quic1, quic2)"
// @Success 200 {file} binary
// @Failure 404 {string} string
// @Security BearerAuth
// @Router /capture/builtin/download [get]
func (api *API) handleDownloadBuiltinPayload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	data, ok := capture.BuiltinBytes(id)
	if !ok {
		http.Error(w, fmt.Sprintf("No built-in payload %q", id), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", "builtin_"+id+".bin"))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.Write(data)
}

const uploadFormOverhead = 16 << 10

func uploadTooLargeText() string {
	return fmt.Sprintf("File too large: a payload may have at most %d bytes", config.MaxCapturePayloadBytes)
}

func readPayloadForm(w http.ResponseWriter, r *http.Request) ([]byte, string, bool) {
	const bodyLimit = config.MaxCapturePayloadBytes + uploadFormOverhead
	r.Body = http.MaxBytesReader(w, r.Body, bodyLimit)

	if err := r.ParseMultipartForm(bodyLimit); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, uploadTooLargeText(), http.StatusBadRequest)
			return nil, "", false
		}
		http.Error(w, "Invalid upload form: "+err.Error(), http.StatusBadRequest)
		return nil, "", false
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "File required", http.StatusBadRequest)
		return nil, "", false
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, config.MaxCapturePayloadBytes+1))
	if err != nil {
		http.Error(w, "Failed to read file", http.StatusInternalServerError)
		return nil, "", false
	}
	if len(data) > config.MaxCapturePayloadBytes {
		http.Error(w, uploadTooLargeText(), http.StatusBadRequest)
		return nil, "", false
	}
	return data, header.Filename, true
}

// @Summary Describe a payload file without saving it
// @Description Reads an uploaded file and answers what it is: its facts, the protocol to save it under, its server name, whether it fits one packet as a TCP and as a UDP fake, and the limits behind those answers. Nothing is saved.
// @Tags Capture
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "Payload binary file"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {string} string
// @Security BearerAuth
// @Router /capture/inspect [post]
func (api *API) handleInspectCapture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	data, _, ok := readPayloadForm(w, r)
	if !ok {
		return
	}

	facts := suggestedFacts(data)
	protocol := capture.SuggestedProtocol(facts.Kind)
	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"size":     len(data),
		"facts":    facts,
		"protocol": protocol,
		"name":     facts.SNI,
		"fits": map[string]bool{
			"tls":  capture.FitsOnePacket("tls", len(data)),
			"quic": capture.FitsOnePacket("quic", len(data)),
		},
		"limits": map[string]int{
			"tls":  config.MaxTCPFakeLenInOnePacket,
			"quic": config.MaxUDPFakeLen,
			"file": config.MaxCapturePayloadBytes,
		},
	})
}

// @Summary Upload a capture file
// @Description Saves a payload file under a name and protocol. A name and protocol that already exist are replaced only with replace=1; otherwise the answer is 409 with code "exists" and the sets that use the payload in used_by.
// @Tags Capture
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "Capture binary file"
// @Param domain formData string true "Payload name, at most 200 characters"
// @Param protocol formData string false "Protocol (tls or quic)" default(tls)
// @Param replace formData string false "1 to replace an existing payload of this name and protocol"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {string} string
// @Failure 409 {object} map[string]interface{}
// @Security BearerAuth
// @Router /capture/upload [post]
func (api *API) handleUploadCapture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	data, filename, ok := readPayloadForm(w, r)
	if !ok {
		return
	}

	domain := strings.ToLower(strings.TrimSpace(r.FormValue("domain")))
	protocol := r.FormValue("protocol")

	if domain == "" {
		http.Error(w, "Domain required", http.StatusBadRequest)
		return
	}
	if n := utf8.RuneCountInString(domain); n > capture.MaxNameLength {
		http.Error(w, fmt.Sprintf("Name too long: a payload name may have at most %d characters, this one has %d", capture.MaxNameLength, n), http.StatusBadRequest)
		return
	}
	if protocol == "" {
		protocol = "tls"
	}
	if protocol != "tls" && protocol != "quic" {
		http.Error(w, "Protocol must be 'tls' or 'quic'", http.StatusBadRequest)
		return
	}

	if len(data) == 0 {
		http.Error(w, "Empty file", http.StatusBadRequest)
		return
	}

	manager := capture.GetManager(api.getCfg())
	err := manager.SaveUploadedCapture(protocol, domain, data, r.FormValue("replace") == "1")
	if errors.Is(err, capture.ErrPayloadExists) {
		usedBy := []string{}
		if c, ok := manager.GetCapture(protocol, domain); ok {
			usedBy = append(usedBy, setsNamingCapture(api.getCfg(), c.Filepath)...)
		}
		setJsonHeader(w)
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"code":    "exists",
			"error":   fmt.Sprintf("A %s payload named %s already exists; send replace=1 to replace it", protocol, domain),
			"used_by": usedBy,
		})
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Infof("Uploaded %s capture for %s: %s (%d bytes)", protocol, domain, filename, len(data))

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
