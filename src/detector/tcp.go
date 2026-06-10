package detector

import (
	"context"
	"crypto/tls"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/netprobe"
)

const (
	fatProbeIterations = 16
	padSizePerIter     = 4000
	randomPoolSize     = 100_000
	defaultSNI         = "example.com"
	fatConnectTimeout  = 8 * time.Second
	fatReadTimeout     = 12 * time.Second
	minDynamicTimeout  = 1500 * time.Millisecond
	interRequestDelay  = 50 * time.Millisecond
)

var randomPool string

func init() {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, randomPoolSize)
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := range b {
		b[i] = chars[r.Intn(len(chars))]
	}
	randomPool = string(b)
}

// FatProbeResult holds the outcome of a single fat probe against one IP.
type FatProbeResult struct {
	Alive    bool
	Detected bool
	DropAtKB int
	RTT      float64 // measured RTT in milliseconds
	Detail   string
}

// fatProbeState tracks mutable state across fat probe iterations.
type fatProbeState struct {
	client         *http.Client
	url            string
	sni            string
	dynamicTimeout time.Duration
	measuredRTT    float64
	rttSamples     []time.Duration
	rttCalibrated  bool
}

// FatProbe runs 16 sequential HEAD requests on a persistent keep-alive connection.
// It detects TSPU-style connection drops by gradually increasing header size.
// sni overrides the TLS ServerName; rttHint > 0 skips RTT calibration.
func FatProbe(ctx context.Context, mark uint, ip string, port int, sni string, rttHint float64) FatProbeResult {
	state := newFatProbeState(mark, ip, port, sni, rttHint)
	defer state.client.Transport.(*http.Transport).CloseIdleConnections()

	for i := 0; i < fatProbeIterations; i++ {
		select {
		case <-ctx.Done():
			return FatProbeResult{Detail: "canceled"}
		default:
		}

		elapsed, err := state.sendIteration(ctx, i)
		if err != nil {
			return state.handleError(err, i)
		}

		state.updateRTT(elapsed, i, rttHint)
		time.Sleep(interRequestDelay)
	}

	return FatProbeResult{Alive: true, RTT: state.measuredRTT}
}

func newFatProbeState(mark uint, ip string, port int, sni string, rttHint float64) *fatProbeState {
	scheme := "https"
	if port == 80 {
		scheme = "http"
	}

	effectiveSNI := sni
	if effectiveSNI == "" && port != 80 {
		effectiveSNI = defaultSNI
	}

	addr := fmt.Sprintf("%s:%d", ip, port)

	var tlsConf *tls.Config
	if port != 80 {
		tlsConf = &tls.Config{
			InsecureSkipVerify: true,
			ServerName:         effectiveSNI,
		}
	}

	transport := &http.Transport{
		MaxConnsPerHost:     1,
		MaxIdleConnsPerHost: 1,
		IdleConnTimeout:     30 * time.Second,
		DisableKeepAlives:   false,
		TLSClientConfig:     tlsConf,
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return netprobe.Dialer(int(mark), fatConnectTimeout, 0).DialContext(ctx, "tcp", addr)
		},
	}

	state := &fatProbeState{
		client: &http.Client{
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		url: fmt.Sprintf("%s://%s/", scheme, addr),
		sni: effectiveSNI,
	}

	if rttHint > 0 {
		state.dynamicTimeout = clampTimeout(time.Duration(rttHint*float64(time.Millisecond)) * 3)
		state.measuredRTT = rttHint
		state.rttCalibrated = true
	}

	return state
}

func (s *fatProbeState) sendIteration(ctx context.Context, i int) (time.Duration, error) {
	timeout := fatReadTimeout
	if s.dynamicTimeout > 0 {
		timeout = s.dynamicTimeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "HEAD", s.url, nil)
	if err != nil {
		return 0, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Connection", "keep-alive")
	if s.sni != "" {
		req.Host = s.sni
	}

	if i > 0 {
		startIdx := rand.Intn(len(randomPool) - padSizePerIter - 1)
		req.Header.Set("X-Pad", randomPool[startIdx:startIdx+padSizePerIter])
	}

	start := time.Now()
	resp, err := s.client.Do(req)
	elapsed := time.Since(start)

	if err != nil {
		return elapsed, err
	}
	resp.Body.Close()
	return elapsed, nil
}

func (s *fatProbeState) updateRTT(elapsed time.Duration, i int, rttHint float64) {
	if s.rttCalibrated || i >= 2 {
		return
	}
	s.rttSamples = append(s.rttSamples, elapsed)
	if len(s.rttSamples) == 2 {
		maxRTT := s.rttSamples[0]
		if s.rttSamples[1] > maxRTT {
			maxRTT = s.rttSamples[1]
		}
		s.measuredRTT = float64(maxRTT) / float64(time.Millisecond)
		s.dynamicTimeout = clampTimeout(maxRTT * 3)
		s.rttCalibrated = true
	}
}

func (s *fatProbeState) handleError(err error, iteration int) FatProbeResult {
	detail := classifyFatProbeError(err)
	if iteration == 0 {
		return FatProbeResult{Alive: false, Detail: detail}
	}
	dropAtKB := (iteration * padSizePerIter) / 1024
	suffix := ""
	if dropAtKB >= 12 && dropAtKB <= 69 {
		suffix = " [TSPU 12-69KB window]"
	}
	return FatProbeResult{
		Alive:    true,
		Detected: true,
		DropAtKB: dropAtKB,
		RTT:      s.measuredRTT,
		Detail:   fmt.Sprintf("%s at %dKB%s", detail, dropAtKB, suffix),
	}
}

func clampTimeout(d time.Duration) time.Duration {
	if d < minDynamicTimeout {
		return minDynamicTimeout
	}
	if d > fatReadTimeout {
		return fatReadTimeout
	}
	return d
}

func classifyFatProbeError(err error) string {
	msg := strings.ToLower(err.Error())

	switch {
	case strings.Contains(msg, "refused") || strings.Contains(msg, "10061"):
		return "Refused"
	case strings.Contains(msg, "reset") || strings.Contains(msg, "10054"):
		return "TCP RST"
	case strings.Contains(msg, "abort") || strings.Contains(msg, "10053"):
		return "TCP ABORT"
	case strings.Contains(msg, "eof") || strings.Contains(msg, "closed"):
		return "TCP FIN"
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline"):
		return "Blackhole"
	case strings.Contains(msg, "broken pipe"):
		return "Broken pipe"
	default:
		return "Error"
	}
}

func fatProbeResultToStatus(fp FatProbeResult) TCPStatus {
	switch {
	case fp.Detected:
		return TCPDetected
	case !fp.Alive:
		if strings.Contains(fp.Detail, "Blackhole") {
			return TCPTimeout
		}
		return TCPError
	default:
		return TCPOk
	}
}

func (s *DetectorSuite) runTCPCheck(ctx context.Context) *TCPResult {
	log.DiscoveryLogf("[Detector] Starting TCP fat probe test for %d targets", len(TCPTargets))

	result := &TCPResult{}
	results := make([]TCPTargetResult, len(TCPTargets))

	var wg sync.WaitGroup
	sem := make(chan struct{}, 15)

	for i, target := range TCPTargets {
		if s.isCanceled() {
			break
		}
		wg.Add(1)
		go func(idx int, tgt TCPTarget) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			fp := FatProbe(ctx, s.mark, tgt.IP, tgt.Port, tgt.SNI, 0)
			tr := TCPTargetResult{
				Target:   tgt,
				Alive:    fp.Alive,
				RTT:      fp.RTT,
				DropAtKB: fp.DropAtKB,
				Detail:   fp.Detail,
				Status:   fatProbeResultToStatus(fp),
			}
			if tr.Status == TCPOk && tr.Detail == "" {
				tr.Detail = "All 16 iterations OK"
			}
			results[idx] = tr

			s.mu.Lock()
			s.CompletedChecks++
			s.mu.Unlock()
		}(i, target)
	}

	wg.Wait()

	for _, tr := range results {
		result.Targets = append(result.Targets, tr)
		switch tr.Status {
		case TCPOk:
			result.OkCount++
		case TCPDetected:
			result.DetectedCount++
		}
	}

	total := len(results)
	result.Summary = fmt.Sprintf("%d/%d OK, %d TSPU drops detected",
		result.OkCount, total, result.DetectedCount)

	if result.DetectedCount > 0 {
		pct := float64(result.DetectedCount) / float64(total) * 100
		result.Summary += fmt.Sprintf(" (%.0f%% of endpoints affected)", pct)
	}

	log.DiscoveryLogf("[Detector] TCP check complete: %s", result.Summary)
	return result
}
