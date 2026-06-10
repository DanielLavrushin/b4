package handler

import (
	"encoding/json"
	"net"
	"net/http"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

func (api *API) RegisterSocks5Api() {
	api.mux.HandleFunc("/api/socks5/config", api.handleSocks5Config)
}

// @Summary Get SOCKS5 configuration
// @Tags SOCKS5
// @Produce json
// @Success 200 {object} object
// @Security BearerAuth
// @Router /socks5/config [get]
func (api *API) handleSocks5Config(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		sendResponse(w, map[string]interface{}{
			"success": true,
			"config":  api.getCfg().System.Socks5,
		})
	case http.MethodPost:
		api.updateSocks5Config(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// @Summary Update SOCKS5 configuration
// @Tags SOCKS5
// @Accept json
// @Produce json
// @Param body body config.Socks5Config true "SOCKS5 configuration"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @Router /socks5/config [post]
func (api *API) updateSocks5Config(w http.ResponseWriter, r *http.Request) {
	var req config.Socks5Config
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Port < 1 || req.Port > 65535 {
		writeJsonError(w, http.StatusBadRequest, "Port must be between 1 and 65535")
		return
	}

	if req.BindAddress != "" {
		if net.ParseIP(req.BindAddress) == nil {
			writeJsonError(w, http.StatusBadRequest, "Invalid bind address")
			return
		}
	}

	// Username and password must both be set or both be empty
	if (req.Username == "") != (req.Password == "") {
		writeJsonError(w, http.StatusBadRequest, "Username and password must both be provided or both be empty")
		return
	}

	cur := api.getCfg()
	newCfg := cur.Clone()
	newCfg.System.Socks5 = req

	if err := api.saveAndPushConfig(newCfg); err != nil {
		writeAPIError(w, err)
		return
	}

	log.Infof("SOCKS5 configuration updated: enabled=%v, port=%d", req.Enabled, req.Port)

	sendResponse(w, map[string]interface{}{
		"success": true,
		"message": "SOCKS5 configuration updated. Restart required for changes to take effect.",
	})
}
