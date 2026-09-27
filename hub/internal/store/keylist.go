package store

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
)

const (
	TagStaff = "staff"
	TagTest  = "test"

	MaxKeyNameRunes = 40
	MaxKeyNoteRunes = 2000
)

func ValidTag(tag string) bool {
	return tag == "" || tag == TagStaff || tag == TagTest
}

type KeyProfile struct {
	Name string
	Note string
	Tag  string
}

type KeyRow struct {
	Key
	KeyProfile
	ProfileUpdatedAt time.Time
	Records          int
	LastSeen         time.Time
	Sets             int
	ListedSets       int
	PendingVersions  int
	Votes            int
	ManualVotes      int
	Reports          int
	ReportsAgainst   int
	LastASN          string
	LastCountry      string
}

type KeyQuery struct {
	Status      string
	Tag         string
	Sets        string
	ActiveSince time.Time
	Text        string
	Prefix      string
	Exact       string
	Sort        string
	Desc        bool
	Limit       int
	Offset      int
}

type KeyCounts struct {
	All      int
	Banned   int
	Trusted  int
	Staff    int
	Test     int
	WithSets int
	Active7d int
}

func scanKeyRow(rows interface{ Scan(...interface{}) error }) (KeyRow, error) {
	var k KeyRow
	var banned, trusted int
	var firstSeen, bannedAt, trustedAt, profileAt string
	err := rows.Scan(&k.KeyHMAC, &firstSeen, &banned, &k.BanReason, &bannedAt, &trusted, &trustedAt, &k.Name, &k.Note, &k.Tag, &profileAt)
	k.FirstSeen = parseTime(firstSeen)
	k.Banned = banned != 0
	k.BannedAt = parseTime(bannedAt)
	k.Trusted = trusted != 0
	k.TrustedAt = parseTime(trustedAt)
	k.ProfileUpdatedAt = parseTime(profileAt)
	return k, err
}

const keyRowColumns = `key_hmac, first_seen, banned, ban_reason, banned_at, trusted, trusted_at, name, note, tag, profile_updated_at`

func countInto(ctx context.Context, q querier, query string, apply func(key string, a, b int, last string)) error {
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key, last string
		var a, b int
		if err := rows.Scan(&key, &a, &b, &last); err != nil {
			return err
		}
		apply(key, a, b, last)
	}
	return rows.Err()
}

func (s *Store) keyRows(ctx context.Context, where string, args ...interface{}) ([]KeyRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+keyRowColumns+` FROM keys`+where, args...)
	if err != nil {
		return nil, err
	}
	out := make([]KeyRow, 0)
	index := map[string]int{}
	for rows.Next() {
		k, err := scanKeyRow(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		index[k.KeyHMAC] = len(out)
		out = append(out, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	at := func(key string) *KeyRow {
		if i, ok := index[key]; ok {
			return &out[i]
		}
		return nil
	}
	if err := countInto(ctx, s.db, `SELECT key_hmac, COUNT(*), 0, MAX(received_at) FROM records GROUP BY key_hmac`, func(key string, a, _ int, last string) {
		if k := at(key); k != nil {
			k.Records = a
			k.LastSeen = parseTime(last)
		}
	}); err != nil {
		return nil, err
	}
	if err := countInto(ctx, s.db, `SELECT s.author_hmac, COUNT(DISTINCT s.id),
		COUNT(DISTINCT CASE WHEN v.status = 'active' THEN s.id END), ''
		FROM sets s LEFT JOIN set_versions v ON v.set_id = s.id GROUP BY s.author_hmac`, func(key string, a, b int, _ string) {
		if k := at(key); k != nil {
			k.Sets = a
			k.ListedSets = b
		}
	}); err != nil {
		return nil, err
	}
	if err := countInto(ctx, s.db, `SELECT uploader_hmac, COUNT(*), 0, '' FROM set_versions WHERE status = 'pending' GROUP BY uploader_hmac`, func(key string, a, _ int, _ string) {
		if k := at(key); k != nil {
			k.PendingVersions = a
		}
	}); err != nil {
		return nil, err
	}
	if err := countInto(ctx, s.db, `SELECT key_hmac, COUNT(*), SUM(kind <> 'upload'), MAX(received_at) FROM votes GROUP BY key_hmac`, func(key string, a, b int, last string) {
		if k := at(key); k != nil {
			k.Votes = a
			k.ManualVotes = b
			if t := parseTime(last); t.After(k.LastSeen) {
				k.LastSeen = t
			}
		}
	}); err != nil {
		return nil, err
	}
	if err := countInto(ctx, s.db, `SELECT key_hmac, COUNT(*), 0, MAX(received_at) FROM reports GROUP BY key_hmac`, func(key string, a, _ int, _ string) {
		if k := at(key); k != nil {
			k.Reports = a
		}
	}); err != nil {
		return nil, err
	}
	if err := countInto(ctx, s.db, `SELECT s.author_hmac, COUNT(*), 0, '' FROM reports r JOIN sets s ON s.id = r.set_id GROUP BY s.author_hmac`, func(key string, a, _ int, _ string) {
		if k := at(key); k != nil {
			k.ReportsAgainst = a
		}
	}); err != nil {
		return nil, err
	}
	origins, err := s.db.QueryContext(ctx, `SELECT key_hmac, asn_observed, country_observed FROM (
		SELECT key_hmac, asn_observed, country_observed, ROW_NUMBER() OVER (PARTITION BY key_hmac ORDER BY received_at DESC, id DESC) AS rn
		FROM votes WHERE asn_observed <> '') WHERE rn = 1`)
	if err != nil {
		return nil, err
	}
	defer origins.Close()
	for origins.Next() {
		var key, asn, country string
		if err := origins.Scan(&key, &asn, &country); err != nil {
			return nil, err
		}
		if k := at(key); k != nil {
			k.LastASN = asn
			k.LastCountry = country
		}
	}
	return out, origins.Err()
}

func keyMatches(k KeyRow, f KeyQuery) bool {
	switch f.Status {
	case "banned":
		if !k.Banned {
			return false
		}
	case "trusted":
		if !k.Trusted {
			return false
		}
	case "ok":
		if k.Banned || k.Trusted {
			return false
		}
	}
	switch f.Tag {
	case TagStaff, TagTest:
		if k.Tag != f.Tag {
			return false
		}
	case "none":
		if k.Tag != "" {
			return false
		}
	case "any":
		if k.Tag == "" && k.Name == "" {
			return false
		}
	}
	switch f.Sets {
	case "any":
		if k.Sets == 0 {
			return false
		}
	case "listed":
		if k.ListedSets == 0 {
			return false
		}
	case "pending":
		if k.PendingVersions == 0 {
			return false
		}
	case "none":
		if k.Sets > 0 {
			return false
		}
	}
	if !f.ActiveSince.IsZero() && k.LastSeen.Before(f.ActiveSince) {
		return false
	}
	if f.Exact != "" && k.KeyHMAC != f.Exact {
		return false
	}
	if f.Prefix != "" || f.Text != "" {
		hit := f.Prefix != "" && strings.HasPrefix(k.KeyHMAC, f.Prefix)
		if !hit && f.Text != "" {
			hit = strings.Contains(strings.ToLower(k.Name+"\n"+k.Note), f.Text)
		}
		if !hit {
			return false
		}
	}
	return true
}

func keyLess(a, b KeyRow, by string) (bool, bool) {
	switch by {
	case "first_seen":
		return a.FirstSeen.Before(b.FirstSeen), a.FirstSeen.Equal(b.FirstSeen)
	case "sets":
		return a.Sets < b.Sets, a.Sets == b.Sets
	case "listed":
		return a.ListedSets < b.ListedSets, a.ListedSets == b.ListedSets
	case "votes":
		return a.ManualVotes < b.ManualVotes, a.ManualVotes == b.ManualVotes
	case "reports":
		return a.Reports < b.Reports, a.Reports == b.Reports
	case "records":
		return a.Records < b.Records, a.Records == b.Records
	case "name":
		return strings.ToLower(a.Name) < strings.ToLower(b.Name), strings.EqualFold(a.Name, b.Name)
	}
	return a.LastSeen.Before(b.LastSeen), a.LastSeen.Equal(b.LastSeen)
}

func (s *Store) KeyList(ctx context.Context, f KeyQuery) ([]KeyRow, int, KeyCounts, error) {
	all, err := s.keyRows(ctx, ``)
	if err != nil {
		return nil, 0, KeyCounts{}, err
	}
	var counts KeyCounts
	week := time.Now().Add(-7 * 24 * time.Hour)
	matched := make([]KeyRow, 0, len(all))
	for _, k := range all {
		counts.All++
		if k.Banned {
			counts.Banned++
		}
		if k.Trusted {
			counts.Trusted++
		}
		switch k.Tag {
		case TagStaff:
			counts.Staff++
		case TagTest:
			counts.Test++
		}
		if k.Sets > 0 {
			counts.WithSets++
		}
		if k.LastSeen.After(week) {
			counts.Active7d++
		}
		if keyMatches(k, f) {
			matched = append(matched, k)
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		less, equal := keyLess(matched[i], matched[j], f.Sort)
		if equal {
			return matched[i].KeyHMAC < matched[j].KeyHMAC
		}
		if f.Desc {
			return !less
		}
		return less
	})
	total := len(matched)
	start := min(max(f.Offset, 0), total)
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	return matched[start:min(total, start+limit)], total, counts, nil
}

func (s *Store) KeyRowOf(ctx context.Context, keyHMAC string) (*KeyRow, error) {
	rows, err := s.keyRows(ctx, ` WHERE key_hmac = ?`, keyHMAC)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return &rows[0], nil
}

func (s *Store) ResolveKeyPrefix(ctx context.Context, prefix string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key_hmac FROM keys WHERE key_hmac >= ? AND key_hmac < ? ORDER BY key_hmac LIMIT 5`, prefix, prefix+"g")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	return out, rows.Err()
}

func setKeyProfileTx(ctx context.Context, q querier, keyHMAC string, p KeyProfile, now time.Time) (KeyProfile, error) {
	var before KeyProfile
	err := q.QueryRowContext(ctx, `SELECT name, note, tag FROM keys WHERE key_hmac = ?`, keyHMAC).Scan(&before.Name, &before.Note, &before.Tag)
	if err != nil {
		if errors.Is(err, errNoRows) {
			return before, ErrNotFound
		}
		return before, err
	}
	if _, err := q.ExecContext(ctx, `UPDATE keys SET name = ?, note = ?, tag = ?, profile_updated_at = ? WHERE key_hmac = ?`, p.Name, p.Note, p.Tag, formatTime(now), keyHMAC); err != nil {
		return before, err
	}
	if (before.Tag == TagTest) != (p.Tag == TagTest) {
		if err := markDirtyTx(ctx, q); err != nil {
			return before, err
		}
	}
	return before, nil
}

func (t *Tx) SetKeyProfile(ctx context.Context, keyHMAC string, p KeyProfile, now time.Time) (KeyProfile, error) {
	return setKeyProfileTx(ctx, t.tx, keyHMAC, p, now)
}

type NotableKey struct {
	KeyHMAC string
	Name    string
	Tag     string
	Banned  bool
	Trusted bool
}

func (s *Store) NotableKeys(ctx context.Context) ([]NotableKey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key_hmac, name, tag, banned, trusted FROM keys WHERE banned = 1 OR trusted = 1 OR tag <> '' OR name <> '' ORDER BY key_hmac`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]NotableKey, 0)
	for rows.Next() {
		var k NotableKey
		var banned, trusted int
		if err := rows.Scan(&k.KeyHMAC, &k.Name, &k.Tag, &banned, &trusted); err != nil {
			return nil, err
		}
		k.Banned = banned != 0
		k.Trusted = trusted != 0
		out = append(out, k)
	}
	return out, rows.Err()
}
