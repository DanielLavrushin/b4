package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

const (
	ActorConsole = "console"
	ActorBasic   = "basic"
	ActorCLI     = "cli"
	ActorSystem  = "system"

	TargetSet       = "set"
	TargetKey       = "key"
	TargetMirror    = "mirror"
	TargetReport    = "report"
	TargetCatalogue = "catalogue"
	TargetSettings  = "settings"
	TargetNotify    = "notify"
)

type AuditEntry struct {
	ID         int64
	At         time.Time
	Actor      string
	ActorRef   string
	ActorIP    string
	Action     string
	TargetKind string
	TargetID   string
	Version    int
	Reason     string
	Before     map[string]interface{}
	After      map[string]interface{}
	BatchID    string
}

func encodeSnapshot(m map[string]interface{}) string {
	if len(m) == 0 {
		return ""
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(raw)
}

func decodeSnapshot(raw string) map[string]interface{} {
	if raw == "" {
		return nil
	}
	var m map[string]interface{}
	if json.Unmarshal([]byte(raw), &m) != nil {
		return nil
	}
	return m
}

func auditTx(ctx context.Context, q querier, e AuditEntry) (int64, error) {
	res, err := q.ExecContext(ctx, `INSERT INTO audit_log(at, actor, actor_ref, actor_ip, action, target_kind, target_id, version, reason, before_json, after_json, batch_id)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		formatTime(e.At), e.Actor, e.ActorRef, e.ActorIP, e.Action, e.TargetKind, e.TargetID, e.Version, e.Reason,
		encodeSnapshot(e.Before), encodeSnapshot(e.After), e.BatchID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) Audit(ctx context.Context, e AuditEntry) (int64, error) {
	return auditTx(ctx, s.db, e)
}

type AuditQuery struct {
	Before     int64
	Limit      int
	Action     string
	Actor      string
	TargetKind string
	TargetID   string
	BatchID    string
	Since      time.Time
	Until      time.Time
}

func (s *Store) AuditLog(ctx context.Context, f AuditQuery) ([]AuditEntry, int64, error) {
	where := []string{"1 = 1"}
	args := []interface{}{}
	if f.Before > 0 {
		where = append(where, "id < ?")
		args = append(args, f.Before)
	}
	if f.TargetKind != "" {
		where = append(where, "target_kind = ?")
		args = append(args, f.TargetKind)
	}
	if f.TargetID != "" {
		where = append(where, "target_id = ?")
		args = append(args, f.TargetID)
	}
	if f.Action != "" {
		if strings.HasSuffix(f.Action, ".") {
			where = append(where, "action LIKE ?")
			args = append(args, f.Action+"%")
		} else {
			where = append(where, "action = ?")
			args = append(args, f.Action)
		}
	}
	if f.Actor != "" {
		where = append(where, "actor = ?")
		args = append(args, f.Actor)
	}
	if f.BatchID != "" {
		where = append(where, "batch_id = ?")
		args = append(args, f.BatchID)
	}
	if !f.Since.IsZero() {
		where = append(where, "at >= ?")
		args = append(args, formatTime(f.Since))
	}
	if !f.Until.IsZero() {
		where = append(where, "at < ?")
		args = append(args, formatTime(f.Until))
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, `SELECT id, at, actor, actor_ref, actor_ip, action, target_kind, target_id, version, reason, before_json, after_json, batch_id
		FROM audit_log WHERE `+strings.Join(where, " AND ")+` ORDER BY id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]AuditEntry, 0)
	for rows.Next() {
		var e AuditEntry
		var at, before, after string
		if err := rows.Scan(&e.ID, &at, &e.Actor, &e.ActorRef, &e.ActorIP, &e.Action, &e.TargetKind, &e.TargetID, &e.Version, &e.Reason, &before, &after, &e.BatchID); err != nil {
			return nil, 0, err
		}
		e.At = parseTime(at)
		e.Before = decodeSnapshot(before)
		e.After = decodeSnapshot(after)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var next int64
	if len(out) > limit {
		out = out[:limit]
		next = out[len(out)-1].ID
	}
	return out, next, nil
}
