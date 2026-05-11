package handler

import (
	"encoding/json"
	"io"
	"net"
	"net/http"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/mtproto"
)

func (api *API) RegisterMTProtoApi() {
	api.mux.HandleFunc("/api/mtproto/generate-secret", api.handleMTProtoGenerateSecret)
	api.mux.HandleFunc("/api/mtproto/config", api.handleMTProtoConfig)
	api.mux.HandleFunc("/api/mtproto/refresh-dcs", api.handleMTProtoRefreshDCs)
	api.mux.HandleFunc("/api/mtproto/test-ws", api.handleMTProtoTestWS)
}

// @Summary Probe MTProto upstream transports
// @Tags MTProto
// @Accept json
// @Produce json
// @Param body body object false "optional overrides: upstream_mode, ws_custom_domain, dc"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @Router /mtproto/test-ws [post]
func (api *API) handleMTProtoTestWS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		UpstreamMode   string  `json:"upstream_mode"`
		WSCustomDomain *string `json:"ws_custom_domain"`
		WSFallbackTCP  *bool   `json:"ws_fallback_tcp"`
		WSEndpointHost *string `json:"ws_endpoint_host"`
		DC             int     `json:"dc"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeJsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	switch req.UpstreamMode {
	case "", "tcp", "ws", "auto":
	default:
		writeJsonError(w, http.StatusBadRequest, "upstream_mode must be tcp, ws or auto")
		return
	}

	cfg := api.getCfg()
	probeCfg := cfg.System.MTProto
	if req.UpstreamMode != "" {
		probeCfg.UpstreamMode = req.UpstreamMode
	}
	if req.WSCustomDomain != nil {
		probeCfg.WSCustomDomain = *req.WSCustomDomain
	}
	if req.WSFallbackTCP != nil {
		probeCfg.WSFallbackTCP = *req.WSFallbackTCP
	}
	if req.WSEndpointHost != nil {
		probeCfg.WSEndpointHost = *req.WSEndpointHost
	}
	if probeCfg.UpstreamMode == "" {
		probeCfg.UpstreamMode = "auto"
	}
	dc := req.DC
	if dc == 0 {
		dc = 2
	}

	results, err := mtproto.ProbeTransports(&probeCfg, cfg.Queue, dc)
	if err != nil {
		writeJsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	sendResponse(w, map[string]interface{}{
		"success": true,
		"dc":      dc,
		"results": results,
	})
}

// @Summary Refresh MTProto DCs
// @Tags MTProto
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @Router /mtproto/refresh-dcs [post]
func (api *API) handleMTProtoRefreshDCs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := mtproto.RefreshDCs(); err != nil {
		log.Warnf("MTProto manual DC refresh failed: %v", err)
		writeJsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	snap := mtproto.DCSnapshot()
	sendResponse(w, map[string]interface{}{
		"success": true,
		"count":   len(snap),
		"dcs":     snap,
	})
}

// @Summary Generate MTProto secret
// @Tags MTProto
// @Accept json
// @Produce json
// @Param body body object true "fake_sni field required"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @Router /mtproto/generate-secret [post]
func (api *API) handleMTProtoGenerateSecret(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		FakeSNI string `json:"fake_sni"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.FakeSNI == "" {
		writeJsonError(w, http.StatusBadRequest, "fake_sni is required")
		return
	}

	sec, err := mtproto.GenerateSecret(req.FakeSNI)
	if err != nil {
		writeJsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	sendResponse(w, map[string]interface{}{
		"success": true,
		"secret":  sec.Hex(),
	})
}

// @Summary Get MTProto configuration
// @Tags MTProto
// @Produce json
// @Success 200 {object} object
// @Security BearerAuth
// @Router /mtproto/config [get]
func (api *API) handleMTProtoConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		sendResponse(w, map[string]interface{}{
			"success": true,
			"config":  api.getCfg().System.MTProto,
		})
	case http.MethodPost:
		api.updateMTProtoConfig(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// @Summary Update MTProto configuration
// @Tags MTProto
// @Accept json
// @Produce json
// @Param body body config.MTProtoConfig true "MTProto configuration"
// @Success 200 {object} map[string]interface{}
// @Security BearerAuth
// @Router /mtproto/config [post]
func (api *API) updateMTProtoConfig(w http.ResponseWriter, r *http.Request) {
	var req config.MTProtoConfig
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

	if req.Enabled && req.Secret == "" && req.FakeSNI == "" {
		writeJsonError(w, http.StatusBadRequest, "Either secret or fake SNI domain is required when enabled")
		return
	}

	if req.Secret != "" {
		if _, err := mtproto.ParseSecret(req.Secret); err != nil {
			writeJsonError(w, http.StatusBadRequest, "Invalid secret: "+err.Error())
			return
		}
	}

	if req.DCRelay != "" {
		if _, _, err := net.SplitHostPort(req.DCRelay); err != nil {
			writeJsonError(w, http.StatusBadRequest, "Invalid DC relay address, expected host:port")
			return
		}
	}

	switch req.UpstreamMode {
	case "", "tcp", "ws", "auto":
	default:
		writeJsonError(w, http.StatusBadRequest, "upstream_mode must be tcp, ws or auto")
		return
	}

	cfg := api.getCfg()
	cfg.System.MTProto = req

	if err := cfg.SaveToFile(cfg.ConfigPath); err != nil {
		log.Errorf("Failed to save MTProto config: %v", err)
		writeJsonError(w, http.StatusInternalServerError, "Failed to save configuration")
		return
	}

	log.Infof("MTProto configuration updated: enabled=%v, port=%d", req.Enabled, req.Port)

	sendResponse(w, map[string]interface{}{
		"success": true,
		"message": "MTProto configuration updated. Restart required for changes to take effect.",
	})
}
