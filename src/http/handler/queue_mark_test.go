package handler

import (
	"errors"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestSavingAQueueMarkWithTheInjectedBitIsRefused(t *testing.T) {
	cfg := mcpTestCfg()
	api := configDownloadAPI(cfg)
	incoming := cfg.Clone()
	incoming.Queue.Mark = 0x2008000
	err := api.pushConfigLocked(incoming)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "validation_failed" {
		t.Fatalf("pushConfigLocked = %v, want a validation error: with bit 0x2000000 in the queue mark b4 takes the connections it sends packets into for its own", err)
	}
	if len(apiErr.Fields) != 1 || apiErr.Fields[0].Path != "queue.mark" || apiErr.Fields[0].Code != "mark_conflict" || apiErr.Fields[0].Params["mark"] != "0x2008000" {
		t.Errorf("fields = %+v, want queue.mark / mark_conflict naming 0x2008000", apiErr.Fields)
	}
}

func TestAQueueMarkThatAlreadyHasTheInjectedBitKeepsSaving(t *testing.T) {
	old := config.NewConfig()
	old.Queue.Mark = 0x2008000
	for _, c := range []struct {
		name string
		mark uint
		want bool
	}{
		{"the same mark, while other settings change", 0x2008000, false},
		{"another mark with the bit", 0x2010000, true},
		{"a mark without the bit", 0x10000, false},
		{"the default", 0, false},
	} {
		cur := old.Clone()
		cur.Queue.Mark = c.mark
		if refused := queueMarkRefusal(&old, cur) != nil; refused != c.want {
			t.Errorf("%s (0x%x): refused %v, want %v; a configuration that already holds such a mark must keep loading and saving", c.name, c.mark, refused, c.want)
		}
	}
}
