package store

import (
	"context"
	"errors"
	"strings"
	"time"
)

const (
	ScopeReject        = "reject"
	ScopeHide          = "hide"
	ScopeWithdraw      = "withdraw"
	ScopeBan           = "ban"
	ScopeMirrorReject  = "mirror_reject"
	ScopeReportDismiss = "report_dismiss"
)

var ReasonScopes = []string{ScopeReject, ScopeHide, ScopeWithdraw, ScopeBan, ScopeMirrorReject, ScopeReportDismiss}

var ErrPresetExists = errors.New("a preset with this text exists in the scope")

func ValidScope(scope string) bool {
	for _, s := range ReasonScopes {
		if s == scope {
			return true
		}
	}
	return false
}

type ReasonPreset struct {
	ID        int64
	Scope     string
	Label     string
	Text      string
	Position  int
	Uses      int
	LastUsed  time.Time
	CreatedAt time.Time
}

func (s *Store) ReasonPresets(ctx context.Context) ([]ReasonPreset, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, scope, label, text, position, uses, last_used, created_at FROM reason_presets ORDER BY scope, position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ReasonPreset, 0)
	for rows.Next() {
		var p ReasonPreset
		var lastUsed, createdAt string
		if err := rows.Scan(&p.ID, &p.Scope, &p.Label, &p.Text, &p.Position, &p.Uses, &lastUsed, &createdAt); err != nil {
			return nil, err
		}
		p.LastUsed = parseTime(lastUsed)
		p.CreatedAt = parseTime(createdAt)
		out = append(out, p)
	}
	return out, rows.Err()
}

func uniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE")
}

func (s *Store) CreateReasonPreset(ctx context.Context, p ReasonPreset, now time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO reason_presets(scope, label, text, position, created_at)
		VALUES(?, ?, ?, COALESCE((SELECT MAX(position) + 1 FROM reason_presets WHERE scope = ?), 0), ?)`,
		p.Scope, p.Label, p.Text, p.Scope, formatTime(now))
	if uniqueViolation(err) {
		return 0, ErrPresetExists
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateReasonPreset(ctx context.Context, p ReasonPreset) error {
	res, err := s.db.ExecContext(ctx, `UPDATE reason_presets SET label = ?, text = ?, position = ? WHERE id = ?`, p.Label, p.Text, p.Position, p.ID)
	if uniqueViolation(err) {
		return ErrPresetExists
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteReasonPreset(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM reason_presets WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func touchReasonTx(ctx context.Context, q querier, scope, text string, now time.Time) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	_, err := q.ExecContext(ctx, `UPDATE reason_presets SET uses = uses + 1, last_used = ? WHERE scope = ? AND text = ?`, formatTime(now), scope, text)
	return err
}

func (t *Tx) TouchReason(ctx context.Context, scope, text string, now time.Time) error {
	return touchReasonTx(ctx, t.tx, scope, text, now)
}

func (s *Store) TouchReason(ctx context.Context, scope, text string, now time.Time) error {
	return touchReasonTx(ctx, s.db, scope, text, now)
}
