package watchdog

import (
	"context"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func allowLoopback(netip.Addr) bool { return false }

func strictLocal(url string) URLCheck {
	return strictCheck(context.Background(), url, strictOptions{Timeout: 5 * time.Second, isReserved: allowLoopback})
}

func page(n int) string {
	return "<html><body>" + strings.Repeat("a", n) + "</body></html>"
}

func TestStrictCheckStatusCodes(t *testing.T) {
	codes := map[int]string{
		http.StatusOK:                         URLStatusOK,
		http.StatusNotFound:                   URLStatusOK,
		http.StatusBadRequest:                 URLStatusFailed,
		http.StatusUnavailableForLegalReasons: URLStatusFailed,
		http.StatusInternalServerError:        URLStatusFailed,
		http.StatusServiceUnavailable:         URLStatusFailed,
	}
	for code, want := range codes {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(code)
			fmt.Fprint(w, page(8000))
		}))
		res := strictLocal(srv.URL + "/")
		srv.Close()
		if res.Status != want || res.StatusCode != code {
			t.Errorf("HTTP %d: got %s (%d, %s), want %s", code, res.Status, res.StatusCode, res.Error, want)
		}
	}
}

func TestStrictCheckBodyRules(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/short", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "<html>tiny</html>")
	})
	mux.HandleFunc("/truncated", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "50000")
		fmt.Fprint(w, strings.Repeat("a", 20000))
	})
	mux.HandleFunc("/truncated-but-closed", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "50000")
		fmt.Fprint(w, page(20000))
	})
	mux.HandleFunc("/big", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "300000")
		fmt.Fprint(w, strings.Repeat("a", 300000))
	})
	mux.HandleFunc("/chunked", func(w http.ResponseWriter, _ *http.Request) {
		for i := 0; i < 4; i++ {
			fmt.Fprint(w, strings.Repeat("a", 2000))
			w.(http.Flusher).Flush()
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	if res := strictLocal(srv.URL + "/short"); res.Status != URLStatusFailed || !strings.Contains(res.Error, "insufficient") {
		t.Errorf("under 1024 bytes fails: %+v", res)
	}
	if res := strictLocal(srv.URL + "/truncated"); res.Status != URLStatusFailed || !strings.Contains(res.Error, "truncated") {
		t.Errorf("a body cut well short of its Content-Length fails: %+v", res)
	}
	if res := strictLocal(srv.URL + "/truncated-but-closed"); res.Status != URLStatusOK {
		t.Errorf("a short body that closes its html is complete: %+v", res)
	}
	res := strictLocal(srv.URL + "/big")
	if res.Status != URLStatusOK || res.BytesRead != strictMaxRead {
		t.Errorf("reading stops at 100 KB and that counts as complete: %+v", res)
	}
	if res := strictLocal(srv.URL + "/chunked"); res.Status != URLStatusOK || res.BytesRead != 8000 {
		t.Errorf("a clean chunked body without a length is fine: %+v", res)
	}
}

func TestStrictCheckBlockPages(t *testing.T) {
	body := "<html><body>Доступ к информационному ресурсу ограничен" + strings.Repeat(" ", 2000) + "</body></html>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "http://warning.rt.ru/?id=1", http.StatusFound)
			return
		}
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	if res := strictLocal(srv.URL + "/"); res.Status != URLStatusFailed || !strings.Contains(res.Error, "block page") {
		t.Errorf("a block page body fails: %+v", res)
	}
	if res := strictLocal(srv.URL + "/redirect"); res.Status != URLStatusFailed || !strings.Contains(res.Error, "block page") {
		t.Errorf("a redirect to a block page fails without following it: %+v", res)
	}
}

func TestStrictCheckRedirectLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var hops int
		fmt.Sscanf(r.URL.Query().Get("left"), "%d", &hops)
		if hops > 0 {
			http.Redirect(w, r, fmt.Sprintf("/?left=%d", hops-1), http.StatusFound)
			return
		}
		fmt.Fprint(w, page(4000))
	}))
	defer srv.Close()

	if res := strictLocal(srv.URL + "/?left=3"); res.Status != URLStatusOK {
		t.Errorf("three redirects are followed: %+v", res)
	}
	if res := strictLocal(srv.URL + "/?left=4"); res.Status != URLStatusFailed || !strings.Contains(res.Error, "redirects") {
		t.Errorf("a fourth redirect fails: %+v", res)
	}
}

func TestStrictCheckRefusesReservedDestinations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/lan" {
			http.Redirect(w, r, "http://10.1.2.3/admin", http.StatusFound)
			return
		}
		fmt.Fprint(w, page(4000))
	}))
	defer srv.Close()

	if res := checkURLStrict(context.Background(), srv.URL+"/", false, 3*time.Second); res.Status != URLStatusUnusable {
		t.Errorf("a loopback destination is never probed: %+v", res)
	}

	tenOnly := func(a netip.Addr) bool { return a.Is4() && a.As4()[0] == 10 }
	res := strictCheck(context.Background(), srv.URL+"/lan", strictOptions{Timeout: 3 * time.Second, isReserved: tenOnly})
	if res.Status != URLStatusUnusable || !strings.Contains(res.Error, "10.1.2.3") {
		t.Errorf("a redirect into the LAN is refused and can never be healed: %+v", res)
	}
}

func TestStrictCheckCertificateErrorsAreUnusable(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, page(4000))
	}))
	defer srv.Close()

	untrusted := strictOptions{Timeout: 5 * time.Second, RootCAs: x509.NewCertPool(), isReserved: allowLoopback}
	if res := strictCheck(context.Background(), srv.URL+"/", untrusted); res.Status != URLStatusUnusable || !strings.Contains(res.Error, "certificate") {
		t.Errorf("an untrusted certificate cannot be fixed by a strategy: %+v", res)
	}

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	res := strictCheck(context.Background(), srv.URL+"/", strictOptions{Timeout: 5 * time.Second, RootCAs: pool, isReserved: allowLoopback})
	if res.Status != URLStatusOK {
		t.Errorf("a trusted certificate verifies: %+v", res)
	}
}

func TestStrictCheckRejectsNonHTTPURLs(t *testing.T) {
	for _, raw := range []string{"", "ftp://example.com/", "example.com"} {
		if res := checkURLStrict(context.Background(), raw, false, time.Second); res.Status != URLStatusFailed {
			t.Errorf("%q: %+v", raw, res)
		}
	}
}

func TestStrictCheckSendsTheDiscoveryUserAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.UserAgent(), "Chrome/") {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, "<html>Sorry, something went wrong</html>")
			return
		}
		fmt.Fprint(w, page(8000))
	}))
	defer srv.Close()
	if res := strictLocal(srv.URL + "/"); res.Status != URLStatusOK {
		t.Errorf("a site that refuses a Chrome User-Agent from a non-browser client must load: %s (%d, %s)", res.Status, res.StatusCode, res.Error)
	}
}
