package hubwire

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func TestGroupRecordSignsAndVerifiesWithItsMembers(t *testing.T) {
	id, err := NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	set := sampleSet()
	env, _, err := Build(&set, BuildOptions{B4Version: "1.94.0"})
	if err != nil {
		t.Fatal(err)
	}
	body := GroupBody{Title: "youtube", Members: []GroupMember{{Envelope: *env, Escalate: &GroupEscalation{To: 1, RstThreshold: 4}}, {Envelope: *env}}}
	rec, err := SignRecord(id, RecordGroup, body, time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(rec)
	var decoded Record
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyRecord(&decoded); err != nil {
		t.Fatalf("a group record must verify: %v", err)
	}
	var back GroupBody
	if err := json.Unmarshal(decoded.Body, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Members) != 2 || back.Members[0].Escalate.To != 1 || back.Members[0].Envelope.Fingerprint != env.Fingerprint {
		t.Errorf("the members did not survive the round trip: %+v", back)
	}
	tampered := decoded
	tampered.Body = json.RawMessage(`{"title":"other","members":[]}`)
	if _, err := VerifyRecord(&tampered); err != ErrBadSignature {
		t.Errorf("a changed member list must fail verification, got %v", err)
	}
}

func TestGroupVoteRecordSignsAndVerifies(t *testing.T) {
	id, err := NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	body := GroupVoteBody{GroupID: "G1", Version: 2, Members: []GroupVoteMember{{SetID: "S1", Version: 3}, {SetID: "S2", Version: 1}}, Kind: VoteWorks, Domain: "youtube.com"}
	rec, err := SignRecord(id, RecordGroupVote, body, time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("a group vote must sign: %v", err)
	}
	raw, _ := json.Marshal(rec)
	var decoded Record
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyRecord(&decoded); err != nil {
		t.Fatalf("a group vote must verify: %v", err)
	}
	var back GroupVoteBody
	if err := json.Unmarshal(decoded.Body, &back); err != nil || back.GroupID != body.GroupID || back.Kind != body.Kind || len(back.Members) != 2 || back.Members[0] != body.Members[0] || back.Members[1] != body.Members[1] {
		t.Fatalf("the vote did not survive the round trip: %+v %v", back, err)
	}
	decoded.Body = json.RawMessage(`{"group_id":"G1","version":2,"members":[{"set_id":"S1","version":3},{"set_id":"S2","version":1}],"kind":"broken"}`)
	if _, err := VerifyRecord(&decoded); err != ErrBadSignature {
		t.Errorf("a flipped vote must fail verification, got %v", err)
	}
}

func TestGroupFingerprintFollowsTheStrategiesTheirOrderAndTheEscalation(t *testing.T) {
	a, b := strings.Repeat("a", 64), strings.Repeat("b", 64)
	base := []GroupFingerprintMember{{FP: a, Escalate: &GroupEscalation{To: 1, RstThreshold: 3}}, {FP: b}}
	fp := GroupFingerprint(base)
	if len(fp) != 64 {
		t.Fatalf("a group fingerprint is a sha256 in hex: %q", fp)
	}
	upper := []GroupFingerprintMember{{FP: strings.ToUpper(a), Escalate: &GroupEscalation{To: 1, RstThreshold: 3}}, {FP: b}}
	if GroupFingerprint(upper) != fp {
		t.Error("the case of a member fingerprint must not matter")
	}
	for name, other := range map[string][]GroupFingerprintMember{
		"order":      {{FP: b}, {FP: a, Escalate: &GroupEscalation{To: 1, RstThreshold: 3}}},
		"strategy":   {{FP: a, Escalate: &GroupEscalation{To: 1, RstThreshold: 3}}, {FP: a}},
		"escalation": {{FP: a, Escalate: &GroupEscalation{To: 1, RstThreshold: 4}}, {FP: b}},
		"no link":    {{FP: a}, {FP: b}},
		"members":    {{FP: a, Escalate: &GroupEscalation{To: 1, RstThreshold: 3}}},
	} {
		if GroupFingerprint(other) == fp {
			t.Errorf("a different %s must give another group fingerprint", name)
		}
	}
}

func TestGroupEscalationKeepsTheThresholdsAndTakesTheNewID(t *testing.T) {
	local := config.EscalateConfig{To: "old-id", RstThreshold: 5, RstWindowSec: 40, TtlSec: 600, StallThreshold: 2, StallTimeoutMs: 2500, DNSThreshold: 1}
	wire := EscalationTo(local, 2)
	if wire.To != 2 {
		t.Fatalf("the link must point at the member position, got %d", wire.To)
	}
	back := wire.Config("new-id")
	want := local
	want.To = "new-id"
	if back != want {
		t.Errorf("escalation round trip: got %+v, want %+v", back, want)
	}
}

func TestGroupEscalationMustPointAtAnotherMember(t *testing.T) {
	cases := []struct {
		esc     *GroupEscalation
		self, n int
		ok      bool
	}{
		{nil, 0, 3, true},
		{&GroupEscalation{To: 1}, 0, 3, true},
		{&GroupEscalation{To: 0}, 0, 3, false},
		{&GroupEscalation{To: 3}, 0, 3, false},
		{&GroupEscalation{To: -1}, 0, 3, false},
	}
	for _, c := range cases {
		if got := ValidGroupEscalation(c.esc, c.self, c.n); got != c.ok {
			t.Errorf("ValidGroupEscalation(%+v, %d, %d) = %v, want %v", c.esc, c.self, c.n, got, c.ok)
		}
	}
}

func TestGroupEscalationThresholdsStayInTheSetEditorRange(t *testing.T) {
	if field, _ := EscalationOutOfRange(nil); field != "" {
		t.Errorf("no escalation is in range, got %q", field)
	}
	if field, _ := EscalationOutOfRange(&GroupEscalation{To: 1, RstThreshold: 20, RstWindowSec: 600, TtlSec: 86400, StallThreshold: 20, StallTimeoutMs: 15000, DNSThreshold: 10}); field != "" {
		t.Errorf("the set editor's maxima are in range, got %q", field)
	}
	cases := []struct {
		esc   GroupEscalation
		field string
		limit int
	}{
		{GroupEscalation{RstThreshold: 21}, "rst_threshold", 20},
		{GroupEscalation{RstWindowSec: 601}, "rst_window_sec", 600},
		{GroupEscalation{TtlSec: 86401}, "ttl_sec", 86400},
		{GroupEscalation{StallThreshold: 21}, "stall_threshold", 20},
		{GroupEscalation{StallTimeoutMs: 15001}, "stall_timeout_ms", 15000},
		{GroupEscalation{DNSThreshold: 11}, "dns_threshold", 10},
		{GroupEscalation{TtlSec: -1}, "ttl_sec", 86400},
	}
	for _, c := range cases {
		if field, limit := EscalationOutOfRange(&c.esc); field != c.field || limit != c.limit {
			t.Errorf("EscalationOutOfRange(%+v) = %q %d, want %q %d", c.esc, field, limit, c.field, c.limit)
		}
	}
}

func TestAPublishedEscalationIsClampedToWhatTheHubAccepts(t *testing.T) {
	local := config.EscalateConfig{To: "x", RstThreshold: -3, RstWindowSec: 601, TtlSec: 172800, StallThreshold: 20, StallTimeoutMs: 60000, DNSThreshold: 3000}
	wire := EscalationTo(local, 1)
	want := GroupEscalation{To: 1, RstThreshold: 0, RstWindowSec: 600, TtlSec: 86400, StallThreshold: 20, StallTimeoutMs: 15000, DNSThreshold: 10}
	if *wire != want {
		t.Errorf("got %+v, want %+v", *wire, want)
	}
}
