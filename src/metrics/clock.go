package metrics

import "time"

var procStart = time.Now()

const (
	minuteNs    = int64(time.Minute)
	tenMinuteNs = int64(10 * time.Minute)
	hourNs      = int64(time.Hour)
	offsetSlack = int64(100 * time.Millisecond)
)

func realNow() (int64, int64) {
	t := time.Now()
	return int64(t.Sub(procStart)), t.UnixNano()
}

func (m *MetricsCollector) now() (int64, int64) {
	if m.nowFn != nil {
		return m.nowFn()
	}
	return realNow()
}

func (m *MetricsCollector) adoptOffset(mono, wall int64) {
	next := wall - mono
	if d := next - m.off.Load(); d > offsetSlack || d < -offsetSlack {
		m.off.Store(next)
		m.esc.relabelled()
	}
}

func wallMs(mono, off int64) int64 {
	return floorDiv(mono+off, int64(time.Millisecond))
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b < 0 {
		q--
	}
	return q
}

func nextBoundary(start, off, step int64) int64 {
	return (floorDiv(start+off, step)+1)*step - off
}

func hasMonotonic(t time.Time) bool {
	return t != t.Round(0)
}

func (m *MetricsCollector) WallMillis(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	if !hasMonotonic(t) {
		return t.UnixMilli()
	}
	m.initOnce.Do(m.init)
	return wallMs(int64(t.Sub(procStart)), m.off.Load())
}

func relabelTime(t time.Time, off int64) time.Time {
	if t.IsZero() || !hasMonotonic(t) {
		return t
	}
	return time.UnixMilli(wallMs(int64(t.Sub(procStart)), off))
}
