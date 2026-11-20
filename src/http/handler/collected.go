package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strings"

	"github.com/daniellavrushin/b4/metrics"
)

type CollectedIPsResponse struct {
	Items []metrics.CollectedEntry `json:"items"`
}
type CollectedIPsClearRequest struct {
	IP string `json:"ip"`
}
type CollectedIPsClearResponse struct {
	Success bool `json:"success"`
	Cleared int  `json:"cleared"`
}

func (api *API) RegisterCollectedIPsApi() {
	api.mux.HandleFunc("/api/collected-ips", api.getCollectedIPs)
	api.mux.HandleFunc("/api/collected-ips/clear", api.clearCollectedIPs)
}

// @Summary List collected IP addresses
// @Description Returns the current shared collected pool, sorted by collection time, latest first. Addresses are runtime candidates, not bans. since and until are wall-clock milliseconds: until is when the address leaves the pool unless an answer removes it earlier.
// @Tags CollectedIPs
// @Produce json
// @Success 200 {object} CollectedIPsResponse
// @Failure 405 {string} string "Method not allowed"
// @Security BearerAuth
// @Router /collected-ips [get]
func (api *API) getCollectedIPs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	items := globalPool.GetCollectedIPs()
	setJsonHeader(w)
	_ = json.NewEncoder(w).Encode(CollectedIPsResponse{Items: items})
}

// @Summary Remove collected IP addresses
// @Description Removes one IP or clears the pool when ip is empty or the body is missing. Static targets remain. New detections can collect removed addresses again. A failed update retains the entry and returns an error.
// @Tags CollectedIPs
// @Accept json
// @Produce json
// @Param body body CollectedIPsClearRequest false "IP address to remove; empty clears all collected addresses"
// @Success 200 {object} CollectedIPsClearResponse
// @Failure 400 {object} APIError "invalid_json or bad_request"
// @Failure 500 {object} APIError "internal"
// @Failure 405 {string} string "Method not allowed"
// @Security BearerAuth
// @Router /collected-ips/clear [post]
func (api *API) clearCollectedIPs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body map[string]json.RawMessage
	r.Body = http.MaxBytesReader(w, r.Body, ipBansBodyLimit)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&body); errors.Is(err, io.EOF) {
		body = map[string]json.RawMessage{}
	} else if err != nil || body == nil {
		writeAPIError(w, ErrInvalidJSON())
		return
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		writeAPIError(w, ErrInvalidJSON())
		return
	}
	var req CollectedIPsClearRequest
	for key, value := range body {
		if key != "ip" || string(value) == "null" || json.Unmarshal(value, &req.IP) != nil {
			writeAPIError(w, ErrInvalidJSON())
			return
		}
	}
	ip := strings.TrimSpace(req.IP)
	if ip != "" {
		address, err := netip.ParseAddr(ip)
		if err != nil || address.Zone() != "" {
			writeAPIError(w, ErrBadRequest("ip must be an IPv4 or IPv6 address"))
			return
		}
		ip = address.Unmap().String()
	}
	cleared, err := globalPool.ClearCollectedIPs(ip)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	setJsonHeader(w)
	_ = json.NewEncoder(w).Encode(CollectedIPsClearResponse{Success: true, Cleared: cleared})
}
