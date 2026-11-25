package handler

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/geodat"
	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/utils"
	"golang.org/x/sys/unix"
)

const canaryGeoPath = "/canarypath/geosite.dat?token=canaryquery"

var canaryLogin = base64.StdEncoding.EncodeToString([]byte("canaryuser:canarypass"))

func withLogin(t *testing.T, base string) string {
	t.Helper()
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword("canaryuser", "canarypass")
	return u.String()
}

func refusedAddr(t *testing.T) string {
	t.Helper()
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	if err := unix.Bind(fd, &unix.SockaddrInet4{Addr: [4]byte{127, 0, 0, 1}}); err != nil {
		t.Fatal(err)
	}
	bound, err := unix.Getsockname(fd)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("127.0.0.1:%d", bound.(*unix.SockaddrInet4).Port)
}

func rawServer(t *testing.T, answer func(conn net.Conn, r *http.Request)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				if r, err := http.ReadRequest(bufio.NewReader(conn)); err == nil {
					answer(conn, r)
				}
			}()
		}
	}()
	return ln.Addr().String()
}

func replyServer(t *testing.T, reply func(r *http.Request) string) string {
	t.Helper()
	return withLogin(t, "http://"+rawServer(t, func(conn net.Conn, r *http.Request) {
		_, _ = io.WriteString(conn, reply(r))
	})+canaryGeoPath)
}

func echoServer(t *testing.T, format string) string {
	t.Helper()
	return replyServer(t, func(r *http.Request) string { return fmt.Sprintf(format, r.RequestURI) })
}

func geoServer(t *testing.T, answer http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(answer)
	t.Cleanup(srv.Close)
	return withLogin(t, srv.URL+canaryGeoPath)
}

func redirectTo(location string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", location)
		w.WriteHeader(http.StatusFound)
	}
}

func TestAFailedGeodataDownloadDoesNotTellTheLogin(t *testing.T) {
	prevStall := downloadStallTimeout
	downloadStallTimeout = 300 * time.Millisecond
	t.Cleanup(func() { downloadStallTimeout = prevStall })
	dead := "http://" + refusedAddr(t)
	dropped := rawServer(t, func(net.Conn, *http.Request) {})

	for _, tc := range []struct {
		name string
		url  string
		says string
	}{
		{"refused connection", withLogin(t, dead+canaryGeoPath), "connection refused"},
		{"wrong login", geoServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) }), "401"},
		{"block page", geoServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>blocked</html>")) }), "was rejected"},
		{"cut off", geoServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "1000")
			_, _ = w.Write([]byte{0x0A})
		}), "failed to download"},
		{"stalled", geoServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "1000")
			_, _ = w.Write([]byte{0x0A})
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}), "download stalled"},
		{"silent server", geoServer(t, func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }), "download stalled"},
		{"redirect to a dead host", geoServer(t, redirectTo(withLogin(t, dead+canaryGeoPath))), "connection refused"},
		{"redirect to a host that hangs up", geoServer(t, redirectTo("http://canaryuser:canarypass@"+strings.Replace(dropped, "127.0.0.1", "localhost", 1)+canaryGeoPath)), "redirected to " + strings.Replace(dropped, "127.0.0.1", "localhost", 1)},
		{"status line that repeats the request", echoServer(t, "HTTP/1.1 404 %s\r\nContent-Length: 0\r\n\r\n"), "404 Not Found"},
		{"status code that repeats the request", echoServer(t, "HTTP/1.1 %s\r\n\r\n"), "malformed HTTP status code"},
		{"header that repeats the request", echoServer(t, "HTTP/1.1 200 OK\r\n%s\r\n\r\n"), "missing colon"},
		{"length that repeats the request", echoServer(t, "HTTP/1.1 200 OK\r\nContent-Length: %s\r\n\r\n"), "bad Content-Length"},
		{"trailer that repeats the request", echoServer(t, "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n1\r\n\n\r\n0\r\n%s\r\n\r\n"), "missing colon"},
		{"reply that is the token", replyServer(t, func(r *http.Request) string { return r.FormValue("token") + "\r\n\r\n" }), "malformed HTTP response"},
		{"header that repeats the login", replyServer(t, func(r *http.Request) string {
			return "HTTP/1.1 200 OK\r\n" + r.Header.Get("Authorization") + "\r\n\r\n"
		}), "missing colon"},
		{"redirect to a host that repeats its own path", geoServer(t, redirectTo(strings.Replace(echoServer(t, "HTTP/1.1 %s\r\n\r\n"), canaryGeoPath, "/canarysigned?sig=canarysig", 1))), "malformed HTTP status code"},
		{"redirect that does not parse", geoServer(t, redirectTo("http://[canaryhost/canarypath")), utils.ErrURLRedirect.Error()},
		{"redirect loop", geoServer(t, redirectTo(canaryGeoPath)), "redirects"},
		{"forgotten host", "https://canaryuser:canarypass/geosite.dat", errDownloadAddress.Error()},
		{"forgotten host with a number for a password", "https://canaryuser:123456789/canarypath", errDownloadAddress.Error()},
		{"leading space", " " + withLogin(t, dead+canaryGeoPath), errDownloadAddress.Error()},
		{"space in the password", "https://canaryuser:canary pass@" + strings.TrimPrefix(dead, "http://") + canaryGeoPath, errDownloadAddress.Error()},
		{"escape that does not parse", "https://canaryuser:canary%zzpass@example.com/", errDownloadAddress.Error()},
		{"no scheme", "canaryuser:canarypass@example.com" + canaryGeoPath, errDownloadAddress.Error()},
		{"another scheme", "ftp://canaryuser:canarypass@example.com" + canaryGeoPath, errDownloadAddress.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errorLog := filepath.Join(t.TempDir(), "errors.log")
			if err := log.InitErrorFile(errorLog); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = log.SetErrorFile("") })
			logs := captureLog(t)
			api, _ := asnAPI(t)
			api.RegisterGeodatApi()

			rec := postJSON(t, api.mux, "/api/geodat/download", GeodatDownloadRequest{GeositeURL: tc.url, DestinationPath: t.TempDir()})
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("want 500, got %d (%s)", rec.Code, rec.Body.String())
			}
			saved, err := os.ReadFile(errorLog)
			if err != nil {
				t.Fatal(err)
			}
			for where, text := range map[string]string{"the answer": rec.Body.String(), "the log": logs.String(), "errors.log": string(saved)} {
				if !strings.Contains(text, tc.says) {
					t.Errorf("%s must say what went wrong (%q): %s", where, tc.says, text)
				}
				if strings.Contains(text, "canary") || strings.Contains(text, "123456789") || strings.Contains(text, canaryLogin) {
					t.Errorf("%s holds a private part of the address: %s", where, text)
				}
				if strings.Contains(text, "redirected to") && !strings.HasPrefix(tc.name, "redirect") {
					t.Errorf("%s names a redirect that never was: %s", where, text)
				}
			}
			if u, err := url.Parse(tc.url); err == nil && tc.says != errDownloadAddress.Error() && !strings.Contains(rec.Body.String(), u.Host) {
				t.Errorf("the answer must still name the host %s: %s", u.Host, rec.Body.String())
			}
		})
	}
}

func TestAFailedDownloadIsStillRecognised(t *testing.T) {
	block := geoServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) })
	if _, err := downloadFile(context.Background(), block, filepath.Join(t.TempDir(), "geosite.dat"), verifyGeodat(geodat.KindSite)); !errors.Is(err, geodat.ErrUnusable) {
		t.Errorf("the scheduler stops retrying an unusable file by its error, got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	hang := geoServer(t, func(_ http.ResponseWriter, r *http.Request) {
		cancel()
		<-r.Context().Done()
	})
	if _, err := downloadFile(ctx, hang, filepath.Join(t.TempDir(), "geosite.dat"), nil); !errors.Is(err, context.Canceled) {
		t.Errorf("an upload that interrupts a download is recognised by context.Canceled, got %v", err)
	}
}

func TestDownloadLabelNamesPublicFilesInFull(t *testing.T) {
	swapBases(t, "https://api.github.com", "https://raw.githubusercontent.com", "https://github.com")
	sources, err := geodat.Sources()
	if err != nil || len(sources) == 0 {
		t.Fatalf("no built-in sources: %v", err)
	}
	for _, public := range []string{
		sources[0].GeositeURL,
		sources[0].GeoipURL,
		"https://github.com/DanielLavrushin/b4/releases/download/v1.93.0/b4-linux-arm64.tar.gz",
	} {
		if got := downloadLabel(public); got != public {
			t.Errorf("downloadLabel(%q) = %q, a public file is named in full", public, got)
		}
	}
	for raw, want := range map[string]string{
		"https://raw.githubusercontent.com/Loyalsoldier/private/main/geosite.dat?token=canaryquery": "https://raw.githubusercontent.com/[redacted]?token=[redacted]",
		"https://canaryuser:canarypass@files.example/canarypath":                                    "https://[redacted]@files.example/[redacted]",
		"https://files.example/geosite.dat?Y2FuYXJ5dG9rZW4=":                                        "https://files.example/[redacted]?[redacted]",
		"https://files.example/geosite.dat?sig=canaryquery==&canarytoken":                           "https://files.example/[redacted]?sig=[redacted]&[redacted]",
	} {
		if got := downloadLabel(raw); got != want {
			t.Errorf("downloadLabel(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestMirroredDownloadsDoNotLogAToken(t *testing.T) {
	for name, status := range map[string]int{"mirror works": http.StatusOK, "mirror fails": http.StatusBadGateway} {
		t.Run(name, func(t *testing.T) {
			good := geositeBytes(t)
			mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/b4/health" {
					return
				}
				w.WriteHeader(status)
				_, _ = w.Write(good)
			}))
			defer mirror.Close()
			dead := "http://" + refusedAddr(t)
			swapBases(t, dead, dead, dead)
			swapMirrors(t, []string{mirror.URL})
			logs := captureLog(t)

			_, _ = downloadFileMirrored(context.Background(), dead+"/Loyalsoldier/private/geosite.dat?token=canaryquery", filepath.Join(t.TempDir(), "geosite.dat"), b4Mirrors, nil)
			if !strings.Contains(logs.String(), "Direct download of "+dead) {
				t.Errorf("the direct failure must be logged with its host:\n%s", logs)
			}
			if status == http.StatusOK && !strings.Contains(logs.String(), "via mirror "+mirror.URL) || status != http.StatusOK && !strings.Contains(logs.String(), "Mirror "+mirror.URL+" failed for") {
				t.Errorf("the mirror's result must be logged:\n%s", logs)
			}
			if strings.Contains(logs.String(), "canary") {
				t.Errorf("the log holds the token:\n%s", logs)
			}
		})
	}
}

func TestASharedSetNamesOnlyABuiltInGeodataSource(t *testing.T) {
	sources, err := geodat.Sources()
	if err != nil || len(sources) == 0 {
		t.Fatalf("no built-in sources: %v", err)
	}
	private := "https://canaryuser:canarypass@files.example" + canaryGeoPath
	for _, tc := range []struct {
		name     string
		site, ip string
		want     *hubwire.GeoSource
	}{
		{"private", private, private, nil},
		{"one private", private, sources[0].GeoipURL, &hubwire.GeoSource{IPURL: sources[0].GeoipURL}},
		{"built in", sources[0].GeositeURL, sources[0].GeoipURL, &hubwire.GeoSource{SiteURL: sources[0].GeositeURL, IPURL: sources[0].GeoipURL}},
		{"built in with a login", strings.Replace(sources[0].GeositeURL, "https://", "https://canaryuser:canarypass@", 1), "", nil},
		{"none", "", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newHubEnv(t)
			local := hubStrategySet("My strategy", "example.com")
			local.Id = "local-1"
			env.update(func(cfg *config.Config) {
				cfg.Sets = []*config.SetConfig{&local}
				cfg.System.Geo.GeoSiteURL, cfg.System.Geo.GeoIpURL = tc.site, tc.ip
			})
			same := func(where string, got *hubwire.GeoSource, raw string) {
				t.Helper()
				if strings.Contains(raw, "canary") || strings.Contains(raw, "files.example") {
					t.Errorf("%s holds the private source: %s", where, raw)
				}
				if (got == nil) != (tc.want == nil) || got != nil && *got != *tc.want {
					t.Errorf("%s names %+v, want %+v", where, got, tc.want)
				}
			}

			rec := postJSON(t, env.mux, "/api/hub/envelope", local)
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
			}
			raw := rec.Body.String()
			var prepared HubEnvelopeResponse
			decodeInto(t, rec, &prepared)
			same("the prepared set", prepared.Envelope.Geo, raw)

			rec = postJSON(t, env.mux, "/api/hub/share", HubShareRequest{SetID: "local-1"})
			if rec.Code != http.StatusAccepted {
				t.Fatalf("expected 202, got %d (%s)", rec.Code, rec.Body.String())
			}
			records := env.hub.Records()
			if len(records) != 1 {
				t.Fatalf("the hub must have received one share, got %d", len(records))
			}
			var body hubwire.ShareBody
			if err := json.Unmarshal(records[0].Body, &body); err != nil {
				t.Fatal(err)
			}
			same("the published set", body.Envelope.Geo, string(records[0].Body))
		})
	}
}
