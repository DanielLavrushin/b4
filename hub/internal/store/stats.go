package store

import (
	"context"
	"time"
)

const notTest = `NOT IN (SELECT key_hmac FROM keys WHERE tag = 'test')`

type DayCount struct {
	Day   string
	Kind  string
	Count int
}

func (s *Store) dayCounts(ctx context.Context, query string, args ...interface{}) ([]DayCount, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]DayCount, 0)
	for rows.Next() {
		var d DayCount
		if err := rows.Scan(&d.Day, &d.Kind, &d.Count); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) DailyActivity(ctx context.Context, since time.Time) ([]DayCount, error) {
	from := formatTime(since)
	var out []DayCount
	queries := []string{
		`SELECT substr(r.received_at, 1, 10), CASE WHEN sv.id IS NULL THEN 'duplicate' ELSE 'share' END, COUNT(*)
			FROM records r LEFT JOIN set_versions sv ON sv.record_id = r.id
			WHERE r.kind = 'share' AND r.received_at >= ? AND r.key_hmac ` + notTest + ` GROUP BY 1, 2`,
		`SELECT substr(received_at, 1, 10), CASE WHEN weight < 0 THEN 'broken' ELSE 'works' END, COUNT(*)
			FROM votes WHERE received_at >= ? AND kind <> 'upload' AND key_hmac ` + notTest + ` GROUP BY 1, 2`,
		`SELECT substr(received_at, 1, 10), 'report', COUNT(*) FROM reports WHERE received_at >= ? AND key_hmac ` + notTest + ` GROUP BY 1`,
		`SELECT substr(first_seen, 1, 10), 'new_key', COUNT(*) FROM keys WHERE first_seen >= ? AND tag <> 'test' GROUP BY 1`,
		`SELECT substr(received_at, 1, 10), 'mirror', COUNT(*) FROM records WHERE kind = 'mirror' AND received_at >= ? GROUP BY 1`,
		`SELECT substr(at, 1, 10), CASE WHEN actor = 'system' THEN 'auto_hidden' ELSE substr(action, 5) END, COUNT(*)
			FROM audit_log WHERE at >= ? AND target_kind = 'set' AND action IN ('set.approve', 'set.reject', 'set.hide', 'set.withdraw') GROUP BY 1, 2`,
		`SELECT substr(started_at, 1, 10), CASE WHEN ok = 1 THEN 'build' ELSE 'build_failed' END, COUNT(*) FROM builds WHERE started_at >= ? AND finished_at <> '' GROUP BY 1, 2`,
	}
	for _, q := range queries {
		rows, err := s.dayCounts(ctx, q, from)
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	return out, nil
}

type MixRow struct {
	Key     string
	Name    string
	Country string
	Votes   int
	Keys    int
}

func (s *Store) mix(ctx context.Context, query string, args ...interface{}) ([]MixRow, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]MixRow, 0)
	for rows.Next() {
		var m MixRow
		if err := rows.Scan(&m.Key, &m.Name, &m.Country, &m.Votes, &m.Keys); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) CountryMix(ctx context.Context, since time.Time, limit int) ([]MixRow, error) {
	return s.mix(ctx, `SELECT country_observed, '', '', COUNT(*), COUNT(DISTINCT key_hmac) FROM votes
		WHERE received_at >= ? AND country_observed <> '' AND key_hmac `+notTest+`
		GROUP BY country_observed ORDER BY 5 DESC, 4 DESC LIMIT ?`, formatTime(since), limit)
}

func (s *Store) ASNMix(ctx context.Context, since time.Time, limit int) ([]MixRow, error) {
	return s.mix(ctx, `SELECT v.asn_observed, COALESCE(n.name, ''), COALESCE(n.country, ''), COUNT(*), COUNT(DISTINCT v.key_hmac)
		FROM votes v LEFT JOIN asn_names n ON n.asn = v.asn_observed
		WHERE v.received_at >= ? AND v.asn_observed <> '' AND v.key_hmac `+notTest+`
		GROUP BY v.asn_observed ORDER BY 5 DESC, 4 DESC LIMIT ?`, formatTime(since), limit)
}

func (s *Store) ClientMix(ctx context.Context, since time.Time) ([]MixRow, error) {
	return s.mix(ctx, `SELECT b4_version, engine, '', COUNT(*), COUNT(*) FROM (
		SELECT b4_version, engine, ROW_NUMBER() OVER (PARTITION BY key_hmac ORDER BY received_at DESC, id DESC) AS rn
		FROM votes WHERE received_at >= ? AND b4_version <> '' AND key_hmac `+notTest+`) WHERE rn = 1
		GROUP BY b4_version, engine ORDER BY 4 DESC`, formatTime(since))
}

type Coverage struct {
	Votes      int
	Unverified int
	Devices    int
}

func (s *Store) VoteCoverage(ctx context.Context, since time.Time) (Coverage, error) {
	var c Coverage
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(origin_verified = 0), 0), COUNT(DISTINCT key_hmac) FROM votes
		WHERE received_at >= ? AND kind <> 'upload' AND key_hmac `+notTest, formatTime(since)).Scan(&c.Votes, &c.Unverified, &c.Devices)
	return c, err
}

type SetActivity struct {
	SetID  string
	Votes  int
	Keys   int
	Works  int
	Broken int
}

func (s *Store) TopSets(ctx context.Context, since time.Time, limit int) ([]SetActivity, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT set_id, COUNT(*), COUNT(DISTINCT key_hmac), COALESCE(SUM(weight > 0), 0), COALESCE(SUM(weight < 0), 0)
		FROM votes WHERE received_at >= ? AND kind <> 'upload' AND key_hmac `+notTest+`
		GROUP BY set_id ORDER BY 3 DESC, 2 DESC LIMIT ?`, formatTime(since), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SetActivity, 0)
	for rows.Next() {
		var a SetActivity
		if err := rows.Scan(&a.SetID, &a.Votes, &a.Keys, &a.Works, &a.Broken); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
