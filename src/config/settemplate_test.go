package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func decodeTemplate(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return m
}

func templateJSON(t *testing.T, m map[string]interface{}) string {
	t.Helper()
	if m == nil {
		return "null"
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return string(data)
}

func TestNormalizeSetTemplateKeepsOnlyTheChangedFields(t *testing.T) {
	routingOnly := `{"faking":{"sni":false},"fragmentation":{"strategy":"none"}}`

	full := NewSetConfig()
	full.Id = "c0ffee"
	full.Name = "Proxy"
	full.Enabled = false
	full.Targets.SNIDomains = []string{"example.com"}
	full.Discovery.URLs = []string{"https://example.com/"}
	full.Fragmentation.Strategy = ConfigNone
	full.Faking.SNI = false
	fullJSON, err := json.Marshal(&full)
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"the set editor's export": `{"b4_version":"dev","name":"Сет 2","fragmentation":{"strategy":"none"},"faking":{"sni":false},"enabled":true}`,
		"a whole set":             string(fullJSON),
		"only the changes":        routingOnly,
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := NormalizeSetTemplate(decodeTemplate(t, input))
			if err != nil {
				t.Fatalf("normalize: %v", err)
			}
			if templateJSON(t, got) != routingOnly {
				t.Errorf("template = %s, want %s", templateJSON(t, got), routingOnly)
			}
		})
	}
}

func TestNormalizeSetTemplateDropsTheSetsOwnFields(t *testing.T) {
	input := `{"id":"x","name":"YouTube","enabled":false,"targets":{"sni_domains":["youtube.com"]},"hub":{"id":"h"},"discovery":{"urls":["https://youtube.com/"]},"revision":"r","b4_version":"1.91.0"}`
	got, err := NormalizeSetTemplate(decodeTemplate(t, input))
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if got != nil {
		t.Errorf("template = %s, want none: a set's own fields never become defaults for new sets", templateJSON(t, got))
	}
}

func TestNormalizeSetTemplateOfTheDefaultsIsEmpty(t *testing.T) {
	for _, input := range []string{`{}`, `{"fragmentation":{"strategy":"combo"},"faking":{"sni":true}}`, `null`} {
		got, err := NormalizeSetTemplate(decodeTemplate(t, input))
		if err != nil {
			t.Fatalf("normalize %s: %v", input, err)
		}
		if got != nil {
			t.Errorf("normalize %s = %s, want no template", input, templateJSON(t, got))
		}
	}
}

func TestNormalizeSetTemplateIsIdempotent(t *testing.T) {
	input := `{"fragmentation":{"strategy":"disorder","disorder":{"shuffle_mode":"reverse"}},"tcp":{"desync":{"mode":"rst","ttl":4}},"routing":{"enabled":true,"mode":"proxy","upstream":{"host":"192.168.1.2","port":1080}}}`
	once, err := NormalizeSetTemplate(decodeTemplate(t, input))
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	twice, err := NormalizeSetTemplate(once)
	if err != nil {
		t.Fatalf("normalize again: %v", err)
	}
	if !reflect.DeepEqual(once, twice) {
		t.Errorf("a second pass changed the template:\n%s\n%s", templateJSON(t, once), templateJSON(t, twice))
	}
}

func TestNormalizeSetTemplateReportsInvalidValuesUnderItsOwnPath(t *testing.T) {
	cases := []struct {
		name, input, path, code string
	}{
		{"unknown routing mode", `{"routing":{"mode":"nonsense"}}`, "ui.new_set_template.routing.mode", "invalid_routing_mode"},
		{"wrong type", `{"tcp":"fast"}`, "ui.new_set_template", "invalid_set_template"},
		{"plain DoH", `{"dns":{"enabled":true,"doh_url":"http://dns.example/dns-query"}}`, "ui.new_set_template.dns.doh_url", "doh_url_must_be_https"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeSetTemplate(decodeTemplate(t, tc.input))
			if got != nil {
				t.Errorf("template = %s, want none on error", templateJSON(t, got))
			}
			var ve *ValidationError
			if !errors.As(err, &ve) || len(ve.Fields) != 1 {
				t.Fatalf("error = %v, want one validation field", err)
			}
			if f := ve.Fields[0]; f.Path != tc.path || f.Code != tc.code {
				t.Errorf("field = %s %s, want %s %s", f.Path, f.Code, tc.path, tc.code)
			}
		})
	}
}

func TestTheSetTemplateIsSavedShortAndLoadedBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "b4.json")
	cfg := NewConfig()
	cfg.UI.NewSetTemplate = decodeTemplate(t, `{"fragmentation":{"strategy":"none"},"faking":{"sni":false}}`)
	if err := cfg.SaveToFile(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		UI struct {
			NewSetTemplate map[string]interface{} `json:"new_set_template"`
		} `json:"ui"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if got := templateJSON(t, file.UI.NewSetTemplate); got != `{"faking":{"sni":false},"fragmentation":{"strategy":"none"}}` {
		t.Errorf("stored template = %s", got)
	}

	loaded := NewConfig()
	if _, err := loaded.LoadWithMigration(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	if !reflect.DeepEqual(loaded.UI.NewSetTemplate, cfg.UI.NewSetTemplate) {
		t.Errorf("loaded template = %s, want %s", templateJSON(t, loaded.UI.NewSetTemplate), templateJSON(t, cfg.UI.NewSetTemplate))
	}

	empty := NewConfig()
	if err := empty.SaveToFile(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "new_set_template") {
		t.Errorf("a config without a template stores the key:\n%s", raw)
	}
}

func TestLoadDropsASetTemplateThatIsNotAValidSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "b4.json")
	body := `{"version":52,"ui":{"new_set_template":{"routing":{"mode":"nonsense"}}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := NewConfig()
	if _, err := cfg.LoadWithMigration(path); err != nil {
		t.Fatalf("an invalid template must not stop the start-up: %v", err)
	}
	if cfg.UI.NewSetTemplate != nil {
		t.Errorf("template = %s, want it dropped", templateJSON(t, cfg.UI.NewSetTemplate))
	}
}

func TestTheSetTemplateNeverChangesHowStoredSetsRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "b4.json")
	body := `{"version":52,"ui":{"new_set_template":{"fragmentation":{"strategy":"none"},"faking":{"sni":false}}},"sets":[{"id":"a","name":"A","enabled":true,"targets":{"sni_domains":["example.com"]}}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := NewConfig()
	if _, err := cfg.LoadWithMigration(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Sets) != 1 {
		t.Fatalf("loaded %d sets, want 1", len(cfg.Sets))
	}
	set := cfg.Sets[0]
	if set.Fragmentation.Strategy != DefaultSetConfig.Fragmentation.Strategy || set.Faking.SNI != DefaultSetConfig.Faking.SNI {
		t.Errorf("a stored set that omits its strategy reads strategy %q, fake %v; want b4's defaults %q, %v",
			set.Fragmentation.Strategy, set.Faking.SNI, DefaultSetConfig.Fragmentation.Strategy, DefaultSetConfig.Faking.SNI)
	}
}
