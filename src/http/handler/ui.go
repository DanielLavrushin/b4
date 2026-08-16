// src/http/handler/ui.go
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

func (api *API) RegisterUIApi() {
	api.mux.HandleFunc("/api/ui/dashboard", api.handleDashboardLayout)
}

// @Summary Get the dashboard layout
// @Description Panel order, hidden panels and column widths for the dashboard.
// @Tags UI
// @Produce json
// @Success 200 {object} config.DashboardLayout
// @Security BearerAuth
// @Router /ui/dashboard [get]
func (a *API) handleDashboardLayout(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		setJsonHeader(w)
		_ = json.NewEncoder(w).Encode(a.getCfg().UI.Dashboard)

	case http.MethodPut:
		var layout config.DashboardLayout
		if err := json.NewDecoder(r.Body).Decode(&layout); err != nil {
			writeAPIError(w, ErrValidation("Invalid dashboard layout"))
			return
		}

		newCfg := a.getCfg().Clone()
		newCfg.UI.Dashboard = layout.Sanitized()

		if err := newCfg.SaveToFile(newCfg.ConfigPath); err != nil {
			log.Errorf("Failed to save dashboard layout: %v", err)
			writeAPIError(w, ErrInternal("Failed to save dashboard layout"))
			return
		}
		a.cfgPtr.Store(newCfg)

		setJsonHeader(w)
		_ = json.NewEncoder(w).Encode(newCfg.UI.Dashboard)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
