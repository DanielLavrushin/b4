package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	metaSchemaVersion = "schema_version"
	metaEpoch         = "epoch"
	metaSeq           = "seq"
	metaDirty         = "dirty"
	metaBuiltAt       = "built_at"

	timeLayout = time.RFC3339
)

var ErrNotFound = errors.New("not found")

const keptBackups = 2

type Store struct {
	db       *sql.DB
	path     string
	settings settingsCache
}

func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?" + url.Values{
		"_pragma": []string{"journal_mode(WAL)", "busy_timeout(5000)", "foreign_keys(1)", "synchronous(NORMAL)"},
	}.Encode()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, path: path}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	var hasMeta int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'meta'`).Scan(&hasMeta); err != nil {
		return err
	}
	current := 0
	if hasMeta > 0 {
		raw, err := s.Meta(ctx, metaSchemaVersion)
		if err != nil {
			return err
		}
		if raw != "" {
			current, err = strconv.Atoi(raw)
			if err != nil {
				return fmt.Errorf("meta %s holds %q", metaSchemaVersion, raw)
			}
		}
	}
	if current > 0 && current < len(migrations) {
		if err := s.backup(ctx, current); err != nil {
			return fmt.Errorf("backup before migration %d: %w", current+1, err)
		}
	}
	for i := current; i < len(migrations); i++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, metaSchemaVersion, strconv.Itoa(i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func backupPath(dbPath string, schema int) string {
	return dbPath + ".v" + strconv.Itoa(schema) + ".bak"
}

func (s *Store) backup(ctx context.Context, schema int) error {
	if s.path == "" || strings.HasPrefix(s.path, ":memory:") {
		return nil
	}
	target := backupPath(s.path, schema)
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, target); err != nil {
		return err
	}
	return pruneBackups(s.path, keptBackups)
}

func pruneBackups(dbPath string, keep int) error {
	matches, err := filepath.Glob(dbPath + ".v*.bak")
	if err != nil {
		return err
	}
	type backup struct {
		path   string
		schema int
	}
	var backups []backup
	for _, m := range matches {
		raw := strings.TrimSuffix(strings.TrimPrefix(m, dbPath+".v"), ".bak")
		n, err := strconv.Atoi(raw)
		if err != nil {
			continue
		}
		backups = append(backups, backup{path: m, schema: n})
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i].schema > backups[j].schema })
	for i := keep; i < len(backups); i++ {
		if err := os.Remove(backups[i].path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	raw, err := s.Meta(ctx, metaSchemaVersion)
	if err != nil || raw == "" {
		return 0, err
	}
	return strconv.Atoi(raw)
}

func (s *Store) Meta(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func setMetaTx(ctx context.Context, tx *sql.Tx, key, value string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func metaTx(ctx context.Context, tx *sql.Tx, key string) (string, error) {
	var value string
	err := tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func (s *Store) Epoch(ctx context.Context) (int64, error) {
	raw, err := s.Meta(ctx, metaEpoch)
	if err != nil || raw == "" {
		return 0, err
	}
	return strconv.ParseInt(raw, 10, 64)
}

func (s *Store) NewEpoch(ctx context.Context, now time.Time) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	raw, err := metaTx(ctx, tx, metaEpoch)
	if err != nil {
		return 0, err
	}
	previous, _ := strconv.ParseInt(raw, 10, 64)
	epoch := now.Unix()
	if epoch <= previous {
		epoch = previous + 1
	}
	if err := setMetaTx(ctx, tx, metaEpoch, strconv.FormatInt(epoch, 10)); err != nil {
		return 0, err
	}
	if err := setMetaTx(ctx, tx, metaSeq, "0"); err != nil {
		return 0, err
	}
	if err := setMetaTx(ctx, tx, metaDirty, "1"); err != nil {
		return 0, err
	}
	return epoch, tx.Commit()
}

func (s *Store) NextSeq(ctx context.Context, now time.Time) (epoch, seq int64, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	rawEpoch, err := metaTx(ctx, tx, metaEpoch)
	if err != nil {
		return 0, 0, err
	}
	epoch, _ = strconv.ParseInt(rawEpoch, 10, 64)
	if epoch == 0 {
		epoch = now.Unix()
		if err := setMetaTx(ctx, tx, metaEpoch, strconv.FormatInt(epoch, 10)); err != nil {
			return 0, 0, err
		}
	}
	rawSeq, err := metaTx(ctx, tx, metaSeq)
	if err != nil {
		return 0, 0, err
	}
	seq, _ = strconv.ParseInt(rawSeq, 10, 64)
	seq++
	if err := setMetaTx(ctx, tx, metaSeq, strconv.FormatInt(seq, 10)); err != nil {
		return 0, 0, err
	}
	return epoch, seq, tx.Commit()
}

func (s *Store) MarkDirty(ctx context.Context) error {
	return s.SetMeta(ctx, metaDirty, "1")
}

func (s *Store) Dirty(ctx context.Context) (bool, error) {
	raw, err := s.Meta(ctx, metaDirty)
	return raw == "1", err
}

func (s *Store) MarkBuilt(ctx context.Context, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := setMetaTx(ctx, tx, metaDirty, "0"); err != nil {
		return err
	}
	if err := setMetaTx(ctx, tx, metaBuiltAt, formatTime(now)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) BuiltAt(ctx context.Context) (time.Time, error) {
	raw, err := s.Meta(ctx, metaBuiltAt)
	if err != nil || raw == "" {
		return time.Time{}, err
	}
	return parseTime(raw), nil
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(timeLayout)
}

func parseTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	t, err := time.Parse(timeLayout, raw)
	if err != nil {
		return time.Time{}
	}
	return t
}
