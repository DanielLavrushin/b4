package ingest

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"github.com/daniellavrushin/b4hub/internal/store"
	"github.com/daniellavrushin/b4hub/internal/testkit"
)

func TestShareAcceptsASetThatTargetsOnlyASNs(t *testing.T) {
	f := newFixture(t)
	author := testkit.Identity(t)
	set := testkit.SampleSet("Telegram")
	set.Targets.ASNs = []string{"62041", "44907"}

	resp := f.share(t, author, set, peerA)
	expect(t, resp, http.StatusAccepted, "")
	setID := resp.Body["set_id"].(string)
	v, err := f.store.GetVersion(context.Background(), setID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := store.TargetList(v.Projection, "asns"); !reflect.DeepEqual(got, []string{"62041", "44907"}) {
		t.Errorf("the stored projection must carry the ASNs, got %v", got)
	}
	if v.B4Min != "1.83.0" {
		t.Errorf("a set with ASNs needs the release that reads them, got %q", v.B4Min)
	}

	other := testkit.SampleSet("Telegram")
	other.Targets.ASNs = []string{"15169"}
	resp = f.share(t, testkit.Identity(t), other, peerB)
	expect(t, resp, http.StatusAccepted, "")
	if resp.Body["set_id"] == setID {
		t.Errorf("the same strategy for other ASNs is another set, got %v", resp.Body)
	}

	widened := testkit.SampleSet("Telegram")
	widened.Targets.ASNs = []string{"62041", "211157"}
	widened.Faking.TTL = 9
	resp = f.share(t, author, widened, peerA)
	expect(t, resp, http.StatusAccepted, "")
	if resp.Body["set_id"] != setID || resp.Body["version"] != 2 {
		t.Errorf("the author's set with the same title and a shared ASN must become version 2, got %v", resp.Body)
	}
}
