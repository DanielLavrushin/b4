package web

import (
	"net/http"
	"time"
)

type BadgesView struct {
	Pending        int            `json:"pending"`
	OldestPending  *time.Time     `json:"oldest_pending,omitempty"`
	ReportsOpen    int            `json:"reports_open"`
	MirrorsPending int            `json:"mirrors_pending"`
	Attention      int            `json:"attention"`
	Build          BuildStateView `json:"build"`
}

func (s *Server) counts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	b, err := s.Store.Badges(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	view := BadgesView{
		Pending:        b.Pending,
		OldestPending:  optionalTime(b.OldestPending),
		ReportsOpen:    b.ReportsOpen,
		MirrorsPending: b.MirrorsPending,
		Build:          *s.buildState(ctx),
	}
	view.Attention = s.attentionCount(ctx)
	writeJSON(w, http.StatusOK, view)
}
