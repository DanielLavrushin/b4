package mtproto

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

var (
	telegramSetDial atomic.Bool
	telegramSetMu   sync.Mutex
	telegramSetNote string
)

func setTelegramSet(cfg *config.Config) {
	telegramSetDial.Store(cfg.TelegramUpstreamSet() != nil)
	noteTelegramSet(telegramSetProblem(cfg))
}

func telegramSetProblem(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	id := cfg.System.MTProto.UpstreamSet
	if id == "" {
		return ""
	}
	set := cfg.GetSetById(id)
	switch {
	case set == nil:
		return "the set chosen for b4's own Telegram connections (id " + id + ") no longer exists"
	case !set.Enabled:
		return "the set " + set.Name + " chosen for b4's own Telegram connections is disabled"
	case !config.CarriesTelegram(set):
		return "the set " + set.Name + " chosen for b4's own Telegram connections blocks its traffic or routes it through Telegram over WebSocket"
	case cfg.Queue.Mode == "tun":
		return "the set " + set.Name + " chosen for b4's own Telegram connections does not apply in TUN mode"
	case cfg.TelegramSetMarkConflict():
		return fmt.Sprintf("the set %s chosen for b4's own Telegram connections does not apply while the packet mark or a Discovery mark carries the bits of 0x%x, the mark of those connections", set.Name, config.TelegramSetDialMark)
	}
	return ""
}

func noteTelegramSet(problem string) {
	telegramSetMu.Lock()
	defer telegramSetMu.Unlock()
	if problem == telegramSetNote {
		return
	}
	telegramSetNote = problem
	if problem != "" {
		log.Warnf("%s %s, so those connections go as they would without one", tg(""), problem)
	}
}

func telegramSetChanged(old, next *config.Config) bool {
	return (old.TelegramUpstreamSet() == nil) != (next.TelegramUpstreamSet() == nil)
}

func dialMarkFor(cfg *config.Config) uint {
	base := coverDialMark()
	if base != 0 && cfg.TelegramUpstreamSet() != nil {
		return base | uint(config.TelegramSetBit)
	}
	return base
}

func coverDialMark() uint {
	return selfDialMark() &^ uint(config.TelegramSetBit)
}
