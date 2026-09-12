package handler

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/mtproto"
)

func (api *API) webProxyPagePath() string {
	return mtproto.WebProxyPagePath(api.getCfg())
}

func reloadWebProxyPage() {
	if p, ok := globalMTProtoServer.(interface{ ReloadWebProxyPage() }); ok {
		p.ReloadWebProxyPage()
	}
}

// @Summary Custom WEB proxy placeholder page
// @Description Reports whether a custom placeholder page is installed for the Telegram WEB proxy relay. With download=1 the page itself is returned.
// @Tags MTProto
// @Produce json
// @Param download query bool false "Return the page body instead of its status"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @Router /mtproto/web-proxy/page [get]
func (api *API) handleMTProtoWebProxyPage(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		api.getWebProxyPage(w, r)
	case http.MethodPost:
		api.uploadWebProxyPage(w, r)
	case http.MethodDelete:
		api.deleteWebProxyPage(w)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (api *API) getWebProxyPage(w http.ResponseWriter, r *http.Request) {
	path := api.webProxyPagePath()
	if path == "" {
		writeJsonError(w, http.StatusInternalServerError, "config path is not set")
		return
	}
	st, err := os.Stat(path)
	custom := err == nil && !st.IsDir() && st.Size() > 0 && st.Size() <= mtproto.WebProxyPageMaxSize
	if r.URL.Query().Get("download") == "1" {
		if !custom {
			writeJsonError(w, http.StatusNotFound, "no custom page installed")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+mtproto.WebProxyPageFile+"\"")
		http.ServeFile(w, r, path)
		return
	}
	resp := map[string]interface{}{
		"success":  true,
		"custom":   custom,
		"path":     path,
		"max_size": mtproto.WebProxyPageMaxSize,
	}
	if custom {
		resp["size"] = st.Size()
		resp["modified"] = st.ModTime().UTC()
	}
	sendResponse(w, resp)
}

// @Summary Upload a custom WEB proxy placeholder page
// @Description Installs a self-contained HTML file that the relay serves to every visitor that is not a Telegram client. Replaces the built-in placeholder.
// @Tags MTProto
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "HTML file, at most 1 MiB"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Security BearerAuth
// @Router /mtproto/web-proxy/page [post]
func (api *API) uploadWebProxyPage(w http.ResponseWriter, r *http.Request) {
	path := api.webProxyPagePath()
	if path == "" {
		writeJsonError(w, http.StatusInternalServerError, "config path is not set")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, mtproto.WebProxyPageMaxSize+64<<10)
	if err := r.ParseMultipartForm(mtproto.WebProxyPageMaxSize); err != nil {
		writeJsonError(w, http.StatusBadRequest, "file too large (max 1 MiB)")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeJsonError(w, http.StatusBadRequest, "file required")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, mtproto.WebProxyPageMaxSize+1))
	if err != nil {
		writeJsonError(w, http.StatusBadRequest, "failed to read file")
		return
	}
	if len(data) == 0 {
		writeJsonError(w, http.StatusBadRequest, "file is empty")
		return
	}
	if len(data) > mtproto.WebProxyPageMaxSize {
		writeJsonError(w, http.StatusBadRequest, "file too large (max 1 MiB)")
		return
	}
	head := strings.ToLower(string(data[:min(len(data), 512)]))
	if !strings.Contains(head, "<") {
		writeJsonError(w, http.StatusBadRequest, "file does not look like HTML")
		return
	}
	if err := writeFileAtomic(path, data); err != nil {
		log.Errorf("MTProto WEB proxy page: write failed: %v", err)
		writeJsonError(w, http.StatusInternalServerError, "failed to save page: "+err.Error())
		return
	}
	reloadWebProxyPage()
	log.Infof("MTProto WEB proxy placeholder page installed (%d bytes)", len(data))
	sendResponse(w, map[string]interface{}{"success": true, "size": len(data)})
}

// @Summary Remove the custom WEB proxy placeholder page
// @Description Deletes the uploaded page. The relay falls back to the built-in placeholder.
// @Tags MTProto
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @Router /mtproto/web-proxy/page [delete]
func (api *API) deleteWebProxyPage(w http.ResponseWriter) {
	path := api.webProxyPagePath()
	if path == "" {
		writeJsonError(w, http.StatusInternalServerError, "config path is not set")
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		writeJsonError(w, http.StatusInternalServerError, "failed to remove page: "+err.Error())
		return
	}
	reloadWebProxyPage()
	log.Infof("MTProto WEB proxy placeholder page removed, built-in page restored")
	sendResponse(w, map[string]interface{}{"success": true})
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
