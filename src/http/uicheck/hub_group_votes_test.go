package uicheck

import (
	"regexp"
	"strings"
	"testing"
)

var (
	groupVoteRe       = regexp.MustCompile(`\{actionLabel\}\s*</Button>\s*\{applied\.state !== "none" && \(\s*<>\s*<Box sx=\{\{ flex: 1 \}\} />\s*<HubVoteActions\s+applied=\{applied\}\s+busy=\{busy\}\s+blocked=\{voteBlocked\}\s+onVote=\{\(kind\) => onVote\(hubGroup, kind\)\}`)
	memberTestRe      = regexp.MustCompile(`set\.applied &&\s*!hubGroupEscalationTarget\(hubGroup, position\) && \(\s*<Button\s+size="small"\s+startIcon=\{<TestIcon />\}\s+disabled=\{busy\}\s+onClick=\{\(\) => onTest\(set\)\}`)
	memberRowsRe      = regexp.MustCompile(`(?s)hubGroup\.members\.map\(.*?\{actionLabel\}`)
	searchedOnlyRe    = regexp.MustCompile(`defaultTestDomain\(\s*testSet,\s*testSet\.match \? domain : ""`)
	groupCardOpenRe   = regexp.MustCompile(`<HubGroupCard\b`)
	setCardGroupRe    = regexp.MustCompile(`onVote=\{\s*applied\.group_id \? undefined : \(kind\) => onVote\(set, kind\)\s*\}`)
	setsPageGroupRe   = regexp.MustCompile(`const canVote =\s*Boolean\(hubId\) &&\s*!ratedByGroup &&`)
	listedGroupsRe    = regexp.MustCompile(`const hubGroups = useHubGroups\(hubReady\);\s*const listedGroupIds = useMemo\(\s*\(\) => new Set\(\(hubGroups\.data\?\.groups \?\? \[\]\)\.map\(\(g\) => g\.id\)\),`)
	ratedByGroupRe    = regexp.MustCompile(`ratedByGroup=\{\s*Boolean\(set\.group\) &&\s*listedGroupIds\.has\(set\.hub\?\.group_id \?\? ""\)\s*\}`)
	groupVoteSearchRe = regexp.MustCompile(`groupVote\.mutateAsync\(\{\s*id: hubGroup\.id,\s*kind,\s*domain: domain \|\| undefined,`)
)

func TestTheGroupCardRatesTheGroupAndItsMembersOnlyGetTest(t *testing.T) {
	card := readUI(t, "components/hub/HubGroupCard.tsx")
	if !groupVoteRe.MatchString(card) {
		t.Error("the group card footer offers no Works and Broken for the whole group after Apply")
	}
	if n := strings.Count(card, "<HubVoteActions"); n != 1 {
		t.Errorf("the group card must hold one vote block, the group's, found %d", n)
	}
	rows := memberRowsRe.FindString(card)
	if rows == "" {
		t.Fatal("the group card renders no member rows")
	}
	if strings.Contains(rows, "<HubVoteActions") || strings.Contains(rows, "onVote") || strings.Contains(rows, "scorePercent(set.display)") {
		t.Error("a member row still offers its own Works and Broken or shows its own score")
	}
	if !memberTestRe.MatchString(card) {
		t.Error("Test is missing for applied members, or offered on a member another member escalates to, whose traffic a fetch never reaches")
	}

	page := readUI(t, "components/hub/Hub.tsx")
	loc := groupCardOpenRe.FindStringIndex(page)
	if loc == nil {
		t.Fatal("the Community page renders no group card")
	}
	element := page[loc[0]:]
	element = element[:strings.Index(element, "/>")]
	if !strings.Contains(element, "onVote={(g, kind) => void handleGroupVote(g, kind)}") || !strings.Contains(element, "onTest={setTestSet}") {
		t.Error("the Community page does not hand the group card its group vote and test handlers")
	}
	if !groupVoteSearchRe.MatchString(page) {
		t.Error("a group vote does not send the domain the page is filtered by")
	}
	if !searchedOnlyRe.MatchString(page) {
		t.Error("Test on a member prefills the searched domain even when that member does not cover it")
	}
}

func TestASetAppliedWithItsGroupIsRatedThroughTheGroup(t *testing.T) {
	if !setCardGroupRe.MatchString(readUI(t, "components/hub/HubSetCard.tsx")) {
		t.Error("the Community set card offers Works and Broken for a set applied as part of a hub group")
	}
	if !setsPageGroupRe.MatchString(readUI(t, "components/sets/SetCard.tsx")) {
		t.Error("the Sets page offers Works and Broken for a set applied as part of a hub group")
	}
	manager := readUI(t, "components/sets/Manager.tsx")
	if !listedGroupsRe.MatchString(manager) || !ratedByGroupRe.MatchString(manager) {
		t.Error("the Sets page hides Works and Broken for a set of a hub group the hub does not list, so it can be rated neither on its own nor through the group")
	}
}
