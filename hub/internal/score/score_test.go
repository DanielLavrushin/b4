package score

import (
	"math"
	"testing"
	"time"
)

var now = time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)

func oldKey() time.Time { return now.Add(-30 * 24 * time.Hour) }

func near(a, b float64) bool { return math.Abs(a-b) < 1e-4 }

func TestEffectiveWeightAppliesEveryMultiplier(t *testing.T) {
	base := Vote{Kind: KindManualWorks, Weight: 1, OriginVerified: true, KeyFirstSeen: oldKey(), ReceivedAt: now}
	if w := EffectiveWeight(base, now); !near(w, 1) {
		t.Errorf("fresh verified vote from an old key must weigh 1, got %v", w)
	}
	unverified := base
	unverified.OriginVerified = false
	if w := EffectiveWeight(unverified, now); !near(w, 0.25) {
		t.Errorf("unverified origin must weigh 0.25, got %v", w)
	}
	young := base
	young.KeyFirstSeen = now.Add(-24 * time.Hour)
	if w := EffectiveWeight(young, now); !near(w, 0.25) {
		t.Errorf("young key must weigh 0.25, got %v", w)
	}
	aged := base
	aged.ReceivedAt = now.Add(-HalfLife)
	if w := EffectiveWeight(aged, now); !near(w, 0.5) {
		t.Errorf("a vote one half-life old must weigh 0.5, got %v", w)
	}
	broken := base
	broken.Kind = KindManualBroken
	broken.Weight = -1
	if w := EffectiveWeight(broken, now); !near(w, -1) {
		t.Errorf("broken must weigh -1, got %v", w)
	}
}

func TestAggregateCellsAndPrior(t *testing.T) {
	votes := []Vote{
		{KeyHMAC: "a", Kind: KindUpload, Weight: 1, ASN: "12345", Country: "ru", OriginVerified: true, KeyFirstSeen: oldKey(), ReceivedAt: now},
		{KeyHMAC: "b", Kind: KindManualWorks, Weight: 1, ASN: "12345", Country: "RU", OriginVerified: true, KeyFirstSeen: oldKey(), ReceivedAt: now},
		{KeyHMAC: "c", Kind: KindManualBroken, Weight: -1, ASN: "999", Country: "DE", OriginVerified: true, KeyFirstSeen: oldKey(), ReceivedAt: now},
		{KeyHMAC: "d", Kind: KindManualWorks, Weight: 1, OriginVerified: false, KeyFirstSeen: oldKey(), ReceivedAt: now},
	}
	scores := Aggregate(votes, now)
	if !near(scores.Global.Score, (2.25+1)/(2.25+1+2)) {
		t.Errorf("global score: got %v", scores.Global.Score)
	}
	if scores.Global.Devices != 4 || !near(scores.Global.N, 3.25) {
		t.Errorf("global n/devices: got %+v", scores.Global)
	}
	asn := scores.ASN["12345"]
	if !near(asn.Score, 3.0/4.0) || asn.Devices != 2 || !near(asn.N, 2) {
		t.Errorf("asn cell: got %+v", asn)
	}
	if _, ok := scores.ASN[""]; ok {
		t.Errorf("unverified votes must not create an asn cell")
	}
	ru := scores.CC["RU"]
	if ru.Devices != 2 {
		t.Errorf("country codes must be merged case-insensitively, got %+v", scores.CC)
	}
	if one := Aggregate(votes[2:3], now).Global; !near(one.Score, 1.0/3.0) {
		t.Errorf("one broken vote must score 1/3, got %v", one.Score)
	}
	if scores.Global.Newest != "2026-09-12T00:00:00Z" {
		t.Errorf("newest must be published at day granularity, got %q", scores.Global.Newest)
	}
}

func TestAutomatedVotesCappedAtHalf(t *testing.T) {
	votes := []Vote{
		{KeyHMAC: "a", Kind: KindManualWorks, Weight: 1, OriginVerified: true, KeyFirstSeen: oldKey(), ReceivedAt: now},
		{KeyHMAC: "x", Kind: KindWatchdogVerified, Weight: 0.7, OriginVerified: true, KeyFirstSeen: oldKey(), ReceivedAt: now},
		{KeyHMAC: "y", Kind: KindWatchdogVerified, Weight: 0.7, OriginVerified: true, KeyFirstSeen: oldKey(), ReceivedAt: now.Add(-time.Hour)},
	}
	s := Aggregate(votes, now).Global
	if !near(s.N, 1.7) || s.Devices != 2 {
		t.Errorf("automated mass must stop at the human mass, got %+v", s)
	}
	only := Aggregate(votes[1:], now).Global
	if only.N != 0 || only.Devices != 0 {
		t.Errorf("automated votes alone must not count, got %+v", only)
	}
}

func TestACellOfOneDeviceIsNotPublished(t *testing.T) {
	vote := func(key, asn, cc string, age time.Duration) Vote {
		return Vote{KeyHMAC: key, Kind: KindManualWorks, Weight: 1, ASN: asn, Country: cc, OriginVerified: true, KeyFirstSeen: oldKey(), ReceivedAt: now.Add(-age)}
	}
	week := 7 * 24 * time.Hour
	scores := Aggregate([]Vote{
		vote("alone", "100", "RU", 0),
		vote("alone", "100", "RU", week),
		vote("alone", "100", "RU", 2*week),
		vote("a", "200", "RU", 0),
		vote("b", "200", "RU", 0),
		vote("c", "300", "KZ", 0),
	}, now)
	if _, ok := scores.ASN["100"]; ok {
		t.Errorf("several votes of one device are still one device, got %+v", scores.ASN)
	}
	if _, ok := scores.ASN["300"]; ok {
		t.Errorf("a provider with one device must not be listed, got %+v", scores.ASN)
	}
	if scores.ASN["200"].Devices != 2 {
		t.Errorf("a provider with two devices must be listed, got %+v", scores.ASN)
	}
	if _, ok := scores.CC["KZ"]; ok || scores.CC["RU"].Devices != 3 {
		t.Errorf("a country with one device must not be listed, got %+v", scores.CC)
	}
	if scores.Global.Devices != 4 {
		t.Errorf("the overall score counts every device, got %+v", scores.Global)
	}
	if one := Aggregate([]Vote{vote("c", "300", "KZ", 0)}, now); one.ASN != nil || one.CC != nil || one.Global.Devices != 1 {
		t.Errorf("a set with one device has only its overall score, got %+v", one)
	}
}

func TestOneDeviceCellsDoNotPushOutListedOnes(t *testing.T) {
	votes := make([]Vote, 0, 80)
	for i := 0; i < 40; i++ {
		asn := "1" + string(rune('A'+i))
		for week := 0; week < 4; week++ {
			votes = append(votes, Vote{KeyHMAC: asn, Kind: KindManualWorks, Weight: 1, ASN: asn, OriginVerified: true, KeyFirstSeen: oldKey(), ReceivedAt: now.Add(-time.Duration(week) * Bucket)})
		}
	}
	for _, key := range []string{"a", "b"} {
		votes = append(votes, Vote{KeyHMAC: key, Kind: KindManualWorks, Weight: 1, ASN: "200", OriginVerified: true, KeyFirstSeen: oldKey(), ReceivedAt: now.Add(-HalfLife)})
	}
	if scores := Aggregate(votes, now); len(scores.ASN) != 1 || scores.ASN["200"].Devices != 2 {
		t.Errorf("the cap must count only cells that can be listed, got %+v", scores.ASN)
	}
}

func TestTopByMassKeepsThirtyTwo(t *testing.T) {
	votes := make([]Vote, 0, 80)
	for i := 0; i < 40; i++ {
		asn := string(rune('A' + i))
		for _, key := range []string{asn, asn + "2"} {
			votes = append(votes, Vote{KeyHMAC: key, Kind: KindManualWorks, Weight: 1, ASN: asn, OriginVerified: true, KeyFirstSeen: oldKey(), ReceivedAt: now.Add(-time.Duration(i) * 24 * time.Hour)})
		}
	}
	scores := Aggregate(votes, now)
	if len(scores.ASN) != MaxASNCells {
		t.Errorf("expected %d asn cells, got %d", MaxASNCells, len(scores.ASN))
	}
	if _, ok := scores.ASN["A"]; !ok {
		t.Errorf("the heaviest cell must survive")
	}
}

func TestDemotion(t *testing.T) {
	votes := []Vote{
		{KeyHMAC: "a", Kind: KindManualBroken, Weight: -1, OriginVerified: true, KeyFirstSeen: oldKey(), ReceivedAt: now},
		{KeyHMAC: "b", Kind: KindManualBroken, Weight: -1, OriginVerified: true, KeyFirstSeen: oldKey(), ReceivedAt: now},
		{KeyHMAC: "c", Kind: KindManualBroken, Weight: -1, OriginVerified: true, KeyFirstSeen: oldKey(), ReceivedAt: now},
	}
	s := Aggregate(votes, now).Global
	if reason, ok := Demotion(s, now, now); !ok || reason != ReasonLowScore {
		t.Errorf("three broken votes must demote, got %v %v", reason, ok)
	}
	stale := []Vote{{KeyHMAC: "a", Kind: KindUpload, Weight: 1, OriginVerified: true, KeyFirstSeen: oldKey(), ReceivedAt: now.Add(-70 * 24 * time.Hour)}}
	st := Aggregate(stale, now).Global
	if reason, ok := Demotion(st, Newest(stale), now); !ok || reason != ReasonStale {
		t.Errorf("a set untouched for 70 days with decayed n %v must demote, got %v %v", st.N, reason, ok)
	}
	fresh := []Vote{{KeyHMAC: "a", Kind: KindUpload, Weight: 1, OriginVerified: true, KeyFirstSeen: oldKey(), ReceivedAt: now}}
	if _, ok := Demotion(Aggregate(fresh, now).Global, now, now); ok {
		t.Errorf("a fresh upload must not demote")
	}
}

func TestBucketOfIsSevenDays(t *testing.T) {
	if BucketOf(now) != BucketOf(now.Add(6*24*time.Hour)) && BucketOf(now) == BucketOf(now.Add(8*24*time.Hour)) {
		t.Errorf("bucket boundaries are not weekly")
	}
	if BucketOf(now) == BucketOf(now.Add(14*24*time.Hour)) {
		t.Errorf("two weeks apart must be different buckets")
	}
}
