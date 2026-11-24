package ingest

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/testkit"
)

func TestAShareOfAnOlderRouterLosesItsPersonalResolver(t *testing.T) {
	f := newFixture(t)
	set := testkit.SampleSet("Personal resolver", "example.com")
	env := testkit.BuildEnvelope(t, &set)
	env.Set["dns"] = map[string]interface{}{"enabled": true, "doh_url": "https://dns.nextdns.io/canaryid?token=canaryquery"}
	env.Fingerprint = hubwire.Fingerprint(env.Set)

	resp := f.post(t, testkit.SignShare(t, testkit.Identity(t), env, f.clock), peerA)
	expect(t, resp, http.StatusAccepted, "")
	id, _ := resp.Body["set_id"].(string)
	version, err := f.store.GetVersion(context.Background(), id, 1)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := json.Marshal(version)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), "canary") {
		t.Errorf("the stored version holds a private part of the resolver: %s", stored)
	}
	dns, _ := version.Projection["dns"].(map[string]interface{})
	if dns["enabled"] != true {
		t.Errorf("only the resolver goes, the stored set has %v", dns)
	}
}
