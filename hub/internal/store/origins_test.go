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
