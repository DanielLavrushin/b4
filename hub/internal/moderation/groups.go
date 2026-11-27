package moderation

import (
	"context"
	"errors"
	"strings"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/store"
)

var groupDoneCodes = map[string]string{
	ActionApprove: "group.approved",
	ActionReject:  "group.rejected",
	ActionHide:    "group.hidden",
	ActionRestore: "group.restored",
}

type GroupRef struct {
	GroupID      string
	Version      int
	ExpectStatus string
	Members      []Ref
}

type GroupEffect struct {
	GroupID     string
	Version     int
	Title       string
	From        string
	To          string
	Listed      int
	ListedAfter int
	Withheld    string
	OK          bool
	Code        string
	Params      map[string]interface{}
}

type MemberItem struct {
	Item
	Position int
	Brought  bool
	Missing  bool
}

type GroupResult struct {
	Result
	Group   GroupEffect
	Members []MemberItem
}

type memberPlan struct {
	item    MemberItem
	version *store.Version
	refusal *Error
}

type setState struct {
	listed   int
	withheld string
}

type memberScope struct {
	group  string
	owned  map[string]bool
	newer  map[string]int
	expect map[string]string
	kept   map[string]bool
}

func versionKey(setID string, version int) string {
	return setID + "/" + itoa(version)
}

func scopeOf(versions []store.GroupVersion, gv *store.GroupVersion, expect []Ref) memberScope {
	sc := memberScope{group: gv.GroupID, owned: map[string]bool{}, newer: map[string]int{}, expect: map[string]string{}, kept: map[string]bool{}}
	for _, other := range versions {
		live := other.Version > gv.Version && other.Status != hubwire.SetStatusRejected
		for _, m := range other.Members {
			key := versionKey(m.SetID, m.SetVersion)
			if m.Brought {
				sc.owned[key] = true
			}
			if live && other.Version > sc.newer[key] {
				sc.newer[key] = other.Version
			}
		}
	}
	for _, r := range expect {
		if r.ExpectStatus != "" {
			sc.expect[versionKey(r.SetID, r.Version)] = r.ExpectStatus
		}
		if r.ExpectKept != nil {
			sc.kept[versionKey(r.SetID, r.Version)] = *r.ExpectKept
		}
	}
	return sc
}

func otherGroupUse(ctx context.Context, t *store.Tx, m store.GroupMember, groupID string) (*store.Membership, error) {
	uses, err := t.Memberships(ctx, m.SetID)
	if err != nil {
		return nil, err
	}
	for i := range uses {
		if u := &uses[i]; u.GroupID != groupID && u.SetVersion == m.SetVersion && u.Status != hubwire.SetStatusRejected {
			return u, nil
		}
	}
	return nil, nil
}

func keyBanned(ctx context.Context, t *store.Tx, keyHMAC string) (bool, error) {
	return authorBanned(ctx, t, &store.Set{AuthorHMAC: keyHMAC})
}

func setListedTx(ctx context.Context, t *store.Tx, setID string) (setState, error) {
	set, err := t.Set(ctx, setID)
	if errors.Is(err, store.ErrNotFound) {
		return setState{}, nil
	}
	if err != nil {
		return setState{}, err
	}
	versions, err := t.Versions(ctx, setID)
	if err != nil {
		return setState{}, err
	}
	banned, err := authorBanned(ctx, t, set)
	if err != nil {
		return setState{}, err
	}
	withdrawn := !set.WithdrawnAt.IsZero()
	return setState{listed: catalogueListed(versions, withdrawn || banned), withheld: withheldReason(withdrawn, banned)}, nil
}

func groupListed(versions []store.GroupVersion, withheld bool, listed func(setID string) (int, error)) (int, error) {
	if withheld {
		return 0, nil
	}
	var newest *store.GroupVersion
	for i := range versions {
		if versions[i].Status == hubwire.SetStatusActive && (newest == nil || versions[i].Version > newest.Version) {
			newest = &versions[i]
		}
	}
	if newest == nil {
		return 0, nil
	}
	for _, m := range newest.Members {
		n, err := listed(m.SetID)
		if err != nil || n == 0 {
			return 0, err
		}
	}
	return newest.Version, nil
}

func memberOptions(opts Options) Options {
	return Options{Reason: opts.Reason, Force: opts.Force, KeepReports: opts.KeepReports, grouped: true}
}

func cascades(action string) bool {
	return action == ActionApprove || action == ActionReject
}

func (p *memberPlan) refuse(code, message string, params map[string]interface{}) {
	p.refusal = fail(code, message, params)
	p.item.OK, p.item.Code, p.item.Params = false, code, params
}

func (s *Service) planMember(ctx context.Context, t *store.Tx, action string, m store.GroupMember, opts Options, sc memberScope, state func(string) (setState, error)) (memberPlan, error) {
	p := memberPlan{item: MemberItem{Item: Item{Effect: Effect{SetID: m.SetID, Version: m.SetVersion}, OK: true}, Position: m.Position, Brought: m.Brought}}
	key := versionKey(m.SetID, m.SetVersion)
	target := cascades(action) && (m.Brought || (action == ActionApprove && sc.owned[key]))
	expect := sc.expect[key]
	current, err := t.Version(ctx, m.SetID, m.SetVersion)
	if errors.Is(err, store.ErrNotFound) {
		p.item.Missing = true
		if target && expect != "" {
			p.refuse(CodeStale, ref(m.SetID, m.SetVersion)+" is deleted now", map[string]interface{}{"set_id": m.SetID, "version": m.SetVersion})
		}
		return p, nil
	}
	if err != nil {
		return p, err
	}
	rejecting := action == ActionReject && target && current.Status == hubwire.SetStatusPending
	var other *store.Membership
	if rejecting && sc.newer[key] == 0 {
		if other, err = otherGroupUse(ctx, t, m, sc.group); err != nil {
			return p, err
		}
	}
	kept := rejecting && (sc.newer[key] > 0 || other != nil)
	keeper := "group version " + itoa(sc.newer[key])
	if other != nil {
		keeper = "group " + ref(other.GroupID, other.GroupVersion)
	}
	wantKept, pinned := sc.kept[key]
	switch {
	case !target:
	case expect != "" && expect != current.Status:
		p.refuse(CodeStale, ref(m.SetID, m.SetVersion)+" is "+current.Status+" now", map[string]interface{}{"set_id": m.SetID, "version": m.SetVersion, "from": current.Status})
	case pinned && wantKept != kept:
		change := " is no longer kept by another group version"
		if kept {
			change = " is kept by " + keeper + " now"
		}
		p.refuse(CodeStale, ref(m.SetID, m.SetVersion)+change, map[string]interface{}{"set_id": m.SetID, "version": m.SetVersion, "from": current.Status})
	case kept && other != nil:
		p.item.Code, p.item.Params = CodeKeptOtherGroup, map[string]interface{}{"set_id": m.SetID, "version": m.SetVersion, "group_id": other.GroupID, "group_version": other.GroupVersion, "title": other.Title}
	case kept:
		p.item.Code, p.item.Params = CodeKeptNewerGroupVersion, map[string]interface{}{"set_id": m.SetID, "version": m.SetVersion, "group_version": sc.newer[key]}
	case allowedFrom(action, current.Status) && (m.Brought || current.Status == hubwire.SetStatusPending):
		eff, v, err := s.plan(ctx, t, action, Ref{SetID: m.SetID, Version: m.SetVersion}, opts)
		p.item.Effect = eff
		var refusal *Error
		switch {
		case errors.As(err, &refusal):
			p.item.OK, p.item.Code, p.item.Params = false, refusal.Code, refusal.Params
			p.refusal = refusal
		case err != nil:
			return p, err
		default:
			p.version = v
		}
		return p, nil
	}
	st, err := state(m.SetID)
	if err != nil {
		return p, err
	}
	p.item.Title, p.item.From = current.Title, current.Status
	p.item.Listed, p.item.ListedAfter, p.item.Withheld = st.listed, st.listed, st.withheld
	return p, nil
}

func (s *Service) planGroup(ctx context.Context, t *store.Tx, action string, r GroupRef, opts Options) (GroupEffect, *store.GroupVersion, []memberPlan, error) {
	eff := GroupEffect{GroupID: r.GroupID, Version: r.Version}
	g, versions, err := t.Group(ctx, r.GroupID)
	if errors.Is(err, store.ErrNotFound) {
		return eff, nil, nil, fail(CodeNotFound, "no such group version "+ref(r.GroupID, r.Version), nil)
	}
	if err != nil {
		return eff, nil, nil, err
	}
	var gv *store.GroupVersion
	for i := range versions {
		if versions[i].Version == r.Version {
			gv = &versions[i]
		}
	}
	if gv == nil {
		return eff, nil, nil, fail(CodeNotFound, "no such group version "+ref(r.GroupID, r.Version), nil)
	}
	banned, err := keyBanned(ctx, t, g.AuthorHMAC)
	if err != nil {
		return eff, gv, nil, err
	}
	withheld := banned || !g.WithdrawnAt.IsZero()
	eff.Title, eff.From, eff.Withheld = gv.Title, gv.Status, withheldReason(!g.WithdrawnAt.IsZero(), banned)
	params := map[string]interface{}{"group_id": r.GroupID, "version": r.Version, "from": gv.Status}
	if r.ExpectStatus != "" && r.ExpectStatus != gv.Status {
		return eff, gv, nil, fail(CodeStale, "group "+ref(r.GroupID, r.Version)+" is "+gv.Status+" now", params)
	}
	if !allowedFrom(action, gv.Status) {
		return eff, gv, nil, fail(CodeInvalidTransition, "cannot "+action+" a "+gv.Status+" group version", params)
	}
	var refusal error
	switch action {
	case ActionApprove:
		eff.To = hubwire.SetStatusActive
		if banned && !opts.Force {
			refusal = fail(CodeAuthorBanned, "the author of group "+ref(r.GroupID, r.Version)+" is banned", params)
		}
	case ActionReject:
		eff.To = hubwire.SetStatusRejected
		if strings.TrimSpace(opts.Reason) == "" {
			refusal = fail(CodeReasonRequired, "a rejection needs a reason", params)
		}
	case ActionHide:
		eff.To = hubwire.SetStatusHidden
	case ActionRestore:
		eff.To = hubwire.SetStatusActive
		if gv.HiddenFrom == hubwire.SetStatusPending {
			eff.To = hubwire.SetStatusPending
		}
	}
	cache := map[string]setState{}
	state := func(setID string) (setState, error) {
		if st, ok := cache[setID]; ok {
			return st, nil
		}
		st, err := setListedTx(ctx, t, setID)
		cache[setID] = st
		return st, err
	}
	memberOpts := memberOptions(opts)
	sc := scopeOf(versions, gv, r.Members)
	members := make([]memberPlan, 0, len(gv.Members))
	changed := map[string]int{}
	for _, m := range gv.Members {
		p, err := s.planMember(ctx, t, action, m, memberOpts, sc, state)
		if err != nil {
			return eff, gv, nil, err
		}
		if p.version != nil {
			changed[m.SetID] = p.item.ListedAfter
		}
		members = append(members, p)
	}
	before := func(setID string) (int, error) {
		st, err := state(setID)
		return st.listed, err
	}
	after := func(setID string) (int, error) {
		if n, ok := changed[setID]; ok {
			return n, nil
		}
		return before(setID)
	}
	if eff.Listed, err = groupListed(versions, withheld, before); err != nil {
		return eff, gv, nil, err
	}
	next := make([]store.GroupVersion, len(versions))
	copy(next, versions)
	for i := range next {
		if next[i].Version == gv.Version {
			next[i].Status = eff.To
		}
	}
	if eff.ListedAfter, err = groupListed(next, withheld, after); err != nil {
		return eff, gv, nil, err
	}
	return eff, gv, members, refusal
}

func (s *Service) applyGroup(ctx context.Context, t *store.Tx, a Actor, action string, gv *store.GroupVersion, eff GroupEffect, opts Options, changed int, batchID string) error {
	now := s.now()
	var err error
	switch action {
	case ActionApprove:
		err = t.SetGroupStatus(ctx, gv.GroupID, gv.Version, hubwire.SetStatusActive, "", now)
	case ActionReject:
		err = t.SetGroupStatus(ctx, gv.GroupID, gv.Version, hubwire.SetStatusRejected, opts.Reason, now)
	case ActionHide:
		err = t.SetGroupStatus(ctx, gv.GroupID, gv.Version, hubwire.SetStatusHidden, opts.Reason, now)
	case ActionRestore:
		_, err = t.RestoreGroup(ctx, gv.GroupID, gv.Version, now)
	}
	if err != nil {
		return err
	}
	switch action {
	case ActionReject:
		err = t.TouchReason(ctx, store.ScopeReject, opts.Reason, now)
	case ActionHide:
		err = t.TouchReason(ctx, store.ScopeHide, opts.Reason, now)
	}
	if err != nil {
		return err
	}
	e := entry(a, now, "group."+action, store.TargetGroup, gv.GroupID, gv.Version, opts.Reason)
	e.BatchID = batchID
	e.Before = map[string]interface{}{"title": gv.Title, "status": gv.Status, "listed": eff.Listed}
	if gv.StatusReason != "" {
		e.Before["status_reason"] = gv.StatusReason
	}
	e.After = map[string]interface{}{"status": eff.To, "listed": eff.ListedAfter, "members": changed}
	if opts.Reason != "" && (action == ActionReject || action == ActionHide) {
		e.After["status_reason"] = opts.Reason
	}
	if opts.Force {
		e.After["forced"] = true
	}
	_, err = t.Audit(ctx, e)
	return err
}

func (s *Service) ModerateGroup(ctx context.Context, a Actor, action string, r GroupRef, opts Options) (GroupResult, error) {
	if !IsVersionAction(action) {
		return GroupResult{}, fail(CodeUnknownAction, "moderation knows approve, reject, hide and restore", nil)
	}
	opts.Reason = CleanText(opts.Reason, MaxReasonRunes)
	var res GroupResult
	var refused *Error
	changed := 0
	err := s.Store.Update(ctx, func(t *store.Tx) error {
		eff, gv, members, err := s.planGroup(ctx, t, action, r, opts)
		res.Group = eff
		res.Group.OK = true
		switch {
		case errors.As(err, &refused) && refused.Code != CodeNotFound:
			res.Group.OK, res.Group.Code, res.Group.Params = false, refused.Code, refused.Params
		case err != nil:
			return err
		}
		res.Members = make([]MemberItem, 0, len(members))
		for _, p := range members {
			res.Members = append(res.Members, p.item)
			if p.refusal != nil && refused == nil {
				refused = p.refusal
			}
			if p.version != nil {
				changed++
			}
		}
		if refused != nil || opts.DryRun {
			return errDryRun
		}
		if changed > 0 {
			res.BatchID = newBatchID()
		}
		memberOpts := memberOptions(opts)
		for _, p := range members {
			if p.version == nil {
				continue
			}
			if err := s.apply(ctx, t, a, action, p.version, p.item.Effect, memberOpts, opts.Reason, res.BatchID); err != nil {
				return err
			}
		}
		return s.applyGroup(ctx, t, a, action, gv, eff, opts, changed, res.BatchID)
	})
	if err != nil && !errors.Is(err, errDryRun) {
		return res, err
	}
	if opts.DryRun {
		res.Code = "moderation.preview"
		res.Notice = "preview of " + action
		return res, nil
	}
	if refused != nil {
		items := make([]Item, 0, len(res.Members))
		for _, m := range res.Members {
			items = append(items, m.Item)
		}
		return res, &Error{Code: refused.Code, Message: refused.Message, Params: refused.Params, Items: items}
	}
	s.request("moderation")
	res.Code = groupDoneCodes[action]
	res.Params = map[string]interface{}{"group_id": r.GroupID, "version": r.Version, "title": res.Group.Title, "members": changed, "listed_after": res.Group.ListedAfter}
	res.Notice = doneWords[action] + " group " + ref(r.GroupID, r.Version)
	if changed > 0 {
		res.Notice += " and " + itoa(changed) + " member versions"
	}
	return res, nil
}
