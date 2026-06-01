package detector

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/mtproto"
)

const (
	tgStallTimeout    = 10 * time.Second
	tgTotalTimeout    = 60 * time.Second
	tgOkThreshold     = 0.98
	tgDCPingTimeout   = 5 * time.Second
	tgDownloadDefault = "https://telegram.org/img/Telegram200million.png"
	tgDownloadSize    = 32477141
	tgReadChunk       = 65536
)

func (s *DetectorSuite) runTelegramCheck(ctx context.Context) *TelegramResult {
	log.DiscoveryLogf("[Detector] Starting Telegram reachability check")

	result := &TelegramResult{}

	result.DCPings = s.telegramDCPings(ctx)
	for _, p := range result.DCPings {
		result.DCTotal++
		if p.Ok {
			result.DCReachable++
		}
	}

	if s.isCanceled() {
		result.Verdict = TGError
		return result
	}

	result.Download = s.telegramDownload(ctx)
	s.mu.Lock()
	s.CompletedChecks++
	s.mu.Unlock()

	result.Verdict = telegramVerdict(result)
	result.Summary = fmt.Sprintf("DL %s, DC %d/%d reachable",
		result.Download.Verdict, result.DCReachable, result.DCTotal)

	log.DiscoveryLogf("[Detector] Telegram check complete: %s", result.Summary)
	return result
}

func (s *DetectorSuite) telegramDCPings(ctx context.Context) []TelegramDCPing {
	endpoints := telegramDCEndpoints()

	pings := make([]TelegramDCPing, len(endpoints))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)

	for i, ep := range endpoints {
		wg.Add(1)
		go func(idx int, e dcEndpoint) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			p := TelegramDCPing{DC: e.dc, Address: e.addr}
			pingCtx, cancel := context.WithTimeout(ctx, tgDCPingTimeout)
			defer cancel()

			start := time.Now()
			conn, err := markedDialer(s.mark, tgDCPingTimeout).DialContext(pingCtx, "tcp", e.addr)
			if err == nil {
				p.Ok = true
				p.RTTMs = round1(float64(time.Since(start).Microseconds()) / 1000.0)
				conn.Close()
			}
			pings[idx] = p

			s.mu.Lock()
			s.CompletedChecks++
			s.mu.Unlock()
		}(i, ep)
	}

	wg.Wait()
	return pings
}

type dcEndpoint struct {
	dc   int
	addr string
}

func telegramDCEndpoints() []dcEndpoint {
	snap := mtproto.DCSnapshot()
	var endpoints []dcEndpoint
	for dc := 1; dc <= 5; dc++ {
		addr := snap[dc]
		if addr == "" {
			addrs, err := mtproto.ResolveDCAll(dc, false, "")
			if err != nil || len(addrs) == 0 {
				continue
			}
			addr = addrs[0]
		}
		endpoints = append(endpoints, dcEndpoint{dc: dc, addr: addr})
	}
	return endpoints
}

func (s *DetectorSuite) telegramDownload(ctx context.Context) TelegramThroughput {
	url := TelegramConfig.DownloadURL
	if url == "" {
		url = tgDownloadDefault
	}
	expected := TelegramConfig.DownloadSize
	if expected == 0 {
		expected = tgDownloadSize
	}

	tp := TelegramThroughput{Expected: expected}

	runCtx, cancel := context.WithTimeout(ctx, tgTotalTimeout)
	defer cancel()

	client := telegramClient(s.mark)
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(runCtx, http.MethodGet, url, nil)
	if err != nil {
		tp.Verdict = TGError
		tp.Detail = err.Error()
		return tp
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := client.Do(req)
	if err != nil {
		tp.Verdict = TGBlocked
		tp.Detail = err.Error()
		return tp
	}
	defer resp.Body.Close()

	bytesRead, peak, dur, stalled := streamWithStall(runCtx, cancel, func(p []byte) (int, error) {
		return resp.Body.Read(p)
	}, tgReadChunk)

	tp.Bytes = bytesRead
	tp.DurationMs = dur.Milliseconds()
	tp.MbpsPeak = bpsToMbps(peak)
	if dur > 0 {
		tp.MbpsAvg = bpsToMbps(float64(bytesRead) / dur.Seconds())
	}
	if tp.MbpsPeak == 0 {
		tp.MbpsPeak = tp.MbpsAvg
	}
	if expected > 0 {
		tp.PctOk = round1(float64(bytesRead) / float64(expected) * 100)
	}
	tp.Verdict = throughputVerdict(bytesRead, expected, stalled)
	return tp
}

func streamWithStall(ctx context.Context, cancel context.CancelFunc, read func([]byte) (int, error), chunkSize int) (int64, float64, time.Duration, bool) {
	var total int64
	var lastProgress atomic.Int64
	start := time.Now()
	lastProgress.Store(start.UnixNano())

	var peak float64
	var peakMu sync.Mutex
	stalledCh := make(chan struct{})

	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		var lastBytes int64
		lastTime := start
		for {
			select {
			case <-ctx.Done():
				return
			case <-stalledCh:
				return
			case now := <-ticker.C:
				last := time.Unix(0, lastProgress.Load())
				if now.Sub(last) >= tgStallTimeout {
					cancel()
					return
				}
				cur := atomic.LoadInt64(&total)
				if d := now.Sub(lastTime); d > 0 {
					bps := float64(cur-lastBytes) / d.Seconds()
					peakMu.Lock()
					if bps > peak {
						peak = bps
					}
					peakMu.Unlock()
				}
				lastBytes = cur
				lastTime = now
			}
		}
	}()

	buf := make([]byte, chunkSize)
	stalled := false
	for {
		n, err := read(buf)
		if n > 0 {
			atomic.AddInt64(&total, int64(n))
			lastProgress.Store(time.Now().UnixNano())
		}
		if err != nil {
			if ctx.Err() != nil && time.Since(time.Unix(0, lastProgress.Load())) >= tgStallTimeout {
				stalled = true
			}
			break
		}
		if ctx.Err() != nil {
			stalled = time.Since(time.Unix(0, lastProgress.Load())) >= tgStallTimeout
			break
		}
	}
	close(stalledCh)

	peakMu.Lock()
	p := peak
	peakMu.Unlock()
	return atomic.LoadInt64(&total), p, time.Since(start), stalled
}

func throughputVerdict(bytes, expected int64, stalled bool) TelegramVerdict {
	if bytes == 0 {
		return TGBlocked
	}
	if expected > 0 && bytes >= int64(float64(expected)*tgOkThreshold) {
		return TGOk
	}
	if stalled {
		return TGStalled
	}
	return TGSlow
}

func telegramVerdict(r *TelegramResult) TelegramVerdict {
	dl := r.Download.Verdict
	if dl == TGBlocked && r.DCReachable == 0 {
		return TGBlocked
	}
	if dl == TGStalled {
		return TGStalled
	}
	if dl == TGSlow || dl == TGBlocked {
		return TGSlow
	}
	if r.DCTotal > 0 && r.DCReachable > 0 && r.DCReachable < r.DCTotal {
		return TGPartial
	}
	if dl == TGOk {
		return TGOk
	}
	return TGError
}

func telegramClient(mark uint) *http.Client {
	d := markedDialer(mark, fatConnectTimeout)
	return &http.Client{
		Transport: &http.Transport{
			DialContext:         d.DialContext,
			ForceAttemptHTTP2:   true,
			TLSHandshakeTimeout: fatConnectTimeout,
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
		},
	}
}

func bpsToMbps(bps float64) float64 {
	return round1(bps * 8 / 1_000_000)
}
