package store

import (
	"context"
	"strconv"
	"strings"
	"time"
)

type VoteFilter struct {
	SetID    string
	Version  int
	FP       string
	KeyHMAC  string
	Sign     string
	Kind     string
	ASN      string
	Country  string
	Verified string
	Author   string
	Since    time.Time
	Until    time.Time
	Before   string
	Limit    int
}

func (s *Store) QueryVotes(ctx context.Context, f VoteFilter) ([]Vote, int, string, error) {
	where := []string{"1 = 1"}
	args := []interface{}{}
	add := func(clause string, values ...interface{}) {
		where = append(where, clause)
		args = append(args, values...)
	}
	if f.SetID != "" {
		add("v.set_id = ?", f.SetID)
	}
	if f.Version > 0 {
		add("v.version = ?", f.Version)
	}
	if f.FP != "" {
		add("v.fp = ?", f.FP)
	}
	if f.KeyHMAC != "" {
		add("v.key_hmac = ?", f.KeyHMAC)
	}
	switch f.Sign {
	case "works":
		add("v.weight > 0")
	case "broken":
		add("v.weight < 0")
	}
	if f.Kind != "" {
		add("v.kind = ?", f.Kind)
	}
	if f.ASN != "" {
		add("v.asn_observed = ?", f.ASN)
	}
	if f.Country != "" {
		add("v.country_observed = ?", strings.ToUpper(f.Country))
	}
	switch f.Verified {
	case "1":
		add("v.origin_verified = 1")
	case "0":
		add("v.origin_verified = 0")
	}
	switch f.Author {
	case "only":
		add("v.key_hmac = (SELECT author_hmac FROM sets WHERE id = v.set_id)")
	case "exclude":
		add("v.key_hmac <> COALESCE((SELECT author_hmac FROM sets WHERE id = v.set_id), '')")
	}
	if !f.Since.IsZero() {
		add("v.received_at >= ?", formatTime(f.Since))
	}
	if !f.Until.IsZero() {
		add("v.received_at < ?", formatTime(f.Until))
	}
	clause := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM votes v WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, "", err
	}
	page := clause
	pageArgs := append([]interface{}{}, args...)
	if at, id, ok := parseCursor(f.Before); ok {
		page += " AND (v.received_at < ? OR (v.received_at = ? AND v.id < ?))"
		pageArgs = append(pageArgs, at, at, id)
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	pageArgs = append(pageArgs, limit+1)
	votes, err := s.queryVotes(ctx, `SELECT `+voteColumns+` FROM votes v LEFT JOIN keys k ON k.key_hmac = v.key_hmac WHERE `+page+` ORDER BY v.received_at DESC, v.id DESC LIMIT ?`, pageArgs...)
	if err != nil {
		return nil, 0, "", err
	}
	next := ""
	if len(votes) > limit {
		votes = votes[:limit]
		last := votes[len(votes)-1]
		next = formatTime(last.ReceivedAt) + "_" + strconv.FormatInt(last.ID, 10)
	}
	return votes, total, next, nil
}

func (s *Store) VersionsByAuthor(ctx context.Context, keyHMAC string) ([]Version, error) {
	return s.queryVersions(ctx, `SELECT `+versionColumns+` FROM set_versions WHERE set_id IN (SELECT id FROM sets WHERE author_hmac = ?) ORDER BY set_id, version`, keyHMAC)
}

func (s *Store) ReportsByKey(ctx context.Context, keyHMAC string, limit int) ([]Report, error) {
	return queryReports(ctx, s.db, `SELECT `+reportColumns+reportFrom+` WHERE r.key_hmac = ? ORDER BY r.received_at DESC, r.id DESC LIMIT ?`, keyHMAC, limit)
}

type ActivityKind struct {
	Kind  string
	Count int
	First time.Time
	Last  time.Time
}

type ActivityDay struct {
	Day   string
	Kind  string
	Count int
}

func (s *Store) KeyActivity(ctx context.Context, keyHMAC string, since time.Time) ([]ActivityKind, []ActivityDay, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT kind, COUNT(*), MIN(received_at), MAX(received_at) FROM records WHERE key_hmac = ? GROUP BY kind ORDER BY kind`, keyHMAC)
	if err != nil {
		return nil, nil, err
	}
	kinds := make([]ActivityKind, 0)
	for rows.Next() {
		var a ActivityKind
		var first, last string
		if err := rows.Scan(&a.Kind, &a.Count, &first, &last); err != nil {
			rows.Close()
			return nil, nil, err
		}
		a.First = parseTime(first)
		a.Last = parseTime(last)
		kinds = append(kinds, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT substr(received_at, 1, 10), kind, COUNT(*) FROM records WHERE key_hmac = ? AND received_at >= ? GROUP BY 1, 2 ORDER BY 1`, keyHMAC, formatTime(since))
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	days := make([]ActivityDay, 0)
	for rows.Next() {
		var d ActivityDay
		if err := rows.Scan(&d.Day, &d.Kind, &d.Count); err != nil {
			return nil, nil, err
		}
		days = append(days, d)
	}
	return kinds, days, rows.Err()
}

type KeyOrigin struct {
	ASN     string
	Name    string
	Country string
	Count   int
	First   time.Time
	Last    time.Time
	Sources []string
}

func (s *Store) KeyOrigins(ctx context.Context, keyHMAC string) ([]KeyOrigin, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT o.asn, COALESCE(n.name, ''), MAX(o.country), COUNT(*), MIN(o.at), MAX(o.at), GROUP_CONCAT(DISTINCT o.source) FROM (
		SELECT asn_observed AS asn, country_observed AS country, received_at AS at, 'vote' AS source FROM votes WHERE key_hmac = ?1 AND asn_observed <> ''
		UNION ALL SELECT asn_observed, country_observed, created_at, 'share' FROM set_versions WHERE uploader_hmac = ?1 AND asn_observed <> ''
		UNION ALL SELECT asn_observed, '', received_at, 'report' FROM reports WHERE key_hmac = ?1 AND asn_observed <> ''
	) o LEFT JOIN asn_names n ON n.asn = o.asn GROUP BY o.asn ORDER BY MAX(o.at) DESC`, keyHMAC)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]KeyOrigin, 0)
	for rows.Next() {
		var o KeyOrigin
		var first, last, sources string
		if err := rows.Scan(&o.ASN, &o.Name, &o.Country, &o.Count, &first, &last, &sources); err != nil {
			return nil, err
		}
		o.First = parseTime(first)
		o.Last = parseTime(last)
		if sources != "" {
			o.Sources = strings.Split(sources, ",")
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

type KeyClient struct {
	B4Version string
	Engine    string
	Count     int
	Last      time.Time
}

func (s *Store) KeyClients(ctx context.Context, keyHMAC string) ([]KeyClient, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT b4_version, engine, COUNT(*), MAX(at) FROM (
		SELECT b4_version, engine, received_at AS at FROM votes WHERE key_hmac = ?1 AND b4_version <> ''
		UNION ALL SELECT b4_version, engine, created_at FROM set_versions WHERE uploader_hmac = ?1 AND b4_version <> ''
	) GROUP BY b4_version, engine ORDER BY MAX(at) DESC`, keyHMAC)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]KeyClient, 0)
	for rows.Next() {
		var c KeyClient
		var last string
		if err := rows.Scan(&c.B4Version, &c.Engine, &c.Count, &last); err != nil {
			return nil, err
		}
		c.Last = parseTime(last)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) MirrorsByKey(ctx context.Context, keyHMAC string) ([]Mirror, error) {
	return queryMirrors(ctx, s.db, `SELECT `+mirrorColumns+` FROM mirrors WHERE key_hmac = ? ORDER BY id`, keyHMAC)
}
