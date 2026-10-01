package store

import (
	"context"
	"time"

	"github.com/daniellavrushin/b4hub/internal/score"
)

type Vote struct {
	ID              int64
	RecordID        string
	SetID           string
	Version         int
	FP              string
	KeyHMAC         string
	Kind            string
	Weight          float64
	ASNObserved     string
	CountryObserved string
	ASNHint         string
	CountryHint     string
	OriginVerified  bool
	Domain          string
	B4Version       string
	Engine          string
	Bucket          int64
	ReceivedAt      time.Time
	KeyFirstSeen    time.Time
}

func (v Vote) ScoreVote() score.Vote {
	return score.Vote{
		KeyHMAC:        v.KeyHMAC,
		Kind:           v.Kind,
		Weight:         v.Weight,
		ASN:            v.ASNObserved,
		Country:        v.CountryObserved,
		OriginVerified: v.OriginVerified,
		KeyFirstSeen:   v.KeyFirstSeen,
		ReceivedAt:     v.ReceivedAt,
	}
}

func (s *Store) UpsertVote(ctx context.Context, v Vote) error {
	verified := 0
	if v.OriginVerified {
		verified = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO votes(record_id, set_id, version, fp, key_hmac, kind, weight, asn_observed, country_observed, asn_hint, country_hint, origin_verified, domain, b4_version, engine, bucket, received_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(key_hmac, fp, asn_observed, bucket) DO UPDATE SET
			record_id = excluded.record_id, set_id = excluded.set_id, version = excluded.version, kind = excluded.kind, weight = excluded.weight,
			country_observed = excluded.country_observed, asn_hint = excluded.asn_hint, country_hint = excluded.country_hint,
			origin_verified = excluded.origin_verified, domain = excluded.domain, b4_version = excluded.b4_version, engine = excluded.engine,
			received_at = excluded.received_at
		WHERE excluded.received_at >= votes.received_at`,
		v.RecordID, v.SetID, v.Version, v.FP, v.KeyHMAC, v.Kind, v.Weight, v.ASNObserved, v.CountryObserved, v.ASNHint, v.CountryHint, verified, v.Domain, v.B4Version, v.Engine, v.Bucket, formatTime(v.ReceivedAt))
	if err != nil {
		return err
	}
	return s.MarkDirty(ctx)
}

const voteColumns = `v.id, v.record_id, v.set_id, v.version, v.fp, v.key_hmac, v.kind, v.weight, v.asn_observed, v.country_observed, v.asn_hint, v.country_hint,
	v.origin_verified, v.domain, v.b4_version, v.engine, v.bucket, v.received_at, COALESCE(k.first_seen, '')`

func (s *Store) queryVotes(ctx context.Context, query string, args ...interface{}) ([]Vote, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Vote, 0)
	for rows.Next() {
		var v Vote
		var verified int
		var receivedAt, firstSeen string
		if err := rows.Scan(&v.ID, &v.RecordID, &v.SetID, &v.Version, &v.FP, &v.KeyHMAC, &v.Kind, &v.Weight, &v.ASNObserved, &v.CountryObserved, &v.ASNHint, &v.CountryHint,
			&verified, &v.Domain, &v.B4Version, &v.Engine, &v.Bucket, &receivedAt, &firstSeen); err != nil {
			return nil, err
		}
		v.OriginVerified = verified != 0
		v.ReceivedAt = parseTime(receivedAt)
		v.KeyFirstSeen = parseTime(firstSeen)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) AllVotes(ctx context.Context) ([]Vote, error) {
	return s.queryVotes(ctx, `SELECT `+voteColumns+` FROM votes v LEFT JOIN keys k ON k.key_hmac = v.key_hmac ORDER BY v.received_at`)
}

func (s *Store) VotesByFP(ctx context.Context) (map[string][]Vote, error) {
	votes, err := s.AllVotes(ctx)
	if err != nil {
		return nil, err
	}
	return groupByFP(votes), nil
}

func groupByFP(votes []Vote) map[string][]Vote {
	out := make(map[string][]Vote)
	for _, v := range votes {
		out[v.FP] = append(out[v.FP], v)
	}
	return out
}

func (s *Store) ScoringVotesByFP(ctx context.Context) (map[string][]Vote, error) {
	votes, err := s.queryVotes(ctx, `SELECT `+voteColumns+` FROM votes v LEFT JOIN keys k ON k.key_hmac = v.key_hmac
		WHERE COALESCE(k.banned, 0) = 0 AND COALESCE(k.tag, '') <> 'test' ORDER BY v.received_at`)
	if err != nil {
		return nil, err
	}
	return groupByFP(votes), nil
}

func (s *Store) VotesForVersion(ctx context.Context, setID string, version int) ([]Vote, error) {
	return s.queryVotes(ctx, `SELECT `+voteColumns+` FROM votes v LEFT JOIN keys k ON k.key_hmac = v.key_hmac WHERE v.set_id = ? AND v.version = ? ORDER BY v.received_at DESC`, setID, version)
}

func (s *Store) VotesForFP(ctx context.Context, fp string) ([]Vote, error) {
	return s.queryVotes(ctx, `SELECT `+voteColumns+` FROM votes v LEFT JOIN keys k ON k.key_hmac = v.key_hmac WHERE v.fp = ? ORDER BY v.received_at DESC`, fp)
}

func (s *Store) RecentVotes(ctx context.Context, limit int) ([]Vote, error) {
	return s.queryVotes(ctx, `SELECT `+voteColumns+` FROM votes v LEFT JOIN keys k ON k.key_hmac = v.key_hmac ORDER BY v.received_at DESC, v.id DESC LIMIT ?`, limit)
}

type VoteTotals struct {
	Works  int
	Broken int
}

func (s *Store) VoteTotals(ctx context.Context) (map[string]map[int]VoteTotals, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT set_id, version, kind, COUNT(*) FROM votes GROUP BY set_id, version, kind`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]map[int]VoteTotals)
	for rows.Next() {
		var setID, kind string
		var version, count int
		if err := rows.Scan(&setID, &version, &kind, &count); err != nil {
			return nil, err
		}
		if out[setID] == nil {
			out[setID] = make(map[int]VoteTotals)
		}
		totals := out[setID][version]
		switch weight := score.Weights[kind]; {
		case weight > 0:
			totals.Works += count
		case weight < 0:
			totals.Broken += count
		}
		out[setID][version] = totals
	}
	return out, rows.Err()
}

func (s *Store) CountVotes(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM votes`).Scan(&n)
	return n, err
}
