package store

import (
	"context"

	"github.com/daniellavrushin/b4/hubwire"
)

const groupVoteColumns = `v.id, v.record_id, v.group_id, v.version, v.gfp, v.key_hmac, v.kind, v.weight, v.asn_observed, v.country_observed, v.asn_hint, v.country_hint,
	v.origin_verified, v.domain, v.b4_version, v.engine, v.bucket, v.received_at, COALESCE(k.first_seen, ''),
	CASE WHEN (v.gfp, v.key_hmac) IN (SELECT gv.gfp, g.author_hmac FROM group_versions gv JOIN groups g ON g.id = gv.group_id
		WHERE gv.status <> '` + hubwire.SetStatusRejected + `' AND gv.gfp <> '')
	OR (v.group_id, v.key_hmac) IN (SELECT id, author_hmac FROM groups) THEN 1 ELSE 0 END`

func (s *Store) UpsertGroupVote(ctx context.Context, v Vote) error {
	return upsertGroupVoteTx(ctx, s.db, v)
}

func (t *Tx) UpsertGroupVote(ctx context.Context, v Vote) error {
	return upsertGroupVoteTx(ctx, t.tx, v)
}

func upsertGroupVoteTx(ctx context.Context, q querier, v Vote) error {
	verified := 0
	if v.OriginVerified {
		verified = 1
	}
	_, err := q.ExecContext(ctx, `INSERT INTO group_votes(record_id, group_id, version, gfp, key_hmac, kind, weight, asn_observed, country_observed, asn_hint, country_hint, origin_verified, domain, b4_version, engine, bucket, received_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(key_hmac, gfp, asn_observed, bucket) DO UPDATE SET
			record_id = excluded.record_id, group_id = excluded.group_id, version = excluded.version, kind = excluded.kind, weight = excluded.weight,
			country_observed = excluded.country_observed, asn_hint = excluded.asn_hint, country_hint = excluded.country_hint,
			origin_verified = excluded.origin_verified, domain = excluded.domain, b4_version = excluded.b4_version, engine = excluded.engine,
			received_at = excluded.received_at
		WHERE excluded.received_at >= group_votes.received_at`,
		v.RecordID, v.SetID, v.Version, v.FP, v.KeyHMAC, v.Kind, v.Weight, v.ASNObserved, v.CountryObserved, v.ASNHint, v.CountryHint, verified, v.Domain, v.B4Version, v.Engine, v.Bucket, formatTime(v.ReceivedAt))
	if err != nil {
		return err
	}
	return markDirtyTx(ctx, q)
}

func (s *Store) ScoringGroupVotesByGFP(ctx context.Context) (map[string][]Vote, error) {
	votes, err := s.queryVotes(ctx, `SELECT `+groupVoteColumns+` FROM group_votes v LEFT JOIN keys k ON k.key_hmac = v.key_hmac
		WHERE COALESCE(k.banned, 0) = 0 AND COALESCE(k.tag, '') <> 'test' ORDER BY v.received_at`)
	if err != nil {
		return nil, err
	}
	return groupByFP(votes), nil
}
