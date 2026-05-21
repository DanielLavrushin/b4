package handler

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/log"
)

type GeodatDownloadRequest struct {
	GeositeURL      string `json:"geosite_url"`
	GeoipURL        string `json:"geoip_url"`
	DestinationPath string `json:"destination_path"`
}

type GeodatDownloadResponse struct {
	Success     bool   `json:"success"`
	Message     string `json:"message"`
	GeositePath string `json:"geosite_path"`
	GeoipPath   string `json:"geoip_path"`
	GeositeSize int64  `json:"geosite_size"`
	GeoipSize   int64  `json:"geoip_size"`
}

type GeodatSource struct {
	Name       string `json:"name"`
	GeositeURL string `json:"geosite_url"`
	GeoipURL   string `json:"geoip_url"`
}

func (api *API) RegisterGeodatApi() {
	api.mux.HandleFunc("/api/geodat/download", api.handleGeodatDownload)
	api.mux.HandleFunc("/api/geodat/upload", api.handleGeodatUpload)
	api.mux.HandleFunc("/api/geodat/sources", api.handleGeodatSources)
	api.mux.HandleFunc("/api/geodat/info", api.handleFileInfo)
}

//go:embed geodat.json
var geodatJSON []byte

var (
	geodatSources []GeodatSource
	geodatOnce    sync.Once
)

func loadGeodatSources() {
	geodatOnce.Do(func() {
		if err := json.Unmarshal(geodatJSON, &geodatSources); err != nil {
			log.Errorf("Failed to parse embedded geodat.json: %v", err)
			geodatSources = []GeodatSource{}
		}
	})
}

// @Summary List available geodat sources
// @Tags Geodat
// @Produce json
// @Success 200 {array} GeodatSource
// @Security BearerAuth
// @Router /geodat/sources [get]
func (api *API) handleGeodatSources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	loadGeodatSources()
	setJsonHeader(w)
	json.NewEncoder(w).Encode(geodatSources)
}

// @Summary Download geodat files
// @Tags Geodat
// @Accept json
// @Produce json
// @Param body body GeodatDownloadRequest true "Download request"
// @Success 200 {object} GeodatDownloadResponse
// @Security BearerAuth
// @Router /geodat/download [post]
func (api *API) handleGeodatDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req GeodatDownloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.DestinationPath == "" {
		http.Error(w, "Destination path required", http.StatusBadRequest)
		return
	}

	if err := validateDestinationPath(req.DestinationPath); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.DestinationPath = filepath.Clean(req.DestinationPath)

	if req.GeositeURL == "" && req.GeoipURL == "" {
		http.Error(w, "At least one of geosite_url or geoip_url is required", http.StatusBadRequest)
		return
	}

	geositeSize, geoipSize, err := api.RefreshGeodat(req.DestinationPath, req.GeositeURL, req.GeoipURL)
	if err != nil {
		log.Errorf("geodat download: %v", err)
		writeJsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	parts := []string{}
	if req.GeositeURL != "" {
		parts = append(parts, fmt.Sprintf("geosite.dat (%d bytes)", geositeSize))
	}
	if req.GeoipURL != "" {
		parts = append(parts, fmt.Sprintf("geoip.dat (%d bytes)", geoipSize))
	}
	log.Infof("Downloaded geodat files: %s", strings.Join(parts, ", "))

	response := GeodatDownloadResponse{
		Success:     true,
		Message:     "Downloaded: " + strings.Join(parts, ", "),
		GeositePath: api.getCfg().System.Geo.GeoSitePath,
		GeoipPath:   api.getCfg().System.Geo.GeoIpPath,
		GeositeSize: geositeSize,
		GeoipSize:   geoipSize,
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(response)
}

func (api *API) RefreshGeodat(destPath, geositeURL, geoipURL string) (int64, int64, error) {
	if err := os.MkdirAll(destPath, 0755); err != nil {
		return 0, 0, fmt.Errorf("failed to create directory %s: %v", destPath, err)
	}

	var geositeSize, geoipSize int64
	var newGeoSitePath, newGeoIpPath string

	if geositeURL != "" {
		geositePath := filepath.Join(destPath, "geosite.dat")
		size, err := downloadFile(geositeURL, geositePath)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to download geosite.dat: %v", err)
		}
		geositeSize = size
		newGeoSitePath = geositePath
	}

	if geoipURL != "" {
		geoipPath := filepath.Join(destPath, "geoip.dat")
		size, err := downloadFile(geoipURL, geoipPath)
		if err != nil {
			return geositeSize, 0, fmt.Errorf("failed to download geoip.dat: %v", err)
		}
		geoipSize = size
		newGeoIpPath = geoipPath
	}

	if newGeoSitePath != "" {
		api.getCfg().System.Geo.GeoSitePath = newGeoSitePath
		api.getCfg().System.Geo.GeoSiteURL = geositeURL
	}
	if newGeoIpPath != "" {
		api.getCfg().System.Geo.GeoIpPath = newGeoIpPath
		api.getCfg().System.Geo.GeoIpURL = geoipURL
	}

	api.geodataManager.UpdatePaths(api.getCfg().System.Geo.GeoSitePath, api.getCfg().System.Geo.GeoIpPath)
	api.geodataManager.ClearCache()

	for _, set := range api.getCfg().Sets {
		log.Infof("Reloading geo targets for set: %s", set.Name)
		api.loadTargetsForSetCached(set)
	}

	if err := api.saveAndPushConfig(api.getCfg()); err != nil {
		return geositeSize, geoipSize, fmt.Errorf("failed to save configuration: %v", err)
	}

	return geositeSize, geoipSize, nil
}

var deniedPathPrefixes = []string{
	"/proc", "/sys", "/dev", "/boot", "/run",
}

func validateDestinationPath(destPath string) error {
	cleaned := filepath.Clean(destPath)
	if !filepath.IsAbs(cleaned) {
		return fmt.Errorf("destination path must be absolute")
	}
	if strings.Contains(cleaned, "..") {
		return fmt.Errorf("destination path must not contain '..'")
	}
	for _, prefix := range deniedPathPrefixes {
		if cleaned == prefix || strings.HasPrefix(cleaned, prefix+"/") {
			return fmt.Errorf("destination path must not be under %s", prefix)
		}
	}
	return nil
}

// @Summary Upload geodat file
// @Tags Geodat
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "Geodat file (.dat or .db)"
// @Param type formData string true "File type (geosite or geoip)"
// @Param destination_path formData string true "Destination directory path"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @Router /geodat/upload [post]
func (api *API) handleGeodatUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	const maxUploadSize = 500 * 1024 * 1024 // 500MB
	const maxMemory = 32 << 20              // 32MB in-memory limit for multipart parsing

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	if err := r.ParseMultipartForm(maxMemory); err != nil {
		http.Error(w, "Failed to parse upload or file too large", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "File required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	fileType := r.FormValue("type")
	if fileType != "geosite" && fileType != "geoip" {
		http.Error(w, "Type must be 'geosite' or 'geoip'", http.StatusBadRequest)
		return
	}

	destPath := r.FormValue("destination_path")
	if destPath == "" {
		http.Error(w, "Destination path required", http.StatusBadRequest)
		return
	}

	if err := validateDestinationPath(destPath); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	destPath = filepath.Clean(destPath)

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".dat" && ext != ".db" {
		http.Error(w, "Only .dat and .db files are accepted", http.StatusBadRequest)
		return
	}

	if err := os.MkdirAll(destPath, 0755); err != nil {
		msg := fmt.Sprintf("Failed to create directory %s: %v", destPath, err)
		log.Errorf("geodat upload: %s", msg)
		writeJsonError(w, http.StatusInternalServerError, msg)
		return
	}

	destFile := filepath.Join(destPath, fileType+".dat")

	tmpFile, err := os.CreateTemp(destPath, ".geodat-upload-*.tmp")
	if err != nil {
		msg := fmt.Sprintf("Failed to create temp file: %v", err)
		log.Errorf("geodat upload: %s", msg)
		writeJsonError(w, http.StatusInternalServerError, msg)
		return
	}
	tmpPath := tmpFile.Name()

	size, err := io.Copy(tmpFile, file)
	if err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		msg := fmt.Sprintf("Failed to write uploaded file: %v", err)
		log.Errorf("geodat upload: %s", msg)
		writeJsonError(w, http.StatusInternalServerError, msg)
		return
	}

	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		msg := fmt.Sprintf("Failed to flush file to disk: %v", err)
		log.Errorf("geodat upload: %s", msg)
		writeJsonError(w, http.StatusInternalServerError, msg)
		return
	}
	tmpFile.Close()

	if err := os.Rename(tmpPath, destFile); err != nil {
		os.Remove(tmpPath)
		msg := fmt.Sprintf("Failed to move uploaded file to %s: %v", destFile, err)
		log.Errorf("geodat upload: %s", msg)
		writeJsonError(w, http.StatusInternalServerError, msg)
		return
	}

	if fileType == "geosite" {
		api.getCfg().System.Geo.GeoSitePath = destFile
		api.getCfg().System.Geo.GeoSiteURL = ""
	} else {
		api.getCfg().System.Geo.GeoIpPath = destFile
		api.getCfg().System.Geo.GeoIpURL = ""
	}

	api.geodataManager.UpdatePaths(api.getCfg().System.Geo.GeoSitePath, api.getCfg().System.Geo.GeoIpPath)
	api.geodataManager.ClearCache()

	for _, set := range api.getCfg().Sets {
		log.Infof("Reloading geo targets for set: %s", set.Name)
		api.loadTargetsForSetCached(set)
	}

	if err := api.saveAndPushConfig(api.getCfg()); err != nil {
		msg := fmt.Sprintf("Failed to save configuration: %v", err)
		log.Errorf("geodat upload: %s", msg)
		writeJsonError(w, http.StatusInternalServerError, msg)
		return
	}

	log.Infof("Uploaded %s.dat (%d bytes) from %s", fileType, size, header.Filename)

	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Uploaded %s.dat (%d bytes)", fileType, size),
		"path":    destFile,
		"size":    size,
	})
}

// @Summary Get geodat file info
// @Tags Geodat
// @Produce json
// @Param path query string true "File path"
// @Success 200 {object} object
// @Security BearerAuth
// @Router /geodat/info [get]
func (api *API) handleFileInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "Path parameter required", http.StatusBadRequest)
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			setJsonHeader(w)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"exists": false,
			})
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"exists":        true,
		"size":          info.Size(),
		"last_modified": info.ModTime().Format(time.RFC3339),
	})
}
