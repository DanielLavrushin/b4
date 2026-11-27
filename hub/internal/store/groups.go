package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
)

type Group struct {
	ID                 string
	AuthorHMAC         string
	DerivedFromID      string
	DerivedFromVersion int
	CreatedAt          time.Time
	UpdatedAt          time.Time
	WithdrawnAt        time.Time
	WithdrawReason     string
}

type GroupVersion struct {
	RowID           int64
	GroupID         string
	Version         int
	Title           string
	Description     string
	Status          string
	StatusReason    string
	HiddenFrom      string
	RecordID        string
	UploaderHMAC    string
	ASNObserved     string
	CountryObserved string
	ASNHint         string
	CountryHint     string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Members         []GroupMember
}

type GroupMember struct {
	Position   int
	SetID      string
	SetVersion int
	Brought    bool
	Escalate   *hubwire.GroupEscalation
}

type Membership struct {
	GroupID      string
	GroupVersion int
	Title        string
	Status       string
	SetVersion   int
	Brought      bool
}

type ListedGroup struct {
	Group   Group
	Version GroupVersion
}

const groupColumns = `id, author_hmac, derived_from_id, derived_from_version, created_at, updated_at, withdrawn_at, withdraw_reason`

const groupVersionColumns = `id, group_id, version, title, description, status, status_reason, hidden_from, record_id, uploader_hmac,
	asn_observed, country_observed, asn_hint, country_hint, created_at, updated_at`

const withheldGroups = `SELECT g.id FROM groups g LEFT JOIN keys k ON k.key_hmac = g.author_hmac WHERE g.withdrawn_at <> '' OR COALESCE(k.banned, 0) = 1`

func scanGroup(row rowScanner) (*Group, error) {
	var g Group
	var createdAt, updatedAt, withdrawnAt string
	if err := row.Scan(&g.ID, &g.AuthorHMAC, &g.DerivedFromID, &g.DerivedFromVersion, &createdAt, &updatedAt, &withdrawnAt, &g.WithdrawReason); err != nil {
		return nil, err
	}
	g.CreatedAt = parseTime(createdAt)
	g.UpdatedAt = parseTime(updatedAt)
	g.WithdrawnAt = parseTime(withdrawnAt)
	return &g, nil
}

func scanGroupVersion(row rowScanner) (*GroupVersion, error) {
	var v GroupVersion
	var createdAt, updatedAt string
	if err := row.Scan(&v.RowID, &v.GroupID, &v.Version, &v.Title, &v.Description, &v.Status, &v.StatusReason, &v.HiddenFrom, &v.RecordID, &v.UploaderHMAC,
		&v.ASNObserved, &v.CountryObserved, &v.ASNHint, &v.CountryHint, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	v.CreatedAt = parseTime(createdAt)
	v.UpdatedAt = parseTime(updatedAt)
	v.Members = []GroupMember{}
	return &v, nil
}

func memberKey(groupID string, version int) string {
	return groupID + "/" + strconv.Itoa(version)
}

func queryGroupVersions(ctx context.Context, q querier, query string, args ...interface{}) ([]GroupVersion, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	out := make([]GroupVersion, 0)
	for rows.Next() {
		v, err := scanGroupVersion(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, *v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, fillGroupMembers(ctx, q, out)
}

func fillGroupMembers(ctx context.Context, q querier, versions []GroupVersion) error {
	if len(versions) == 0 {
		return nil
	}
	index := make(map[string]*GroupVersion, len(versions))
	ids := make([]interface{}, 0, len(versions))
	placeholders := ""
	for i := range versions {
		index[memberKey(versions[i].GroupID, versions[i].Version)] = &versions[i]
		if len(ids) > 0 {
			placeholders += ", "
		}
		placeholders += "?"
		ids = append(ids, versions[i].RowID)
	}
	rows, err := q.QueryContext(ctx, `SELECT m.group_id, m.version, m.position, m.set_id, m.set_version, m.brought, m.escalate_json
		FROM group_members m JOIN group_versions gv ON gv.group_id = m.group_id AND gv.version = m.version
		WHERE gv.id IN (`+placeholders+`) ORDER BY m.group_id, m.version, m.position`, ids...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var groupID, escalate string
		var version, brought int
		var m GroupMember
		if err := rows.Scan(&groupID, &version, &m.Position, &m.SetID, &m.SetVersion, &brought, &escalate); err != nil {
			return err
		}
		m.Brought = brought == 1
		if escalate != "" {
			m.Escalate = &hubwire.GroupEscalation{}
			if err := json.Unmarshal([]byte(escalate), m.Escalate); err != nil {
				return fmt.Errorf("group %s version %d member %d escalation: %w", groupID, version, m.Position, err)
			}
		}
		if v := index[memberKey(groupID, version)]; v != nil {
			v.Members = append(v.Members, m)
		}
	}
	return rows.Err()
}

func insertGroupVersionTx(ctx context.Context, q querier, v *GroupVersion) error {
	res, err := q.ExecContext(ctx, `INSERT INTO group_versions(group_id, version, title, description, status, status_reason, record_id, uploader_hmac,
		asn_observed, country_observed, asn_hint, country_hint, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		v.GroupID, v.Version, v.Title, v.Description, v.Status, v.StatusReason, v.RecordID, v.UploaderHMAC,
		v.ASNObserved, v.CountryObserved, v.ASNHint, v.CountryHint, formatTime(v.CreatedAt), formatTime(v.UpdatedAt))
	if err != nil {
		return err
	}
	v.RowID, _ = res.LastInsertId()
	for i := range v.Members {
		m := &v.Members[i]
		m.Position = i
		escalate := ""
		if m.Escalate != nil {
			raw, err := json.Marshal(m.Escalate)
			if err != nil {
				return err
			}
			escalate = string(raw)
		}
		brought := 0
		if m.Brought {
			brought = 1
		}
		if _, err := q.ExecContext(ctx, `INSERT INTO group_members(group_id, version, position, set_id, set_version, brought, escalate_json) VALUES(?, ?, ?, ?, ?, ?, ?)`,
			v.GroupID, v.Version, m.Position, m.SetID, m.SetVersion, brought, escalate); err != nil {
			return err
		}
	}
	return nil
}

func createGroupTx(ctx context.Context, q querier, g Group, v *GroupVersion) error {
	if _, err := q.ExecContext(ctx, `INSERT INTO groups(id, author_hmac, derived_from_id, derived_from_version, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?)`,
		g.ID, g.AuthorHMAC, g.DerivedFromID, g.DerivedFromVersion, formatTime(g.CreatedAt), formatTime(g.UpdatedAt)); err != nil {
		return err
	}
	v.GroupID = g.ID
	v.Version = 1
	if err := insertGroupVersionTx(ctx, q, v); err != nil {
		return err
	}
	return markDirtyTx(ctx, q)
}

func addGroupVersionTx(ctx context.Context, q querier, v *GroupVersion) error {
	var highest int
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM group_versions WHERE group_id = ?`, v.GroupID).Scan(&highest); err != nil {
		return err
	}
	if highest == 0 {
		return ErrNotFound
	}
	v.Version = highest + 1
	if err := insertGroupVersionTx(ctx, q, v); err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, `UPDATE groups SET updated_at = ? WHERE id = ?`, formatTime(v.CreatedAt), v.GroupID); err != nil {
		return err
	}
	return markDirtyTx(ctx, q)
}

func getGroupTx(ctx context.Context, q querier, id string) (*Group, []GroupVersion, error) {
	g, err := scanGroup(q.QueryRowContext(ctx, `SELECT `+groupColumns+` FROM groups WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	versions, err := queryGroupVersions(ctx, q, `SELECT `+groupVersionColumns+` FROM group_versions WHERE group_id = ? ORDER BY version`, id)
	if err != nil {
		return nil, nil, err
	}
	return g, versions, nil
}

func getGroupVersionTx(ctx context.Context, q querier, groupID string, version int) (*GroupVersion, error) {
	versions, err := queryGroupVersions(ctx, q, `SELECT `+groupVersionColumns+` FROM group_versions WHERE group_id = ? AND version = ?`, groupID, version)
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		return nil, ErrNotFound
	}
	return &versions[0], nil
}

func broughtVersionsTx(ctx context.Context, q querier, groupID string, version int) ([]Version, error) {
	gv, err := getGroupVersionTx(ctx, q, groupID, version)
	if err != nil {
		return nil, err
	}
	out := make([]Version, 0, len(gv.Members))
	for _, m := range gv.Members {
		if !m.Brought {
			continue
		}
		v, err := getVersion(ctx, q, m.SetID, m.SetVersion)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, nil
}

func setGroupStatusTx(ctx context.Context, q querier, groupID string, version int, status, reason string, now time.Time) error {
	res, err := q.ExecContext(ctx, `UPDATE group_versions SET
		hidden_from = CASE WHEN ?1 = 'hidden' THEN (CASE WHEN status = 'hidden' THEN hidden_from ELSE status END) ELSE '' END,
		status = ?1, status_reason = ?2, updated_at = ?3 WHERE group_id = ?4 AND version = ?5`,
		status, reason, formatTime(now), groupID, version)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if status == hubwire.SetStatusActive {
		if _, err := q.ExecContext(ctx, `UPDATE groups SET updated_at = ? WHERE id = ?`, formatTime(now), groupID); err != nil {
			return err
		}
	}
	return markDirtyTx(ctx, q)
}

func restoreGroupTx(ctx context.Context, q querier, groupID string, version int, now time.Time) (string, error) {
	v, err := getGroupVersionTx(ctx, q, groupID, version)
	if err != nil {
		return "", err
	}
	target := hubwire.SetStatusActive
	if v.HiddenFrom == hubwire.SetStatusPending {
		target = hubwire.SetStatusPending
	}
	return target, setGroupStatusTx(ctx, q, groupID, version, target, "", now)
}

func (s *Store) GetGroup(ctx context.Context, id string) (*Group, []GroupVersion, error) {
	return getGroupTx(ctx, s.db, id)
}

func (s *Store) GetGroupVersion(ctx context.Context, groupID string, version int) (*GroupVersion, error) {
	return getGroupVersionTx(ctx, s.db, groupID, version)
}

func (s *Store) PendingGroupVersions(ctx context.Context) ([]GroupVersion, error) {
	return queryGroupVersions(ctx, s.db, `SELECT `+groupVersionColumns+` FROM group_versions WHERE status = ? ORDER BY created_at, id`, hubwire.SetStatusPending)
}

func (s *Store) PendingGroupHistories(ctx context.Context) ([]GroupVersion, error) {
	return queryGroupVersions(ctx, s.db, `SELECT `+groupVersionColumns+` FROM group_versions WHERE group_id IN
		(SELECT group_id FROM group_versions WHERE status = ?) ORDER BY created_at, id`, hubwire.SetStatusPending)
}

func (s *Store) BroughtVersions(ctx context.Context, groupID string, version int) ([]Version, error) {
	return broughtVersionsTx(ctx, s.db, groupID, version)
}

func (s *Store) Memberships(ctx context.Context, setID string) ([]Membership, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.group_id, m.version, gv.title, gv.status, m.set_version, m.brought
		FROM group_members m JOIN group_versions gv ON gv.group_id = m.group_id AND gv.version = m.version
		WHERE m.set_id = ? ORDER BY m.group_id, m.version`, setID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Membership, 0)
	for rows.Next() {
		var m Membership
		var brought int
		if err := rows.Scan(&m.GroupID, &m.GroupVersion, &m.Title, &m.Status, &m.SetVersion, &brought); err != nil {
			return nil, err
		}
		m.Brought = brought == 1
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) CatalogueGroups(ctx context.Context) ([]ListedGroup, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+groupColumns+` FROM groups WHERE id NOT IN (`+withheldGroups+`)`)
	if err != nil {
		return nil, err
	}
	groups := make(map[string]Group)
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		groups[g.ID] = *g
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	active, err := queryGroupVersions(ctx, s.db, `SELECT `+groupVersionColumns+` FROM group_versions gv WHERE status = ?
		AND version = (SELECT MAX(o.version) FROM group_versions o WHERE o.group_id = gv.group_id AND o.status = ?) ORDER BY group_id`,
		hubwire.SetStatusActive, hubwire.SetStatusActive)
	if err != nil {
		return nil, err
	}
	out := make([]ListedGroup, 0, len(active))
	for _, v := range active {
		if g, ok := groups[v.GroupID]; ok {
			out = append(out, ListedGroup{Group: g, Version: v})
		}
	}
	return out, nil
}

func (t *Tx) CurrentMatches(ctx context.Context, fp, targetsKey string) ([]Version, error) {
	return currentMatchesTx(ctx, t.tx, fp, targetsKey)
}

func (t *Tx) CreateSet(ctx context.Context, set Set, v *Version) error {
	return createSetTx(ctx, t.tx, set, v)
}

func (t *Tx) AddVersion(ctx context.Context, v *Version) error {
	return addVersionTx(ctx, t.tx, v)
}

func (t *Tx) UpsertVote(ctx context.Context, v Vote) error {
	return upsertVoteTx(ctx, t.tx, v)
}

func (t *Tx) InsertRecord(ctx context.Context, r Record) error {
	return insertRecordTx(ctx, t.tx, r)
}

func (t *Tx) CreateGroup(ctx context.Context, g Group, v *GroupVersion) error {
	return createGroupTx(ctx, t.tx, g, v)
}

func (t *Tx) AddGroupVersion(ctx context.Context, v *GroupVersion) error {
	return addGroupVersionTx(ctx, t.tx, v)
}

func (t *Tx) Group(ctx context.Context, id string) (*Group, []GroupVersion, error) {
	return getGroupTx(ctx, t.tx, id)
}

func (t *Tx) SetGroupStatus(ctx context.Context, groupID string, version int, status, reason string, now time.Time) error {
	return setGroupStatusTx(ctx, t.tx, groupID, version, status, reason, now)
}

func (t *Tx) RestoreGroup(ctx context.Context, groupID string, version int, now time.Time) (string, error) {
	return restoreGroupTx(ctx, t.tx, groupID, version, now)
}
