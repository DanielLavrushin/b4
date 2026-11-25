package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestVoteOriginsCoverEveryVote(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	if err := st.UpsertASNName(ctx, "100", "HUNDRED-NET, DE", "DE", testNow); err != nil {
		t.Fatal(err)
	}
	votes := []struct {
		key, fp, asn, country string
	}{
		{"a", "fp1", "100", "DE"},
		{"b", "fp1", "100", "DE"},
		{"c", "fp1", "100", "NL"},
		{"d", "fp1", "200", "KZ"},
		{"e", "fp1", "200", "RU"},
		{"f", "fp1", "300", ""},
		{"g", "fp1", "50", "RU"},
		{"g", "fp2", "50", "RU"},
		{"g", "fp3", "50", "RU"},
		{"h", "fp1", "", "RU"},
	}
	for _, v := range votes {
		if err := st.UpsertVote(ctx, Vote{SetID: "set", Version: 1, FP: v.fp, KeyHMAC: v.key, Kind: "works", Weight: 1, ASNObserved: v.asn, CountryObserved: v.country, Bucket: 1, ReceivedAt: testNow}); err != nil {
			t.Fatal(err)
		}
	}
	tagKey(t, st, "h", TagTest)

	asns, err := st.VoteASNs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wantASNs := []MixRow{
		{Key: "100", Name: "HUNDRED-NET, DE", Country: "DE", Votes: 3, Keys: 3},
		{Key: "50", Country: "RU", Votes: 3, Keys: 1},
		{Key: "200", Country: "KZ", Votes: 2, Keys: 2},
		{Key: "300", Votes: 1, Keys: 1},
	}
	if !reflect.DeepEqual(asns, wantASNs) {
		t.Fatalf("every observed ASN with its most common country, by votes then key:\n got %+v\nwant %+v", asns, wantASNs)
	}
	seen, err := st.VoteASNCountries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wantSeen := map[string][]string{"100": {"DE", "NL"}, "200": {"KZ", "RU"}, "50": {"RU"}}
	if !reflect.DeepEqual(seen, wantSeen) {
		t.Fatalf("every known country of each ASN, by votes then code:\n got %v\nwant %v", seen, wantSeen)
	}

	countries, err := st.VoteCountries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wantCountries := []MixRow{
		{Key: "RU", Votes: 5, Keys: 3},
		{Key: "DE", Votes: 2, Keys: 2},
		{Key: "KZ", Votes: 1, Keys: 1},
		{Key: "NL", Votes: 1, Keys: 1},
	}
	if !reflect.DeepEqual(countries, wantCountries) {
		t.Fatalf("every observed country, test keys included:\n got %+v\nwant %+v", countries, wantCountries)
	}
}

func TestTheLastNetworkOfAKeyOutlivesItsVotes(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	days := func(n int) time.Time { return testNow.Add(-time.Duration(n) * 24 * time.Hour) }
	vote := func(key, asn, country string, at time.Time) {
		t.Helper()
		if err := st.UpsertVote(ctx, Vote{SetID: "set", Version: 1, FP: "fp", KeyHMAC: key, Kind: "manual_works", Weight: 1, ASNObserved: asn, CountryObserved: country, Bucket: at.Unix(), ReceivedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	share := func(id, key, asn, country string, at time.Time) {
		t.Helper()
		v := sampleVersion("fp-"+id, "targets-"+id)
		v.UploaderHMAC, v.ASNObserved, v.CountryObserved, v.CreatedAt = key, asn, country, at
		if err := st.CreateSet(ctx, Set{ID: id, AuthorHMAC: key, CreatedAt: at, UpdatedAt: at}, v); err != nil {
			t.Fatal(err)
		}
		vote(key, asn, country, at)
	}
	complain := func(key, asn string, at time.Time) {
		t.Helper()
		if err := st.InsertReport(ctx, Report{RecordID: key + at.String(), SetID: "set", Version: 1, KeyHMAC: key, ASNObserved: asn, Reason: "reason", ReceivedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	share("published", "published", "100", "RU", days(400))
	complain("complained", "100", days(400))
	vote("complained", "100", "RU", days(401))
	vote("roaming", "200", "NL", days(3))
	vote("roaming", "200", "DE", days(2))
	complain("roaming", "200", days(1))
	share("moved", "moved", "100", "RU", days(5))
	vote("moved", "300", "KZ", days(4))
	vote("reported", "100", "RU", days(5))
	complain("reported", "300", days(4))
	vote("unknown", "100", "RU", days(5))
	vote("unknown", "", "", days(4))
	for _, key := range []string{"published", "complained", "roaming", "moved", "reported", "unknown"} {
		if _, _, err := st.TouchKey(ctx, key, days(500)); err != nil {
			t.Fatal(err)
		}
	}
	networks := func() map[string]string {
		t.Helper()
		rows, _, _, err := st.KeyList(ctx, KeyQuery{Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, k := range rows {
			out[k.KeyHMAC] = k.LastASN + " " + k.LastCountry
		}
		return out
	}
	want := map[string]string{"published": "100 RU", "complained": "100 RU", "roaming": "200 DE", "moved": "300 KZ", "reported": "300 ", "unknown": "100 RU"}
	if got := networks(); !reflect.DeepEqual(got, want) {
		t.Fatalf("the newest vote, publication or complaint names the network:\n got %v\nwant %v", got, want)
	}
	if n, err := st.PruneVotes(ctx, testNow, 365*24*time.Hour); err != nil || n != 2 {
		t.Fatalf("two votes are older than a year, got %d %v", n, err)
	}
	want["complained"] = "100 "
	if got := networks(); !reflect.DeepEqual(got, want) {
		t.Fatalf("a key without votes keeps the network of its other records:\n got %v\nwant %v", got, want)
	}
}

func TestFindDuplicateExceptSkipsTheGivenRow(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	first := sampleVersion("fp-a", "targets-a")
	if err := st.CreateSet(ctx, Set{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", AuthorHMAC: "author", CreatedAt: testNow, UpdatedAt: testNow}, first); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FindDuplicateExcept(ctx, "fp-a", "targets-a", first.RowID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a version is not its own duplicate, got %v", err)
	}
	second := sampleVersion("fp-a", "targets-a")
	second.CreatedAt = testNow.Add(time.Minute)
	if err := st.CreateSet(ctx, Set{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAW", AuthorHMAC: "other", CreatedAt: testNow, UpdatedAt: testNow}, second); err != nil {
		t.Fatal(err)
	}
	if v, err := st.FindDuplicate(ctx, "fp-a", "targets-a"); err != nil || v.SetID != "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
		t.Fatalf("the older pending version ranks first: %+v %v", v, err)
	}
	if v, err := st.FindDuplicateExcept(ctx, "fp-a", "targets-a", first.RowID); err != nil || v.SetID != "01ARZ3NDEKTSV4RRFFQ69G5FAW" {
		t.Fatalf("skipping the first version must find the second: %+v %v", v, err)
	}
}
