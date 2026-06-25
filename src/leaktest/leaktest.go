package leaktest

import (
	"testing"

	"go.uber.org/goleak"
)

// Options returns the goleak options that ignore b4's long-lived background
// daemons (started lazily or in package init), which are not leaks. Use it for
// per-test goleak.VerifyNone calls; VerifyTestMain applies the same set.
func Options(extra ...goleak.Option) []goleak.Option {
	base := []goleak.Option{
		goleak.IgnoreTopFunction("github.com/daniellavrushin/b4/quic.cleanupStaleEntries"),
		goleak.IgnoreTopFunction("github.com/daniellavrushin/b4/log.startFlusherLocked.func1"),
		goleak.IgnoreTopFunction("github.com/daniellavrushin/b4/metrics.(*MetricsCollector).updateLoop"),
	}
	return append(base, extra...)
}

func VerifyTestMain(m *testing.M, extra ...goleak.Option) {
	goleak.VerifyTestMain(m, Options(extra...)...)
}
