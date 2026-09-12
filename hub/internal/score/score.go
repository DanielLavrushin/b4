package score

import (
	"math"
	"sort"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
)

const (
	KindManualWorks          = "manual_works"
	KindManualBroken         = "manual_broken"
	KindUpload               = "upload"
	KindDetectorFixed        = "detector_fixed"
	KindDetectorBrokenByB4   = "detector_broken_by_b4"
	KindDetectorStillBlocked = "detector_still_blocked"
	KindWatchdogVerified     = "watchdog_verified"
	KindWatchdogDegraded     = "watchdog_degraded"
	KindDiscoveryConfirmed   = "discovery_confirmed"
	KindDiscoveryFailed      = "discovery_failed"
)

var Weights = map[string]float64{
	KindManualWorks:          1.0,
	KindManualBroken:         -1.0,
	KindUpload:               1.0,
	KindDetectorFixed:        0.8,
	KindDetectorBrokenByB4:   -1.0,
	KindDetectorStillBlocked: -0.6,
	KindWatchdogVerified:     0.7,
	KindWatchdogDegraded:     -0.5,
	KindDiscoveryConfirmed:   0.4,
	KindDiscoveryFailed:      -0.2,
}

const (
	HalfLife                   = 14 * 24 * time.Hour
	YoungKeyAge                = 7 * 24 * time.Hour
	YoungKeyMultiplier         = 0.25
	UnverifiedOriginMultiplier = 0.25
	Bucket                     = 7 * 24 * time.Hour
	MaxASNCells                = 32
	DemoteScoreBelow           = 0.3
	DemoteMinN                 = 3.0
	DemoteIdle                 = 60 * 24 * time.Hour
	DemoteIdleNBelow           = 0.25

	ReasonLowScore = "low_score"
	ReasonStale    = "stale"
)

type Vote struct {
	KeyHMAC        string
	Kind           string
	Weight         float64
	ASN            string
	Country        string
	OriginVerified bool
	KeyFirstSeen   time.Time
	ReceivedAt     time.Time
}

func IsHuman(kind string) bool {
	switch kind {
	case KindManualWorks, KindManualBroken, KindUpload:
		return true
	}
	return false
}

func BucketOf(t time.Time) int64 {
	return t.Unix() / int64(Bucket/time.Second)
}

func EffectiveWeight(v Vote, now time.Time) float64 {
	w := v.Weight
	if !v.OriginVerified {
		w *= UnverifiedOriginMultiplier
	}
	if !v.KeyFirstSeen.IsZero() && now.Sub(v.KeyFirstSeen) < YoungKeyAge {
		w *= YoungKeyMultiplier
	}
	age := now.Sub(v.ReceivedAt)
	if age < 0 {
		age = 0
	}
	return w * math.Pow(0.5, age.Hours()/HalfLife.Hours())
}

type cell struct {
	votes []Vote
}

func (c *cell) fold(now time.Time) hubwire.Score {
	var positive, negative, humanMass float64
	devices := make(map[string]struct{})
	var newest time.Time
	add := func(v Vote, w float64) {
		if w >= 0 {
			positive += w
		} else {
			negative += -w
		}
		devices[v.KeyHMAC] = struct{}{}
		if v.ReceivedAt.After(newest) {
			newest = v.ReceivedAt
		}
	}
	automated := make([]Vote, 0)
	for _, v := range c.votes {
		if !IsHuman(v.Kind) {
			automated = append(automated, v)
			continue
		}
		w := EffectiveWeight(v, now)
		add(v, w)
		humanMass += math.Abs(w)
	}
	sort.SliceStable(automated, func(i, j int) bool { return automated[i].ReceivedAt.After(automated[j].ReceivedAt) })
	var automatedMass float64
	for _, v := range automated {
		w := EffectiveWeight(v, now)
		if automatedMass+math.Abs(w) > humanMass {
			break
		}
		add(v, w)
		automatedMass += math.Abs(w)
	}
	total := positive + negative
	s := hubwire.Score{
		Score:   round((positive+1)/(total+2), 4),
		N:       round(total, 4),
		Devices: len(devices),
	}
	if !newest.IsZero() {
		s.Newest = DayStamp(newest)
	}
	return s
}

func DayStamp(t time.Time) string {
	return t.UTC().Truncate(24 * time.Hour).Format(time.RFC3339)
}

func round(v float64, places int) float64 {
	factor := math.Pow(10, float64(places))
	return math.Round(v*factor) / factor
}

func Aggregate(votes []Vote, now time.Time) hubwire.Scores {
	global := &cell{}
	byASN := make(map[string]*cell)
	byCC := make(map[string]*cell)
	for _, v := range votes {
		global.votes = append(global.votes, v)
		if !v.OriginVerified {
			continue
		}
		if v.ASN != "" {
			c := byASN[v.ASN]
			if c == nil {
				c = &cell{}
				byASN[v.ASN] = c
			}
			c.votes = append(c.votes, v)
		}
		if v.Country != "" {
			cc := normaliseCountry(v.Country)
			c := byCC[cc]
			if c == nil {
				c = &cell{}
				byCC[cc] = c
			}
			c.votes = append(c.votes, v)
		}
	}
	out := hubwire.Scores{Global: global.fold(now)}
	if len(byASN) > 0 {
		out.ASN = make(map[string]hubwire.Score, len(byASN))
		for asn, c := range byASN {
			out.ASN[asn] = c.fold(now)
		}
		out.ASN = topByMass(out.ASN, MaxASNCells)
	}
	if len(byCC) > 0 {
		out.CC = make(map[string]hubwire.Score, len(byCC))
		for cc, c := range byCC {
			out.CC[cc] = c.fold(now)
		}
	}
	return out
}

func topByMass(cells map[string]hubwire.Score, limit int) map[string]hubwire.Score {
	if len(cells) <= limit {
		return cells
	}
	keys := make([]string, 0, len(cells))
	for k := range cells {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if cells[keys[i]].N != cells[keys[j]].N {
			return cells[keys[i]].N > cells[keys[j]].N
		}
		return keys[i] < keys[j]
	})
	kept := make(map[string]hubwire.Score, limit)
	for _, k := range keys[:limit] {
		kept[k] = cells[k]
	}
	return kept
}

func normaliseCountry(cc string) string {
	out := make([]byte, 0, len(cc))
	for i := 0; i < len(cc); i++ {
		c := cc[i]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		out = append(out, c)
	}
	return string(out)
}

func Newest(votes []Vote) time.Time {
	var newest time.Time
	for _, v := range votes {
		if v.ReceivedAt.After(newest) {
			newest = v.ReceivedAt
		}
	}
	return newest
}

func Demotion(global hubwire.Score, lastActivity time.Time, now time.Time) (string, bool) {
	if global.N >= DemoteMinN && global.Score < DemoteScoreBelow {
		return ReasonLowScore, true
	}
	if !lastActivity.IsZero() && now.Sub(lastActivity) > DemoteIdle && global.N < DemoteIdleNBelow {
		return ReasonStale, true
	}
	return "", false
}
