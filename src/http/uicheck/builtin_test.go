package uicheck

import (
	"regexp"
	"strings"
	"testing"
)

var builtinLabelKeys = map[string]string{
	"google": "sets.faking.fakeSni.payloadGoogle",
	"vk":     "sets.faking.fakeSni.payloadVk",
	"stun":   "sets.faking.fakeSni.payloadStun",
	"quic1":  "sets.udp.fakePayloadFilePreset1",
	"quic2":  "sets.udp.fakePayloadFilePreset2",
}

func TestBuiltinPayloadsAreListedReadOnly(t *testing.T) {
	api := readUI(t, "api/capture.ts")
	for _, route := range []string{`"/api/capture/builtin"`, "/api/capture/builtin/download?"} {
		if !strings.Contains(api, route) {
			t.Errorf("captureApi never calls %s", route)
		}
	}
	body := regexp.MustCompile(`(?s)interface BuiltinPayload \{(.*?)\n\}`).FindStringSubmatch(readUI(t, "models/capture.ts"))
	for _, field := range []string{"id", "protocol", "size", "facts"} {
		if body == nil || !regexp.MustCompile(`(?m)^\s*`+field+`\??:`).MatchString(body[1]) {
			t.Errorf("the BuiltinPayload model has no %s field", field)
		}
	}
	if !regexp.MustCompile(`export function useBuiltinPayloads\b`).MatchString(readUI(t, "hooks/useCapture.ts")) {
		t.Error("no useBuiltinPayloads hook for the Payloads page and the set editor")
	}

	page := payloadsPageSource(t)
	m := regexp.MustCompile(`(?s)builtins\.map\(.*?<PayloadCard\b(.*?)/>`).FindStringSubmatch(page)
	if m == nil {
		t.Fatal("the Payloads page lists no built-in payload cards")
	}
	for _, action := range []string{"onDelete", "onRegenerate"} {
		if strings.Contains(m[1], action) {
			t.Errorf("a built-in payload card offers %s, though built-ins are read-only", action)
		}
	}
	for _, action := range []string{"onViewHex", "onDownload"} {
		if !strings.Contains(m[1], action) {
			t.Errorf("a built-in payload card has no %s", action)
		}
	}

	var keys []string
	for id, key := range builtinLabelKeys {
		if !strings.Contains(page, `"`+key+`"`) {
			t.Errorf("the built-in row %s is not labelled with the set editor's key %s", id, key)
		}
		keys = append(keys, key)
	}
	keys = append(keys, "settings.Capture.builtinTitle", "settings.Capture.builtinDesc", "settings.Capture.builtinFailed")
	requireKeysInBothLocales(t, keys)
}
