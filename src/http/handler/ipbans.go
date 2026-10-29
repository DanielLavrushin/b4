package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/daniellavrushin/b4/iphealth"
	"github.com/daniellavrushin/b4/metrics"
)

const ipBansBodyLimit = 4096

type IPBansResponse struct {
	Items []metrics.BanEntry `json:"items"`
}

type IPBansClearRequest struct {
	Key string `json:"key"`
}

type IPBansClearResponse struct {
	Success bool `json:"success"`
	Cleared int  `json:"cleared"`
}

func (api *API) RegisterIPBansApi() {
	api.mux.HandleFunc("/api/ipbans", api.getIPBans)
	api.mux.HandleFunc("/api/ipbans/clear", api.clearIPBans)
}

// @Summary List the banned addresses
// @Description Returns the addresses b4 currently bans: an address and port that a set with IP block detection caught and that did not answer TLS for neutral site names either. key is the address and port, host and set name the connection that triggered the ban, strikes counts the confirmed blocks, repeat is true when the ban uses the repeat duration, since and until are wall-clock milliseconds, passes counts the successful re-tests in a row. The list is empty while system.ip_health.bans_off is set. Sorted by until, latest first.
// @Tags IPBans
// @Produce json
// @Success 200 {object} IPBansResponse
// @Failure 405 {string} string "Method not allowed"
// @Security BearerAuth
// @Router /ipbans [get]
func (a *API) getIPBans(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	items := []metrics.BanEntry{}
	if globalPool != nil {
		items = globalPool.GetBans()
	}

	setJsonHeader(w)
	_ = json.NewEncoder(w).Encode(IPBansResponse{Items: items})
}

// @Summary Unban addresses
// @Description Lifts the ban on one address and port, or on every banned address when key is empty or the body is missing, and forgets the ban history, so a later block of the same address counts as a first ban again. cleared is the number of bans that were in effect.
// @Tags IPBans
// @Accept json
// @Produce json
// @Param body body IPBansClearRequest false "Address and port to unban, such as 74.125.172.167:443 or [2001:db8::1]:443"
// @Success 200 {object} IPBansClearResponse
// @Failure 400 {object} APIError "invalid_json or bad_request"
// @Failure 405 {string} string "Method not allowed"
// @Security BearerAuth
// @Router /ipbans/clear [post]
func (a *API) clearIPBans(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req IPBansClearRequest
	r.Body = http.MaxBytesReader(w, r.Body, ipBansBodyLimit)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeAPIError(w, ErrInvalidJSON())
		return
	}
	key := strings.TrimSpace(req.Key)
	if key != "" {
		if _, _, ok := iphealth.ParseBanKey(key); !ok {
			writeAPIError(w, ErrBadRequest("key must be an address and port, such as 74.125.172.167:443"))
			return
		}
	}

	cleared := 0
	if globalPool != nil {
		cleared = globalPool.ClearBans(key)
	}

	setJsonHeader(w)
	_ = json.NewEncoder(w).Encode(IPBansClearResponse{Success: true, Cleared: cleared})
}
