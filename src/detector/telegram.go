package detector

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/mtproto"
	"github.com/daniellavrushin/b4/netprobe"
)

const (
	tgStallTimeout  = 10 * time.Second
	tgTotalTimeout  = 60 * time.Second
	tgOkThreshold   = 0.98
	tgDCPingTimeout = 5 * time.Second
	tgReadChunk     = 65536
)

func (s *Suite) runTelegram() {
	s.setProgress(ScopeTelegram, "datacenters")
	result := &TelegramResult{DCPings: []TelegramDCPing{}}
	s.mu.Lock()
	s.Telegram = result
	s.mu.Unlock()

	pings := s.telegramDCPings()
	s.mu.Lock()
	result.DCPings = pings
	for _, p := range pings {
		result.DCTotal++
		if p.Ok {
			result.DCReachable++
		}
	}
	s.mu.Unlock()
	if s.canceled() {
		return
	}

	s.setProgress(ScopeTelegram, "download")
	dl := s.telegramDownload()
	s.mu.Lock()
	result.Download = dl
	s.mu.Unlock()
	s.step(1)
	if s.canceled() {
		return
	}

	s.setProgress(ScopeTelegram, "upload")
	ul := s.telegramUpload()
	s.mu.Lock()
	result.Upload = ul
	result.Verdict = telegramVerdict(result)
	s.mu.Unlock()
	s.step(1)
	log.DiscoveryLogf("[Detector] Telegram: DC %d/%d, download %s, upload %s", result.DCReachable, result.DCTotal, dl.Verdict, ul.Verdict)
}

type dcEndpoint struct {
	dc   int
	addr string
}

func telegramDCEndpoints() []dcEndpoint {
	cfg := Lists().Telegram
	if len(cfg.DCs) > 0 {
		port := cfg.DCPort
		if port == 0 {
			port = 443
		}
		endpoints := make([]dcEndpoint, 0, len(cfg.DCs))
		for _, dc := range cfg.DCs {
			endpoints = append(endpoints, dcEndpoint{dc: dc.DC, addr: net.JoinHostPort(dc.IP, strconv.Itoa(port))})
		}
		return endpoints
	}
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

func (s *Suite) telegramDCPings() []TelegramDCPing {
	endpoints := telegramDCEndpoints()
	pings := make([]TelegramDCPing, len(endpoints))
	var wg sync.WaitGroup
	for i, ep := range endpoints {
		wg.Add(1)
		go func(idx int, e dcEndpoint) {
			defer wg.Done()
			p := TelegramDCPing{DC: e.dc, Address: e.addr}
			ctx, cancel := context.WithTimeout(s.ctx, tgDCPingTimeout)
			defer cancel()
			start := time.Now()
			conn, err := netprobe.Dialer(int(s.directMark), tgDCPingTimeout, 0).DialContext(ctx, "tcp", e.addr)
			if err == nil {
				p.Ok = true
				p.RTTMs = round1(float64(time.Since(start).Microseconds()) / 1000.0)
				conn.Close()
			}
			pings[idx] = p
			s.step(1)
		}(i, ep)
	}
	wg.Wait()
	return pings
}

func (s *Suite) telegramClient() *http.Client {
	d := netprobe.Dialer(int(s.directMark), fatConnectTimeout, 0)
	return &http.Client{Transport: &http.Transport{
		DialContext:         d.DialContext,
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: fatConnectTimeout,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
	}}
}

func (s *Suite) telegramDownload() TelegramThroughput {
	cfg := Lists().Telegram
	tp := TelegramThroughput{Expected: cfg.DownloadSize}
	ctx, cancel := context.WithTimeout(s.ctx, tgTotalTimeout)
	defer cancel()

	client := s.telegramClient()
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.DownloadURL, nil)
	if err != nil {
		tp.Verdict = TGError
		tp.Detail = err.Error()
		return tp
	}
	req.Header.Set("User-Agent", fetchUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		tp.Verdict = TGBlocked
		tp.Detail = err.Error()
		return tp
	}
	defer resp.Body.Close()

	total, peak, dur, stalled := streamWithStall(ctx, cancel, func(p []byte) (int, error) {
		return resp.Body.Read(p)
	}, tgReadChunk)
	fillThroughput(&tp, total, peak, dur, stalled)
	return tp
}

func (s *Suite) telegramUpload() TelegramThroughput {
	cfg := Lists().Telegram
	tp := TelegramThroughput{Expected: cfg.UploadSize}
	if cfg.UploadIP == "" || cfg.UploadSize == 0 {
		tp.Verdict = TGError
		tp.Detail = "no upload target configured"
		return tp
	}
	ctx, cancel := context.WithTimeout(s.ctx, tgTotalTimeout)
	defer cancel()

	chunk := make([]byte, tgReadChunk)
	rand.Read(chunk)
	var sent int64
	reader := &countingReader{ctx: ctx, chunk: chunk, remaining: cfg.UploadSize, sent: &sent}

	client := s.telegramClient()
	defer client.CloseIdleConnections()
	addr := net.JoinHostPort(cfg.UploadIP, strconv.Itoa(cfg.UploadPort))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+addr+"/upload", reader)
	if err != nil {
		tp.Verdict = TGError
		tp.Detail = err.Error()
		return tp
	}
	req.ContentLength = cfg.UploadSize
	req.Header.Set("User-Agent", fetchUserAgent)
	req.Header.Set("Content-Type", "application/octet-stream")

	start := time.Now()
	stalledCh := make(chan struct{})
	var lastProgress atomic.Int64
	lastProgress.Store(start.UnixNano())
	reader.progress = &lastProgress
	var peak float64
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
				if now.Sub(time.Unix(0, lastProgress.Load())) >= tgStallTimeout {
					cancel()
					return
				}
				cur := atomic.LoadInt64(&sent)
				if d := now.Sub(lastTime); d > 0 {
					if bps := float64(cur-lastBytes) / d.Seconds(); bps > peak {
						peak = bps
					}
				}
				lastBytes = cur
				lastTime = now
			}
		}
	}()
	resp, err := client.Do(req)
	close(stalledCh)
	dur := time.Since(start)
	if resp != nil {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
	}
	total := atomic.LoadInt64(&sent)
	stalled := err != nil && ctx.Err() != nil && time.Since(time.Unix(0, lastProgress.Load())) >= tgStallTimeout
	if err != nil && total == 0 {
		tp.Verdict = TGBlocked
		tp.Detail = err.Error()
		return tp
	}
	fillThroughput(&tp, total, peak, dur, stalled)
	return tp
}

type countingReader struct {
	ctx       context.Context
	chunk     []byte
	remaining int64
	sent      *int64
	progress  *atomic.Int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	if r.ctx.Err() != nil {
		return 0, r.ctx.Err()
	}
	n := len(p)
	if int64(n) > r.remaining {
		n = int(r.remaining)
	}
	if n > len(r.chunk) {
		n = len(r.chunk)
	}
	copy(p, r.chunk[:n])
	r.remaining -= int64(n)
	atomic.AddInt64(r.sent, int64(n))
	if r.progress != nil {
		r.progress.Store(time.Now().UnixNano())
	}
	return n, nil
}

func fillThroughput(tp *TelegramThroughput, total int64, peak float64, dur time.Duration, stalled bool) {
	tp.Bytes = total
	tp.DurationMs = dur.Milliseconds()
	tp.MbpsPeak = bpsToMbps(peak)
	if dur > 0 {
		tp.MbpsAvg = bpsToMbps(float64(total) / dur.Seconds())
	}
	if tp.MbpsPeak == 0 {
		tp.MbpsPeak = tp.MbpsAvg
	}
	if tp.Expected > 0 {
		tp.PctOk = round1(float64(total) / float64(tp.Expected) * 100)
	}
	if stalled {
		tp.DropAtSec = int(dur.Seconds()) - int(tgStallTimeout.Seconds())
		if tp.DropAtSec < 0 {
			tp.DropAtSec = 0
		}
	}
	tp.Verdict = throughputVerdict(total, tp.Expected, stalled)
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
				if now.Sub(time.Unix(0, lastProgress.Load())) >= tgStallTimeout {
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
	if dl == TGStalled || r.Upload.Verdict == TGStalled {
		return TGStalled
	}
	if dl == TGSlow || dl == TGBlocked || r.Upload.Verdict == TGSlow || r.Upload.Verdict == TGBlocked {
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

func bpsToMbps(bps float64) float64 {
	return round1(bps * 8 / 1_000_000)
}
