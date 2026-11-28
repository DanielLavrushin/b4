package store

import (
	"context"
	"time"
)

const foldedVersion = `EXISTS (SELECT 1 FROM group_members m JOIN group_versions gv ON gv.group_id = m.group_id AND gv.version = m.version
	WHERE gv.status = 'pending' AND m.set_id = v.set_id AND m.set_version = v.version
	AND EXISTS (SELECT 1 FROM group_members o WHERE o.brought = 1 AND o.group_id = m.group_id AND o.set_id = m.set_id AND o.set_version = m.set_version))`

const broughtVersion = `EXISTS (SELECT 1 FROM group_members m WHERE m.brought = 1 AND m.set_id = v.set_id AND m.set_version = v.version)`

type Badges struct {
	Pending        int
	OldestPending  time.Time
	ReportsOpen    int
	MirrorsPending int
	Keys           int
	Banned         int
}

func (s *Store) Badges(ctx context.Context) (Badges, error) {
	var b Badges
	var oldest string
	err := s.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM set_versions v WHERE v.status = 'pending' AND NOT `+foldedVersion+`)
			+ (SELECT COUNT(*) FROM group_versions WHERE status = 'pending'),
		(SELECT COALESCE(MIN(created_at), '') FROM (SELECT created_at FROM set_versions WHERE status = 'pending'
			UNION ALL SELECT created_at FROM group_versions WHERE status = 'pending')),
		(SELECT COUNT(*) FROM reports WHERE state = 'open'),
		(SELECT COUNT(*) FROM mirrors WHERE status = 'pending'),
		(SELECT COUNT(*) FROM keys),
		(SELECT COALESCE(SUM(banned), 0) FROM keys)`).Scan(&b.Pending, &oldest, &b.ReportsOpen, &b.MirrorsPending, &b.Keys, &b.Banned)
	b.OldestPending = parseTime(oldest)
	return b, err
}

func (s *Store) FoldedVersions(ctx context.Context) (map[string]map[int]bool, error) {
	return s.versionSet(ctx, `SELECT v.set_id, v.version FROM set_versions v WHERE v.status = 'pending' AND `+foldedVersion)
}

func (s *Store) BroughtVersionSet(ctx context.Context) (map[string]map[int]bool, error) {
	return s.versionSet(ctx, `SELECT v.set_id, v.version FROM set_versions v WHERE `+broughtVersion)
}

func (s *Store) versionSet(ctx context.Context, query string) (map[string]map[int]bool, error) {
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]map[int]bool)
	for rows.Next() {
		var setID string
		var version int
		if err := rows.Scan(&setID, &version); err != nil {
			return nil, err
		}
		if out[setID] == nil {
			out[setID] = make(map[int]bool)
		}
		out[setID][version] = true
	}
	return out, rows.Err()
}

func (s *Store) OpenReportCounts(ctx context.Context) (map[string]map[int]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT set_id, version, COUNT(*) FROM reports WHERE state = 'open' GROUP BY set_id, version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]map[int]int)
	for rows.Next() {
		var setID string
		var version, n int
		if err := rows.Scan(&setID, &version, &n); err != nil {
			return nil, err
		}
		if out[setID] == nil {
			out[setID] = make(map[int]int)
		}
		out[setID][version] = n
	}
	return out, rows.Err()
}
