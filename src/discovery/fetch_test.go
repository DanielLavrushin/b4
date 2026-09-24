package discovery

import (
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/nfq"
)

func TestProbeRefusesARedirectToABlockPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://warning.rt.ru/?id=17", http.StatusFound)
	}))
	defer srv.Close()

	ds := newProbeOnlySuite(srv.URL)
	res := ds.fetchUsingIPForDomain(ds.Domains[0], 5*time.Second, "")

	if res.Status != CheckStatusFailed {
		t.Fatalf("a redirect to a block page scored as %q; the page it lands on is the ISP's, not the site", res.Status)
	}
	if !strings.Contains(res.Error, "ISP block page (redirect to http://warning.rt.ru/") {
		t.Fatalf("error %q, want it to name the block-page redirect", res.Error)
	}
}

func TestProbeFollowsARedirectInsideTheSite(t *testing.T) {
	page := "<html><body>" + strings.Repeat("x", 2048) + "</body></html>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			_, _ = w.Write([]byte(page))
			return
		}
		http.Redirect(w, r, "/login?reason=session_blocked", http.StatusFound)
	}))
	defer srv.Close()

	ds := newProbeOnlySuite(srv.URL)
	res := ds.fetchUsingIPForDomain(ds.Domains[0], 5*time.Second, "")

	if res.Status != CheckStatusComplete {
		t.Fatalf("a redirect within the site scored as %q (%s); only a redirect to another site can be the ISP's page", res.Status, res.Error)
	}
}

func TestProbeReportsAStallBeforeTheTimeout(t *testing.T) {
	const sent = 20 * 1024
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(100*1024))
		_, _ = w.Write(make([]byte, sent))
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(6 * time.Second):
		}
	}))
	defer srv.Close()

	ds := newProbeOnlySuite(srv.URL)
	start := time.Now()
	res := ds.fetchUsingIPForDomain(ds.Domains[0], 8*time.Second, "")
	elapsed := time.Since(start)

	if res.Status != CheckStatusFailed {
		t.Fatalf("a body that stops after %d bytes scored as %q", sent, res.Status)
	}
	if !strings.Contains(res.Error, "stalled after") {
		t.Fatalf("error %q, want the stall to be reported as such", res.Error)
	}
	if res.BytesRead != sent {
		t.Fatalf("bytes read %d, want %d", res.BytesRead, sent)
	}
	if elapsed > 4*time.Second {
		t.Fatalf("the stall took %v to detect, want about %v, not the whole timeout", elapsed, probeStallTimeout)
	}
}

func TestProbeWaitsForTheFirstByteUnderTheTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
			return
		case <-time.After(probeStallTimeout + probeStallTimeout/4):
		}
		_, _ = w.Write([]byte("<html><body>" + strings.Repeat("z", 2048) + "</body></html>"))
	}))
	defer srv.Close()

	ds := newProbeOnlySuite(srv.URL)
	res := ds.fetchUsingIPForDomain(ds.Domains[0], 8*time.Second, "")

	if res.Status != CheckStatusComplete {
		t.Fatalf("a slow first byte scored as %q (%s); only silence in the middle of a transfer is a stall", res.Status, res.Error)
	}
}

func throttledProxy(t *testing.T, backend string, bytesPerSec int) string {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var conns sync.WaitGroup
	t.Cleanup(func() {
		ln.Close()
		conns.Wait()
	})
	conns.Add(1)
	go func() {
		defer conns.Done()
		for {
			client, err := ln.Accept()
			if err != nil {
				return
			}
			conns.Add(1)
			go func(client net.Conn) {
				defer conns.Done()
				defer client.Close()
				server, err := net.Dial("tcp4", backend)
				if err != nil {
					return
				}
				defer server.Close()
				done := make(chan struct{})
				go func() {
					_, _ = io.Copy(server, client)
					server.Close()
					close(done)
				}()
				buf := make([]byte, bytesPerSec/20)
				for {
					n, err := server.Read(buf)
					if n > 0 {
						if _, werr := client.Write(buf[:n]); werr != nil {
							break
						}
						time.Sleep(50 * time.Millisecond)
					}
					if err != nil {
						break
					}
				}
				client.Close()
				<-done
			}(client)
		}
	}()
	return ln.Addr().String()
}

func htmlOf(size int) []byte {
	var b bytes.Buffer
	b.WriteString("<html><body>")
	for i := 0; b.Len() < size-20; i++ {
		b.WriteString(strconv.Itoa(i * 7919 % 100003))
		b.WriteByte(' ')
	}
	b.WriteString("</body></html>")
	return b.Bytes()
}

func TestProbeKeepsAThrottledTLSPage(t *testing.T) {
	page := htmlOf(20 * 1024)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(page)))
		_, _ = w.Write(page)
	}))
	srv.TLS = &tls.Config{DynamicRecordSizingDisabled: true}
	srv.StartTLS()
	defer srv.Close()

	addr := throttledProxy(t, strings.TrimPrefix(srv.URL, "https://"), 6*1024)
	ds := newProbeOnlySuite("https://" + addr + "/")
	res := ds.fetchUsingIPForDomain(ds.Domains[0], 8*time.Second, "")

	if res.Status != CheckStatusComplete {
		t.Fatalf("a page arriving steadily at 6 KB/s in 16 KB TLS records scored as %q (%s); the wire never went quiet", res.Status, res.Error)
	}
}

func TestProbeKeepsAThrottledGzipPage(t *testing.T) {
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	_, _ = zw.Write(htmlOf(60 * 1024))
	_ = zw.Close()
	body := compressed.Bytes()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	addr := throttledProxy(t, strings.TrimPrefix(srv.URL, "http://"), 5*1024)
	ds := newProbeOnlySuite("http://" + addr + "/")
	res := ds.fetchUsingIPForDomain(ds.Domains[0], 10*time.Second, "")

	if res.Status != CheckStatusComplete {
		t.Fatalf("a gzip page arriving steadily at 5 KB/s scored as %q (%s); the decoder holds output back, the wire does not", res.Status, res.Error)
	}
}

func TestProbeKeepsASlowButMovingBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		for i := 0; i < 3; i++ {
			_, _ = w.Write([]byte(strings.Repeat("y", 1024)))
			flusher.Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(probeStallTimeout / 2):
			}
		}
		_, _ = w.Write([]byte("<html><body></body></html>"))
	}))
	defer srv.Close()

	ds := newProbeOnlySuite(srv.URL)
	res := ds.fetchUsingIPForDomain(ds.Domains[0], 8*time.Second, "")

	if res.Status != CheckStatusComplete {
		t.Fatalf("a body that keeps arriving within the stall window scored as %q (%s)", res.Status, res.Error)
	}
}

func TestRunWithoutAConfigEndsFailedAndIsReleased(t *testing.T) {
	previous := suiteRetention
	suiteRetention = 10 * time.Millisecond
	t.Cleanup(func() { suiteRetention = previous })

	suite := NewCheckSuite([]DomainInput{{Domain: "example.com", CheckURL: "https://example.com/"}})
	suite.SetId = "set-a"
	ds := &DiscoverySuite{
		CheckSuite:    suite,
		pool:          &nfq.Pool{},
		domainResults: map[string]*DomainDiscoveryResult{"example.com": {Domain: "example.com", Results: map[string]*DomainPresetResult{}}},
		dnsResults:    map[string]*DNSDiscoveryResult{},
	}
	ds.initCancelContext()
	RegisterSuite(ds.CheckSuite)

	ds.RunDiscovery()

	if ds.Status != CheckStatusFailed {
		t.Fatalf("status %q, want failed: the run never tested anything", ds.Status)
	}
	if len(ds.DomainDiscoveryResults) != 0 {
		t.Fatalf("per-site results %v published for a run that never fetched a site", ds.DomainDiscoveryResults)
	}
	if ds.SetVerdict == nil || ds.SetVerdict.Status != SetVerdictIncomplete {
		t.Fatalf("set verdict %+v, want incomplete so the page and the watchdog see an end", ds.SetVerdict)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := GetCheckSuite(ds.Id); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the failed run is never dropped from the active suites")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
