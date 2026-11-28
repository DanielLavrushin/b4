// src/http/handler/ui.go
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const newSetTemplateBodyLimit = 1 << 20

type NewSetTemplateResponse struct {
	NewSetTemplate map[string]interface{} `json:"new_set_template"`
}

func (api *API) RegisterUIApi() {
	api.mux.HandleFunc("/api/ui/dashboard", api.handleDashboardLayout)
	api.mux.HandleFunc("/api/ui/new-set-template", api.handleNewSetTemplate)
}

// @Summary Get or replace the new set template
// @Description The values a set created on the Sets page starts with, as the fields that differ from b4's set defaults. PUT takes a set or part of one, keeps only what differs from the defaults and drops the set's own fields (id, name, enabled, targets, hub link, discovery) and the proxy username and password. An empty object clears the template. Imports, shared sets, Discovery, MCP and the sets API keep b4's set defaults.
// @Tags UI
// @Accept json
// @Produce json
// @Param template body object false "A set or part of one (PUT only)"
// @Success 200 {object} NewSetTemplateResponse
// @Security BearerAuth
// @Router /ui/new-set-template [get]
// @Router /ui/new-set-template [put]
func (a *API) handleNewSetTemplate(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		setJsonHeader(w)
		_ = json.NewEncoder(w).Encode(NewSetTemplateResponse{NewSetTemplate: a.getCfg().UI.NewSetTemplate})

	case http.MethodPut:
		r.Body = http.MaxBytesReader(w, r.Body, newSetTemplateBodyLimit)
		var raw map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			writeAPIError(w, ErrInvalidJSON())
			return
		}
		template, err := config.NormalizeSetTemplate(raw)
		if err != nil {
			writeAPIError(w, err)
			return
		}

		unlock := config.LockWrites()
		newCfg := a.getCfg().Clone()
		newCfg.UI.NewSetTemplate = template

		if err := newCfg.SaveToFile(newCfg.ConfigPath); err != nil {
			unlock()
			log.Errorf("Failed to save the new set template: %v", err)
			writeAPIError(w, ErrInternal("Failed to save the new set template"))
			return
		}
		a.publishConfig(newCfg)
		unlock()

		setJsonHeader(w)
		_ = json.NewEncoder(w).Encode(NewSetTemplateResponse{NewSetTemplate: template})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
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

		unlock := config.LockWrites()
		newCfg := a.getCfg().Clone()
		newCfg.UI.Dashboard = layout.Sanitized()

		if err := newCfg.SaveToFile(newCfg.ConfigPath); err != nil {
			unlock()
			log.Errorf("Failed to save dashboard layout: %v", err)
			writeAPIError(w, ErrInternal("Failed to save dashboard layout"))
			return
		}
		a.publishConfig(newCfg)
		unlock()

		setJsonHeader(w)
		_ = json.NewEncoder(w).Encode(newCfg.UI.Dashboard)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
