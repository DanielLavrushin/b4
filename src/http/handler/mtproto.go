package handler

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/mtproto"
	"github.com/google/uuid"
)

func (api *API) RegisterMTProtoApi() {
	api.mux.HandleFunc("/api/mtproto/generate-secret", api.handleMTProtoGenerateSecret)
	api.mux.HandleFunc("/api/mtproto/config", api.handleMTProtoConfig)
	api.mux.HandleFunc("/api/mtproto/refresh-dcs", api.handleMTProtoRefreshDCs)
	api.mux.HandleFunc("/api/mtproto/test-ws", api.handleMTProtoTestWS)
	api.mux.HandleFunc("/api/mtproto/sessions", api.handleMTProtoSessions)
	api.mux.HandleFunc("/api/mtproto/active-clients", api.handleMTProtoActiveClients)
}

func mtprotoSessions() []mtproto.SessionInfo {
	if p, ok := globalMTProtoServer.(interface {
		Sessions() []mtproto.SessionInfo
	}); ok {
		return p.Sessions()
	}
	return nil
}

// @Summary List active MTProto sessions
// @Description One entry per live client connection, including client IP/port, upstream destination and activity timestamps.
// @Tags MTProto
// @Produce json
// @Success 200 {array} object
// @Security BearerAuth
// @Router /mtproto/sessions [get]
func (api *API) handleMTProtoSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	type sessionOut struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		ClientIP    string `json:"client_ip"`
		ClientPort  int    `json:"client_port"`
		Destination string `json:"destination"`
		ConnectedAt string `json:"connected_at"`
		LastSeen    string `json:"last_seen"`
	}
	sessions := mtprotoSessions()
	out := make([]sessionOut, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, sessionOut{
			ID:          s.ID,
			Name:        s.Name,
			ClientIP:    s.ClientIP,
			ClientPort:  s.ClientPort,
			Destination: s.Destination,
			ConnectedAt: s.ConnectedAt.UTC().Format(time.RFC3339),
			LastSeen:    s.LastSeen.UTC().Format(time.RFC3339),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ConnectedAt < out[j].ConnectedAt
	})
	sendResponse(w, out)
}

// @Summary List active MTProto clients per secret
// @Description Aggregated per-secret view: active connection count, the distinct client IPs currently using each secret, and last activity.
// @Tags MTProto
// @Produce json
// @Success 200 {array} object
// @Security BearerAuth
// @Router /mtproto/active-clients [get]
func (api *API) handleMTProtoActiveClients(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	type clientOut struct {
		ID                string   `json:"id"`
		Name              string   `json:"name"`
		ActiveConnections int      `json:"active_connections"`
		ActiveIPs         []string `json:"active_ips"`
		ActiveIPCount     int      `json:"active_ip_count"`
		LastSeen          string   `json:"last_seen"`
	}

	type agg struct {
		out      clientOut
		ips      []string
		seenIP   map[string]struct{}
		lastSeen time.Time
	}
	order := make([]string, 0)
	byID := make(map[string]*agg)

	for _, s := range mtprotoSessions() {
		a := byID[s.ID]
		if a == nil {
			a = &agg{
				out:    clientOut{ID: s.ID, Name: s.Name, ActiveIPs: []string{}},
				seenIP: make(map[string]struct{}),
			}
			byID[s.ID] = a
			order = append(order, s.ID)
		}
		a.out.ActiveConnections++
		if s.ClientIP != "" {
			if _, ok := a.seenIP[s.ClientIP]; !ok {
				a.seenIP[s.ClientIP] = struct{}{}
				a.ips = append(a.ips, s.ClientIP)
			}
		}
		if s.LastSeen.After(a.lastSeen) {
			a.lastSeen = s.LastSeen
		}
	}

	out := make([]clientOut, 0, len(order))
	for _, id := range order {
		a := byID[id]
		sort.Strings(a.ips)
		a.out.ActiveIPs = a.ips
		a.out.ActiveIPCount = len(a.ips)
		a.out.LastSeen = a.lastSeen.UTC().Format(time.RFC3339)
		out = append(out, a.out)
	}
	sendResponse(w, out)
}

// @Summary Probe MTProto upstream transports
// @Tags MTProto
// @Accept json
// @Produce json
// @Param body body object false "optional overrides: upstream_mode, ws_custom_domain, ws_endpoint_host, cfworker_domain, cfproxy_enabled, dc_relay, dc"
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
		WSEndpointHost *string `json:"ws_endpoint_host"`
		CFWorkerDomain *string `json:"cfworker_domain"`
		CFProxyEnabled *bool   `json:"cfproxy_enabled"`
		DCRelay        *string `json:"dc_relay"`
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
	if req.WSEndpointHost != nil {
		probeCfg.WSEndpointHost = *req.WSEndpointHost
	}
	if req.CFWorkerDomain != nil {
		probeCfg.CFWorkerDomain = *req.CFWorkerDomain
	}
	if req.CFProxyEnabled != nil {
		probeCfg.CFProxyEnabled = *req.CFProxyEnabled
	}
	if req.DCRelay != nil {
		probeCfg.DCRelay = *req.DCRelay
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
	mt := api.getCfg().System.MTProto
	if err := mtproto.RefreshDCs(mt.DCFallbackEnabled, mt.DCFallbackURL); err != nil {
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
// sanitizeSecretName strips control characters (newlines, CR, tab, other C0
// controls and DEL) from a user-provided secret name so it cannot corrupt the
// plain log lines it appears in. Commas are legal in names; the connection-log
// emitter sanitizes every CSV field on write (log.emitConnection).
func sanitizeSecretName(name string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, name)
	return strings.TrimSpace(cleaned)
}

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

	if req.MaxConnections < 0 || req.MaxConnections > 100000 {
		writeJsonError(w, http.StatusBadRequest, "Max connections must be between 0 (default) and 100000")
		return
	}

	for i := range req.Secrets {
		s := &req.Secrets[i]
		s.Name = sanitizeSecretName(s.Name)
		s.Secret = strings.TrimSpace(s.Secret)
		if s.Secret != "" {
			if _, err := mtproto.ParseSecret(s.Secret); err != nil {
				label := s.Name
				if label == "" {
					label = "#" + strconv.Itoa(i+1)
				}
				writeJsonError(w, http.StatusBadRequest, "Invalid secret "+label+": "+err.Error())
				return
			}
		}
		if s.ID == "" {
			s.ID = uuid.NewString()
		}
	}

	hasSecret := len(req.EffectiveSecrets()) > 0
	if req.Enabled && !hasSecret && req.FakeSNI == "" {
		writeJsonError(w, http.StatusBadRequest, "At least one secret or a fake SNI domain is required when enabled")
		return
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

	cur := api.getCfg()
	newCfg := cur.Clone()
	newCfg.System.MTProto = req

	if err := api.saveAndPushConfig(newCfg); err != nil {
		writeAPIError(w, err)
		return
	}

	log.Infof("MTProto configuration updated: enabled=%v, port=%d", req.Enabled, req.Port)

	sendResponse(w, map[string]interface{}{
		"success": true,
		"message": "MTProto configuration updated and applied.",
	})
}
