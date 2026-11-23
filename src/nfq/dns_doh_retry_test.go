package nfq

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/daniellavrushin/b4/dns"
)

func TestADeviceQueryLostWithItsConnectionIsAskedAgain(t *testing.T) {
	var asked atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		query, _ := io.ReadAll(r.Body)
		if asked.Add(1) == 2 {
			conn, _, err := rw.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
			return
		}
		_, _ = rw.Write(dns.BuildAnswerFromIPs(query, 60, []net.IP{net.ParseIP("203.0.113.9")}))
	}))
	t.Cleanup(ts.Close)
	w := redirectWorker(t, false, redirectSet("", ts.URL+"/dns-query"))
	t.Cleanup(getDoHClient(0, w.getConfig().DNSQueryTimeout()).CloseIdleConnections)

	for i := 0; i < 2; i++ {
		answer, err := w.resolveDoHRedirect(context.Background(), ts.URL+"/dns-query", 0, dns.BuildQuery("www.example.com", uint16(100+i), dnsTypeA))
		if err != nil {
			t.Fatalf("query %d: %v", i+1, err)
		}
		onlyAddress(t, answer, "203.0.113.9")
	}
	if n := asked.Load(); n != 3 {
		t.Fatalf("%d queries reached the server, want the lost one asked once more", n)
	}
}
