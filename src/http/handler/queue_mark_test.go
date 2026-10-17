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
	for _, c := range []struct {
		name             string
		oldMode, newMode string
		oldMark, newMark uint
		want             bool
	}{
		{"the same mark, while other settings change", "", "", 0x2008000, 0x2008000, false},
		{"another mark with the bit", "", "", 0x2008000, 0x2010000, true},
		{"a mark without the bit", "", "", 0x2008000, 0x10000, false},
		{"the default", "", "", 0x2008000, 0, false},
		{"TUN mode, where b4 does not add the bit", "", "tun", 0x8000, 0x2008000, false},
		{"TUN mode keeping such a mark", "tun", "tun", 0x2008000, 0x2008000, false},
		{"leaving TUN mode with such a mark", "tun", "", 0x2008000, 0x2008000, true},
	} {
		old := config.NewConfig()
		old.Queue.Mode, old.Queue.Mark = c.oldMode, c.oldMark
		cur := old.Clone()
		cur.Queue.Mode, cur.Queue.Mark = c.newMode, c.newMark
		if refused := queueMarkRefusal(&old, cur) != nil; refused != c.want {
			t.Errorf("%s (0x%x): refused %v, want %v; only a save that newly puts such a mark on NFQUEUE mode is refused, so a configuration that already runs with one keeps loading and saving", c.name, c.newMark, refused, c.want)
		}
	}
}
