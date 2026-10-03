package metrics

import "sync/atomic"

var (
	packetCounterProvider  atomic.Pointer[func() uint64]
	upstreamProvider       atomic.Pointer[func() []UpstreamAttention]
	binaryReplacedProvider atomic.Pointer[func() bool]
	rulesProvider          atomic.Pointer[func() RulesInfo]
	proxyOpenProvider      atomic.Pointer[func() map[string]int64]
	setsProvider           atomic.Pointer[func() ([]SetMeta, int)]
	overloadProvider       atomic.Pointer[func() (uint64, uint64, uint64)]
)

func SetPacketCounterProvider(fn func() uint64) {
	if fn == nil {
		packetCounterProvider.Store(nil)
		return
	}
	packetCounterProvider.Store(&fn)
}

func SetUpstreamProvider(fn func() []UpstreamAttention) {
	if fn == nil {
		upstreamProvider.Store(nil)
		return
	}
	upstreamProvider.Store(&fn)
}

func SetBinaryReplacedProvider(fn func() bool) {
	if fn == nil {
		binaryReplacedProvider.Store(nil)
		return
	}
	binaryReplacedProvider.Store(&fn)
}

func SetRulesProvider(fn func() RulesInfo) {
	if fn == nil {
		rulesProvider.Store(nil)
		return
	}
	rulesProvider.Store(&fn)
}

func SetProxyOpenProvider(fn func() map[string]int64) {
	if fn == nil {
		proxyOpenProvider.Store(nil)
		return
	}
	proxyOpenProvider.Store(&fn)
}

func SetSetsProvider(fn func() ([]SetMeta, int)) {
	if fn == nil {
		setsProvider.Store(nil)
		return
	}
	setsProvider.Store(&fn)
}

func SetOverloadProvider(fn func() (injectSkipped, rawSendDropped, queueOverflow uint64)) {
	if fn == nil {
		overloadProvider.Store(nil)
		return
	}
	overloadProvider.Store(&fn)
}

func loadPacketCounter() (uint64, bool) {
	if fn := packetCounterProvider.Load(); fn != nil {
		return (*fn)(), true
	}
	return 0, false
}

func loadUpstreams() []UpstreamAttention {
	if fn := upstreamProvider.Load(); fn != nil {
		return (*fn)()
	}
	return nil
}

func loadBinaryReplaced() bool {
	if fn := binaryReplacedProvider.Load(); fn != nil {
		return (*fn)()
	}
	return false
}

func loadRules() RulesInfo {
	if fn := rulesProvider.Load(); fn != nil {
		return (*fn)()
	}
	return RulesInfo{}
}

func loadProxyOpen() map[string]int64 {
	if fn := proxyOpenProvider.Load(); fn != nil {
		return (*fn)()
	}
	return nil
}

func loadSets() ([]SetMeta, int) {
	if fn := setsProvider.Load(); fn != nil {
		return (*fn)()
	}
	return nil, 0
}

func loadOverload() [3]uint64 {
	if fn := overloadProvider.Load(); fn != nil {
		a, b, c := (*fn)()
		return [3]uint64{a, b, c}
	}
	return [3]uint64{}
}

func loadMTProto() *MTProtoStats {
	if fn := mtprotoStatsProvider.Load(); fn != nil {
		return (*fn)()
	}
	return nil
}
