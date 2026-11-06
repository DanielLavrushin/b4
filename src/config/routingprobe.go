package config

import "sync/atomic"

var routingLiveProbe atomic.Pointer[func(string) bool]

func SetRoutingLiveProbe(fn func(string) bool) {
	if fn == nil {
		routingLiveProbe.Store(nil)
		return
	}
	routingLiveProbe.Store(&fn)
}

func RoutingLive(setID string) bool {
	fn := routingLiveProbe.Load()
	if fn == nil {
		return true
	}
	return (*fn)(setID)
}
