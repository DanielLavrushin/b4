package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/capture"
	"github.com/daniellavrushin/b4/config"
)

var generateClaimWords = []string{"optimiz", "optimis", "sni-first", "sni first", "dpi bypass", "bypass", "оптимиз", "обход"}

func generateClaimIn(text string) string {
	lower := strings.ToLower(text)
	for _, w := range generateClaimWords {
		if strings.Contains(lower, w) {
			return w
		}
	}
	return ""
}

func TestGenerateDescribesThePayloadWithoutABypassClaim(t *testing.T) {
	cfg := config.NewConfig()
	cfg.ConfigPath = hubTestConfigPath(t)
	manager := capture.GetManager(&cfg)
	if err := os.MkdirAll(manager.GetOutputPath(), 0755); err != nil {
		t.Fatal(err)
	}
	api := &API{cfgPtr: testCfgPtr(&cfg)}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterCaptureApi()

	const domain = "claim.generate.example"
	manager.DeleteCapture("tls", domain)
	t.Cleanup(func() { manager.DeleteCapture("tls", domain) })

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/capture/generate", strings.NewReader(`{"domain":"`+domain+`"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("generate: %d (%s)", rec.Code, rec.Body.String())
	}
	var out struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("generate answered %q: %v", rec.Body.String(), err)
	}
	if w := generateClaimIn(out.Message); w != "" {
		t.Fatalf("the generate response claims a property no measurement supports (%q): %s", w, strings.TrimSpace(rec.Body.String()))
	}
	saved, ok := manager.GetCapture("tls", domain)
	if !ok {
		t.Fatal("generate stored no payload")
	}
	if want := fmt.Sprintf("Generated a %d-byte TLS ClientHello for %s", saved.Size, domain); out.Message != want {
		t.Errorf("message = %q, want %q", out.Message, want)
	}
}

func generateClaimLocale(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return doc
}

func generateClaimBlock(t *testing.T, doc map[string]any, path ...string) map[string]any {
	t.Helper()
	cur := doc
	for _, p := range path {
		next, ok := cur[p].(map[string]any)
		if !ok {
			t.Fatalf("no %s block", strings.Join(path, "."))
		}
		cur = next
	}
	return cur
}

func TestPayloadGenerationStringsMakeNoBypassClaim(t *testing.T) {
	checked := map[string][]string{
		"settings.Capture":    {"domainHelp", "domainInfo", "generateDescription", "generateSuccess", "generatedDesc", "emptyDesc"},
		"sets.faking.fakeSni": {"noPayloads", "selectPayloadHelper"},
	}
	plurals := map[string][]string{
		"en.json": {"_one", "_other"},
		"ru.json": {"_one", "_few", "_many"},
	}
	for locale, forms := range plurals {
		t.Run(locale, func(t *testing.T) {
			doc := generateClaimLocale(t, "../ui/src/i18n/"+locale)
			for blockPath, keys := range checked {
				block := generateClaimBlock(t, doc, strings.Split(blockPath, ".")...)
				for _, key := range keys {
					found := 0
					for k, v := range block {
						if k != key && !strings.HasPrefix(k, key+"_") {
							continue
						}
						found++
						text, _ := v.(string)
						if w := generateClaimIn(text); w != "" {
							t.Errorf("%s.%s says %q: %q", blockPath, k, w, text)
						}
					}
					if found == 0 {
						t.Errorf("%s.%s is missing", blockPath, key)
					}
				}
			}
			capture := generateClaimBlock(t, doc, "settings", "Capture")
			for _, form := range forms {
				if _, ok := capture["generatedDesc"+form].(string); !ok {
					t.Errorf("settings.Capture.generatedDesc has no %s form", form)
				}
			}
		})
	}
}
