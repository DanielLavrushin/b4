package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

const (
	MirrorPending  = "pending"
	MirrorApproved = "approved"
	MirrorRejected = "rejected"

	metaRevokedKeys = "revoked_keys"
)

type Mirror struct {
	ID        int64
	URL       string
	KeyHMAC   string
	FirstSeen time.Time
	LastSeen  time.Time
	Status    string
	LastCheck time.Time
	LastOK    time.Time
	Reason    string
	Version   string
}

func (m Mirror) Healthy() bool {
	return !m.LastCheck.IsZero() && m.LastOK.Equal(m.LastCheck)
}

const mirrorColumns = `id, url, key_hmac, first_seen, last_seen, status, last_check, last_ok, reason, version`

func scanMirror(row rowScanner) (*Mirror, error) {
	var m Mirror
	var firstSeen, lastSeen, lastCheck, lastOK string
	if err := row.Scan(&m.ID, &m.URL, &m.KeyHMAC, &firstSeen, &lastSeen, &m.Status, &lastCheck, &lastOK, &m.Reason, &m.Version); err != nil {
		return nil, err
	}
	m.FirstSeen = parseTime(firstSeen)
	m.LastSeen = parseTime(lastSeen)
	m.LastCheck = parseTime(lastCheck)
	m.LastOK = parseTime(lastOK)
	return &m, nil
}

func (s *Store) AnnounceMirror(ctx context.Context, url, keyHMAC, version string, now time.Time) (*Mirror, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO mirrors(url, key_hmac, first_seen, last_seen, status, version) VALUES(?, ?, ?, ?, ?, ?)
		ON CONFLICT(url) DO UPDATE SET last_seen = excluded.last_seen, key_hmac = excluded.key_hmac, version = excluded.version`,
		url, keyHMAC, formatTime(now), formatTime(now), MirrorPending, version)
	if err != nil {
		return nil, err
	}
	return s.MirrorByURL(ctx, url)
}

func (s *Store) MirrorByURL(ctx context.Context, url string) (*Mirror, error) {
	m, err := scanMirror(s.db.QueryRowContext(ctx, `SELECT `+mirrorColumns+` FROM mirrors WHERE url = ?`, url))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return m, err
}

func (s *Store) GetMirror(ctx context.Context, id int64) (*Mirror, error) {
	m, err := scanMirror(s.db.QueryRowContext(ctx, `SELECT `+mirrorColumns+` FROM mirrors WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return m, err
}

func (s *Store) queryMirrors(ctx context.Context, query string, args ...interface{}) ([]Mirror, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Mirror, 0)
	for rows.Next() {
		m, err := scanMirror(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (s *Store) Mirrors(ctx context.Context) ([]Mirror, error) {
	return s.queryMirrors(ctx, `SELECT `+mirrorColumns+` FROM mirrors ORDER BY
		CASE status WHEN 'pending' THEN 0 WHEN 'approved' THEN 1 ELSE 2 END, first_seen, id`)
}

func (s *Store) MirrorsByStatus(ctx context.Context, status string) ([]Mirror, error) {
	return s.queryMirrors(ctx, `SELECT `+mirrorColumns+` FROM mirrors WHERE status = ? ORDER BY first_seen, id`, status)
}

func (s *Store) SetMirrorStatus(ctx context.Context, id int64, status, reason string, now time.Time) error {
	switch status {
	case MirrorPending, MirrorApproved, MirrorRejected:
	default:
		return fmt.Errorf("unknown mirror status %q", status)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE mirrors SET status = ?, reason = ? WHERE id = ?`, status, reason, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return s.MarkDirty(ctx)
}

func (s *Store) DeleteMirror(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM mirrors WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return s.MarkDirty(ctx)
}

func (s *Store) RecordMirrorCheck(ctx context.Context, id int64, checkedAt time.Time, ok bool, reason string) error {
	if ok {
		_, err := s.db.ExecContext(ctx, `UPDATE mirrors SET last_check = ?, last_ok = ?, reason = '' WHERE id = ?`, formatTime(checkedAt), formatTime(checkedAt), id)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE mirrors SET last_check = ?, reason = ? WHERE id = ?`, formatTime(checkedAt), reason, id)
	return err
}

func (s *Store) RevokedKeys(ctx context.Context) ([]string, error) {
	raw, err := s.Meta(ctx, metaRevokedKeys)
	if err != nil || raw == "" {
		return nil, err
	}
	var keys []string
	if err := json.Unmarshal([]byte(raw), &keys); err != nil {
		return nil, fmt.Errorf("meta %s holds %q", metaRevokedKeys, raw)
	}
	return keys, nil
}

func (s *Store) RevokeKey(ctx context.Context, keyID string) error {
	keys, err := s.RevokedKeys(ctx)
	if err != nil {
		return err
	}
	if slices.Contains(keys, keyID) {
		return nil
	}
	keys = append(keys, keyID)
	slices.Sort(keys)
	raw, err := json.Marshal(keys)
	if err != nil {
		return err
	}
	if err := s.SetMeta(ctx, metaRevokedKeys, string(raw)); err != nil {
		return err
	}
	return s.MarkDirty(ctx)
}
