package web

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/geo"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/ingest"
	"github.com/daniellavrushin/b4hub/internal/store"
	"github.com/daniellavrushin/b4hub/internal/testkit"
)

func withTargets(t *testing.T, projection map[string]interface{}, changes map[string]interface{}) map[string]interface{} {
	t.Helper()
	out := projectionCopy(t, projection)
	targets, _ := out["targets"].(map[string]interface{})
	if targets == nil {
		targets = map[string]interface{}{}
		out["targets"] = targets
	}
	for key, value := range changes {
		if value == nil {
			delete(targets, key)
			continue
		}
		targets[key] = value
	}
	return out
}

func (f *fixture) preview(id string, req EditRequest) EditPreview {
	f.t.Helper()
	resp := f.admin(http.MethodPost, setPath(id, 1, "preview"), req)
	if resp.status != http.StatusOK {
		f.t.Fatalf("preview: %d %s", resp.status, resp.body)
	}
	var p EditPreview
	resp.decode(f.t, &p)
	return p
}

func warningOf(warnings []hubwire.Warning, code string) *hubwire.Warning {
	for i := range warnings {
		if warnings[i].Code == code {
			return &warnings[i]
		}
	}
	return nil
}

func warnedList(warnings []hubwire.Warning, code, param string) []string {
	w := warningOf(warnings, code)
	if w == nil {
		return nil
	}
	raw, _ := w.Params[param].([]interface{})
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		s, _ := item.(string)
		out = append(out, s)
	}
	return out
}

func hasFilter(code, version string) func(TargetsView) bool {
	return func(v TargetsView) bool {
		for _, f := range v.Filters {
			if f.Code == code {
				got, _ := f.Params["version"].(string)
				return got == version
			}
		}
		return false
	}
}

func (f *fixture) lastEditAudit(id string) AuditEntryView {
	f.t.Helper()
	page := f.auditLog("?action=set.edit&target_id=" + id)
	if len(page.Items) == 0 {
		f.t.Fatalf("no set.edit audit entry for %s", id)
	}
	return page.Items[0]
}

func auditStrings(value interface{}) []string {
	raw, _ := value.([]interface{})
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		s, _ := item.(string)
		out = append(out, s)
	}
	return out
}

func writeGeoFile(t *testing.T, path string, tags ...string) {
	t.Helper()
	field := func(out, data []byte) []byte {
		out = append(out, 0x0a)
		out = binary.AppendUvarint(out, uint64(len(data)))
		return append(out, data...)
	}
	var raw []byte
	for _, tag := range tags {
		raw = field(raw, field(nil, []byte(tag)))
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) insertVersion(title string, projection map[string]interface{}, like *store.Version) string {
	f.t.Helper()
	id, err := hubdata.NewSetID(f.clock)
	if err != nil {
		f.t.Fatal(err)
	}
	at := f.clock.Add(time.Minute)
	v := &store.Version{Title: title, Projection: projection, Status: hubwire.SetStatusPending, B4Min: hubwire.BaselineVersion, UploaderHMAC: strings.Repeat("cd", 32), CreatedAt: at, UpdatedAt: at}
	if like != nil {
		v.FP, v.TargetsKey, v.Payloads, v.Flags, v.Family = like.FP, like.TargetsKey, like.Payloads, like.Flags, like.Family
	}
	if err := f.store.CreateSet(context.Background(), store.Set{ID: id, AuthorHMAC: v.UploaderHMAC, CreatedAt: at, UpdatedAt: at}, v); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func TestEditEachTargetKind(t *testing.T) {
	cases := []struct {
		name    string
		changes map[string]interface{}
		list    bool
		want    func(TargetsView) bool
	}{
		{"ip", map[string]interface{}{"ip": []string{"93.184.216.0/24"}}, true, func(v TargetsView) bool { return reflect.DeepEqual(v.IPs, []string{"93.184.216.0/24"}) }},
		{"geosite", map[string]interface{}{"geosite_categories": []string{"youtube"}}, true, func(v TargetsView) bool { return reflect.DeepEqual(v.GeoSite, []string{"youtube"}) }},
		{"geoip", map[string]interface{}{"geoip_categories": []string{"ru"}}, true, func(v TargetsView) bool { return reflect.DeepEqual(v.GeoIP, []string{"ru"}) }},
		{"asns", map[string]interface{}{"asns": []string{"13335"}}, true, func(v TargetsView) bool { return reflect.DeepEqual(v.ASNs, []string{"13335"}) }},
		{"tls", map[string]interface{}{"tls": "1.3"}, false, hasFilter(FilterTLSOnly, "1.3")},
		{"ip_version", map[string]interface{}{"ip_version": "4"}, false, hasFilter(FilterIPOnly, "4")},
		{"domain_only", map[string]interface{}{"domain_only": true}, false, hasFilter(FilterDomainOnly, "")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, password)
			ctx := context.Background()
			id, _ := f.share("Kinds", authorAddress, "kinds.example")
			before := f.versionOf(id, 1)
			req := EditRequest{Title: before.Title, Projection: withTargets(t, before.Projection, c.changes)}
			preview := f.preview(id, req)
			if !preview.Changed || preview.FPChanged || preview.FP != before.FP || preview.Duplicate != nil {
				t.Fatalf("a %s edit changes targets only: %+v", c.name, preview)
			}
			if !c.want(preview.Targets) || !reflect.DeepEqual(preview.Targets.Domains, []string{"kinds.example"}) {
				t.Fatalf("previewed targets: %+v", preview.Targets)
			}
			f.expectOK(f.admin(http.MethodPost, setPath(id, 1, "edit"), req))
			after := f.versionOf(id, 1)
			if after.FP != before.FP {
				t.Fatalf("a target edit keeps the fingerprint: %s -> %s", before.FP, after.FP)
			}
			if keyChanged := after.TargetsKey != before.TargetsKey; keyChanged != c.list {
				t.Fatalf("targets key changed %v, want %v", keyChanged, c.list)
			}
			if votes, err := f.store.VotesForVersion(ctx, id, 1); err != nil || len(votes) != 1 || votes[0].FP != before.FP {
				t.Fatalf("votes must survive a target edit: %v %+v", err, votes)
			}
			if stored := targetsView(TargetsOf(after.Projection)); !reflect.DeepEqual(stored, preview.Targets) {
				t.Fatalf("stored targets %+v, previewed %+v", stored, preview.Targets)
			}
		})
	}
}

func TestEditASNsMoveTheMinimumVersion(t *testing.T) {
	f := newFixture(t, password)
	id, _ := f.share("Cloudflare", authorAddress, "cf.example")
	before := f.versionOf(id, 1)
	if before.B4Min != hubwire.BaselineVersion {
		t.Fatalf("a domain set needs the baseline release, got %q", before.B4Min)
	}
	req := EditRequest{Title: before.Title, Projection: withTargets(t, before.Projection, map[string]interface{}{"asns": []string{"AS13335", "as13335", " 15169 "}})}
	preview := f.preview(id, req)
	if preview.B4Min != "1.83.0" || !reflect.DeepEqual(preview.Targets.ASNs, []string{"13335", "15169"}) || preview.FPChanged {
		t.Fatalf("ASN targets: %+v", preview)
	}
	changed := warningOf(preview.Warnings, "values_changed")
	if changed == nil || !strings.Contains(fmt.Sprint(changed.Params["fields"]), "targets.asns") {
		t.Fatalf("the canonical ASNs must be reported as changed values: %+v", preview.Warnings)
	}
	f.expectOK(f.admin(http.MethodPost, setPath(id, 1, "edit"), req))
	after := f.versionOf(id, 1)
	if after.B4Min != "1.83.0" || !reflect.DeepEqual(store.TargetList(after.Projection, "asns"), []string{"13335", "15169"}) {
		t.Fatalf("stored ASN edit: %q %v", after.B4Min, after.Projection["targets"])
	}
	entry := f.lastEditAudit(id)
	if entry.Before["b4_min"] != hubwire.BaselineVersion || entry.After["b4_min"] != "1.83.0" {
		t.Fatalf("the audit must record the release change: %v -> %v", entry.Before, entry.After)
	}
	if got := auditStrings(entry.After["targets_added"]); !reflect.DeepEqual(got, []string{"asn:13335", "asn:15169"}) {
		t.Fatalf("added targets: %v", got)
	}

	f.clock = f.clock.Add(time.Minute)
	removed := EditRequest{Title: after.Title, Projection: withTargets(t, after.Projection, map[string]interface{}{"asns": nil})}
	if preview = f.preview(id, removed); preview.B4Min != hubwire.BaselineVersion {
		t.Fatalf("removing the ASNs must lower the release again: %q", preview.B4Min)
	}
	f.expectOK(f.admin(http.MethodPost, setPath(id, 1, "edit"), removed))
	if v := f.versionOf(id, 1); v.B4Min != hubwire.BaselineVersion {
		t.Fatalf("stored release after removing the ASNs: %q", v.B4Min)
	}
	entry = f.lastEditAudit(id)
	if entry.Before["b4_min"] != "1.83.0" || entry.After["b4_min"] != hubwire.BaselineVersion {
		t.Fatalf("the audit must record the release going back: %v -> %v", entry.Before, entry.After)
	}
	if got := auditStrings(entry.After["targets_removed"]); !reflect.DeepEqual(got, []string{"asn:13335", "asn:15169"}) {
		t.Fatalf("removed targets: %v", got)
	}
}

func invalidFields(t *testing.T, body ErrorBody) []InvalidFieldView {
	t.Helper()
	raw, err := json.Marshal(body.Params["fields"])
	if err != nil {
		t.Fatal(err)
	}
	var out []InvalidFieldView
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestEditRefusesInvalidTargetsByField(t *testing.T) {
	f := newFixture(t, password)
	id, _ := f.share("Invalid", authorAddress, "invalid.example")
	v := f.versionOf(id, 1)

	reserved := EditRequest{Title: v.Title, Projection: withTargets(t, v.Projection, map[string]interface{}{"asns": []string{"15169", "64512"}})}
	for _, action := range []string{"preview", "edit"} {
		body := f.expectError(f.admin(http.MethodPost, setPath(id, 1, action), reserved), http.StatusBadRequest, codeInvalidSet)
		fields := invalidFields(t, body)
		if len(fields) != 1 || fields[0].Path != "targets.asns" || fields[0].Code != "asn_invalid" || fields[0].Params["value"] != "64512" || fields[0].Message == "" {
			t.Fatalf("%s: a reserved ASN must be named by field: %+v", action, fields)
		}
	}

	clamp := projectionCopy(t, v.Projection)
	clamp["mss_clamp"] = map[string]interface{}{"enabled": true, "size": 88}
	body := f.expectError(f.admin(http.MethodPost, setPath(id, 1, "preview"), EditRequest{Title: v.Title, Projection: clamp}), http.StatusBadRequest, codeInvalidSet)
	if fields := invalidFields(t, body); len(fields) != 1 || fields[0].Path != "mss_clamp" || fields[0].Code != "mss_clamp_scope_required" {
		t.Fatalf("an MSS clamp without an address target must be named by field: %+v", fields)
	}
	scoped := f.preview(id, EditRequest{Title: v.Title, Projection: withTargets(t, clamp, map[string]interface{}{"ip": []string{"93.184.216.0/24"}})})
	if !scoped.FPChanged {
		t.Fatalf("an MSS clamp with an address target is a strategy change: %+v", scoped)
	}

	if body := f.expectError(f.admin(http.MethodPost, setPath(id, 1, "preview"), EditRequest{Title: v.Title, Projection: map[string]interface{}{"tcp": "nonsense"}}), http.StatusBadRequest, codeInvalidSet); body.Params != nil {
		t.Fatalf("a set that does not decode has no field list: %+v", body.Params)
	}
}

func TestEditFlagsFollowTheTargets(t *testing.T) {
	f := newFixture(t, password)
	id, _ := f.share("Flags", authorAddress, "flags.example")
	v := f.versionOf(id, 1)

	regexp := f.preview(id, EditRequest{Title: v.Title, Projection: withTargets(t, v.Projection, map[string]interface{}{"sni_domains": []string{`regexp:.*\.flags\.example$`}})})
	if !slices.Contains(regexp.Flags, ingest.FlagCatchAll) {
		t.Fatalf("a regexp domain is a catch-all: %v", regexp.Flags)
	}
	everything := f.preview(id, EditRequest{Title: v.Title, Projection: withTargets(t, v.Projection, map[string]interface{}{"ip": []string{"0.0.0.0/0"}})})
	if slices.Contains(everything.Flags, ingest.FlagCatchAll) {
		t.Fatalf("an address catch-all is a warning, not the catch_all flag: %v", everything.Flags)
	}
	geoipOnly := f.preview(id, EditRequest{Title: v.Title, Projection: withTargets(t, v.Projection, map[string]interface{}{"sni_domains": nil, "geoip_categories": []string{"ru"}})})
	if slices.Contains(geoipOnly.Flags, ingest.FlagBlanket) {
		t.Fatalf("a GeoIP-only set is not blanket: %v", geoipOnly.Flags)
	}
	blanket := EditRequest{Title: v.Title, Projection: withTargets(t, v.Projection, map[string]interface{}{"sni_domains": nil, "geosite_categories": []string{"youtube"}})}
	if p := f.preview(id, blanket); !slices.Contains(p.Flags, ingest.FlagBlanket) {
		t.Fatalf("a geosite-only set is blanket: %v", p.Flags)
	}
	f.expectOK(f.admin(http.MethodPost, setPath(id, 1, "edit"), blanket))
	if after := f.versionOf(id, 1); !slices.Contains(after.Flags, ingest.FlagBlanket) {
		t.Fatalf("the stored flags must follow the edit: %v", after.Flags)
	}
}

func TestEditDropsAPinWithItsDomain(t *testing.T) {
	f := newFixture(t, password)
	set := testkit.SampleSet("Pinned", "pinned.example", "kept.example")
	set.DNS.Pins = map[string][]string{"pinned.example": {"93.184.216.34"}}
	id, _ := f.shareAs(testkit.Identity(t), set, authorAddress)
	v := f.versionOf(id, 1)
	if !slices.Contains(v.Flags, ingest.FlagHasPins) {
		t.Fatalf("the shared set must carry its pin: %v", v.Flags)
	}
	req := EditRequest{Title: v.Title, Projection: withTargets(t, v.Projection, map[string]interface{}{"sni_domains": []string{"kept.example"}})}
	preview := f.preview(id, req)
	pin := warningOf(preview.Warnings, pinNotTargetedWarning)
	if pin == nil || pin.Params["domain"] != "pinned.example" {
		t.Fatalf("the dropped pin must be reported: %+v", preview.Warnings)
	}
	for _, s := range preview.Stripped {
		if strings.HasPrefix(s.Path, pinsPath) {
			t.Fatalf("a pin dropped with its domain is reported by its warning, not as a stripped field: %+v", preview.Stripped)
		}
	}
	if _, ok := lookupPath(preview.Projection, pinsPath); ok || slices.Contains(preview.Flags, ingest.FlagHasPins) || preview.FPChanged {
		t.Fatalf("the pin must go without touching the strategy: %+v", preview)
	}
	typed := withTargets(t, v.Projection, map[string]interface{}{"sni_domains": []string{"kept.example"}})
	typed["dns"] = map[string]interface{}{"pins": map[string]interface{}{"Pinned.Example.": []string{"93.184.216.34"}, "kept.example": []string{"10.0.0.1"}}}
	typedPreview := f.preview(id, EditRequest{Title: v.Title, Projection: typed})
	if warningOf(typedPreview.Warnings, pinNotTargetedWarning) == nil || warningOf(typedPreview.Warnings, pinPrivateAddressWarning) == nil {
		t.Fatalf("both dropped pins must be reported: %+v", typedPreview.Warnings)
	}
	for _, s := range typedPreview.Stripped {
		if strings.HasPrefix(s.Path, pinsPath) {
			t.Fatalf("pins named in another case are covered by their warnings too: %+v", typedPreview.Stripped)
		}
	}
	f.expectOK(f.admin(http.MethodPost, setPath(id, 1, "edit"), req))
	after := f.versionOf(id, 1)
	if _, ok := lookupPath(after.Projection, pinsPath); ok || slices.Contains(after.Flags, ingest.FlagHasPins) || after.FP != v.FP {
		t.Fatalf("stored after dropping the pinned domain: %v %v", after.Projection, after.Flags)
	}
}

func TestEditTargetCollisionIsADuplicate(t *testing.T) {
	f := newFixture(t, password)
	first, _ := f.share("First", authorAddress, "first.example")
	second, _ := f.share("Second", otherAddress, "second.example")
	b := f.versionOf(second, 1)
	req := EditRequest{Title: b.Title, Projection: withTargets(t, b.Projection, map[string]interface{}{"sni_domains": []string{"FIRST.example."}})}
	preview := f.preview(second, req)
	if preview.FPChanged || preview.Duplicate == nil || preview.Duplicate.SetID != first || preview.Duplicate.Version != 1 {
		t.Fatalf("a target edit onto another set's targets must name it: %+v", preview.Duplicate)
	}
	body := f.expectError(f.admin(http.MethodPost, setPath(second, 1, "edit"), req), http.StatusConflict, codeDuplicate)
	if body.Params["set_id"] != first {
		t.Fatalf("the refusal must name the duplicate: %+v", body)
	}
}

func TestEditRefusesEmptiedTargets(t *testing.T) {
	f := newFixture(t, password)
	id, _ := f.share("Emptied", authorAddress, "emptied.example")
	v := f.versionOf(id, 1)
	for _, changes := range []map[string]interface{}{
		{"sni_domains": nil, "tls": "1.3", "domain_only": true},
		{"sni_domains": []string{}, "ip": []string{}, "geosite_categories": []string{}, "geoip_categories": []string{}, "asns": []string{}, "ip_version": "6"},
	} {
		req := EditRequest{Title: v.Title, Projection: withTargets(t, v.Projection, changes)}
		for _, action := range []string{"preview", "edit"} {
			body := f.expectError(f.admin(http.MethodPost, setPath(id, 1, action), req), http.StatusBadRequest, codeInvalidSet)
			if !strings.Contains(body.Error, "no targets") {
				t.Fatalf("%s with only filters left: %+v", action, body)
			}
		}
	}
}

func TestEditWarnsAboutAddressesRoutersSkip(t *testing.T) {
	f := newFixture(t, password)
	id, _ := f.share("Addresses", authorAddress, "addresses.example")
	v := f.versionOf(id, 1)
	addresses := []string{"0.0.0.0/0", "::/0", "1.2.3.4-1.2.3.9", "not-an-ip", "10.0.0.0/8", "8.8.8.8/24", "2606:4700::/32", "93.184.216.34", "not-an-ip"}
	req := EditRequest{Title: v.Title, Projection: withTargets(t, v.Projection, map[string]interface{}{"ip": addresses})}
	preview := f.preview(id, req)
	if got := warnedList(preview.Warnings, privateAddressesWarning, "addresses"); !reflect.DeepEqual(got, []string{"10.0.0.0/8"}) {
		t.Fatalf("private addresses: %v", got)
	}
	if got := warnedList(preview.Warnings, invalidAddressesWarning, "addresses"); !reflect.DeepEqual(got, []string{"1.2.3.4-1.2.3.9", "not-an-ip"}) {
		t.Fatalf("addresses routers never match: %v", got)
	}
	if got := warnedList(preview.Warnings, catchAllAddressesWarning, "addresses"); !reflect.DeepEqual(got, []string{"0.0.0.0/0", "::/0"}) {
		t.Fatalf("catch-all addresses: %v", got)
	}
	f.expectOK(f.admin(http.MethodPost, setPath(id, 1, "edit"), req))
	if got := store.TargetList(f.versionOf(id, 1).Projection, "ip"); !reflect.DeepEqual(got, addresses) {
		t.Fatalf("addresses are warned about, never rewritten: %v", got)
	}

	only := f.preview(id, EditRequest{Title: v.Title, Projection: withTargets(t, v.Projection, map[string]interface{}{"ip": []string{"::/0"}})})
	if warningOf(only.Warnings, privateAddressesWarning) != nil || !reflect.DeepEqual(warnedList(only.Warnings, catchAllAddressesWarning, "addresses"), []string{"::/0"}) {
		t.Fatalf("a catch-all is reported once, under its own code: %+v", only.Warnings)
	}
	clean := f.preview(id, EditRequest{Title: v.Title, Projection: withTargets(t, v.Projection, map[string]interface{}{"ip": []string{" 93.184.216.34 ", "8.8.8.8/24"}})})
	for _, code := range []string{privateAddressesWarning, invalidAddressesWarning, catchAllAddressesWarning} {
		if warningOf(clean.Warnings, code) != nil {
			t.Fatalf("public addresses and prefixes pass: %+v", clean.Warnings)
		}
	}
}

func TestGeoCategoriesComeFromTheHubFiles(t *testing.T) {
	f := newFixture(t, password)
	categories := func() GeoCategoriesView {
		t.Helper()
		resp := f.admin(http.MethodGet, PathAPI+"/geo/categories", nil)
		if resp.status != http.StatusOK {
			t.Fatalf("categories: %d %s", resp.status, resp.body)
		}
		if !strings.Contains(resp.body, `"geosite":[`) || !strings.Contains(resp.body, `"geoip":[`) {
			t.Fatalf("both lists must be arrays: %s", resp.body)
		}
		var view GeoCategoriesView
		resp.decode(t, &view)
		return view
	}
	if view := categories(); len(view.GeoSite) != 0 || len(view.GeoIP) != 0 {
		t.Fatalf("a hub without a geo service has no categories: %+v", view)
	}
	dir := t.TempDir()
	f.web.Geo = geo.New(geo.Options{Dir: dir})
	if view := categories(); len(view.GeoSite) != 0 || len(view.GeoIP) != 0 {
		t.Fatalf("a hub that has not downloaded its files has no categories: %+v", view)
	}
	writeGeoFile(t, filepath.Join(dir, geo.GeoSiteFile), "YOUTUBE", "CATEGORY-RU", "youtube")
	if view := categories(); !reflect.DeepEqual(view.GeoSite, []string{"category-ru", "youtube"}) || len(view.GeoIP) != 0 {
		t.Fatalf("geosite tags, lowercased and sorted: %+v", view)
	}
	writeGeoFile(t, filepath.Join(dir, geo.GeoIPFile), "RU", "PRIVATE")
	if view := categories(); !reflect.DeepEqual(view.GeoIP, []string{"private", "ru"}) {
		t.Fatalf("geoip tags: %+v", view)
	}
}

func TestEditWarnsAboutCategoriesMissingFromTheHubFiles(t *testing.T) {
	f := newFixture(t, password)
	id, _ := f.share("Categories", authorAddress, "categories.example")
	v := f.versionOf(id, 1)
	req := EditRequest{Title: v.Title, Projection: withTargets(t, v.Projection, map[string]interface{}{
		"geosite_categories": []string{"YouTube", "category-ru@cn", "made-up", "made-up"},
		"geoip_categories":   []string{"RU", "zz"},
	})}
	if p := f.preview(id, req); warningOf(p.Warnings, geositeMissingWarning) != nil || warningOf(p.Warnings, geoipMissingWarning) != nil {
		t.Fatalf("without geo files nothing can be called missing: %+v", p.Warnings)
	}
	dir := t.TempDir()
	f.web.Geo = geo.New(geo.Options{Dir: dir})
	writeGeoFile(t, filepath.Join(dir, geo.GeoSiteFile), "YOUTUBE", "CATEGORY-RU")
	p := f.preview(id, req)
	if got := warnedList(p.Warnings, geositeMissingWarning, "categories"); !reflect.DeepEqual(got, []string{"made-up"}) {
		t.Fatalf("missing geosite categories, attributes and case ignored: %v", got)
	}
	if warningOf(p.Warnings, geoipMissingWarning) != nil {
		t.Fatalf("without a geoip file no GeoIP category is missing: %+v", p.Warnings)
	}
	writeGeoFile(t, filepath.Join(dir, geo.GeoIPFile), "RU")
	p = f.preview(id, req)
	if got := warnedList(p.Warnings, geoipMissingWarning, "categories"); !reflect.DeepEqual(got, []string{"zz"}) {
		t.Fatalf("missing geoip categories: %v", got)
	}
	f.expectOK(f.admin(http.MethodPost, setPath(id, 1, "edit"), req))
	if got := store.TargetList(f.versionOf(id, 1).Projection, "geosite_categories"); !reflect.DeepEqual(got, []string{"YouTube", "category-ru@cn", "made-up", "made-up"}) {
		t.Fatalf("a missing category never refuses the edit or rewrites the list: %v", got)
	}
}

func TestEditAuditListsTargetChanges(t *testing.T) {
	f := newFixture(t, password)
	id, _ := f.share("Audited", authorAddress, "a.example", "b.example")
	v := f.versionOf(id, 1)
	req := EditRequest{Title: v.Title, Projection: withTargets(t, v.Projection, map[string]interface{}{
		"sni_domains": []string{"b.example", "C.example", "d.example"},
		"ip":          []string{"93.184.216.0/24"},
	})}
	f.expectOK(f.admin(http.MethodPost, setPath(id, 1, "edit"), req))
	entry := f.lastEditAudit(id)
	if got := auditStrings(entry.After["targets_added"]); !reflect.DeepEqual(got, []string{"ip:93.184.216.0/24", "sni:c.example", "sni:d.example"}) {
		t.Fatalf("added targets: %v", got)
	}
	if got := auditStrings(entry.After["targets_removed"]); !reflect.DeepEqual(got, []string{"sni:a.example"}) {
		t.Fatalf("removed targets: %v", got)
	}
	_, before := entry.Before["b4_min"]
	_, after := entry.After["b4_min"]
	_, more := entry.After["targets_added_more"]
	if before || after || more {
		t.Fatalf("an unchanged release and a short list carry no extra keys: %v -> %v", entry.Before, entry.After)
	}

	many := make([]string, 0, 25)
	for i := range 25 {
		many = append(many, fmt.Sprintf("host%02d.example", i))
	}
	f.clock = f.clock.Add(time.Minute)
	edited := f.versionOf(id, 1)
	f.expectOK(f.admin(http.MethodPost, setPath(id, 1, "edit"), EditRequest{Title: edited.Title, Projection: withTargets(t, edited.Projection, map[string]interface{}{"sni_domains": many, "ip": nil})}))
	entry = f.lastEditAudit(id)
	if got := auditStrings(entry.After["targets_added"]); len(got) != 20 || got[0] != "sni:host00.example" || entry.After["targets_added_more"] != float64(5) {
		t.Fatalf("a long list is capped with the rest counted: %v more %v", got, entry.After["targets_added_more"])
	}
	if got := auditStrings(entry.After["targets_removed"]); !reflect.DeepEqual(got, []string{"ip:93.184.216.0/24", "sni:b.example", "sni:c.example", "sni:d.example"}) {
		t.Fatalf("removed targets: %v", got)
	}
}

func TestPreviewDuplicateSkipsTheEditedVersion(t *testing.T) {
	f := newFixture(t, password)
	id, _ := f.share("Original", authorAddress, "original.example")
	v := f.versionOf(id, 1)
	copyID := f.insertVersion("Copy", v.Projection, v)
	req := EditRequest{Title: "Original (renamed)", Projection: v.Projection}
	preview := f.preview(id, req)
	if preview.Duplicate == nil || preview.Duplicate.SetID != copyID || preview.Duplicate.Version != 1 {
		t.Fatalf("the preview must name the other version even when the edited one ranks first: %+v", preview.Duplicate)
	}
	body := f.expectError(f.admin(http.MethodPost, setPath(id, 1, "edit"), req), http.StatusConflict, codeDuplicate)
	if body.Params["set_id"] != copyID || body.Params["version"] != float64(1) {
		t.Fatalf("the save must refuse the same duplicate: %+v", body)
	}
}

func TestEntriesCarryTheFullConfig(t *testing.T) {
	f := newFixture(t, password)
	id, _ := f.share("Configured", authorAddress, "configured.example")
	brokenID := f.insertVersion("Broken", map[string]interface{}{"tcp": "nonsense", "targets": map[string]interface{}{"sni_domains": []interface{}{"broken.example"}}}, nil)

	full := func(config map[string]interface{}) bool {
		frag, _ := config["fragmentation"].(map[string]interface{})
		faking, _ := config["faking"].(map[string]interface{})
		targets, _ := config["targets"].(map[string]interface{})
		geosite, isList := targets["geosite_categories"].([]interface{})
		_, hasUDP := config["udp"].(map[string]interface{})
		return config["name"] == "Configured" && frag["strategy"] == "tls" && faking["ttl"] == float64(7) && isList && len(geosite) == 0 && hasUDP
	}

	var queue QueueView
	f.admin(http.MethodGet, PathAPI+"/queue", nil).decode(t, &queue)
	if e := entryOf(queue.Items, id, 1); e == nil || !full(e.Config) {
		t.Fatalf("a queue entry carries the set with defaults filled: %+v", e)
	}
	if e := entryOf(queue.Items, brokenID, 1); e == nil || e.DecodeError == "" || e.Config != nil {
		t.Fatalf("an entry that does not decode has no config: %+v", e)
	}
	var raw struct {
		Items []map[string]interface{} `json:"items"`
	}
	f.admin(http.MethodGet, PathAPI+"/queue", nil).decode(t, &raw)
	for _, item := range raw.Items {
		if _, has := item["config"]; item["set_id"] == brokenID && has {
			t.Fatalf("config must be omitted, not null: %v", item["config"])
		}
	}

	var rows SetRowsView
	f.admin(http.MethodGet, PathAPI+"/sets/rows?group=pending", nil).decode(t, &rows)
	for _, row := range rows.Rows {
		switch row.SetID {
		case id:
			if !full(row.Config) || row.DecodeError {
				t.Fatalf("a set row carries the set with defaults filled: %+v", row)
			}
		case brokenID:
			if row.Config != nil || !row.DecodeError {
				t.Fatalf("a row that does not decode has no config: %+v", row)
			}
		}
	}

	var detail SetDetailView
	f.admin(http.MethodGet, PathAPI+"/sets/"+id, nil).decode(t, &detail)
	if len(detail.Versions) != 1 || !full(detail.Versions[0].Config) {
		t.Fatalf("every version in the set detail carries its config: %+v", detail.Versions)
	}
	if sets := f.setsView(); entryOf(sets.Pending, id, 1) == nil || !full(entryOf(sets.Pending, id, 1).Config) {
		t.Fatalf("the grouped sets view carries the config too")
	}
}

func TestVoteOriginsListEveryRecordedOrigin(t *testing.T) {
	f := newFixture(t, password)
	id, env := f.share("Origins", authorAddress, "origins.example")
	f.expectOK(f.admin(http.MethodPost, setPath(id, 1, "approve"), nil))
	f.build()
	f.vote(id, 1, env.Fingerprint, voterAddress)
	f.vote(id, 1, env.Fingerprint, otherAddress)
	f.vote(id, 1, env.Fingerprint, otherAddress)
	f.vote(id, 1, env.Fingerprint, fourthAddress)

	resp := f.admin(http.MethodGet, PathAPI+"/votes/origins", nil)
	if resp.status != http.StatusOK {
		t.Fatalf("origins: %d %s", resp.status, resp.body)
	}
	var view VoteOriginsView
	resp.decode(t, &view)
	wantASNs := []MixView{
		{Key: "64501", Name: "EXAMPLE-B ISP B", Country: "DE", Votes: 3, Keys: 3},
		{Key: "64500", Name: "EXAMPLE-A ISP A", Country: "RU", Votes: 2, Keys: 2},
	}
	if !reflect.DeepEqual(view.ASNs, wantASNs) {
		t.Fatalf("ASNs with names and their most common country:\n got %+v\nwant %+v", view.ASNs, wantASNs)
	}
	wantCountries := []MixView{{Key: "DE", Votes: 2, Keys: 2}, {Key: "RU", Votes: 2, Keys: 2}, {Key: "NL", Votes: 1, Keys: 1}}
	if !reflect.DeepEqual(view.Countries, wantCountries) {
		t.Fatalf("countries:\n got %+v\nwant %+v", view.Countries, wantCountries)
	}

	for query, want := range map[string]int{"?asn=AS64501": 3, "?asn=64500": 2, "?cc=nl": 1, "?cc=DE": 2} {
		var page VotesPageView
		f.admin(http.MethodGet, PathAPI+"/votes"+query, nil).decode(t, &page)
		if page.Total != want {
			t.Fatalf("every listed origin must select its votes: %s gave %d, want %d", query, page.Total, want)
		}
	}
	if resp := f.get(PathAPI + "/votes/origins"); resp.status != http.StatusUnauthorized {
		t.Fatalf("origins are a console read: %d", resp.status)
	}
}

func TestVoteOriginsAreEmptyArraysWithoutVotes(t *testing.T) {
	f := newFixture(t, password)
	resp := f.admin(http.MethodGet, PathAPI+"/votes/origins", nil)
	if resp.status != http.StatusOK || strings.TrimSpace(resp.body) != `{"asns":[],"countries":[]}` {
		t.Fatalf("a hub without votes: %d %s", resp.status, resp.body)
	}
}
