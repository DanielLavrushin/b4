package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/google/uuid"
)

type setGroupDocument struct {
	Group string            `json:"group"`
	Sets  []json.RawMessage `json:"sets"`
}

type ImportSetsResult struct {
	Group string              `json:"group,omitempty"`
	Sets  []*config.SetConfig `json:"sets"`
}

// @Summary Put sets into a group, or take them out of one
// @Description Sets every listed set's group to "group" (an empty group takes them out). The group's sets then stand together in the list: the block keeps the place of the group's first set from before the call, or, for a new group, of the first listed set. A group that a listed set left stays together too: a set taken out of its middle moves to just after it.
// @Tags Sets
// @Accept json
// @Produce json
// @Param body body object true "ids and group"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} APIError
// @Security BearerAuth
// @Router /sets/batch-set-group [post]
func (api *API) handleBatchSetGroup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Ids   []string `json:"ids"`
		Group string   `json:"group"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, ErrInvalidJSON())
		return
	}
	if len(req.Ids) == 0 {
		writeAPIError(w, ErrBadRequest("No set IDs provided"))
		return
	}
	group := config.NormalizeSetGroup(req.Group)
	target := make(map[string]bool, len(req.Ids))
	for _, id := range req.Ids {
		target[id] = true
	}

	updated := 0
	oldCfg, newCfg, err := api.editConfig(func(next *config.Config) error {
		anchor := ""
		for _, set := range next.Sets {
			if set != nil && group != "" && set.Group == group && !target[set.Id] {
				anchor = set.Id
				break
			}
		}
		matched := 0
		updated = 0
		var left []string
		for _, set := range next.Sets {
			if set == nil || !target[set.Id] {
				continue
			}
			matched++
			if anchor == "" {
				anchor = set.Id
			}
			if set.Group != group {
				if set.Group != "" && !slices.Contains(left, set.Group) {
					left = append(left, set.Group)
				}
				set.Group = group
				updated++
			}
		}
		if matched == 0 {
			return ErrNotFound("No matching sets found")
		}
		before := slices.Clone(next.Sets)
		next.Sets = config.GatherSetGroup(next.Sets, group, anchor)
		for _, former := range left {
			next.Sets = config.GatherSetGroup(next.Sets, former, "")
		}
		if updated == 0 && slices.Equal(before, next.Sets) {
			return errConfigUnchanged
		}
		return nil
	})
	switch {
	case errors.Is(err, errConfigUnchanged):
		sendResponse(w, map[string]interface{}{"success": true, "updated": 0})
		return
	case err != nil:
		if !isRefusal(err) {
			log.Errorf("Failed to save config after grouping sets: %v", err)
		}
		writeAPIError(w, err)
		return
	}

	if api.PerformSoftRestart(newCfg, oldCfg) {
		log.Infof("Soft restart completed successfully")
	}
	log.Infof("Set group %q on %d sets", group, updated)
	sendResponse(w, map[string]interface{}{"success": true, "updated": updated})
}

// @Summary Import a group of sets
// @Description Creates every set of the document in one write, at the top of the list and in the document's order, all in one group. A field a set leaves out takes b4's set default, as in POST /sets. A set name or group name already in use gets a number appended.
// @Tags Sets
// @Accept json
// @Produce json
// @Param body body setGroupDocument true "The group, as the web interface exports it"
// @Success 201 {object} ImportSetsResult
// @Failure 400 {object} APIError
// @Security BearerAuth
// @Router /sets/import [post]
func (api *API) handleImportSets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var doc setGroupDocument
	if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
		writeAPIError(w, ErrInvalidJSON())
		return
	}
	if len(doc.Sets) == 0 {
		writeAPIError(w, ErrBadRequest("The document holds no sets"))
		return
	}

	created := make([]*config.SetConfig, 0, len(doc.Sets))
	for i, raw := range doc.Sets {
		set := config.NewSetConfig()
		if err := json.Unmarshal(raw, &set); err != nil {
			writeAPIError(w, ErrBadRequest(fmt.Sprintf("Set %d is not a valid set: %v", i+1, err)))
			return
		}
		if strings.TrimSpace(set.Name) == "" {
			writeAPIError(w, ErrBadRequest(fmt.Sprintf("Set %d has no name", i+1)))
			return
		}
		set.Id = uuid.New().String()
		api.initializeSetDefaults(&set)
		api.loadTargetsForSetCached(&set)
		created = append(created, &set)
	}

	group := config.NormalizeSetGroup(doc.Group)
	oldCfg, newCfg, err := api.editConfig(func(next *config.Config) error {
		if group != "" {
			group = config.UniqueSetGroup(group, next.Sets)
		}
		for _, set := range created {
			set.Group = group
		}
		uniqueSetNames(created, next.Sets)
		next.Sets = append(slices.Clone(created), next.Sets...)
		return nil
	})
	if err != nil {
		if !isRefusal(err) {
			log.Errorf("Failed to save config after importing sets: %v", err)
		}
		writeAPIError(w, err)
		return
	}

	if api.PerformSoftRestart(newCfg, oldCfg) {
		log.Infof("Soft restart completed successfully")
	}
	log.Infof("Imported %d sets into group %q", len(created), group)
	setJsonHeader(w)
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(ImportSetsResult{Group: group, Sets: created})
}
