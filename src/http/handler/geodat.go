package handler

import (
	_ "embed"
	"encoding/json"
	"errors"
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
	Success     bool     `json:"success"`
	Message     string   `json:"message"`
	GeositePath string   `json:"geosite_path"`
	GeoipPath   string   `json:"geoip_path"`
	GeositeSize int64    `json:"geosite_size"`
	GeoipSize   int64    `json:"geoip_size"`
	Removed     []string `json:"removed,omitempty"`
}

type GeodatRemoveRequest struct {
	Type string `json:"type"`
}

type GeodatRemoveResponse struct {
	Success bool     `json:"success"`
	Message string   `json:"message"`
	Removed []string `json:"removed"`
	Kept    []string `json:"kept"`
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
	api.mux.HandleFunc("/api/geodat/remove", api.handleGeodatRemove)
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

	geositeSize, geoipSize, removed, err := api.RefreshGeodat(req.DestinationPath, req.GeositeURL, req.GeoipURL)
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

	message := "Downloaded: " + strings.Join(parts, ", ")
	if len(removed) > 0 {
		message += ". Removed previous copy: " + strings.Join(removed, ", ")
	}

	response := GeodatDownloadResponse{
		Success:     true,
		Message:     message,
		GeositePath: api.getCfg().System.Geo.GeoSitePath,
		GeoipPath:   api.getCfg().System.Geo.GeoIpPath,
		GeositeSize: geositeSize,
		GeoipSize:   geoipSize,
		Removed:     removed,
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(response)
}

func (api *API) RefreshGeodat(destPath, geositeURL, geoipURL string) (int64, int64, []string, error) {
	if err := os.MkdirAll(destPath, 0755); err != nil {
		return 0, 0, nil, fmt.Errorf("failed to create directory %s: %v", destPath, err)
	}

	var geositeSize, geoipSize int64
	var newGeoSitePath, newGeoIpPath string

	if geositeURL != "" {
		geositePath := filepath.Join(destPath, "geosite.dat")
		size, err := downloadFile(geositeURL, geositePath)
		if err != nil {
			return 0, 0, nil, fmt.Errorf("failed to download geosite.dat: %v", err)
		}
		geositeSize = size
		newGeoSitePath = geositePath
	}

	if geoipURL != "" {
		geoipPath := filepath.Join(destPath, "geoip.dat")
		size, err := downloadFile(geoipURL, geoipPath)
		if err != nil {
			return geositeSize, 0, nil, fmt.Errorf("failed to download geoip.dat: %v", err)
		}
		geoipSize = size
		newGeoIpPath = geoipPath
	}

	removed := []string{}
	if newGeoSitePath != "" {
		if old := removeRelocatedGeodat(api.getCfg().System.Geo.GeoSitePath, newGeoSitePath, "geosite.dat"); old != "" {
			removed = append(removed, old)
		}
		api.getCfg().System.Geo.GeoSitePath = newGeoSitePath
		api.getCfg().System.Geo.GeoSiteURL = geositeURL
	}
	if newGeoIpPath != "" {
		if old := removeRelocatedGeodat(api.getCfg().System.Geo.GeoIpPath, newGeoIpPath, "geoip.dat"); old != "" {
			removed = append(removed, old)
		}
		api.getCfg().System.Geo.GeoIpPath = newGeoIpPath
		api.getCfg().System.Geo.GeoIpURL = geoipURL
	}

	api.applyGeodatPaths()

	if err := api.saveAndPushConfig(api.getCfg()); err != nil {
		return geositeSize, geoipSize, removed, fmt.Errorf("failed to save configuration: %v", err)
	}

	return geositeSize, geoipSize, removed, nil
}

func (api *API) applyGeodatPaths() {
	api.geodataManager.UpdatePaths(api.getCfg().System.Geo.GeoSitePath, api.getCfg().System.Geo.GeoIpPath)
	api.geodataManager.ClearCache()

	for _, set := range api.getCfg().Sets {
		log.Infof("Reloading geo targets for set: %s", set.Name)
		api.loadTargetsForSetCached(set)
	}
}

func removeRelocatedGeodat(oldPath, newPath, managedName string) string {
	if oldPath == "" || oldPath == newPath {
		return ""
	}
	if filepath.Base(oldPath) != managedName {
		log.Infof("geodat: keeping %s, not a b4-managed %s", oldPath, managedName)
		return ""
	}
	if err := os.Remove(oldPath); err != nil {
		if !os.IsNotExist(err) {
			log.Warnf("geodat: failed to remove previous file %s: %v", oldPath, err)
		}
		return ""
	}
	log.Infof("geodat: removed previous file %s (relocated to %s)", oldPath, newPath)
	return oldPath
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

	removed := ""
	if fileType == "geosite" {
		removed = removeRelocatedGeodat(api.getCfg().System.Geo.GeoSitePath, destFile, "geosite.dat")
		api.getCfg().System.Geo.GeoSitePath = destFile
		api.getCfg().System.Geo.GeoSiteURL = ""
	} else {
		removed = removeRelocatedGeodat(api.getCfg().System.Geo.GeoIpPath, destFile, "geoip.dat")
		api.getCfg().System.Geo.GeoIpPath = destFile
		api.getCfg().System.Geo.GeoIpURL = ""
	}

	api.applyGeodatPaths()

	if err := api.saveAndPushConfig(api.getCfg()); err != nil {
		msg := fmt.Sprintf("Failed to save configuration: %v", err)
		log.Errorf("geodat upload: %s", msg)
		writeJsonError(w, http.StatusInternalServerError, msg)
		return
	}

	log.Infof("Uploaded %s.dat (%d bytes) from %s", fileType, size, header.Filename)

	message := fmt.Sprintf("Uploaded %s.dat (%d bytes)", fileType, size)
	if removed != "" {
		message += ". Removed previous copy: " + removed
	}

	setJsonHeader(w)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": message,
		"path":    destFile,
		"size":    size,
		"removed": removed,
	})
}

// @Summary Remove geodat files and disable geo databases
// @Tags Geodat
// @Accept json
// @Produce json
// @Param body body GeodatRemoveRequest true "Remove request"
// @Success 200 {object} GeodatRemoveResponse
// @Security BearerAuth
// @Router /geodat/remove [post]
func (api *API) handleGeodatRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req GeodatRemoveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	switch req.Type {
	case "geosite", "geoip", "both":
	default:
		http.Error(w, "Type must be 'geosite', 'geoip' or 'both'", http.StatusBadRequest)
		return
	}

	geo := &api.getCfg().System.Geo
	removed := []string{}
	kept := []string{}
	cleared := []string{}

	if req.Type == "geosite" || req.Type == "both" {
		deleted, err := deleteGeodatFile(geo.GeoSitePath, "geosite.dat")
		switch {
		case errors.Is(err, errUnmanagedGeodat):
			log.Warnf("geodat remove: keeping %s on disk, %v", geo.GeoSitePath, err)
			kept = append(kept, geo.GeoSitePath)
		case err != nil:
			log.Errorf("geodat remove: %v", err)
			writeJsonError(w, http.StatusInternalServerError, err.Error())
			return
		case deleted != "":
			removed = append(removed, deleted)
		}
		geo.GeoSitePath = ""
		geo.GeoSiteURL = ""
		cleared = append(cleared, "geosite")
	}

	if req.Type == "geoip" || req.Type == "both" {
		deleted, err := deleteGeodatFile(geo.GeoIpPath, "geoip.dat")
		switch {
		case errors.Is(err, errUnmanagedGeodat):
			log.Warnf("geodat remove: keeping %s on disk, %v", geo.GeoIpPath, err)
			kept = append(kept, geo.GeoIpPath)
		case err != nil:
			log.Errorf("geodat remove: %v", err)
			writeJsonError(w, http.StatusInternalServerError, err.Error())
			return
		case deleted != "":
			removed = append(removed, deleted)
		}
		geo.GeoIpPath = ""
		geo.GeoIpURL = ""
		cleared = append(cleared, "geoip")
	}

	api.applyGeodatPaths()

	if err := api.saveAndPushConfig(api.getCfg()); err != nil {
		msg := fmt.Sprintf("Failed to save configuration: %v", err)
		log.Errorf("geodat remove: %s", msg)
		writeJsonError(w, http.StatusInternalServerError, msg)
		return
	}

	message := "Disabled: " + strings.Join(cleared, ", ")
	switch {
	case len(removed) > 0:
		message += ". Deleted: " + strings.Join(removed, ", ")
	case len(kept) > 0:
		message += ". Kept on disk, not written by b4: " + strings.Join(kept, ", ")
	default:
		message += ". No file on disk to delete"
	}
	log.Infof("geodat remove: %s", message)

	setJsonHeader(w)
	json.NewEncoder(w).Encode(GeodatRemoveResponse{
		Success: true,
		Message: message,
		Removed: removed,
		Kept:    kept,
	})
}

var errUnmanagedGeodat = errors.New("file was not written by b4")

func deleteGeodatFile(path, managedName string) (string, error) {
	if path == "" {
		return "", nil
	}

	cleaned := filepath.Clean(path)
	if !filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("%s: %w", path, errUnmanagedGeodat)
	}
	for _, prefix := range deniedPathPrefixes {
		if cleaned == prefix || strings.HasPrefix(cleaned, prefix+"/") {
			return "", fmt.Errorf("%s is under %s: %w", cleaned, prefix, errUnmanagedGeodat)
		}
	}
	if filepath.Base(cleaned) != managedName {
		return "", fmt.Errorf("%s is not named %s: %w", cleaned, managedName, errUnmanagedGeodat)
	}

	if err := os.Remove(cleaned); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to delete %s: %v", cleaned, err)
	}
	return cleaned, nil
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
