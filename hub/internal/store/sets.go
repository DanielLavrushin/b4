package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
)

type Set struct {
	ID                 string
	AuthorHMAC         string
	CurrentVersion     int
	DerivedFromID      string
	DerivedFromVersion int
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type Version struct {
	RowID           int64
	SetID           string
	Version         int
	FP              string
	TargetsKey      string
	Title           string
	Description     string
	Projection      map[string]interface{}
	Payloads        []hubwire.BlobRef
	Flags           []string
	Geo             *hubwire.GeoSource
	B4Min           string
	B4Version       string
	Engine          string
	Family          string
	Status          string
	StatusReason    string
	RecordID        string
	UploaderHMAC    string
	ASNObserved     string
	CountryObserved string
	ASNHint         string
	CountryHint     string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

const versionColumns = `id, set_id, version, fp, targets_key, title, description, projection_json, payloads_json, flags_json, geo_json,
	b4_min, b4_version, engine, family, status, status_reason, record_id, uploader_hmac, asn_observed, country_observed, asn_hint, country_hint, created_at, updated_at`

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanVersion(row rowScanner) (*Version, error) {
	var v Version
	var projection, payloads, flags, geo, createdAt, updatedAt string
	err := row.Scan(&v.RowID, &v.SetID, &v.Version, &v.FP, &v.TargetsKey, &v.Title, &v.Description, &projection, &payloads, &flags, &geo,
		&v.B4Min, &v.B4Version, &v.Engine, &v.Family, &v.Status, &v.StatusReason, &v.RecordID, &v.UploaderHMAC, &v.ASNObserved, &v.CountryObserved, &v.ASNHint, &v.CountryHint, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(projection), &v.Projection); err != nil {
		return nil, fmt.Errorf("set %s version %d projection: %w", v.SetID, v.Version, err)
	}
	if payloads != "" {
		if err := json.Unmarshal([]byte(payloads), &v.Payloads); err != nil {
			return nil, fmt.Errorf("set %s version %d payloads: %w", v.SetID, v.Version, err)
		}
	}
	if flags != "" {
		if err := json.Unmarshal([]byte(flags), &v.Flags); err != nil {
			return nil, fmt.Errorf("set %s version %d flags: %w", v.SetID, v.Version, err)
		}
	}
	if geo != "" {
		var g hubwire.GeoSource
		if err := json.Unmarshal([]byte(geo), &g); err != nil {
			return nil, fmt.Errorf("set %s version %d geo: %w", v.SetID, v.Version, err)
		}
		v.Geo = &g
	}
	v.CreatedAt = parseTime(createdAt)
	v.UpdatedAt = parseTime(updatedAt)
	return &v, nil
}

func encodeVersion(v *Version) (projection, payloads, flags, geo string, err error) {
	raw, err := json.Marshal(v.Projection)
	if err != nil {
		return "", "", "", "", err
	}
	projection = string(raw)
	if v.Payloads == nil {
		v.Payloads = []hubwire.BlobRef{}
	}
	raw, err = json.Marshal(v.Payloads)
	if err != nil {
		return "", "", "", "", err
	}
	payloads = string(raw)
	if v.Flags == nil {
		v.Flags = []string{}
	}
	raw, err = json.Marshal(v.Flags)
	if err != nil {
		return "", "", "", "", err
	}
	flags = string(raw)
	if v.Geo != nil {
		raw, err = json.Marshal(v.Geo)
		if err != nil {
			return "", "", "", "", err
		}
		geo = string(raw)
	}
	return projection, payloads, flags, geo, nil
}

func insertVersionTx(ctx context.Context, tx *sql.Tx, v *Version) error {
	projection, payloads, flags, geo, err := encodeVersion(v)
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO set_versions(set_id, version, fp, targets_key, title, description, projection_json, payloads_json, flags_json, geo_json,
		b4_min, b4_version, engine, family, status, status_reason, record_id, uploader_hmac, asn_observed, country_observed, asn_hint, country_hint, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		v.SetID, v.Version, v.FP, v.TargetsKey, v.Title, v.Description, projection, payloads, flags, geo,
		v.B4Min, v.B4Version, v.Engine, v.Family, v.Status, v.StatusReason, v.RecordID, v.UploaderHMAC, v.ASNObserved, v.CountryObserved, v.ASNHint, v.CountryHint, formatTime(v.CreatedAt), formatTime(v.UpdatedAt))
	if err != nil {
		return err
	}
	v.RowID, _ = res.LastInsertId()
	return nil
}

func (s *Store) CreateSet(ctx context.Context, set Set, v *Version) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO sets(id, author_hmac, current_version, derived_from_id, derived_from_version, created_at, updated_at) VALUES(?, ?, 0, ?, ?, ?, ?)`,
		set.ID, set.AuthorHMAC, set.DerivedFromID, set.DerivedFromVersion, formatTime(set.CreatedAt), formatTime(set.UpdatedAt)); err != nil {
		return err
	}
	v.SetID = set.ID
	v.Version = 1
	if err := insertVersionTx(ctx, tx, v); err != nil {
		return err
	}
	if err := setMetaTx(ctx, tx, metaDirty, "1"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) AddVersion(ctx context.Context, v *Version) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var highest int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM set_versions WHERE set_id = ?`, v.SetID).Scan(&highest); err != nil {
		return err
	}
	if highest == 0 {
		return ErrNotFound
	}
	v.Version = highest + 1
	if err := insertVersionTx(ctx, tx, v); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sets SET updated_at = ? WHERE id = ?`, formatTime(v.CreatedAt), v.SetID); err != nil {
		return err
	}
	if err := setMetaTx(ctx, tx, metaDirty, "1"); err != nil {
		return err
	}
	return tx.Commit()
}

func scanSet(row rowScanner) (*Set, error) {
	var set Set
	var createdAt, updatedAt string
	if err := row.Scan(&set.ID, &set.AuthorHMAC, &set.CurrentVersion, &set.DerivedFromID, &set.DerivedFromVersion, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	set.CreatedAt = parseTime(createdAt)
	set.UpdatedAt = parseTime(updatedAt)
	return &set, nil
}

const setColumns = `id, author_hmac, current_version, derived_from_id, derived_from_version, created_at, updated_at`

func (s *Store) GetSet(ctx context.Context, id string) (*Set, []Version, error) {
	set, err := scanSet(s.db.QueryRowContext(ctx, `SELECT `+setColumns+` FROM sets WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	versions, err := s.queryVersions(ctx, `SELECT `+versionColumns+` FROM set_versions WHERE set_id = ? ORDER BY version`, id)
	if err != nil {
		return nil, nil, err
	}
	return set, versions, nil
}

func (s *Store) GetVersion(ctx context.Context, setID string, version int) (*Version, error) {
	v, err := scanVersion(s.db.QueryRowContext(ctx, `SELECT `+versionColumns+` FROM set_versions WHERE set_id = ? AND version = ?`, setID, version))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return v, err
}

func (s *Store) LatestVersion(ctx context.Context, setID string) (*Version, error) {
	v, err := scanVersion(s.db.QueryRowContext(ctx, `SELECT `+versionColumns+` FROM set_versions WHERE set_id = ? ORDER BY version DESC LIMIT 1`, setID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return v, err
}

func (s *Store) FindDuplicate(ctx context.Context, fp, targetsKey string) (*Version, error) {
	v, err := scanVersion(s.db.QueryRowContext(ctx, `SELECT `+versionColumns+` FROM set_versions WHERE fp = ? AND targets_key = ? AND status <> ? ORDER BY
		CASE status WHEN ? THEN 0 WHEN ? THEN 1 ELSE 2 END, created_at LIMIT 1`, fp, targetsKey, hubwire.SetStatusRejected, hubwire.SetStatusActive, hubwire.SetStatusPending))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return v, err
}

func (s *Store) queryVersions(ctx context.Context, query string, args ...interface{}) ([]Version, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Version, 0)
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (s *Store) PendingVersions(ctx context.Context) ([]Version, error) {
	return s.queryVersions(ctx, `SELECT `+versionColumns+` FROM set_versions WHERE status = ? ORDER BY created_at`, hubwire.SetStatusPending)
}

func (s *Store) VersionsByStatus(ctx context.Context, status string) ([]Version, error) {
	return s.queryVersions(ctx, `SELECT `+versionColumns+` FROM set_versions WHERE status = ? ORDER BY updated_at DESC`, status)
}

func (s *Store) ActiveVersions(ctx context.Context) ([]Version, error) {
	return s.queryVersions(ctx, `SELECT `+versionColumns+` FROM set_versions WHERE status = ? ORDER BY set_id, version`, hubwire.SetStatusActive)
}

func (s *Store) ListedVersions(ctx context.Context) ([]Version, error) {
	return s.queryVersions(ctx, `SELECT `+versionColumns+` FROM set_versions v
		WHERE v.status = ? AND v.version = (SELECT MAX(o.version) FROM set_versions o WHERE o.set_id = v.set_id AND o.status = ?)
		ORDER BY v.set_id`, hubwire.SetStatusActive, hubwire.SetStatusActive)
}

func (s *Store) SetsByAuthor(ctx context.Context, authorHMAC string) ([]Set, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+setColumns+` FROM sets WHERE author_hmac = ? ORDER BY created_at`, authorHMAC)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Set, 0)
	for rows.Next() {
		set, err := scanSet(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *set)
	}
	return out, rows.Err()
}

func (s *Store) setStatus(ctx context.Context, setID string, version int, status, reason string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE set_versions SET status = ?, status_reason = ?, updated_at = ? WHERE set_id = ? AND version = ?`,
		status, reason, formatTime(now), setID, version)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if status == hubwire.SetStatusActive {
		if _, err := tx.ExecContext(ctx, `UPDATE sets SET current_version = MAX(current_version, ?), updated_at = ? WHERE id = ?`, version, formatTime(now), setID); err != nil {
			return err
		}
	}
	if err := setMetaTx(ctx, tx, metaDirty, "1"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Approve(ctx context.Context, setID string, version int, now time.Time) error {
	return s.setStatus(ctx, setID, version, hubwire.SetStatusActive, "", now)
}

func (s *Store) Reject(ctx context.Context, setID string, version int, reason string, now time.Time) error {
	return s.setStatus(ctx, setID, version, hubwire.SetStatusRejected, reason, now)
}

func (s *Store) Hide(ctx context.Context, setID string, version int, reason string, now time.Time) error {
	return s.setStatus(ctx, setID, version, hubwire.SetStatusHidden, reason, now)
}

func (s *Store) ReferencedCategories(ctx context.Context) ([]string, error) {
	versions, err := s.ListedVersions(ctx)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for i := range versions {
		for _, category := range TargetList(versions[i].Projection, "geosite_categories") {
			if _, ok := seen[category]; ok {
				continue
			}
			seen[category] = struct{}{}
			out = append(out, category)
		}
	}
	return out, nil
}

func TargetList(projection map[string]interface{}, key string) []string {
	targets, ok := projection["targets"].(map[string]interface{})
	if !ok {
		return nil
	}
	items, ok := targets[key].([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		if s, ok := it.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func (s *Store) Sets(ctx context.Context) (map[string]Set, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+setColumns+` FROM sets`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]Set)
	for rows.Next() {
		set, err := scanSet(rows)
		if err != nil {
			return nil, err
		}
		out[set.ID] = *set
	}
	return out, rows.Err()
}
