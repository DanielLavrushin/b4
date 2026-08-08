package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/convert"
	"github.com/daniellavrushin/b4/log"
	"github.com/google/uuid"
)

func (api *API) RegisterConvertApi() {
	api.mux.HandleFunc("/api/convert/tools", api.handleConvertTools)
	api.mux.HandleFunc("/api/convert/analyze", api.handleConvertAnalyze)
	api.mux.HandleFunc("/api/convert/apply", api.handleConvertApply)
}

type convertRequest struct {
	Text           string              `json:"text"`
	Tool           string              `json:"tool"`
	Version        string              `json:"version"`
	NamePrefix     string              `json:"name_prefix"`
	Domains        []string            `json:"domains"`
	ProfileDomains map[string][]string `json:"profile_domains"`
}

func (r convertRequest) options() convert.Options {
	var perProfile map[int][]string
	for key, domains := range r.ProfileDomains {
		idx, err := strconv.Atoi(key)
		if err != nil || idx < 0 {
			continue
		}
		if perProfile == nil {
			perProfile = map[int][]string{}
		}
		perProfile[idx] = normalizeDomains(domains)
	}
	return convert.Options{
		Tool:           r.Tool,
		Version:        r.Version,
		NamePrefix:     r.NamePrefix,
		Domains:        normalizeDomains(r.Domains),
		ProfileDomains: perProfile,
	}
}

func normalizeDomains(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, d := range in {
		d = normalizeDomain(d)
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}

// @Summary List supported source tools for conversion
// @Tags Convert
// @Produce json
// @Success 200 {array} convert.ToolInfo
// @Security BearerAuth
// @Router /convert/tools [get]
func (api *API) handleConvertTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	tools, err := convert.Tools()
	if err != nil {
		writeAPIError(w, ErrInternal("Failed to load converter rules"))
		return
	}
	sendResponse(w, tools)
}

// @Summary Analyze a byedpi or zapret command line without applying it
// @Tags Convert
// @Accept json
// @Produce json
// @Param body body convertRequest true "Command line to analyze"
// @Success 200 {object} convert.Result
// @Security BearerAuth
// @Router /convert/analyze [post]
func (api *API) handleConvertAnalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req convertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, ErrInvalidJSON())
		return
	}
	res, err := api.runConvert(req)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	sendResponse(w, res)
}

// @Summary Convert a byedpi or zapret command line and create the resulting sets
// @Tags Convert
// @Accept json
// @Produce json
// @Param body body convertRequest true "Command line to convert"
// @Success 201 {object} convert.Result
// @Security BearerAuth
// @Router /convert/apply [post]
func (api *API) handleConvertApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req convertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, ErrInvalidJSON())
		return
	}
	res, err := api.runConvert(req)
	if err != nil {
		writeAPIError(w, err)
		return
	}

	created := assignSetIDs(res.Sets)
	oldCfg := api.getCfg()
	newCfg := oldCfg.Clone()

	var domains []string
	for _, set := range created {
		config.ApplySetDefaults(set)
		api.initializeSetDefaults(set)
		api.loadTargetsForSetCached(set)
		domains = append(domains, set.Targets.SNIDomains...)
	}

	if len(domains) > 0 {
		api.releaseDomainsFromOtherSets(newCfg.Sets, "", domains)
	}
	newCfg.Sets = append(created, newCfg.Sets...)

	if err := api.saveAndPushConfig(newCfg); err != nil {
		log.Errorf("Failed to save config after converting %s command line: %v", res.Tool, err)
		writeAPIError(w, err)
		return
	}
	if api.PerformSoftRestart(newCfg, oldCfg) {
		log.Infof("Soft restart completed successfully")
	}

	log.Infof("Converted %s configuration into %d set(s)", res.Tool, len(created))
	res.Sets = derefSets(created)
	setJsonHeader(w)
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(res)
}

func (api *API) runConvert(req convertRequest) (*convert.Result, error) {
	res, err := convert.Analyze(req.Text, req.options())
	if err != nil {
		switch {
		case errors.Is(err, convert.ErrNothingToParse):
			return nil, ErrBadRequest("No recognizable options were found in the supplied text")
		case errors.Is(err, convert.ErrUnsupportedTool):
			return nil, ErrBadRequest("These options belong to a tool b4 cannot convert yet. Only byedpi command lines are supported so far")
		}
		return nil, ErrBadRequest(err.Error())
	}
	return res, nil
}

func assignSetIDs(sets []config.SetConfig) []*config.SetConfig {
	ids := make(map[string]string, len(sets))
	out := make([]*config.SetConfig, 0, len(sets))
	for i := range sets {
		set := sets[i]
		ids[set.Id] = uuid.New().String()
		set.Id = ids[set.Id]
		out = append(out, &set)
	}
	for _, set := range out {
		if set.Escalate.To == "" {
			continue
		}
		if id, ok := ids[set.Escalate.To]; ok {
			set.Escalate.To = id
			continue
		}
		set.Escalate.To = ""
	}
	return out
}

func derefSets(sets []*config.SetConfig) []config.SetConfig {
	out := make([]config.SetConfig, 0, len(sets))
	for _, s := range sets {
		out = append(out, *s)
	}
	return out
}
