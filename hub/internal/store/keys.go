package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Key struct {
	KeyHMAC   string
	FirstSeen time.Time
	Banned    bool
	BanReason string
	BannedAt  time.Time
}

func (s *Store) GetKey(ctx context.Context, keyHMAC string) (*Key, error) {
	var k Key
	var banned int
	var firstSeen, bannedAt string
	err := s.db.QueryRowContext(ctx, `SELECT key_hmac, first_seen, banned, ban_reason, banned_at FROM keys WHERE key_hmac = ?`, keyHMAC).
		Scan(&k.KeyHMAC, &firstSeen, &banned, &k.BanReason, &bannedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	k.FirstSeen = parseTime(firstSeen)
	k.Banned = banned != 0
	k.BannedAt = parseTime(bannedAt)
	return &k, nil
}

func (s *Store) TouchKey(ctx context.Context, keyHMAC string, now time.Time) (*Key, bool, error) {
	existing, err := s.GetKey(ctx, keyHMAC)
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, false, err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO keys(key_hmac, first_seen) VALUES(?, ?)`, keyHMAC, formatTime(now)); err != nil {
		return nil, false, err
	}
	return &Key{KeyHMAC: keyHMAC, FirstSeen: now.UTC()}, true, nil
}

func (s *Store) BanKey(ctx context.Context, keyHMAC, reason string, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE keys SET banned = 1, ban_reason = ?, banned_at = ? WHERE key_hmac = ?`, reason, formatTime(now), keyHMAC)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_, err = s.db.ExecContext(ctx, `INSERT INTO keys(key_hmac, first_seen, banned, ban_reason, banned_at) VALUES(?, ?, 1, ?, ?)`, keyHMAC, formatTime(now), reason, formatTime(now))
		if err != nil {
			return err
		}
	}
	return s.MarkDirty(ctx)
}

func (s *Store) UnbanKey(ctx context.Context, keyHMAC string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE keys SET banned = 0, ban_reason = '', banned_at = '' WHERE key_hmac = ?`, keyHMAC)
	if err != nil {
		return err
	}
	return s.MarkDirty(ctx)
}

func (s *Store) BannedKeys(ctx context.Context) ([]Key, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key_hmac, first_seen, ban_reason, banned_at FROM keys WHERE banned = 1 ORDER BY banned_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Key
	for rows.Next() {
		var k Key
		var firstSeen, bannedAt string
		if err := rows.Scan(&k.KeyHMAC, &firstSeen, &k.BanReason, &bannedAt); err != nil {
			return nil, err
		}
		k.Banned = true
		k.FirstSeen = parseTime(firstSeen)
		k.BannedAt = parseTime(bannedAt)
		out = append(out, k)
	}
	return out, rows.Err()
}

type Record struct {
	ID         string
	Kind       string
	KeyHMAC    string
	SetID      string
	Version    int
	ReceivedAt time.Time
}

func (s *Store) GetRecord(ctx context.Context, id string) (*Record, error) {
	var r Record
	var receivedAt string
	err := s.db.QueryRowContext(ctx, `SELECT id, kind, key_hmac, set_id, version, received_at FROM records WHERE id = ?`, id).
		Scan(&r.ID, &r.Kind, &r.KeyHMAC, &r.SetID, &r.Version, &receivedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	r.ReceivedAt = parseTime(receivedAt)
	return &r, nil
}

func (s *Store) InsertRecord(ctx context.Context, r Record) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO records(id, kind, key_hmac, set_id, version, received_at) VALUES(?, ?, ?, ?, ?, ?)`,
		r.ID, r.Kind, r.KeyHMAC, r.SetID, r.Version, formatTime(r.ReceivedAt))
	return err
}

func (s *Store) UpsertASNName(ctx context.Context, asn, name, country string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO asn_names(asn, name, country, updated_at) VALUES(?, ?, ?, ?)
		ON CONFLICT(asn) DO UPDATE SET name = excluded.name, country = excluded.country, updated_at = excluded.updated_at`,
		asn, name, country, formatTime(now))
	return err
}

func (s *Store) ASNNames(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT asn, name FROM asn_names`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var asn, name string
		if err := rows.Scan(&asn, &name); err != nil {
			return nil, err
		}
		out[asn] = name
	}
	return out, rows.Err()
}

type KeySummary struct {
	Key
	Sets    int
	Votes   int
	Reports int
}

func (s *Store) Keys(ctx context.Context) ([]KeySummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT k.key_hmac, k.first_seen, k.banned, k.ban_reason, k.banned_at,
		(SELECT COUNT(*) FROM sets WHERE author_hmac = k.key_hmac),
		(SELECT COUNT(*) FROM votes WHERE key_hmac = k.key_hmac),
		(SELECT COUNT(*) FROM reports WHERE key_hmac = k.key_hmac)
		FROM keys k ORDER BY k.first_seen DESC, k.key_hmac`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]KeySummary, 0)
	for rows.Next() {
		var k KeySummary
		var banned int
		var firstSeen, bannedAt string
		if err := rows.Scan(&k.KeyHMAC, &firstSeen, &banned, &k.BanReason, &bannedAt, &k.Sets, &k.Votes, &k.Reports); err != nil {
			return nil, err
		}
		k.Banned = banned != 0
		k.FirstSeen = parseTime(firstSeen)
		k.BannedAt = parseTime(bannedAt)
		out = append(out, k)
	}
	return out, rows.Err()
}
