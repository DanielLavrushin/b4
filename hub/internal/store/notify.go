package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	NotifyShare       = "share"
	NotifyReport      = "report"
	NotifyAutoHide    = "auto_hide"
	NotifyBuildFailed = "build_failed"
	NotifyMirror      = "mirror"

	notifyCursorPrefix = "notify.cursor."
)

var NotifySources = []string{NotifyShare, NotifyReport, NotifyAutoHide, NotifyBuildFailed, NotifyMirror}

type NotifyEvent struct {
	Kind    string
	ID      int64
	At      time.Time
	SetID   string
	Version int
	Title   string
	KeyHMAC string
	ASN     string
	Country string
	Reason  string
	URL     string
	Detail  string
	Reports int
}

type NotifyBatch struct {
	Kind   string
	Count  int
	MaxID  int64
	Events []NotifyEvent
}

type notifySource struct {
	table   string
	id      string
	from    string
	where   string
	columns string
}

const notTestKey = ` NOT IN (SELECT key_hmac FROM keys WHERE tag = '` + TagTest + `')`

var notifyQueries = map[string]notifySource{
	NotifyShare: {
		table:   "set_versions",
		id:      "v.id",
		from:    `set_versions v`,
		where:   `v.status = 'pending' AND v.uploader_hmac` + notTestKey,
		columns: `v.id, v.created_at, v.set_id, v.version, v.title, v.uploader_hmac, v.asn_observed, v.country_observed, '', '', '', 0`,
	},
	NotifyReport: {
		table:   "reports",
		id:      "r.id",
		from:    `reports r LEFT JOIN set_versions v ON v.set_id = r.set_id AND v.version = r.version`,
		where:   `r.key_hmac` + notTestKey,
		columns: `r.id, r.received_at, r.set_id, r.version, COALESCE(v.title, ''), r.key_hmac, r.asn_observed, '', r.reason, '', '', 0`,
	},
	NotifyAutoHide: {
		table: "audit_log",
		id:    "a.id",
		from:  `audit_log a LEFT JOIN set_versions v ON v.set_id = a.target_id AND v.version = a.version`,
		where: `a.actor = '` + ActorSystem + `' AND a.action = 'set.hide'`,
		columns: `a.id, a.at, a.target_id, a.version, COALESCE(v.title, ''), '', '', '', a.reason, '', '',
			CASE WHEN json_valid(a.after_json) THEN COALESCE(json_extract(a.after_json, '$.independent_reports'), 0) ELSE 0 END`,
	},
	NotifyBuildFailed: {
		table:   "builds",
		id:      "b.id",
		from:    `builds b`,
		where:   `b.ok = 0 AND b.finished_at <> ''`,
		columns: `b.id, b.finished_at, '', 0, '', '', '', '', b.error, '', b.trigger, 0`,
	},
	NotifyMirror: {
		table:   "mirrors",
		id:      "m.id",
		from:    `mirrors m`,
		where:   `m.status = '` + MirrorPending + `'`,
		columns: `m.id, m.first_seen, '', 0, '', m.key_hmac, '', '', '', m.url, m.version, 0`,
	},
}

func (s *Store) NotifyMaxIDs(ctx context.Context) (map[string]int64, error) {
	out := make(map[string]int64, len(NotifySources))
	for _, kind := range NotifySources {
		var id int64
		if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM `+notifyQueries[kind].table).Scan(&id); err != nil {
			return nil, err
		}
		out[kind] = id
	}
	return out, nil
}

func (s *Store) NotifyEvents(ctx context.Context, kind string, after int64, limit int) (NotifyBatch, error) {
	q, ok := notifyQueries[kind]
	if !ok {
		return NotifyBatch{}, fmt.Errorf("unknown notification source %q", kind)
	}
	batch := NotifyBatch{Kind: kind}
	where := ` FROM ` + q.from + ` WHERE ` + q.id + ` > ? AND ` + q.where
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MAX(`+q.id+`), 0)`+where, after).Scan(&batch.Count, &batch.MaxID); err != nil {
		return NotifyBatch{}, err
	}
	if batch.Count == 0 || limit <= 0 {
		return batch, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+q.columns+where+` AND `+q.id+` <= ? ORDER BY `+q.id+` LIMIT ?`, after, batch.MaxID, limit)
	if err != nil {
		return NotifyBatch{}, err
	}
	defer rows.Close()
	for rows.Next() {
		e := NotifyEvent{Kind: kind}
		var at string
		var reports sql.NullInt64
		if err := rows.Scan(&e.ID, &at, &e.SetID, &e.Version, &e.Title, &e.KeyHMAC, &e.ASN, &e.Country, &e.Reason, &e.URL, &e.Detail, &reports); err != nil {
			return NotifyBatch{}, err
		}
		e.At = parseTime(at)
		e.Reports = int(reports.Int64)
		batch.Events = append(batch.Events, e)
	}
	return batch, rows.Err()
}

func notifyCursorKey(channel, kind string) string {
	return notifyCursorPrefix + channel + "." + kind
}

func (s *Store) NotifyCursors(ctx context.Context, channel string) (map[string]int64, error) {
	prefix := notifyCursorPrefix + channel + "."
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM meta WHERE substr(key, 1, ?) = ?`, len(prefix), prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			continue
		}
		out[strings.TrimPrefix(key, prefix)] = id
	}
	return out, rows.Err()
}

func (s *Store) AdvanceNotifyCursors(ctx context.Context, channel string, ids map[string]int64) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for kind, id := range ids {
		if _, err := tx.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES(?, ?)
			ON CONFLICT(key) DO UPDATE SET value = CASE WHEN CAST(value AS INTEGER) < CAST(excluded.value AS INTEGER) THEN excluded.value ELSE value END`,
			notifyCursorKey(channel, kind), strconv.FormatInt(id, 10)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
