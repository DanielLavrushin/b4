package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hub"
	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4/log"
)

const (
	HubGroupStateNone     = "none"
	HubGroupStateApplied  = "applied"
	HubGroupStateUpdate   = "update"
	HubGroupStateModified = "modified"

	HubGroupResultNewSet     = "new_set"
	HubGroupResultNewVersion = "new_version"
	HubGroupResultLinked     = "linked"
)

type hubGroupCopy struct {
	label string
	sets  []*config.SetConfig
}

func hubGroupCopyOf(sets []*config.SetConfig, groupID string) hubGroupCopy {
	if groupID == "" {
		return hubGroupCopy{}
	}
	carries := func(set *config.SetConfig) bool {
		return set != nil && set.Group != "" && set.Hub != nil && set.Hub.GroupID == groupID
	}
	counts := map[string]int{}
	var order []string
	for _, set := range sets {
		if !carries(set) {
			continue
		}
		if counts[set.Group] == 0 {
			order = append(order, set.Group)
		}
		counts[set.Group]++
	}
	best := ""
	for _, label := range order {
		if counts[label] > counts[best] {
			best = label
		}
	}
	if best == "" {
		return hubGroupCopy{}
	}
	found := hubGroupCopy{label: best}
	for _, set := range sets {
		if carries(set) && set.Group == best {
			found.sets = append(found.sets, set)
		}
	}
	return found
}

func (c hubGroupCopy) version() int {
	version := 0
	for _, set := range c.sets {
		version = max(version, set.Hub.GroupVersion)
	}
	return version
}

func hubGroupDerivedFrom(sets, members []*config.SetConfig, label string) *hubwire.Origin {
	var best hubGroupCopy
	bestID := ""
	for _, member := range members {
		if member.Hub == nil || member.Hub.GroupID == "" || member.Hub.GroupID == bestID {
			continue
		}
		found := hubGroupCopyOf(sets, member.Hub.GroupID)
		if found.label == label && len(found.sets) > len(best.sets) {
			best, bestID = found, member.Hub.GroupID
		}
	}
	if bestID == "" {
		return nil
	}
	return &hubwire.Origin{ID: bestID, Version: best.version()}
}

func hubGroupAppliedOf(cfg *config.Config, g *hubwire.CatalogueGroup, members []HubGroupMember) HubGroupApplied {
	found := hubGroupCopyOf(cfg.Sets, g.ID)
	applied := HubGroupApplied{State: HubGroupStateNone, SetIDs: []string{}}
	if len(found.sets) == 0 {
		return applied
	}
	applied.Group = found.label
	applied.Version = found.version()
	byHubID := map[string]*config.SetConfig{}
	modified := false
	for _, set := range found.sets {
		applied.SetIDs = append(applied.SetIDs, set.Id)
		if _, seen := byHubID[set.Hub.ID]; seen {
			continue
		}
		byHubID[set.Hub.ID] = set
		if hubStateOf(cfg, set) != HubStateUnmodified {
			modified = true
		}
	}
	update := applied.Version < g.Version
	if applied.Version <= g.Version {
		listed := map[string]bool{}
		for _, m := range members {
			listed[m.Set.ID] = true
			switch local := byHubID[m.Set.ID]; {
			case local == nil:
				modified = true
			case local.Hub.Version < m.Set.Version:
				update = true
			}
		}
		for hubID := range byHubID {
			if !listed[hubID] {
				modified = true
			}
		}
	}
	switch {
	case update:
		applied.State = HubGroupStateUpdate
	case modified:
		applied.State = HubGroupStateModified
	default:
		applied.State = HubGroupStateApplied
	}
	return applied
}

func hubGroupView(svc *hub.Service, cfg *config.Config, g *hubwire.CatalogueGroup, applied map[string]*HubApplied) (HubGroup, bool) {
	view := HubGroup{
		ID:          g.ID,
		Version:     g.Version,
		Title:       g.Title,
		Description: g.Description,
		Author:      g.Author,
		CreatedAt:   g.CreatedAt,
		UpdatedAt:   g.UpdatedAt,
		Members:     make([]HubGroupMember, 0, len(g.Members)),
	}
	for _, m := range g.Members {
		res, ok := svc.Get(m.SetID)
		if !ok {
			return HubGroup{}, false
		}
		view.Members = append(view.Members, HubGroupMember{Set: hubSetView(res, applied), Escalate: m.Escalate})
	}
	view.Applied = hubGroupAppliedOf(cfg, g, view.Members)
	return view, true
}

func hubGroupViews(svc *hub.Service, cfg *config.Config, groups []*hubwire.CatalogueGroup, applied map[string]*HubApplied) []HubGroup {
	views := make([]HubGroup, 0, len(groups))
	for _, g := range groups {
		if view, ok := hubGroupView(svc, cfg, g, applied); ok {
			views = append(views, view)
		}
	}
	return views
}

func hubFoldedSets(groups []HubGroup) map[string]bool {
	folded := map[string]bool{}
	for _, g := range groups {
		for _, m := range g.Members {
			if g.Author != "" && m.Set.Author == g.Author {
				folded[m.Set.ID] = true
			}
		}
	}
	return folded
}

func hubAPIErrorOf(err error) *APIError {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae
	}
	var ve *config.ValidationError
	if errors.As(err, &ve) {
		return fromValidationError(ve)
	}
	log.Errorf("internal API error: %v", err)
	return ErrInternal("Internal server error")
}

func writeHubMemberError(w http.ResponseWriter, e HubMemberError) {
	setJsonHeader(w)
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(e)
}

func hubMemberErrorOf(member int, err error) HubMemberError {
	ae := hubAPIErrorOf(err)
	return HubMemberError{Status: ae.Status, Code: ae.Code, Message: ae.Message, Fields: ae.Fields, Member: member}
}

// @Summary List hub set groups
// @Description Lists the set groups of the catalogue with their member sets in order; with set, only the groups that hold that hub set.
// @Tags Hub
// @Produce json
// @Param set query string false "Hub set id"
// @Success 200 {object} HubGroupsResponse
// @Security BearerAuth
// @Router /hub/groups [get]
func (api *API) handleHubGroups(w http.ResponseWriter, r *http.Request) {
	if !hubGetRequest(w, r) {
		return
	}
	svc, ok := api.hubService(w)
	if !ok {
		return
	}
	groups := svc.Groups("")
	if setID := strings.TrimSpace(r.URL.Query().Get("set")); setID != "" {
		groups = svc.GroupsOf(setID)
	}
	cfg := api.getCfg()
	sendResponse(w, HubGroupsResponse{Groups: hubGroupViews(svc, cfg, groups, hubAppliedIndex(cfg))})
}

// @Summary Get one hub set group
// @Tags Hub
// @Produce json
// @Param id path string true "Hub group id"
// @Success 200 {object} HubGroup
// @Failure 404 {object} APIError
// @Security BearerAuth
// @Router /hub/groups/{id} [get]
func (api *API) handleHubGroupByID(w http.ResponseWriter, r *http.Request) {
	if !hubGetRequest(w, r) {
		return
	}
	svc, ok := api.hubService(w)
	if !ok {
		return
	}
	cfg := api.getCfg()
	g, found := svc.Group(r.PathValue("id"))
	view, resolved := HubGroup{}, false
	if found {
		view, resolved = hubGroupView(svc, cfg, g, hubAppliedIndex(cfg))
	}
	if !resolved {
		writeAPIError(w, ErrNotFound("Hub group not found"))
		return
	}
	sendResponse(w, view)
}

func hubGroupShareResult(m hubwire.GroupMemberResult) string {
	switch {
	case m.Linked:
		return HubGroupResultLinked
	case m.Version > 1:
		return HubGroupResultNewVersion
	default:
		return HubGroupResultNewSet
	}
}

func writeHubGroupShareError(w http.ResponseWriter, err error, members []*config.SetConfig) {
	var he *hub.HubError
	switch {
	case errors.Is(err, hub.ErrRecordTooBig):
		writeAPIError(w, &APIError{Status: http.StatusBadRequest, Code: "group_too_large", Message: "The group is too large to publish at once: publish fewer sets, or sets without custom payloads"})
	case errors.As(err, &he) && he.Status == http.StatusBadRequest && he.Code == "bad_record" && he.Message == hubwire.ErrUnknownKind.Error():
		writeAPIError(w, &APIError{Status: http.StatusBadRequest, Code: "hub_no_groups", Message: "The hub does not accept set groups yet"})
	case errors.As(err, &he) && he.Member != nil && *he.Member >= 0 && *he.Member < len(members):
		set := members[*he.Member]
		writeHubMemberError(w, HubMemberError{Status: he.Status, Code: he.Code, Message: he.Error(), Member: *he.Member, SetID: set.Id, SetName: set.Name})
	default:
		writeHubError(w, err)
	}
}

// @Summary Publish a set group to the hub
// @Description Builds the shareable envelope of every set in the local group, in list order, and sends them to the hub as one signed group record; an escalation link to another set of the group travels as that set's position, a link outside the group is left out. On acceptance every member is stamped with its hub set and the hub group. A refusal about one set names it in member (its position), set_id and set_name.
// @Tags Hub
// @Accept json
// @Produce json
// @Param body body HubGroupShareRequest true "Group to publish"
// @Success 202 {object} HubGroupShareResponse
// @Failure 400 {object} HubMemberError "a set the hub or this device refused; group_too_large; hub_no_groups; too_many_members"
// @Failure 404 {object} APIError
// @Failure 409 {object} HubRemoteError "duplicate_group"
// @Failure 502 {object} APIError "hub_unreachable"
// @Security BearerAuth
// @Router /hub/groups/share [post]
func (api *API) handleHubGroupShare(w http.ResponseWriter, r *http.Request) {
	if !api.hubRequest(w, r, hubSmallBodyLimit) {
		return
	}
	svc, ok := api.hubService(w)
	if !ok {
		return
	}
	var req HubGroupShareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, ErrInvalidJSON())
		return
	}
	label := config.NormalizeSetGroup(req.Group)
	if label == "" {
		writeAPIError(w, ErrBadRequest("group is required"))
		return
	}
	cfg := api.getCfg()
	var members []*config.SetConfig
	position := map[string]int{}
	for _, set := range cfg.Sets {
		if set != nil && set.Group == label {
			position[set.Id] = len(members)
			members = append(members, set)
		}
	}
	if len(members) == 0 {
		writeAPIError(w, ErrNotFound("Group not found"))
		return
	}
	if len(members) > hubwire.MaxGroupMembers {
		writeAPIError(w, &APIError{Status: http.StatusBadRequest, Code: "too_many_members", Message: fmt.Sprintf("A group can be published with at most %d sets", hubwire.MaxGroupMembers)})
		return
	}
	network := svc.Network()
	body := hubwire.GroupBody{
		Title:       label,
		Description: strings.TrimSpace(req.Description),
		DerivedFrom: hubGroupDerivedFrom(cfg.Sets, members, label),
		ASNHint:     network.ASN,
		CountryHint: network.CC,
		Engine:      cfg.Queue.Mode,
		B4Version:   Version,
	}
	opts := hubBuildOptions(cfg)
	envs := make([]*hubwire.Envelope, len(members))
	for i, set := range members {
		env, _, err := hubwire.Build(set, opts)
		if err != nil {
			e := hubMemberErrorOf(i, err)
			e.SetID, e.SetName = set.Id, set.Name
			writeHubMemberError(w, e)
			return
		}
		envs[i] = env
		member := hubwire.GroupMember{Envelope: *env}
		if j, linked := position[set.Escalate.To]; linked && j != i {
			member.Escalate = hubwire.EscalationTo(set.Escalate, j)
		}
		body.Members = append(body.Members, member)
	}
	rec, err := svc.Sign(hubwire.RecordGroup, body)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	resp, err := svc.Send(r.Context(), rec)
	if err != nil {
		writeHubGroupShareError(w, err, members)
		return
	}
	if resp.GroupID == "" || len(resp.Members) != len(members) || slices.ContainsFunc(resp.Members, func(m hubwire.GroupMemberResult) bool { return m.SetID == "" }) {
		writeAPIError(w, &APIError{Status: http.StatusBadGateway, Code: "bad_hub_answer", Message: "The hub accepted the group but did not name a hub set for every member"})
		return
	}

	if _, _, err := api.editConfig(func(next *config.Config) error {
		for i, local := range members {
			set := hubSetByLocalID(next, local.Id)
			if set == nil {
				continue
			}
			m := resp.Members[i]
			origin := &config.HubOrigin{ID: m.SetID, Version: m.Version, Hash: envs[i].Fingerprint, GroupID: resp.GroupID, GroupVersion: resp.Version}
			if set.Hub != nil {
				origin.AppliedAt = set.Hub.AppliedAt
				if set.Hub.ID == m.SetID && set.Hub.Version == m.Version {
					origin.Vote, origin.VotedAt = set.Hub.Vote, set.Hub.VotedAt
				}
			}
			set.Hub = origin
		}
		return nil
	}); err != nil {
		log.Errorf("Hub: the sets of group '%s' could not be stamped with %s: %v", label, resp.GroupID, err)
	}

	out := HubGroupShareResponse{GroupID: resp.GroupID, Version: resp.Version, Status: resp.Status, Members: make([]HubGroupShareMember, 0, len(members))}
	for i, set := range members {
		m := resp.Members[i]
		out.Members = append(out.Members, HubGroupShareMember{SetID: set.Id, Name: set.Name, HubID: m.SetID, Version: m.Version, Status: m.Status, Linked: m.Linked, Result: hubGroupShareResult(m)})
	}
	log.Infof("Hub: group '%s' shared as %s v%d (%s) with %d sets", label, resp.GroupID, resp.Version, resp.Status, len(members))
	setJsonHeader(w)
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(out)
}

type hubGroupPlacement struct {
	label     string
	block     []*config.SetConfig
	created   []string
	ungrouped []string
	warnings  [][]hubwire.Warning
	moved     [][]DomainReassignment
}

func (api *API) placeHubGroup(next *config.Config, g *hubwire.CatalogueGroup, prepared []*hubPreparedSet, replace bool) (*hubGroupPlacement, error) {
	n := len(prepared)
	place := &hubGroupPlacement{block: make([]*config.SetConfig, n), warnings: make([][]hubwire.Warning, n), moved: make([][]DomainReassignment, n)}
	for i, p := range prepared {
		set := p.set
		set.Hub = &config.HubOrigin{}
		if p.set.Hub != nil {
			*set.Hub = *p.set.Hub
		}
		place.block[i] = &set
		place.warnings[i] = append([]hubwire.Warning{}, p.warnings...)
	}
	found := hubGroupCopyOf(next.Sets, g.ID)
	if replace {
		if len(found.sets) == 0 {
			return nil, &APIError{Status: http.StatusConflict, Code: "not_applied", Message: "No local copy of this hub group was found"}
		}
		place.label = found.label
		used := map[*config.SetConfig]bool{}
		for i, set := range place.block {
			existing := slices.IndexFunc(found.sets, func(s *config.SetConfig) bool { return !used[s] && s.Hub.ID == g.Members[i].SetID })
			if existing < 0 {
				place.created = append(place.created, set.Id)
				continue
			}
			used[found.sets[existing]] = true
			place.warnings[i] = append(place.warnings[i], hubReplaceInPlace(next, found.sets[existing], set)...)
		}
		var dropped, extras []*config.SetConfig
		for _, set := range found.sets {
			if !used[set] {
				dropped = append(dropped, set)
			}
		}
		for _, set := range next.Sets {
			if set != nil && set.Group == found.label && !slices.Contains(found.sets, set) {
				extras = append(extras, set)
			}
		}
		for _, set := range dropped {
			set.Group = ""
			set.Hub.GroupID = ""
			set.Hub.GroupVersion = 0
			place.ungrouped = append(place.ungrouped, set.Id)
		}
		moving := func(set *config.SetConfig) bool {
			return slices.Contains(found.sets, set) || slices.Contains(extras, set)
		}
		anchor := slices.IndexFunc(next.Sets, moving)
		out := make([]*config.SetConfig, 0, len(next.Sets)+n)
		out = append(out, next.Sets[:anchor]...)
		out = append(out, place.block...)
		out = append(out, extras...)
		out = append(out, dropped...)
		for _, set := range next.Sets[anchor:] {
			if !moving(set) {
				out = append(out, set)
			}
		}
		next.Sets = out
	} else {
		label := config.NormalizeSetGroup(g.Title)
		if label == "" {
			label = g.ID
		}
		place.label = config.UniqueSetGroup(label, next.Sets)
		for _, set := range place.block {
			place.created = append(place.created, set.Id)
		}
		next.Sets = append(slices.Clone(place.block), next.Sets...)
	}
	for i, set := range place.block {
		set.Group = place.label
		set.Hub.GroupID = g.ID
		set.Hub.GroupVersion = g.Version
		switch esc := g.Members[i].Escalate; {
		case esc != nil:
			set.Escalate = esc.Config(place.block[esc.To].Id)
		case slices.ContainsFunc(place.block, func(b *config.SetConfig) bool { return b.Id == set.Escalate.To }):
			set.Escalate = config.DefaultSetConfig.Escalate
		}
	}
	outside := slices.DeleteFunc(slices.Clone(next.Sets), func(set *config.SetConfig) bool { return slices.Contains(place.block, set) })
	for i, set := range place.block {
		place.moved[i] = api.releaseDomainsFromOtherSets(outside, set.Id, set.Targets.SNIDomains)
		if place.moved[i] == nil {
			place.moved[i] = []DomainReassignment{}
		}
	}
	return place, nil
}

const hubGroupJointID = "hub-group-joint"

func hubJointFilter(joint *string, member string) bool {
	switch {
	case member == "" || member == *joint:
		return true
	case *joint == "":
		*joint = member
		return true
	}
	return false
}

func hubGroupJointOwner(sets []*config.SetConfig, ids map[string]bool, name string) ([]*config.SetConfig, bool) {
	joint := &config.SetConfig{Id: hubGroupJointID, Name: name, Enabled: true}
	t := &joint.Targets
	out := make([]*config.SetConfig, 0, len(sets))
	for _, set := range sets {
		if set == nil || !ids[set.Id] {
			out = append(out, set)
			continue
		}
		if !slices.Contains(out, joint) {
			out = append(out, joint)
		}
		if !set.Enabled || len(set.Targets.SourceDevices) > 0 || set.BlockRoutingScoped() {
			continue
		}
		if !hubJointFilter(&t.TLSVersion, set.Targets.TLSVersion) || !hubJointFilter(&t.IPVersion, set.Targets.IPVersion) {
			return sets, false
		}
		t.GeoSiteCategories = append(t.GeoSiteCategories, set.Targets.GeoSiteCategories...)
		t.GeoIpCategories = append(t.GeoIpCategories, set.Targets.GeoIpCategories...)
		t.ASNs = append(t.ASNs, set.Targets.ASNs...)
		t.DomainsToMatch = append(t.DomainsToMatch, set.Targets.DomainsToMatch...)
		t.IpsToMatch = append(t.IpsToMatch, set.TargetIPs()...)
	}
	return out, true
}

func hubGroupJointIdle(before, after []*config.SetConfig, block []*config.SetConfig, name string) []CoveredSet {
	ids := map[string]bool{}
	for _, set := range block {
		ids[set.Id] = true
	}
	now, ok := hubGroupJointOwner(after, ids, name)
	if !ok {
		return nil
	}
	was, _ := hubGroupJointOwner(before, ids, name)
	return slices.DeleteFunc(setsCoveredBy(was, now, hubGroupJointID, hubGroupJointID), func(c CoveredSet) bool { return !c.Idle })
}

// @Summary Apply a hub set group
// @Description Fetches every member's payloads, opens each member as POST /hub/sets/{id}/apply does and saves them in one write as one block at the top of the list, in member order, under the group's title (a number appended when the name is taken), with escalation links between members pointed at the new local sets. Domains are taken only from sets outside the block. With replace, the local copy of the group is updated instead: each member replaces its earlier copy in place, keeping the local group name, a new member joins the block at its position, and a set the group no longer lists leaves the group and stays as a set after it.
// @Tags Hub
// @Accept json
// @Produce json
// @Param id path string true "Hub group id"
// @Param body body HubGroupApplyRequest false "Update the applied copy"
// @Success 202 {object} HubGroupApplyResponse
// @Failure 400 {object} HubMemberError "a member this device cannot use"
// @Failure 404 {object} APIError
// @Failure 409 {object} APIError "not_applied"
// @Failure 502 {object} HubMemberError "hub_unreachable or payload_missing"
// @Security BearerAuth
// @Router /hub/groups/{id}/apply [post]
func (api *API) handleHubGroupApply(w http.ResponseWriter, r *http.Request) {
	if !api.hubRequest(w, r, hubSmallBodyLimit) {
		return
	}
	svc, ok := api.hubService(w)
	if !ok {
		return
	}
	var req HubGroupApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeAPIError(w, ErrInvalidJSON())
		return
	}
	g, found := svc.Group(r.PathValue("id"))
	if !found {
		writeAPIError(w, ErrNotFound("Hub group not found"))
		return
	}
	if req.Replace && len(hubGroupCopyOf(api.getCfg().Sets, g.ID).sets) == 0 {
		writeAPIError(w, &APIError{Status: http.StatusConflict, Code: "not_applied", Message: "No local copy of this hub group was found"})
		return
	}
	prepared := make([]*hubPreparedSet, len(g.Members))
	versions := make([]int, len(g.Members))
	asnRefresh := false
	for i, m := range g.Members {
		res, ok := svc.Get(m.SetID)
		if !ok {
			writeHubMemberError(w, HubMemberError{Status: http.StatusNotFound, Code: "not_found", Message: "Hub set not found", Member: i, HubID: m.SetID})
			return
		}
		p, err := api.openHubSet(r.Context(), svc, res.Set)
		if err != nil {
			e := hubMemberErrorOf(i, err)
			e.HubID, e.SetName = m.SetID, res.Set.Title
			writeHubMemberError(w, e)
			return
		}
		prepared[i] = p
		versions[i] = res.Set.Version
		asnRefresh = asnRefresh || len(p.asnWarnings) > 0
	}
	for i, p := range prepared {
		if err := api.installHubSet(p); err != nil {
			e := hubMemberErrorOf(i, err)
			e.HubID, e.SetName = g.Members[i].SetID, p.set.Name
			writeHubMemberError(w, e)
			return
		}
	}

	var place *hubGroupPlacement
	oldCfg, newCfg, err := api.editConfig(func(next *config.Config) error {
		var err error
		place, err = api.placeHubGroup(next, g, prepared, req.Replace)
		return err
	})
	if err != nil {
		if !isRefusal(err) {
			log.Errorf("Hub group apply: failed to save config: %v", err)
		}
		writeAPIError(w, err)
		return
	}
	resp := HubGroupApplyResponse{Group: place.label, Sets: make([]HubGroupAppliedSet, 0, len(place.block)), Created: place.created, Ungrouped: place.ungrouped}
	if resp.Created == nil {
		resp.Created = []string{}
	}
	if resp.Ungrouped == nil {
		resp.Ungrouped = []string{}
	}
	jointIdle := hubGroupJointIdle(oldCfg.Sets, newCfg.Sets, place.block, place.label)
	idle, listed := map[string]bool{}, map[string]bool{}
	for _, c := range jointIdle {
		idle[c.SetId] = true
	}
	for i, set := range place.block {
		covered := slices.DeleteFunc(setsCoveredBy(oldCfg.Sets, newCfg.Sets, set.Id, set.Id), func(c CoveredSet) bool {
			return slices.ContainsFunc(place.block, func(member *config.SetConfig) bool { return member.Id == c.SetId })
		})
		for j := range covered {
			covered[j].Idle = covered[j].Idle || idle[covered[j].SetId]
			listed[covered[j].SetId] = true
		}
		resp.Sets = append(resp.Sets, HubGroupAppliedSet{
			ID:       set.Id,
			Name:     set.Name,
			HubID:    g.Members[i].SetID,
			Version:  versions[i],
			Created:  slices.Contains(place.created, set.Id),
			Moved:    place.moved[i],
			Covered:  covered,
			Warnings: place.warnings[i],
			Payloads: prepared[i].installed,
		})
	}
	for _, c := range jointIdle {
		if !listed[c.SetId] && len(resp.Sets) > 0 {
			resp.Sets[0].Covered = append(resp.Sets[0].Covered, c)
		}
	}
	if asnRefresh {
		config.RequestASNRefresh()
	}
	if api.PerformSoftRestart(newCfg, oldCfg) {
		log.Infof("Soft restart completed successfully")
	}
	verb := "applied"
	if req.Replace {
		verb = "updated"
	}
	log.Infof("Hub: %s group %s v%d as '%s' (%d sets, %d new, %d taken out of the group)", verb, g.ID, g.Version, place.label, len(place.block), len(place.created), len(place.ungrouped))
	setJsonHeader(w)
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(resp)
}
