package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/daniellavrushin/b4hub/internal/moderation"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const maxPresetLabel = 60

type ReasonPresetView struct {
	ID        int64      `json:"id"`
	Scope     string     `json:"scope"`
	Label     string     `json:"label,omitempty"`
	Text      string     `json:"text"`
	Position  int        `json:"position"`
	Uses      int        `json:"uses"`
	LastUsed  *time.Time `json:"last_used,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type ReasonPresetRequest struct {
	Scope    string `json:"scope"`
	Label    string `json:"label"`
	Text     string `json:"text"`
	Position int    `json:"position"`
}

type TextEditRequest struct {
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Note        string     `json:"note"`
	Expect      *time.Time `json:"expect,omitempty"`
}

func presetView(p store.ReasonPreset) ReasonPresetView {
	return ReasonPresetView{ID: p.ID, Scope: p.Scope, Label: p.Label, Text: p.Text, Position: p.Position, Uses: p.Uses, LastUsed: optionalTime(p.LastUsed), CreatedAt: p.CreatedAt}
}

func (s *Server) reasons(w http.ResponseWriter, r *http.Request) {
	presets, err := s.Store.ReasonPresets(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]ReasonPresetView, 0, len(presets))
	for _, p := range presets {
		out = append(out, presetView(p))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) presetFromRequest(w http.ResponseWriter, r *http.Request) (store.ReasonPreset, bool) {
	var req ReasonPresetRequest
	if !s.readJSON(w, r, &req) {
		return store.ReasonPreset{}, false
	}
	p := store.ReasonPreset{Scope: strings.TrimSpace(req.Scope), Label: clipRunes(req.Label, maxPresetLabel), Text: cleanReason(req.Text), Position: req.Position}
	if !store.ValidScope(p.Scope) {
		writeError(w, http.StatusBadRequest, "bad_scope", "unknown reason scope")
		return p, false
	}
	if p.Text == "" {
		writeError(w, http.StatusBadRequest, moderation.CodeReasonRequired, "a preset needs text")
		return p, false
	}
	return p, true
}

func (s *Server) createReason(w http.ResponseWriter, r *http.Request) {
	p, ok := s.presetFromRequest(w, r)
	if !ok {
		return
	}
	id, err := s.Store.CreateReasonPreset(r.Context(), p, s.now())
	if errors.Is(err, store.ErrPresetExists) {
		writeError(w, http.StatusConflict, "preset_exists", err.Error())
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ActionResult{Notice: "saved preset", Code: "reason.saved", Params: map[string]interface{}{"id": id}})
}

func (s *Server) updateReason(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, codeNotFound, "the address does not name a preset")
		return
	}
	p, ok := s.presetFromRequest(w, r)
	if !ok {
		return
	}
	p.ID = id
	err = s.Store.UpdateReasonPreset(r.Context(), p)
	if errors.Is(err, store.ErrPresetExists) {
		writeError(w, http.StatusConflict, "preset_exists", err.Error())
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ActionResult{Notice: "saved preset", Code: "reason.saved"})
}

func (s *Server) deleteReason(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, codeNotFound, "the address does not name a preset")
		return
	}
	if err := s.Store.DeleteReasonPreset(r.Context(), id); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ActionResult{Notice: "deleted preset", Code: "reason.deleted"})
}

func (s *Server) setText(w http.ResponseWriter, r *http.Request) {
	id, version, ok := versionPath(r)
	if !ok {
		writeError(w, http.StatusNotFound, codeNotFound, "the address does not name a set version")
		return
	}
	var req TextEditRequest
	if !s.readJSON(w, r, &req) {
		return
	}
	edit := moderation.TextEdit{Title: req.Title, Description: req.Description, Note: req.Note}
	if req.Expect != nil {
		edit.Expect = *req.Expect
	}
	res, err := s.Moderation.EditText(r.Context(), s.actor(r), moderation.Ref{SetID: id, Version: version}, edit)
	if err != nil {
		s.failModeration(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.result(r.Context(), res))
}
