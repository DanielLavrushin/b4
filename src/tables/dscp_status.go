package tables

import (
	"maps"
	"slices"

	"github.com/daniellavrushin/b4/config"
)

type DSCPSetState struct {
	ID                string
	Name              string
	Value             int
	Refusal           string
	Applied           bool
	Static            int
	LearnedDNS        uint64
	LearnedTLS        uint64
	LearnedPreResolve uint64
}

type DSCPState struct {
	Backend   string
	Pending   bool
	SkipSetup bool
	Sets      []DSCPSetState
	Incapable map[string]string
}

func DSCPStatus(cfg *config.Config) DSCPState {
	var status DSCPState
	if cfg == nil {
		return status
	}
	status.SkipSetup = cfg.System.Tables.SkipSetup
	st := dscpApplied.Load()
	if st != nil {
		status.Backend, status.Pending = st.backend, st.pending
		if st.ipt != nil && len(st.ipt.incapable) > 0 {
			status.Incapable = maps.Clone(st.ipt.incapable)
		}
	}
	learned := dscpLearnStats()
	synced := st != nil && st.plan.equal(dscpPlanFor(cfg))
	seen := make(map[string]bool)
	for _, set := range cfg.Sets {
		if set == nil || !set.Enabled || seen[set.Id] {
			continue
		}
		value, on := set.DSCPStamp()
		if !on {
			continue
		}
		seen[set.Id] = true
		row := DSCPSetState{ID: set.Id, Name: set.Name, Value: value, Refusal: cfg.DSCPRefusal(set)}
		if row.Refusal == "" {
			count := learned[set.Id]
			row.Applied = synced && st.perSetApplied(set.Id, value)
			row.Static = dscpStaticCount(set)
			row.LearnedDNS, row.LearnedTLS, row.LearnedPreResolve = count.dns, count.tls, count.preResolve
		}
		status.Sets = append(status.Sets, row)
	}
	return status
}

func dscpStaticCount(set *config.SetConfig) int {
	member := dscpPlanSet{v4: set.MatchesIPVersion(4), v6: set.MatchesIPVersion(6)}
	count := 0
	for _, raw := range set.Targets.IpsToMatch {
		if prefix, ok := dscpParseTarget(raw); ok && member.matchesFamily(prefix.Addr()) {
			count++
		}
	}
	return count
}

func (st *dscpState) perSetApplied(id string, value int) bool {
	if st == nil || st.plan.empty() {
		return false
	}
	i := slices.IndexFunc(st.plan.sets, func(s dscpPlanSet) bool { return s.id == id })
	if i < 0 || st.plan.sets[i].value != value {
		return false
	}
	member := st.plan.sets[i]
	if st.backend == backendNFTables {
		return !st.pending && st.nft.perSet()
	}
	if st.ipt == nil {
		return false
	}
	for bin, specs := range st.ipt.chains {
		v6 := dscpIptBinV6(bin)
		if dscpIptShapeOf(specs).guard && ((v6 && member.v6) || (!v6 && member.v4)) {
			return true
		}
	}
	return false
}
