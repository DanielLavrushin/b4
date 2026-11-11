package handler

import (
	"encoding/json"
	"net/http"

	"github.com/daniellavrushin/b4/warmtuple"
)

func (a *API) handleWarmTupleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	status := warmtuple.Status{}
	if service, ok := globalSocks5Server.(interface{ WarmTupleStatus() warmtuple.Status }); ok {
		status = service.WarmTupleStatus()
	}
	_ = json.NewEncoder(w).Encode(status)
}
