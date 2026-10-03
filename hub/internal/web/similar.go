package web

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4/sni"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	RelationSameTargets     = "same_targets"
	RelationSameStrategy    = "same_strategy"
	RelationSharedTargets   = "shared_targets"
	RelationCoveringDomains = "covering_domains"
	RelationSameTitle       = "same_title"
	RelationSimilarTitle    = "similar_title"
	RelationSameAuthor      = "same_author"
	RelationSameSet         = "same_set"

	similarDefault   = 12
	similarMax       = 50
	similarTitleBar  = 0.5
	sharedPreviewMax = 6
)

var relationCodes = []string{RelationSameTargets, RelationSameStrategy, RelationSharedTargets, RelationCoveringDomains, RelationSameTitle, RelationSimilarTitle, RelationSameAuthor, RelationSameSet}

type SimilarRelationView struct {
	Code  string `json:"code"`
	Count int    `json:"count,omitempty"`
}

type SimilarItemView struct {
	SetID        string                `json:"set_id"`
	Version      int                   `json:"version"`
	Title        string                `json:"title"`
	Status       string                `json:"status"`
	StatusReason string                `json:"status_reason,omitempty"`
	Listed       bool                  `json:"listed"`
	Score        float64               `json:"score"`
	Relations    []SimilarRelationView `json:"relations"`
	Shared       []string              `json:"shared"`
	Votes        VotesView             `json:"votes"`
	UpdatedAt    time.Time             `json:"updated_at"`
}

type SimilarView struct {
	SetID     string            `json:"set_id"`
	Version   int               `json:"version"`
	Scanned   int               `json:"scanned"`
	Truncated bool              `json:"truncated"`
	Items     []SimilarItemView `json:"items"`
}

type targetProfile struct {
	set      map[string]struct{}
	domains  map[string]struct{}
	suffixes map[string]struct{}
}

func profileOf(projection map[string]interface{}) targetProfile {
	p := targetProfile{set: store.TargetEntries(projection), domains: map[string]struct{}{}, suffixes: map[string]struct{}{}}
	for _, entry := range store.TargetList(projection, "sni_domains") {
		value, isRegex := sni.ParseDomainEntry(entry)
		value = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), ".")
		if value == "" || isRegex || strings.HasPrefix(value, "*") {
			continue
		}
		p.domains[value] = struct{}{}
		for rest := value; ; {
			i := strings.IndexByte(rest, '.')
			if i < 0 {
				break
			}
			rest = rest[i+1:]
			p.suffixes[rest] = struct{}{}
		}
	}
	return p
}

func covering(a, b targetProfile) int {
	n := 0
	for d := range b.domains {
		if _, same := a.domains[d]; same {
			continue
		}
		if _, ok := a.suffixes[d]; ok {
			n++
			continue
		}
		for rest := d; ; {
			i := strings.IndexByte(rest, '.')
			if i < 0 {
				break
			}
			rest = rest[i+1:]
			if _, ok := a.domains[rest]; ok {
				n++
				break
			}
		}
	}
	return n
}

func titleTokens(title string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, f := range strings.FieldsFunc(strings.ToLower(title), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len([]rune(f)) >= 2 {
			out[f] = struct{}{}
		}
	}
	return out
}

func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if _, ok := b[k]; ok {
			inter++
		}
	}
	return float64(inter) / float64(len(a)+len(b)-inter)
}

func sameTitle(a, b string) bool {
	return a != "" && strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func (s *Server) similar(w http.ResponseWriter, r *http.Request) {
	id, version, ok := versionPath(r)
	if !ok {
		writeError(w, http.StatusNotFound, codeNotFound, "the address does not name a set version")
		return
	}
	limit := similarDefault
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, codeBadRequest, "limit must be a positive number")
			return
		}
		limit = min(n, similarMax)
	}
	ctx := r.Context()
	subject, err := s.Store.GetVersion(ctx, id, version)
	if err != nil {
		s.fail(w, err)
		return
	}
	listed, err := s.Store.ListedVersions(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	pending, err := s.Store.PendingVersions(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	hidden, err := s.Store.VersionsByStatus(ctx, hubwire.SetStatusHidden)
	if err != nil {
		s.fail(w, err)
		return
	}
	rejected, err := s.Store.VersionsByStatus(ctx, hubwire.SetStatusRejected)
	if err != nil {
		s.fail(w, err)
		return
	}
	totals, err := s.Store.VoteTotals(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	listedSet := make(map[string]bool, len(listed))
	for _, v := range listed {
		listedSet[v.SetID+"/"+strconv.Itoa(v.Version)] = true
	}
	mine := profileOf(subject.Projection)
	myTitle := titleTokens(subject.Title)
	seen := map[string]bool{id + "/" + strconv.Itoa(version): true}
	view := SimilarView{SetID: id, Version: version, Items: []SimilarItemView{}}
	for _, group := range [][]store.Version{listed, pending, hidden, rejected} {
		for _, v := range group {
			ref := v.SetID + "/" + strconv.Itoa(v.Version)
			if seen[ref] {
				continue
			}
			seen[ref] = true
			view.Scanned++
			theirs := profileOf(v.Projection)
			relations := []SimilarRelationView{}
			shared := []string{}
			for k := range mine.set {
				if _, ok := theirs.set[k]; ok {
					shared = append(shared, k)
				}
			}
			sort.Strings(shared)
			cover := covering(mine, theirs) + covering(theirs, mine)
			targetScore := 0.0
			if v.TargetsKey == subject.TargetsKey {
				relations = append(relations, SimilarRelationView{Code: RelationSameTargets})
				targetScore = 1
			} else {
				if len(shared) > 0 {
					relations = append(relations, SimilarRelationView{Code: RelationSharedTargets, Count: len(shared)})
				}
				if cover > 0 {
					relations = append(relations, SimilarRelationView{Code: RelationCoveringDomains, Count: cover})
				}
				if smaller := min(len(mine.set), len(theirs.set)); smaller > 0 {
					targetScore = min(1, (float64(len(shared))+0.5*float64(cover))/float64(smaller))
				}
			}
			titleScore := jaccard(myTitle, titleTokens(v.Title))
			if sameTitle(subject.Title, v.Title) || sameTitle(subject.Title, v.OriginalTitle) {
				relations = append(relations, SimilarRelationView{Code: RelationSameTitle})
				titleScore = 1
			} else if titleScore >= similarTitleBar {
				relations = append(relations, SimilarRelationView{Code: RelationSimilarTitle})
			}
			strategy := 0.0
			if v.FP == subject.FP {
				relations = append(relations, SimilarRelationView{Code: RelationSameStrategy})
				strategy = 1
			}
			if targetScore == 0 && titleScore < similarTitleBar && strategy == 0 {
				continue
			}
			if v.UploaderHMAC != "" && v.UploaderHMAC == subject.UploaderHMAC {
				relations = append(relations, SimilarRelationView{Code: RelationSameAuthor})
			}
			if v.SetID == subject.SetID {
				relations = append(relations, SimilarRelationView{Code: RelationSameSet})
			}
			if len(shared) > sharedPreviewMax {
				shared = shared[:sharedPreviewMax]
			}
			item := SimilarItemView{
				SetID:        v.SetID,
				Version:      v.Version,
				Title:        v.Title,
				Status:       v.Status,
				StatusReason: v.StatusReason,
				Listed:       listedSet[ref],
				Score:        float64(int((0.55*targetScore+0.30*titleScore+0.15*strategy)*1000+0.5)) / 1000,
				Relations:    relations,
				Shared:       shared,
				UpdatedAt:    v.UpdatedAt,
			}
			if t, ok := totals[v.SetID][v.Version]; ok {
				item.Votes = VotesView{Works: t.Works, Broken: t.Broken}
			}
			view.Items = append(view.Items, item)
		}
	}
	sort.SliceStable(view.Items, func(i, j int) bool {
		if view.Items[i].Score != view.Items[j].Score {
			return view.Items[i].Score > view.Items[j].Score
		}
		return view.Items[i].UpdatedAt.After(view.Items[j].UpdatedAt)
	})
	if len(view.Items) > limit {
		view.Items = view.Items[:limit]
		view.Truncated = true
	}
	writeJSON(w, http.StatusOK, view)
}
