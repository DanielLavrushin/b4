package ingest

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/testkit"
)

func l7Set(name string, classes ...string) config.SetConfig {
	set := config.NewSetConfig()
	set.Name = name
	set.UDP.DPortFilter = "19294-19344,50000-50100"
	set.UDP.FakePayloadFile = config.FakePayloadAutoSTUN
	set.UDP.FilterL7 = classes
	return set
}

func TestShareAcceptsASetThatSelectsUDPProtocols(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	author := testkit.Identity(t)

	resp := f.share(t, author, l7Set("Discord voice", config.UDPL7Discord), peerA)
	expect(t, resp, http.StatusAccepted, "")
	setID, _ := resp.Body["set_id"].(string)
	v, err := f.store.GetVersion(ctx, setID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != hubwire.SetStatusPending || v.B4Min != "1.88.0" {
		t.Errorf("a protocol selection waits for moderation and needs 1.88.0: %q %q", v.Status, v.B4Min)
	}
	if got := hubwire.UDPL7(v.Projection); !reflect.DeepEqual(got, []string{config.UDPL7Discord}) {
		t.Errorf("the stored projection must keep the protocol selection: %v", v.Projection)
	}

	stun := f.share(t, author, l7Set("STUN calls", config.UDPL7STUN), peerA)
	expect(t, stun, http.StatusAccepted, "")
	if stun.Body["set_id"] == setID {
		t.Errorf("a set that selects another protocol is a set of its own, not the next version of %s: %v", setID, stun.Body)
	}

	dup := f.share(t, testkit.Identity(t), l7Set("Discord again", config.UDPL7Discord), peerB)
	expect(t, dup, http.StatusConflict, CodeDuplicateStrategy)
	if dup.Body["set_id"] != setID {
		t.Errorf("the same strategy for the same protocol duplicates %s: %v", setID, dup.Body)
	}
}

func TestShareRefusesTargetlessAndInvalidL7Sets(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	author := testkit.Identity(t)

	noPorts := l7Set("No ports", config.UDPL7Discord)
	noPorts.UDP.DPortFilter = ""
	targeted := l7Set("Targeted", config.UDPL7STUN)
	targeted.Targets.SNIDomains = []string{"discord.com"}
	tcpOnly := l7Set("TCP ports only")
	tcpOnly.TCP.DPortFilter = "443"
	withTCP := l7Set("Discord voice", config.UDPL7Discord)
	withTCP.TCP.DPortFilter = "443,2053,8443"

	for _, c := range []struct {
		name string
		set  config.SetConfig
		want string
	}{
		{"ports without a protocol", l7Set("Ports only"), "the set has no targets"},
		{"TCP ports without a protocol", tcpOnly, "the set has no targets"},
		{"a protocol without ports", noPorts, "udp.dport_filter"},
		{"a protocol with targets", targeted, "udp.filter_l7"},
		{"a protocol with TCP ports", withTCP, "TCP port filter"},
	} {
		resp := f.share(t, author, c.set, peerA)
		expect(t, resp, http.StatusBadRequest, CodeInvalidSet)
		if msg, _ := resp.Body["error"].(string); !strings.Contains(msg, c.want) {
			t.Errorf("%s: the refusal must name %q, got %q", c.name, c.want, msg)
		}
	}
	sets, err := f.store.Sets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sets) != 0 {
		t.Errorf("a refused share stores nothing: %+v", sets)
	}
}

func TestShareKeepsProtocolSelectionsOnOtherPortsApart(t *testing.T) {
	f := newFixture(t)
	author := testkit.Identity(t)
	whatsapp := l7Set("WhatsApp calls", config.UDPL7STUN)
	whatsapp.UDP.DPortFilter = "3478-3481"
	meet := l7Set("Google Meet", config.UDPL7STUN)
	meet.UDP.DPortFilter = "19302-19309"
	meet.UDP.FakeSeqLength = 3

	first := f.share(t, author, whatsapp, peerA)
	expect(t, first, http.StatusAccepted, "")
	second := f.share(t, author, meet, peerA)
	expect(t, second, http.StatusAccepted, "")
	if second.Body["set_id"] == first.Body["set_id"] {
		t.Errorf("a protocol selection on other UDP ports is a set of its own, not the next version of %v: %v", first.Body["set_id"], second.Body)
	}

	other := testkit.Identity(t)
	calls := f.share(t, other, l7Set("Calls", config.UDPL7STUN), peerB)
	expect(t, calls, http.StatusAccepted, "")
	if calls.Body["set_id"] == first.Body["set_id"] || calls.Body["set_id"] == second.Body["set_id"] {
		t.Errorf("the same strategy for the same protocol on other UDP ports duplicates nothing: %v", calls.Body)
	}
	again := f.share(t, testkit.Identity(t), l7Set("Calls again", config.UDPL7STUN), peerC)
	expect(t, again, http.StatusConflict, CodeDuplicateStrategy)
	if again.Body["set_id"] != calls.Body["set_id"] {
		t.Errorf("the same strategy for the same protocol on the same UDP ports duplicates %v: %v", calls.Body["set_id"], again.Body)
	}
}

func TestShareTreatsReorderedProtocolsAsTheSameStrategy(t *testing.T) {
	f := newFixture(t)
	first := f.share(t, testkit.Identity(t), l7Set("Voice", config.UDPL7Discord, config.UDPL7STUN), peerA)
	expect(t, first, http.StatusAccepted, "")

	reordered := l7Set("Voice again", config.UDPL7STUN, config.UDPL7Discord)
	built := f.share(t, testkit.Identity(t), reordered, peerB)
	expect(t, built, http.StatusConflict, CodeDuplicateStrategy)
	if built.Body["set_id"] != first.Body["set_id"] {
		t.Errorf("the same protocols in another order duplicate %v: %v", first.Body["set_id"], built.Body)
	}

	env := testkit.BuildEnvelope(t, &reordered)
	udp, _ := env.Set["udp"].(map[string]interface{})
	udp["filter_l7"] = []interface{}{config.UDPL7STUN, config.UDPL7Discord}
	env.Fingerprint = hubwire.Fingerprint(env.Set)
	crafted := f.post(t, testkit.SignShare(t, testkit.Identity(t), env, f.clock), peerC)
	expect(t, crafted, http.StatusConflict, CodeDuplicateStrategy)
	if crafted.Body["set_id"] != first.Body["set_id"] {
		t.Errorf("an envelope that lists the protocols in another order duplicates %v: %v", first.Body["set_id"], crafted.Body)
	}
}
