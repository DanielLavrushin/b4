package leaktest

import (
	"testing"

	"go.uber.org/goleak"
)

func baseOptions() []goleak.Option {
	return []goleak.Option{
		goleak.IgnoreTopFunction("github.com/daniellavrushin/b4/quic.cleanupStaleEntries"),
		goleak.IgnoreTopFunction("github.com/daniellavrushin/b4/log.startFlusherLocked.func1"),
		goleak.IgnoreTopFunction("github.com/daniellavrushin/b4/metrics.(*MetricsCollector).updateLoop"),
	}
}

func VerifyTestMain(m *testing.M, extra ...goleak.Option) {
	goleak.VerifyTestMain(m, append(baseOptions(), extra...)...)
}
