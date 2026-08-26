package handler

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/log"
)

func (api *API) RegisterBackupApi() {
	api.mux.HandleFunc("/api/backup", api.handleBackup)
	api.mux.HandleFunc("/api/backup/restore", api.handleRestore)
}

type backupEntry struct {
	path string
	rel  string
	info os.FileInfo
}

// @Summary Download configuration backup
// @Tags Backup
// @Produce application/gzip
// @Success 200 {file} binary
// @Failure 500 {object} map[string]interface{}
// @Security BearerAuth
// @Router /backup [get]
func (api *API) handleBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	configDir := filepath.Dir(api.getCfg().ConfigPath)
	if configDir == "" || configDir == "." {
		writeJsonError(w, http.StatusInternalServerError, "Config directory not configured")
		return
	}

	entries, files, err := collectBackupEntries(configDir)
	if err != nil {
		log.Errorf("Backup creation failed while reading %s: %v", configDir, err)
		writeJsonError(w, http.StatusInternalServerError, "Failed to read config directory: "+err.Error())
		return
	}

	if files == 0 {
		log.Errorf("Backup aborted: no files found in %s", configDir)
		writeJsonError(w, http.StatusInternalServerError, "No files to back up in "+configDir)
		return
	}

	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("b4-backup-%s.tar.gz", timestamp)

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))

	if err := writeBackupArchive(w, entries); err != nil {
		log.Errorf("Backup creation failed: %v", err)
	}
}

func collectBackupEntries(configDir string) ([]backupEntry, int, error) {
	var selfInfo os.FileInfo
	if exe, err := os.Executable(); err == nil {
		selfInfo, _ = os.Stat(exe)
	}

	var entries []backupEntry
	files := 0

	err := filepath.Walk(configDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			if path == configDir {
				return walkErr
			}
			log.Warnf("Backup skipping unreadable path %s: %v", path, walkErr)
			if info != nil && info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(configDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		if shouldExcludeFromBackup(info) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if !info.IsDir() && !info.Mode().IsRegular() {
			return nil
		}

		if selfInfo != nil && os.SameFile(info, selfInfo) {
			return nil
		}

		entries = append(entries, backupEntry{path: path, rel: filepath.ToSlash(rel), info: info})
		if !info.IsDir() {
			files++
		}
		return nil
	})

	return entries, files, err
}

func writeBackupArchive(w io.Writer, entries []backupEntry) error {
	gw := gzip.NewWriter(w)
	tw := tar.NewWriter(gw)

	for _, entry := range entries {
		header, err := tar.FileInfoHeader(entry.info, "")
		if err != nil {
			return err
		}
		header.Name = entry.rel
		if entry.info.IsDir() {
			header.Name += "/"
		}

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if entry.info.IsDir() {
			continue
		}

		if err := copyFileToTar(tw, entry.path, header.Size); err != nil {
			return err
		}
	}

	if err := tw.Close(); err != nil {
		return err
	}
	return gw.Close()
}

func copyFileToTar(tw *tar.Writer, path string, size int64) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	written, err := io.Copy(tw, io.LimitReader(f, size))
	if err != nil {
		return err
	}

	remaining := size - written
	if remaining <= 0 {
		return nil
	}

	pad := make([]byte, 32*1024)
	for remaining > 0 {
		chunk := int64(len(pad))
		if remaining < chunk {
			chunk = remaining
		}
		n, err := tw.Write(pad[:chunk])
		if err != nil {
			return err
		}
		remaining -= int64(n)
	}
	return nil
}

// @Summary Restore configuration from backup
// @Tags Backup
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "Backup tar.gz file"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Security BearerAuth
// @Router /backup/restore [post]
func (api *API) handleRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	configDir := filepath.Dir(api.getCfg().ConfigPath)
	if configDir == "" || configDir == "." {
		writeJsonError(w, http.StatusInternalServerError, "Config directory not configured")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)

	if err := r.ParseMultipartForm(50 << 20); err != nil {
		writeJsonError(w, http.StatusBadRequest, "Failed to parse upload: "+err.Error())
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		writeJsonError(w, http.StatusBadRequest, "No file provided")
		return
	}
	defer file.Close()

	gr, err := gzip.NewReader(file)
	if err != nil {
		writeJsonError(w, http.StatusBadRequest, "Invalid gzip file: "+err.Error())
		return
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	restored := 0
	skipped := 0

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeJsonError(w, http.StatusBadRequest, "Invalid tar archive: "+err.Error())
			return
		}

		targetPath, ok := safeRestorePath(configDir, header.Name)
		if !ok {
			if header.Typeflag == tar.TypeReg {
				io.Copy(io.Discard, tr)
			}
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				writeJsonError(w, http.StatusInternalServerError, "Failed to create directory: "+err.Error())
				return
			}

		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				writeJsonError(w, http.StatusInternalServerError, "Failed to create directory: "+err.Error())
				return
			}

			outFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, header.FileInfo().Mode())
			if err != nil {
				log.Warnf("Skipping file during restore (cannot write): %s: %v", header.Name, err)
				io.Copy(io.Discard, tr)
				skipped++
				continue
			}

			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				log.Warnf("Skipping file during restore (write error): %s: %v", header.Name, err)
				io.Copy(io.Discard, tr)
				skipped++
				continue
			}
			outFile.Close()
			restored++
		}
	}

	if restored == 0 {
		writeJsonError(w, http.StatusBadRequest, "Backup archive contains no files to restore")
		return
	}

	log.Infof("Backup restored to %s: %d file(s) written, %d skipped", configDir, restored, skipped)
	sendResponse(w, map[string]interface{}{
		"success": true,
		"files":   restored,
		"skipped": skipped,
		"message": fmt.Sprintf("Restored %d file(s) to %s", restored, configDir),
	})
}

func safeRestorePath(configDir, name string) (string, bool) {
	cleanName := filepath.Clean(filepath.FromSlash(name))
	if cleanName == "." || cleanName == ".." || filepath.IsAbs(cleanName) {
		return "", false
	}
	if strings.HasPrefix(cleanName, ".."+string(filepath.Separator)) {
		return "", false
	}

	targetPath := filepath.Join(configDir, cleanName)
	prefix := strings.TrimSuffix(configDir, string(filepath.Separator)) + string(filepath.Separator)
	if !strings.HasPrefix(targetPath, prefix) {
		return "", false
	}
	return targetPath, true
}

func shouldExcludeFromBackup(info os.FileInfo) bool {
	name := info.Name()

	if strings.HasSuffix(name, ".dat") {
		return true
	}

	if name == "oui.txt" {
		return true
	}

	if info.IsDir() && (name == "out" || strings.HasPrefix(name, ".")) {
		return true
	}

	return false
}
