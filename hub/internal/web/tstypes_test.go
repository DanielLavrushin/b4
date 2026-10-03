package web

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/catalogue"
	"github.com/daniellavrushin/b4hub/internal/notify"
	"github.com/daniellavrushin/b4hub/internal/store"
)

var updateTypes = flag.Bool("update-types", false, "rewrite the console's generated API types")

const tsTypesPath = "../../ui/src/models/api.gen.ts"

type tsUnion struct {
	name   string
	values []string
}

var tsUnions = []tsUnion{
	{"SetStatus", []string{hubwire.SetStatusPending, hubwire.SetStatusActive, hubwire.SetStatusHidden, hubwire.SetStatusRejected}},
	{"MirrorStatus", []string{store.MirrorPending, store.MirrorApproved, store.MirrorRejected}},
	{"LineageKind", lineageKinds},
	{"TidyKind", []string{TidyDeadWildcard, TidyCovered, TidyDuplicate, TidyWWWOnly}},
	{"SetAction", []string{ActionApprove, ActionReject, ActionHide, ActionRestore}},
	{"KeyAction", []string{ActionBan, ActionUnban, ActionTrust, ActionUntrust}},
	{"MirrorAction", []string{ActionApprove, ActionReject, ActionRemove}},
	{"ReportAction", []string{ActionDismiss, ActionResolve, ActionReopen}},
	{"ReportState", []string{store.ReportOpen, store.ReportDismissed, store.ReportResolved}},
	{"BuildState", []string{catalogue.BuildIdle, catalogue.BuildQueued, catalogue.BuildBuilding}},
	{"MirrorLag", []string{LagUnknown, LagCurrent, LagBehind, LagStale, LagAhead}},
	{"WithheldReason", []string{store.WithheldWithdrawn, store.WithheldAuthorBanned}},
	{"SimilarRelation", relationCodes},
	{"AttentionCode", attentionCodes},
	{"TechniqueCode", techniqueCodes},
	{"FilterCode", filterCodes},
	{"SetGroupName", setGroups},
	{"AppliedState", appliedStates},
	{"KeyTag", []string{store.TagStaff, store.TagTest}},
	{"EmitSource", emitCodes},
	{"ReasonScope", store.ReasonScopes},
	{"IssueCode", issueCodes},
	{"Severity", severities},
	{"NotifyEvent", notify.Events},
	{"NotifyChannel", notify.Channels},
	{"NotifyLanguage", []string{notify.LanguageEN, notify.LanguageRU}},
}

var tsRoots = []interface{}{
	ErrorBody{},
	sessionState{},
	OverviewView{},
	SetsView{},
	SetDetailView{},
	EditRequest{},
	EditPreview{},
	MirrorView{},
	FeedbackView{},
	SettingsView{},
	ActionResult{},
	ModerationRequest{},
	ModerationView{},
	KeyImpactView{},
	MirrorsView{},
	MirrorCheckResult{},
	ReportsPageView{},
	ReportsActionRequest{},
	AuditPageView{},
	BuildsPageView{},
	SimilarView{},
	BadgesView{},
	SetRowsView{},
	QueueView{},
	KeyListView{},
	KeyDetailView{},
	KeyProfileRequest{},
	NotableKeyView{},
	VotesPageView{},
	VoteOriginsView{},
	GeoCategoriesView{},
	InvalidFieldView{},
	ReasonPresetView{},
	ReasonPresetRequest{},
	ReasonMoveRequest{},
	TextEditRequest{},
	HealthView{},
	StatsView{},
	NotifyView{},
	NotifyRequest{},
	NotifyTestRequest{},
}

var tsNames = map[reflect.Type]string{
	reflect.TypeOf(ErrorBody{}):        "ApiErrorBody",
	reflect.TypeOf(sessionState{}):     "SessionState",
	reflect.TypeOf(Tidy{}):             "TidyView",
	reflect.TypeOf(Suggestion{}):       "SuggestionView",
	reflect.TypeOf(hubwire.Warning{}):  "WarningView",
	reflect.TypeOf(hubwire.Stripped{}): "StrippedView",
}

var tsFields = map[string]string{
	"EntryView.status":            "SetStatus",
	"EntryView.hidden_from":       "SetStatus",
	"EntryView.withheld":          "WithheldReason",
	"SetDetailView.withheld":      "WithheldReason",
	"DuplicateView.status":        "SetStatus",
	"MirrorView.status":           "MirrorStatus",
	"MirrorView.lag":              "MirrorLag",
	"LineageView.kind":            "LineageKind",
	"SuggestionView.kind":         "TidyKind",
	"ReportView.state":            "ReportState",
	"ReportView.set_status":       "SetStatus",
	"BuildStateView.state":        "BuildState",
	"ModerationItemView.from":     "SetStatus",
	"ModerationItemView.to":       "SetStatus",
	"ModerationItemView.withheld": "WithheldReason",
	"ModerationRequest.action":    "SetAction",
	"ReportsActionRequest.action": "ReportAction",
	"SimilarRelationView.code":    "SimilarRelation",
	"SimilarItemView.status":      "SetStatus",
	"SetRowView.status":           "SetStatus",
	"SetRowView.hidden_from":      "SetStatus",
	"SetRowView.withheld":         "WithheldReason",
	"SetRowView.attention":        "AttentionCode[]",
	"SetRowsView.group":           "SetGroupName",
	"AppliedView.state":           "AppliedState",
	"VoteRowView.set_status":      "SetStatus",
	"KeySetVersionView.status":    "SetStatus",
	"KeySetView.withheld":         "WithheldReason",
	"KeyRowView.tag":              "KeyTag",
	"NotableKeyView.tag":          "KeyTag",
	"EmittedView.source":          "EmitSource",
	"ReasonPresetView.scope":      "ReasonScope",
	"ReasonPresetRequest.scope":   "ReasonScope",
	"IssueView.code":              "IssueCode",
	"IssueView.severity":          "Severity",
	"NotifyView.events":           "NotifyEvent[]",
	"NotifyView.available":        "NotifyEvent[]",
	"NotifyView.language":         "NotifyLanguage",
	"NotifyRequest.events":        "NotifyEvent[]",
	"NotifyRequest.language":      "NotifyLanguage",
	"NotifyTestRequest.channel":   "NotifyChannel",
}

var timeType = reflect.TypeOf(time.Time{})

type tsField struct {
	name     string
	optional bool
	typ      reflect.Type
}

type tsGen struct {
	queue []reflect.Type
	seen  map[reflect.Type]bool
	names map[string]reflect.Type
	err   error
}

func (g *tsGen) nameOf(t reflect.Type) string {
	name, ok := tsNames[t]
	if !ok {
		name = t.Name()
	}
	if other, taken := g.names[name]; taken && other != t {
		g.err = fmt.Errorf("%s and %s both map to %s", other, t, name)
	}
	g.names[name] = t
	return name
}

func (g *tsGen) enqueue(t reflect.Type) string {
	name := g.nameOf(t)
	if !g.seen[t] {
		g.seen[t] = true
		g.queue = append(g.queue, t)
	}
	return name
}

func (g *tsGen) typeOf(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Pointer:
		return g.typeOf(t.Elem())
	case reflect.Struct:
		if t == timeType {
			return "string"
		}
		return g.enqueue(t)
	case reflect.Slice, reflect.Array:
		return g.typeOf(t.Elem()) + "[]"
	case reflect.Map:
		if t.Elem().Kind() == reflect.Interface {
			return "Record<string, unknown>"
		}
		return "Record<string, " + g.typeOf(t.Elem()) + ">"
	case reflect.Interface:
		return "unknown"
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"
	}
	g.err = fmt.Errorf("no TypeScript mapping for %s", t)
	return "unknown"
}

func jsonFields(t reflect.Type) []tsField {
	out := make([]tsField, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" {
			inner := f.Type
			if inner.Kind() == reflect.Pointer {
				inner = inner.Elem()
			}
			if inner.Kind() == reflect.Struct {
				out = append(out, jsonFields(inner)...)
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		omit := strings.Contains(","+opts+",", ",omitempty,")
		optional := f.Type.Kind() == reflect.Pointer || (omit && f.Type.Kind() != reflect.Struct)
		out = append(out, tsField{name: name, optional: optional, typ: f.Type})
	}
	return out
}

func generateTSTypes() (string, error) {
	g := &tsGen{seen: map[reflect.Type]bool{}, names: map[string]reflect.Type{}}
	for _, root := range tsRoots {
		g.enqueue(reflect.TypeOf(root))
	}
	var b bytes.Buffer
	for _, u := range tsUnions {
		quoted := make([]string, 0, len(u.values))
		for _, v := range u.values {
			quoted = append(quoted, fmt.Sprintf("%q", v))
		}
		fmt.Fprintf(&b, "export type %s = %s;\n\n", u.name, strings.Join(quoted, " | "))
	}
	for i := 0; i < len(g.queue); i++ {
		t := g.queue[i]
		name := g.nameOf(t)
		fmt.Fprintf(&b, "export interface %s {\n", name)
		for _, f := range jsonFields(t) {
			ts := g.typeOf(f.typ)
			if override, ok := tsFields[name+"."+f.name]; ok {
				ts = override
			}
			mark := ""
			if f.optional {
				mark = "?"
			}
			fmt.Fprintf(&b, "  %s%s: %s;\n", f.name, mark, ts)
		}
		b.WriteString("}\n")
		if i < len(g.queue)-1 {
			b.WriteString("\n")
		}
	}
	return b.String(), g.err
}

func TestAdminTypes(t *testing.T) {
	generated, err := generateTSTypes()
	if err != nil {
		t.Fatal(err)
	}
	if *updateTypes {
		if err := os.WriteFile(tsTypesPath, []byte(generated), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	current, err := os.ReadFile(tsTypesPath)
	if err != nil {
		t.Fatalf("%s: %v; run make hub-types", tsTypesPath, err)
	}
	if string(current) != generated {
		t.Fatalf("%s is out of date with the admin views; run make hub-types", tsTypesPath)
	}
}
