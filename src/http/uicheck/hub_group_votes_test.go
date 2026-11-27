package uicheck

import (
	"regexp"
	"strings"
	"testing"
)

var (
	memberVotesRe   = regexp.MustCompile(`set\.applied && \(\s*<HubVoteActions`)
	memberTestRe    = regexp.MustCompile(`hubGroupEscalationTarget\(hubGroup, position\)\s*\?\s*undefined\s*:\s*onTest`)
	searchedOnlyRe  = regexp.MustCompile(`defaultTestDomain\(\s*testSet,\s*testSet\.match \? domain : ""`)
	groupCardOpenRe = regexp.MustCompile(`<HubGroupCard\b`)
)

func TestTheGroupCardOffersVotesAndTestForAppliedMembers(t *testing.T) {
	card := readUI(t, "components/hub/HubGroupCard.tsx")
	if !memberVotesRe.MatchString(card) {
		t.Error("an applied member of a group card has no Works, Broken and Test buttons")
	}
	if !memberTestRe.MatchString(card) {
		t.Error("Test is offered on a member another member escalates to, whose traffic a fetch never reaches")
	}
	page := readUI(t, "components/hub/Hub.tsx")
	loc := groupCardOpenRe.FindStringIndex(page)
	if loc == nil {
		t.Fatal("the Community page renders no group card")
	}
	element := page[loc[0]:]
	element = element[:strings.Index(element, "/>")]
	if !strings.Contains(element, "onVote=") || !strings.Contains(element, "onTest={setTestSet}") {
		t.Error("the Community page does not hand the group card its vote and test handlers")
	}
	if !searchedOnlyRe.MatchString(page) {
		t.Error("Test on a member prefills the searched domain even when that member does not cover it")
	}
}
