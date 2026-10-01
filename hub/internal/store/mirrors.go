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
	ID                int64
	URL               string
	KeyHMAC           string
	FirstSeen         time.Time
	LastSeen          time.Time
	Status            string
	LastCheck         time.Time
	LastOK            time.Time
	Reason            string
	Version           string
	CheckCode         string
	CheckError        string
	CheckMillis       int64
	ServedEpoch       int64
	ServedSeq         int64
	ServedGeneratedAt string
}

type MirrorCheck struct {
	At          time.Time
	OK          bool
	Code        string
	Error       string
	Millis      int64
	Epoch       int64
	Seq         int64
	GeneratedAt string
}

func (m Mirror) Healthy() bool {
	return !m.LastCheck.IsZero() && m.LastOK.Equal(m.LastCheck)
}

const mirrorColumns = `id, url, key_hmac, first_seen, last_seen, status, last_check, last_ok, reason, version,
	check_code, check_error, check_ms, served_epoch, served_seq, served_generated_at`

func scanMirror(row rowScanner) (*Mirror, error) {
	var m Mirror
	var firstSeen, lastSeen, lastCheck, lastOK string
	if err := row.Scan(&m.ID, &m.URL, &m.KeyHMAC, &firstSeen, &lastSeen, &m.Status, &lastCheck, &lastOK, &m.Reason, &m.Version,
		&m.CheckCode, &m.CheckError, &m.CheckMillis, &m.ServedEpoch, &m.ServedSeq, &m.ServedGeneratedAt); err != nil {
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
		ON CONFLICT(url) DO UPDATE SET last_seen = excluded.last_seen, key_hmac = excluded.key_hmac, version = excluded.version
		WHERE mirrors.status = 'pending' OR mirrors.key_hmac = excluded.key_hmac`,
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
	return getMirror(ctx, s.db, id)
}

func getMirror(ctx context.Context, q querier, id int64) (*Mirror, error) {
	m, err := scanMirror(q.QueryRowContext(ctx, `SELECT `+mirrorColumns+` FROM mirrors WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return m, err
}

func (s *Store) queryMirrors(ctx context.Context, query string, args ...interface{}) ([]Mirror, error) {
	return queryMirrors(ctx, s.db, query, args...)
}

func queryMirrors(ctx context.Context, q querier, query string, args ...interface{}) ([]Mirror, error) {
	rows, err := q.QueryContext(ctx, query, args...)
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
	return setMirrorStatusTx(ctx, s.db, id, status, reason)
}

func setMirrorStatusTx(ctx context.Context, q querier, id int64, status, reason string) error {
	switch status {
	case MirrorPending, MirrorApproved, MirrorRejected:
	default:
		return fmt.Errorf("unknown mirror status %q", status)
	}
	res, err := q.ExecContext(ctx, `UPDATE mirrors SET status = ?, reason = ? WHERE id = ?`, status, reason, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return markDirtyTx(ctx, q)
}

func (s *Store) DeleteMirror(ctx context.Context, id int64) error {
	return deleteMirrorTx(ctx, s.db, id)
}

func deleteMirrorTx(ctx context.Context, q querier, id int64) error {
	res, err := q.ExecContext(ctx, `DELETE FROM mirrors WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return markDirtyTx(ctx, q)
}

func (s *Store) RecordMirrorCheck(ctx context.Context, id int64, c MirrorCheck) error {
	if c.OK {
		_, err := s.db.ExecContext(ctx, `UPDATE mirrors SET last_check = ?, last_ok = ?, check_code = '', check_error = '', check_ms = ?,
			served_epoch = ?, served_seq = ?, served_generated_at = ? WHERE id = ?`,
			formatTime(c.At), formatTime(c.At), c.Millis, c.Epoch, c.Seq, c.GeneratedAt, id)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE mirrors SET last_check = ?, check_code = ?, check_error = ?, check_ms = ? WHERE id = ?`,
		formatTime(c.At), c.Code, c.Error, c.Millis, id)
	return err
}

func (s *Store) AnnounceableMirrors(ctx context.Context, now time.Time, window time.Duration) ([]string, error) {
	mirrors, err := s.MirrorsByStatus(ctx, MirrorApproved)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(mirrors))
	for _, m := range mirrors {
		if !m.LastOK.IsZero() && now.Sub(m.LastOK) <= window {
			out = append(out, m.URL)
		}
	}
	return out, nil
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
	_, err := revokeKeyTx(ctx, s.db, keyID)
	return err
}

func revokeKeyTx(ctx context.Context, q querier, keyID string) (bool, error) {
	var raw string
	err := q.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, metaRevokedKeys).Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	var keys []string
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &keys); err != nil {
			return false, fmt.Errorf("meta %s holds %q", metaRevokedKeys, raw)
		}
	}
	if slices.Contains(keys, keyID) {
		return false, nil
	}
	keys = append(keys, keyID)
	slices.Sort(keys)
	encoded, err := json.Marshal(keys)
	if err != nil {
		return false, err
	}
	if _, err := q.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, metaRevokedKeys, string(encoded)); err != nil {
		return false, err
	}
	return true, markDirtyTx(ctx, q)
}
