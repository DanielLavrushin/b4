package detector

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"sort"
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
	tgDownloadSize    = 30970000
	tgUploadAddr      = "149.154.167.220:443"
	tgUploadSize      = 10 * 1024 * 1024
	tgUploadChunk     = 16384
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
	s.mu.Lock()
	s.CompletedChecks++
	s.mu.Unlock()

	if s.isCanceled() {
		result.Verdict = TGError
		return result
	}

	result.Download = s.telegramDownload(ctx)
	s.mu.Lock()
	s.CompletedChecks++
	s.mu.Unlock()

	if s.isCanceled() {
		result.Verdict = TGError
		return result
	}

	result.Upload = s.telegramUpload(ctx)
	s.mu.Lock()
	s.CompletedChecks++
	s.mu.Unlock()

	result.Verdict = telegramVerdict(result)
	result.Summary = fmt.Sprintf("DL %s, UL %s, DC %d/%d reachable",
		result.Download.Verdict, result.Upload.Verdict, result.DCReachable, result.DCTotal)

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
	var endpoints []dcEndpoint
	snap := mtproto.DCSnapshot()
	if len(snap) > 0 {
		dcs := make([]int, 0, len(snap))
		for dc := range snap {
			dcs = append(dcs, dc)
		}
		sort.Ints(dcs)
		for _, dc := range dcs {
			endpoints = append(endpoints, dcEndpoint{dc: dc, addr: snap[dc]})
		}
		return endpoints
	}
	for dc := 1; dc <= 5; dc++ {
		addrs, err := mtproto.ResolveDCAll(dc, false, "")
		if err != nil || len(addrs) == 0 {
			continue
		}
		endpoints = append(endpoints, dcEndpoint{dc: dc, addr: addrs[0]})
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

func (s *DetectorSuite) telegramUpload(ctx context.Context) TelegramThroughput {
	addr := TelegramConfig.UploadAddr
	if addr == "" {
		addr = tgUploadAddr
	}
	expected := TelegramConfig.UploadSize
	if expected == 0 {
		expected = tgUploadSize
	}

	tp := TelegramThroughput{Expected: expected}

	runCtx, cancel := context.WithTimeout(ctx, tgTotalTimeout)
	defer cancel()

	conn, err := markedDialer(s.mark, fatConnectTimeout).DialContext(runCtx, "tcp", addr)
	if err != nil {
		tp.Verdict = TGBlocked
		tp.Detail = err.Error()
		return tp
	}
	tlsConn := tls.Client(conn, &tls.Config{InsecureSkipVerify: true})
	if err := tlsConn.HandshakeContext(runCtx); err != nil {
		tlsConn.Close()
		tp.Verdict = TGBlocked
		tp.Detail = err.Error()
		return tp
	}
	defer tlsConn.Close()

	chunk := make([]byte, tgUploadChunk)
	var sent int64
	var peak float64
	start := time.Now()
	lastProgress := start
	lastTick := start
	var lastTickBytes int64
	stalled := false

	for sent < expected {
		select {
		case <-runCtx.Done():
			stalled = time.Since(lastProgress) >= tgStallTimeout
			goto done
		default:
		}

		tlsConn.SetWriteDeadline(time.Now().Add(tgStallTimeout))
		n, werr := tlsConn.Write(chunk)
		sent += int64(n)
		now := time.Now()
		if n > 0 {
			lastProgress = now
		}
		if d := now.Sub(lastTick); d >= 500*time.Millisecond {
			bps := float64(sent-lastTickBytes) / d.Seconds()
			if bps > peak {
				peak = bps
			}
			lastTick = now
			lastTickBytes = sent
		}
		if werr != nil {
			stalled = now.Sub(lastProgress) >= tgStallTimeout || isTimeoutErr(werr)
			break
		}
		if now.Sub(lastProgress) >= tgStallTimeout {
			stalled = true
			break
		}
	}

done:
	dur := time.Since(start)
	tp.Bytes = sent
	tp.DurationMs = dur.Milliseconds()
	tp.MbpsPeak = bpsToMbps(peak)
	if dur > 0 {
		tp.MbpsAvg = bpsToMbps(float64(sent) / dur.Seconds())
	}
	if tp.MbpsPeak == 0 {
		tp.MbpsPeak = tp.MbpsAvg
	}
	if expected > 0 {
		tp.PctOk = round1(float64(sent) / float64(expected) * 100)
	}
	tp.Verdict = throughputVerdict(sent, expected, stalled)
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
	ul := r.Upload.Verdict
	if dl == TGBlocked && ul == TGBlocked {
		return TGBlocked
	}
	if (dl == TGBlocked || ul == TGBlocked) && r.DCReachable == 0 {
		return TGBlocked
	}
	if dl == TGStalled || ul == TGStalled {
		return TGStalled
	}
	if dl == TGSlow || ul == TGSlow || dl == TGBlocked || ul == TGBlocked {
		return TGSlow
	}
	if r.DCTotal > 0 && r.DCReachable > 0 && r.DCReachable < r.DCTotal {
		return TGPartial
	}
	if dl == TGOk && ul == TGOk {
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

func isTimeoutErr(err error) bool {
	type timeout interface{ Timeout() bool }
	if t, ok := err.(timeout); ok {
		return t.Timeout()
	}
	if ne, ok := err.(net.Error); ok {
		return ne.Timeout()
	}
	return false
}
