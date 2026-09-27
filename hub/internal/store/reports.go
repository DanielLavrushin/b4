package store

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	ReportOpen      = "open"
	ReportDismissed = "dismissed"
	ReportResolved  = "resolved"

	ResolutionRestored  = "restored"
	ResolutionHidden    = "hidden"
	ResolutionRejected  = "rejected"
	ResolutionWithdrawn = "withdrawn"
)

type Report struct {
	ID          int64
	RecordID    string
	SetID       string
	Version     int
	KeyHMAC     string
	ASNObserved string
	Reason      string
	ReceivedAt  time.Time
	State       string
	Resolution  string
	Note        string
	ResolvedAt  time.Time
	KeyBanned   bool
	KeyTest     bool
}

func ValidReportState(state string) bool {
	switch state {
	case ReportOpen, ReportDismissed, ReportResolved:
		return true
	}
	return false
}

const reportColumns = `r.id, r.record_id, r.set_id, r.version, r.key_hmac, r.asn_observed, r.reason, r.received_at,
	r.state, r.resolution, r.note, r.resolved_at, COALESCE(k.banned, 0), COALESCE(k.tag = 'test', 0)`

const reportFrom = ` FROM reports r LEFT JOIN keys k ON k.key_hmac = r.key_hmac`

func (s *Store) InsertReport(ctx context.Context, r Report) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO reports(record_id, set_id, version, key_hmac, asn_observed, reason, received_at) VALUES(?, ?, ?, ?, ?, ?, ?)`,
		r.RecordID, r.SetID, r.Version, r.KeyHMAC, r.ASNObserved, r.Reason, formatTime(r.ReceivedAt))
	return err
}

func queryReports(ctx context.Context, q querier, query string, args ...interface{}) ([]Report, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Report, 0)
	for rows.Next() {
		var r Report
		var receivedAt, resolvedAt string
		var banned, test int
		if err := rows.Scan(&r.ID, &r.RecordID, &r.SetID, &r.Version, &r.KeyHMAC, &r.ASNObserved, &r.Reason, &receivedAt,
			&r.State, &r.Resolution, &r.Note, &resolvedAt, &banned, &test); err != nil {
			return nil, err
		}
		r.ReceivedAt = parseTime(receivedAt)
		r.ResolvedAt = parseTime(resolvedAt)
		r.KeyBanned = banned != 0
		r.KeyTest = test != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) ReportsForVersion(ctx context.Context, setID string, version int) ([]Report, error) {
	return queryReports(ctx, s.db, `SELECT `+reportColumns+reportFrom+` WHERE r.set_id = ? AND r.version = ? ORDER BY r.received_at DESC, r.id DESC`, setID, version)
}

func (s *Store) RecentReports(ctx context.Context, limit int) ([]Report, error) {
	return queryReports(ctx, s.db, `SELECT `+reportColumns+reportFrom+` ORDER BY r.received_at DESC, r.id DESC LIMIT ?`, limit)
}

func (s *Store) AllReports(ctx context.Context) ([]Report, error) {
	return queryReports(ctx, s.db, `SELECT `+reportColumns+reportFrom+` ORDER BY r.received_at DESC, r.id DESC`)
}

func reportsByID(ctx context.Context, q querier, ids []int64) ([]Report, error) {
	if len(ids) == 0 {
		return []Report{}, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]interface{}, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	return queryReports(ctx, q, `SELECT `+reportColumns+reportFrom+` WHERE r.id IN (`+marks+`) ORDER BY r.id`, args...)
}

func (s *Store) CountReports(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM reports`).Scan(&n)
	return n, err
}

func (s *Store) ReportCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT state, COUNT(*) FROM reports GROUP BY state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{ReportOpen: 0, ReportDismissed: 0, ReportResolved: 0}
	for rows.Next() {
		var state string
		var n int
		if err := rows.Scan(&state, &n); err != nil {
			return nil, err
		}
		out[state] = n
	}
	return out, rows.Err()
}

func Counts(r Report) bool {
	return r.State == ReportOpen && r.ASNObserved != "" && !r.KeyBanned && !r.KeyTest
}

func IndependentOf(reports []Report) int {
	keysByASN := make(map[string]map[string]struct{})
	for _, r := range reports {
		if !Counts(r) {
			continue
		}
		if keysByASN[r.ASNObserved] == nil {
			keysByASN[r.ASNObserved] = make(map[string]struct{})
		}
		keysByASN[r.ASNObserved][r.KeyHMAC] = struct{}{}
	}
	asns := make([]string, 0, len(keysByASN))
	for asn := range keysByASN {
		asns = append(asns, asn)
	}
	sort.Strings(asns)
	matchedKey := make(map[string]string)
	independent := 0
	for _, asn := range asns {
		if assignKeyToASN(asn, keysByASN, matchedKey, make(map[string]struct{})) {
			independent++
		}
	}
	return independent
}

func (s *Store) IndependentReports(ctx context.Context, setID string, version int) (int, error) {
	reports, err := s.ReportsForVersion(ctx, setID, version)
	if err != nil {
		return 0, err
	}
	return IndependentOf(reports), nil
}

func assignKeyToASN(asn string, keysByASN map[string]map[string]struct{}, matchedKey map[string]string, visited map[string]struct{}) bool {
	keys := make([]string, 0, len(keysByASN[asn]))
	for key := range keysByASN[asn] {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, seen := visited[key]; seen {
			continue
		}
		visited[key] = struct{}{}
		holder, taken := matchedKey[key]
		if !taken || assignKeyToASN(holder, keysByASN, matchedKey, visited) {
			matchedKey[key] = asn
			return true
		}
	}
	return false
}

func resolveVersionReportsTx(ctx context.Context, q querier, setID string, version int, state, resolution, note string, now time.Time) (int, error) {
	res, err := q.ExecContext(ctx, `UPDATE reports SET state = ?, resolution = ?, note = ?, resolved_at = ? WHERE set_id = ? AND version = ? AND state = 'open'`,
		state, resolution, note, formatTime(now), setID, version)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func setReportStateTx(ctx context.Context, q querier, id int64, state, resolution, note string, now time.Time) error {
	resolvedAt := formatTime(now)
	if state == ReportOpen {
		resolution = ""
		resolvedAt = ""
	}
	res, err := q.ExecContext(ctx, `UPDATE reports SET state = ?, resolution = ?, note = ?, resolved_at = ? WHERE id = ?`, state, resolution, note, resolvedAt, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

type ReportFilter struct {
	State   string
	SetID   string
	Version int
	KeyHMAC string
	ASN     string
	Since   time.Time
	Until   time.Time
	Before  string
	Limit   int
}

func reportCursor(r Report) string {
	return formatTime(r.ReceivedAt) + "_" + strconv.FormatInt(r.ID, 10)
}

func (s *Store) QueryReports(ctx context.Context, f ReportFilter) ([]Report, int, string, error) {
	where := []string{"1 = 1"}
	args := []interface{}{}
	if f.State != "" {
		where = append(where, "r.state = ?")
		args = append(args, f.State)
	}
	if f.SetID != "" {
		where = append(where, "r.set_id = ?")
		args = append(args, f.SetID)
	}
	if f.Version > 0 {
		where = append(where, "r.version = ?")
		args = append(args, f.Version)
	}
	if f.KeyHMAC != "" {
		where = append(where, "r.key_hmac = ?")
		args = append(args, f.KeyHMAC)
	}
	if f.ASN != "" {
		where = append(where, "r.asn_observed = ?")
		args = append(args, f.ASN)
	}
	if !f.Since.IsZero() {
		where = append(where, "r.received_at >= ?")
		args = append(args, formatTime(f.Since))
	}
	if !f.Until.IsZero() {
		where = append(where, "r.received_at < ?")
		args = append(args, formatTime(f.Until))
	}
	clause := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*)`+reportFrom+` WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, "", err
	}
	page := clause
	pageArgs := append([]interface{}{}, args...)
	if at, id, ok := parseCursor(f.Before); ok {
		page += " AND (r.received_at < ? OR (r.received_at = ? AND r.id < ?))"
		pageArgs = append(pageArgs, at, at, id)
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	pageArgs = append(pageArgs, limit+1)
	items, err := queryReports(ctx, s.db, `SELECT `+reportColumns+reportFrom+` WHERE `+page+` ORDER BY r.received_at DESC, r.id DESC LIMIT ?`, pageArgs...)
	if err != nil {
		return nil, 0, "", err
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = reportCursor(items[len(items)-1])
	}
	return items, total, next, nil
}

func parseCursor(raw string) (string, int64, bool) {
	at, rawID, ok := strings.Cut(raw, "_")
	if !ok || at == "" {
		return "", 0, false
	}
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		return "", 0, false
	}
	return at, id, true
}

func (s *Store) AutoHide(ctx context.Context, setID string, version int, reason string, independent int, now time.Time) (bool, error) {
	hidden := false
	err := s.Update(ctx, func(t *Tx) error {
		v, err := getVersion(ctx, t.tx, setID, version)
		if err != nil {
			return err
		}
		if v.Status != "active" {
			return nil
		}
		if err := setStatusTx(ctx, t.tx, setID, version, "hidden", reason, now); err != nil {
			return err
		}
		hidden = true
		_, err = auditTx(ctx, t.tx, AuditEntry{
			At:         now,
			Actor:      ActorSystem,
			ActorRef:   reason,
			Action:     "set.hide",
			TargetKind: TargetSet,
			TargetID:   setID,
			Version:    version,
			Reason:     reason,
			Before:     map[string]interface{}{"status": v.Status},
			After:      map[string]interface{}{"status": "hidden", "independent_reports": independent},
		})
		return err
	})
	return hidden, err
}

func resolveSetReportsTx(ctx context.Context, q querier, setID, state, resolution, note string, now time.Time) (int, error) {
	res, err := q.ExecContext(ctx, `UPDATE reports SET state = ?, resolution = ?, note = ?, resolved_at = ? WHERE set_id = ? AND state = 'open'`,
		state, resolution, note, formatTime(now), setID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (t *Tx) ResolveSetReports(ctx context.Context, setID, state, resolution, note string, now time.Time) (int, error) {
	return resolveSetReportsTx(ctx, t.tx, setID, state, resolution, note, now)
}

func (t *Tx) OpenReports(ctx context.Context, setID string, version int) (int, error) {
	var n int
	err := t.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM reports WHERE set_id = ? AND version = ? AND state = 'open'`, setID, version).Scan(&n)
	return n, err
}
