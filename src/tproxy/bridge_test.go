package tproxy

import (
	"sync"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestTelegramBridgePortIsFixedAndOutsideTheEphemeralRange(t *testing.T) {
	mark := MarkForSet(config.TelegramBridgeSetID, config.TelegramBridgeMark)
	if mark != config.TelegramBridgeMark {
		t.Fatalf("the pinned bridge mark 0x%x is not usable, got 0x%x", config.TelegramBridgeMark, mark)
	}
	if !InMarkRange(mark) {
		t.Errorf("bridge mark 0x%x is outside the TPROXY mark range", mark)
	}
	port := PortFor(mark)
	if port != 13443 {
		t.Errorf("bridge port %d, want 13443", port)
	}
	if port >= 32768 && port <= 60999 {
		t.Errorf("bridge port %d is inside the default ephemeral range", port)
	}
}

func TestManagerRecordsAFailedBridgeListenerAndRetries(t *testing.T) {
	m := NewManager(nil)
	defer m.Stop()

	var mu sync.Mutex
	var reports [][2]bool
	m.SetTelegramBridgeHook(func(v4, v6, retried bool) {
		mu.Lock()
		reports = append(reports, [2]bool{v4, v6})
		mu.Unlock()
	})

	cfg := config.NewConfig()
	cfg.System.MTProto.Bridge.Enabled = true
	m.SyncConfig(&cfg)

	st := m.ListenerStatus(config.TelegramBridgeSetID, PortFor(config.TelegramBridgeMark))
	mu.Lock()
	got := append([][2]bool(nil), reports...)
	mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("the bridge hook ran %d times, want once per sync", len(got))
	}
	if st.Running {
		if !got[0][0] && !got[0][1] {
			t.Errorf("the listener runs but the hook reported it down")
		}
		return
	}
	if st.Error == "" {
		t.Error("a listener that failed to start must keep its error for the status")
	}
	if got[0][0] || got[0][1] {
		t.Errorf("the hook reported a listener that is not running: %v", got[0])
	}
	m.mu.Lock()
	scheduled := m.retryTimer != nil
	m.mu.Unlock()
	if !scheduled {
		t.Error("a failed listener must be retried")
	}

	cfg.System.MTProto.Bridge.Enabled = false
	m.SyncConfig(&cfg)
	if st := m.ListenerStatus(config.TelegramBridgeSetID, 0); st.Error != "" {
		t.Errorf("the error of a listener that is no longer wanted was kept: %s", st.Error)
	}
	m.mu.Lock()
	scheduled = m.retryTimer != nil
	m.mu.Unlock()
	if scheduled {
		t.Error("the retry was kept after the bridge was turned off")
	}
}

func TestHashedMarksNeverTakeTheBridgeMark(t *testing.T) {
	for _, id := range []string{"00000000-0000-4000-8000-0000048454d9", "00000000-0000-4000-8000-0000075d7227"} {
		if got := MarkForSet(id, 0); got == config.TelegramBridgeMark {
			t.Errorf("set %s hashes onto the bridge mark 0x%x", id, got)
		}
	}
}
