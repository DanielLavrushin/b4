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
