package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type BuildSetRef struct {
	SetID   string `json:"set_id"`
	Version int    `json:"version"`
	Title   string `json:"title"`
}

type BuildVersionChange struct {
	SetID string `json:"set_id"`
	Title string `json:"title"`
	From  int    `json:"from"`
	To    int    `json:"to"`
}

type BuildChanges struct {
	Added          []BuildSetRef        `json:"added,omitempty"`
	Removed        []BuildSetRef        `json:"removed,omitempty"`
	Updated        []BuildVersionChange `json:"updated,omitempty"`
	Edited         []BuildSetRef        `json:"edited,omitempty"`
	Rescored       int                  `json:"rescored,omitempty"`
	MirrorsAdded   []string             `json:"mirrors_added,omitempty"`
	MirrorsRemoved []string             `json:"mirrors_removed,omitempty"`
	RevokedAdded   []string             `json:"revoked_added,omitempty"`
}

func (c BuildChanges) ContentChanged() bool {
	return len(c.Added) > 0 || len(c.Removed) > 0 || len(c.Updated) > 0 || len(c.Edited) > 0 ||
		len(c.MirrorsAdded) > 0 || len(c.MirrorsRemoved) > 0 || len(c.RevokedAdded) > 0
}

type BuildRun struct {
	ID         int64
	Trigger    string
	StartedAt  time.Time
	FinishedAt time.Time
	OK         bool
	Error      string
	Epoch      int64
	Seq        int64
	File       string
	Size       int64
	Sets       int
	Blobs      int
	Mirrors    int
	DurationMs int64
	Changes    BuildChanges
}

func (s *Store) StartBuild(ctx context.Context, trigger string, at time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO builds(trigger, started_at) VALUES(?, ?)`, trigger, formatTime(at))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) FinishBuild(ctx context.Context, b BuildRun) error {
	changes, err := json.Marshal(b.Changes)
	if err != nil {
		return err
	}
	ok, contentChanged := 0, 0
	if b.OK {
		ok = 1
	}
	if b.Changes.ContentChanged() {
		contentChanged = 1
	}
	_, err = s.db.ExecContext(ctx, `UPDATE builds SET finished_at = ?, ok = ?, error = ?, epoch = ?, seq = ?, file = ?, size = ?, sets = ?, blobs = ?, mirrors = ?,
		duration_ms = ?, content_changed = ?, changes_json = ? WHERE id = ?`,
		formatTime(b.FinishedAt), ok, b.Error, b.Epoch, b.Seq, b.File, b.Size, b.Sets, b.Blobs, b.Mirrors, b.DurationMs, contentChanged, string(changes), b.ID)
	return err
}

const buildColumns = `id, trigger, started_at, finished_at, ok, error, epoch, seq, file, size, sets, blobs, mirrors, duration_ms, changes_json`

func scanBuild(row rowScanner) (*BuildRun, error) {
	var b BuildRun
	var startedAt, finishedAt, changes string
	var ok int
	if err := row.Scan(&b.ID, &b.Trigger, &startedAt, &finishedAt, &ok, &b.Error, &b.Epoch, &b.Seq, &b.File, &b.Size, &b.Sets, &b.Blobs, &b.Mirrors, &b.DurationMs, &changes); err != nil {
		return nil, err
	}
	b.StartedAt = parseTime(startedAt)
	b.FinishedAt = parseTime(finishedAt)
	b.OK = ok != 0
	if changes != "" {
		_ = json.Unmarshal([]byte(changes), &b.Changes)
	}
	return &b, nil
}

type BuildQuery struct {
	Before      int64
	Limit       int
	OnlyChanges bool
	OnlyFailed  bool
}

func (s *Store) BuildRuns(ctx context.Context, f BuildQuery) ([]BuildRun, int64, error) {
	where := []string{"finished_at <> ''"}
	args := []interface{}{}
	if f.Before > 0 {
		where = append(where, "id < ?")
		args = append(args, f.Before)
	}
	if f.OnlyChanges {
		where = append(where, "(content_changed = 1 OR ok = 0)")
	}
	if f.OnlyFailed {
		where = append(where, "ok = 0")
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, `SELECT `+buildColumns+` FROM builds WHERE `+strings.Join(where, " AND ")+` ORDER BY id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]BuildRun, 0)
	for rows.Next() {
		b, err := scanBuild(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *b)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var next int64
	if len(out) > limit {
		out = out[:limit]
		next = out[len(out)-1].ID
	}
	return out, next, nil
}

func (s *Store) LastBuild(ctx context.Context, ok bool) (*BuildRun, error) {
	flag := 0
	if ok {
		flag = 1
	}
	b, err := scanBuild(s.db.QueryRowContext(ctx, `SELECT `+buildColumns+` FROM builds WHERE finished_at <> '' AND ok = ? ORDER BY id DESC LIMIT 1`, flag))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return b, err
}

func (s *Store) PruneBuilds(ctx context.Context, now time.Time, unchangedAge, maxAge time.Duration) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM builds WHERE ok = 1 AND content_changed = 0 AND started_at < ?`, formatTime(now.Add(-unchangedAge))); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM builds WHERE started_at < ?`, formatTime(now.Add(-maxAge)))
	return err
}
