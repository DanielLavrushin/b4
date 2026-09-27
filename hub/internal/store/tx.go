package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Tx struct {
	tx *sql.Tx
}

func (s *Store) Update(ctx context.Context, fn func(*Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(&Tx{tx: tx}); err != nil {
		return err
	}
	return tx.Commit()
}

func (t *Tx) Set(ctx context.Context, id string) (*Set, error) {
	return getSet(ctx, t.tx, id)
}

func (t *Tx) Versions(ctx context.Context, setID string) ([]Version, error) {
	return setVersions(ctx, t.tx, setID)
}

func (t *Tx) Version(ctx context.Context, setID string, version int) (*Version, error) {
	return getVersion(ctx, t.tx, setID, version)
}

func (t *Tx) SetStatus(ctx context.Context, setID string, version int, status, reason string, now time.Time) error {
	return setStatusTx(ctx, t.tx, setID, version, status, reason, now)
}

func (t *Tx) Restore(ctx context.Context, setID string, version int, now time.Time) (string, error) {
	return restoreTx(ctx, t.tx, setID, version, now)
}

func (t *Tx) WithdrawSet(ctx context.Context, setID, reason string, now time.Time) error {
	return withdrawSetTx(ctx, t.tx, setID, reason, now)
}

func (t *Tx) ReinstateSet(ctx context.Context, setID string) error {
	return reinstateSetTx(ctx, t.tx, setID)
}

func (t *Tx) DeleteSet(ctx context.Context, setID string) ([]string, error) {
	return deleteSetTx(ctx, t.tx, setID)
}

func (t *Tx) Key(ctx context.Context, keyHMAC string) (*Key, error) {
	return getKey(ctx, t.tx, keyHMAC)
}

func (t *Tx) BanKey(ctx context.Context, keyHMAC, reason string, now time.Time) error {
	return banKeyTx(ctx, t.tx, keyHMAC, reason, now)
}

func (t *Tx) UnbanKey(ctx context.Context, keyHMAC string) error {
	return unbanKeyTx(ctx, t.tx, keyHMAC)
}

func (t *Tx) TrustKey(ctx context.Context, keyHMAC string, now time.Time) error {
	return trustKeyTx(ctx, t.tx, keyHMAC, now)
}

func (t *Tx) UntrustKey(ctx context.Context, keyHMAC string) error {
	return untrustKeyTx(ctx, t.tx, keyHMAC)
}

func (t *Tx) Mirror(ctx context.Context, id int64) (*Mirror, error) {
	return getMirror(ctx, t.tx, id)
}

func (t *Tx) SetMirrorStatus(ctx context.Context, id int64, status, reason string) error {
	return setMirrorStatusTx(ctx, t.tx, id, status, reason)
}

func (t *Tx) DeleteMirror(ctx context.Context, id int64) error {
	return deleteMirrorTx(ctx, t.tx, id)
}

func (t *Tx) RevokeKey(ctx context.Context, keyID string) (bool, error) {
	return revokeKeyTx(ctx, t.tx, keyID)
}

func (t *Tx) NewEpoch(ctx context.Context, now time.Time) (int64, error) {
	return newEpochTx(ctx, t.tx, now)
}

func (t *Tx) MarkDirty(ctx context.Context) error {
	return markDirtyTx(ctx, t.tx)
}

func (t *Tx) ResolveVersionReports(ctx context.Context, setID string, version int, state, resolution, note string, now time.Time) (int, error) {
	return resolveVersionReportsTx(ctx, t.tx, setID, version, state, resolution, note, now)
}

func (t *Tx) Reports(ctx context.Context, ids []int64) ([]Report, error) {
	return reportsByID(ctx, t.tx, ids)
}

func (t *Tx) SetReportState(ctx context.Context, id int64, state, resolution, note string, now time.Time) error {
	return setReportStateTx(ctx, t.tx, id, state, resolution, note, now)
}

func (t *Tx) Audit(ctx context.Context, e AuditEntry) (int64, error) {
	return auditTx(ctx, t.tx, e)
}

var errRollback = errors.New("rollback")

func (s *Store) View(ctx context.Context, fn func(*Tx) error) error {
	err := s.Update(ctx, func(t *Tx) error {
		if err := fn(t); err != nil {
			return err
		}
		return errRollback
	})
	if errors.Is(err, errRollback) {
		return nil
	}
	return err
}
