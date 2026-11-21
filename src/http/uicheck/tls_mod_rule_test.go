package uicheck

import (
	"regexp"
	"strings"
	"testing"
)

func switchReturns(body string) (map[string]string, string) {
	labels := regexp.MustCompile(`case FakingPayloadType\.(\w+):|default:`).FindAllStringSubmatchIndex(body, -1)
	returns := map[string]string{}
	fallback := ""
	var pending []string
	for i, l := range labels {
		end := len(body)
		if i+1 < len(labels) {
			end = labels[i+1][0]
		}
		name := "default"
		if l[2] >= 0 {
			name = body[l[2]:l[3]]
		}
		pending = append(pending, name)
		ret := regexp.MustCompile(`(?s)return\s+(.*?);`).FindStringSubmatch(body[l[1]:end])
		if ret == nil {
			continue
		}
		for _, p := range pending {
			if p == "default" {
				fallback = ret[1]
			} else {
				returns[p] = ret[1]
			}
		}
		pending = nil
	}
	return returns, fallback
}

func TestTLSModificationSwitchesFollowThePayloadKind(t *testing.T) {
	tcp := readUI(t, tcpFakingFile)
	block := regexp.MustCompile(`\{\s*tlsModChangesTheFake\(\s*config\s*,\s*\w+\s*\)\s*&&\s*\(\s*<Grid[^>]*>\s*<Typography[^>]*>\s*\{\s*t\("sets\.faking\.fakeSni\.tlsModTitle"\)`)
	if !block.MatchString(tcp) {
		t.Error("the Fake Packet TLS Modification block is not rendered by tlsModChangesTheFake")
	}
	if regexp.MustCompile(`sni_type\s*!==\s*FakingPayloadType\.RANDOM\s*&&`).MatchString(tcp) {
		t.Error("the Fake Packet TLS Modification block still shows for every type but Random")
	}

	helpers := readUI(t, fakePayloadHelpers)
	fn := regexp.MustCompile(`(?s)export function tlsModChangesTheFake\b(.*?)\n}\n`).FindStringSubmatch(helpers)
	if fn == nil {
		t.Fatal("no tlsModChangesTheFake rule deciding when Fake Packet TLS Modification can change the fake")
	}
	returns, fallback := switchReturns(fn[1])
	for _, typ := range []string{"DEFAULT", "DEFAULT2", "DOMAIN"} {
		if returns[typ] != "true" {
			t.Errorf("the switches are not always shown for %s, whose fake is a ClientHello: %q", typ, returns[typ])
		}
	}
	capture := returns["CAPTURE"]
	if !strings.Contains(capture, `"tls_client_hello"`) {
		t.Errorf("for My own Payload File the switches do not follow the file's facts.kind: %q", capture)
	}
	if !strings.Contains(capture, `"listed"`) {
		t.Errorf("for My own Payload File the switches are not shown for a missing or unchosen file, whose fallback is a ClientHello: %q", capture)
	}
	if !strings.Contains(returns["INVERTED"], "sendsSynOrDecoyFake(") {
		t.Errorf("for Inverted Original the switches do not follow the SYN fake and the combo decoy: %q", returns["INVERTED"])
	}
	for _, typ := range []string{"RANDOM", "ZERO", "STUN", "CUSTOM"} {
		if r, ok := returns[typ]; ok && r != "false" {
			t.Errorf("the switches are shown for %s, whose fake is no ClientHello: %q", typ, r)
		}
	}
	if fallback != "false" {
		t.Errorf("payload types the rule does not name show the switches: default returns %q", fallback)
	}

	body := regexp.MustCompile(`(?s)export const sendsSynOrDecoyFake\b(.*?);\n`).FindStringSubmatch(helpers)
	if body == nil {
		t.Fatal("no sendsSynOrDecoyFake helper for the fakes that carry Preset: Google (classic) beside an inverted fake")
	}
	for _, field := range []string{"tcp.syn_fake", "tcp.syn_fake_len", "decoy_enabled"} {
		if !strings.Contains(body[1], field) {
			t.Errorf("sendsSynOrDecoyFake does not read %s", field)
		}
	}
}
