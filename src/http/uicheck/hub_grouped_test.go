package uicheck

import (
	"regexp"
	"strings"
	"testing"
)

var (
	groupedHookRe       = regexp.MustCompile(`grouped: set\.data\?\.status === "grouped",`)
	foundGroupedRe      = regexp.MustCompile(`const hubSetId = group\.set\?\.hub\?\.id \?\? "";(?s:.*?)const hubGrouped = useHubSetGrouped\(hubSetId\);`)
	foundApplyRe        = regexp.MustCompile(`(?s)\{!hubGrouped\.grouped && \(\s*<Button\b.*?\{t\("discovery\.results\.apply"\)\}\s*</Button>\s*\)\}`)
	foundGroupOnlyRe    = regexp.MustCompile(`\{hubGrouped\.grouped &&\s*hubGroupOffers\.length === 0 &&\s*!hubGroups\.pending && \(\s*<Typography\b[^>]*>\s*\{hubGroups\.applied\.length > 0\s*\?\s*t\("discovery\.results\.hubGroupApplied",(?s:.*?)\s*:\s*t\("discovery\.results\.hubGroupOnly"\)\}`)
	foundOffersRe       = regexp.MustCompile(`const hubGroups = useHubGroupOffers\(hubSetId\);\s*const hubGroupOffers = hubGroups\.offers;`)
	groupOffersHookRe   = regexp.MustCompile(`offers: groups\.filter\(\(g\) => hubGroupAction\(g\) !== "applied"\),\s*applied: groups\.filter\(\(g\) => hubGroupAction\(g\) === "applied"\),`)
	applyDialogOffersRe = regexp.MustCompile(`const hubGroups = useHubGroupOffers\(hubSetId\);(?s:.*?)\{hubGrouped\.grouped && \(\s*<B4Alert severity="warning">\s*\{hubGroups\.offers\.length === 0 && hubGroups\.applied\.length > 0\s*\?\s*t\("discovery\.results\.hubGroupApplied",(?s:.*?)\{hubGroups\.offers\.map\(\(hubGroup\) => \((?s:.*?)onClick=\{\(\) => onApplyHubGroup\(\{ hubGroup, hubSetId \}\)\}`)
	discoveryHandoffRe  = regexp.MustCompile(`onApplyHubGroup=\{\(offer\) => \{\s*setApplyTarget\(null\);\s*setHubGroupOffer\(offer\);\s*\}\}`)
	applyDialogHookRe   = regexp.MustCompile(`const hubSetId = target\?\.set\.hub\?\.id \?\? "";\s*const hubGrouped = useHubSetGrouped\(hubSetId\);`)
	applyDialogBlockRe  = regexp.MustCompile(`hubGrouped\.pending \|\|\s*\(hubGrouped\.grouped && mode !== "existing"\) \|\|`)
	verdictHookRe       = regexp.MustCompile(`const hubGrouped = useHubSetGrouped\(verdict\.set\?\.hub\?\.id \?\? ""\);`)
	verdictApplyRe      = regexp.MustCompile(`const canApply =\s*!!set &&\s*!!strategy &&\s*covered\.length > 0 &&\s*!hubGrouped\.grouped &&\s*!hubGrouped\.pending &&`)
	detailsApplyRe      = regexp.MustCompile(`\{set &&\s*!\(set\.status === "grouped" && appliedAction\(set\) === "apply"\) && \(\s*<AppliedActionButton\b`)
)

func TestAGroupedHubSetIsOfferedOnlyWithItsGroup(t *testing.T) {
	hooks := readUI(t, "hooks/useHub.ts")
	if !groupedHookRe.MatchString(hooks) {
		t.Error("useHubSetGrouped does not read the grouped status of the hub set")
	}
	if !groupOffersHookRe.MatchString(hooks) {
		t.Error("useHubGroupOffers does not split the groups of a hub set into the ones to offer and the ones already applied")
	}

	results := readUI(t, "components/discovery/ResultsPanel.tsx")
	start := strings.Index(results, "const FoundCard = (")
	if start < 0 {
		t.Fatal("ResultsPanel has no FoundCard")
	}
	card := results[start:]
	if end := strings.Index(card, "\ninterface SiteAddressesProps"); end > 0 {
		card = card[:end]
	}
	if !foundGroupedRe.MatchString(card) {
		t.Error("the found card does not look up whether its hub set is grouped")
	}
	if !foundApplyRe.MatchString(card) {
		t.Error("the found card offers Apply as a set for a grouped hub set, which works only with its group")
	}
	if !foundOffersRe.MatchString(card) {
		t.Error("the found card does not take its group offers from useHubGroupOffers")
	}
	if !foundGroupOnlyRe.MatchString(card) {
		t.Error("a grouped hub set without a group offer does not say that its group is already applied, or that it is applied only with its group")
	}

	dialog := readUI(t, "components/discovery/ApplyDialog.tsx")
	if !applyDialogHookRe.MatchString(dialog) || !applyDialogBlockRe.MatchString(dialog) {
		t.Error("the apply dialog, which alternates and history reach too, still creates or rewrites a set from a grouped hub set")
	}
	if !applyDialogOffersRe.MatchString(dialog) {
		t.Error("the apply dialog names a group for a grouped hub set but offers no way to apply it, or does not say that the group is already applied")
	}
	if !discoveryHandoffRe.MatchString(readUI(t, "components/discovery/Discovery.tsx")) {
		t.Error("Discovery does not hand a group offer from the apply dialog over to the group dialog")
	}

	verdict := readUI(t, "components/discovery/SetVerdictCard.tsx")
	if !verdictHookRe.MatchString(verdict) || !verdictApplyRe.MatchString(verdict) {
		t.Error("a set run's verdict still writes a grouped hub set's strategy into the set")
	}

	if !detailsApplyRe.MatchString(readUI(t, "components/hub/DetailsDialog.tsx")) {
		t.Error("the details dialog still applies a grouped hub set on its own")
	}

	requireKeysInBothLocales(t, []string{"discovery.results.hubGroupOnly", "discovery.results.hubGroupApplied", "hub.status.set.grouped", "errors.hub_grouped"})
}
