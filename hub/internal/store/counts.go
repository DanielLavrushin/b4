package store

import (
	"context"
	"time"
)

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
		(SELECT COUNT(*) FROM set_versions WHERE status = 'pending'),
		(SELECT COALESCE(MIN(created_at), '') FROM set_versions WHERE status = 'pending'),
		(SELECT COUNT(*) FROM reports WHERE state = 'open'),
		(SELECT COUNT(*) FROM mirrors WHERE status = 'pending'),
		(SELECT COUNT(*) FROM keys),
		(SELECT COALESCE(SUM(banned), 0) FROM keys)`).Scan(&b.Pending, &oldest, &b.ReportsOpen, &b.MirrorsPending, &b.Keys, &b.Banned)
	b.OldestPending = parseTime(oldest)
	return b, err
}

type VersionCounts struct {
	Pending    int
	Active     int
	Eligible   int
	Listed     int
	Withheld   int
	Hidden     int
	Rejected   int
	Superseded int
}

func (s *Store) VersionCounts(ctx context.Context) (VersionCounts, error) {
	var c VersionCounts
	err := s.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM set_versions WHERE status = 'pending'),
		(SELECT COUNT(*) FROM set_versions WHERE status = 'active'),
		(SELECT COUNT(DISTINCT set_id) FROM set_versions WHERE status = 'active'),
		(SELECT COUNT(DISTINCT set_id) FROM set_versions WHERE status = 'active' AND set_id NOT IN (`+withheldSets+`)),
		(SELECT COUNT(DISTINCT set_id) FROM set_versions WHERE status = 'active' AND set_id IN (`+withheldSets+`)),
		(SELECT COUNT(*) FROM set_versions WHERE status = 'hidden'),
		(SELECT COUNT(*) FROM set_versions WHERE status = 'rejected')`).Scan(&c.Pending, &c.Active, &c.Eligible, &c.Listed, &c.Withheld, &c.Hidden, &c.Rejected)
	c.Superseded = c.Active - c.Eligible
	return c, err
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
