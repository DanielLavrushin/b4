package handler

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/geodat"
	"github.com/urlesistiana/v2dat/v2data"
	"google.golang.org/protobuf/proto"
)

func geositeBytes(t *testing.T) []byte {
	t.Helper()
	b, err := proto.Marshal(&v2data.GeoSiteList{Entry: []*v2data.GeoSite{
		{CountryCode: "YOUTUBE", Domain: []*v2data.Domain{{Type: v2data.Domain_Domain, Value: "youtube.com"}}},
		{CountryCode: "DISCORD", Domain: []*v2data.Domain{{Type: v2data.Domain_Domain, Value: "discord.com"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func existingGeosite(t *testing.T) (string, []byte) {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "geosite.dat")
	good := geositeBytes(t)
	if err := os.WriteFile(dest, good, 0o644); err != nil {
		t.Fatal(err)
	}
	return dest, good
}

func assertKept(t *testing.T, dest string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("the previous file is gone: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("the previous file was replaced (%d bytes, want %d)", len(got), len(want))
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(dest), ".download-*.tmp"))
	if len(leftovers) != 0 {
		t.Fatalf("temp files left behind: %v", leftovers)
	}
}

func closeDelimitedServer(t *testing.T, body []byte) string {
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
			go func(c net.Conn) {
				defer c.Close()
				_, _ = http.ReadRequest(bufio.NewReader(c))
				_, _ = c.Write([]byte("HTTP/1.1 200 OK\r\nConnection: close\r\n\r\n"))
				_, _ = c.Write(body)
			}(conn)
		}
	}()
	return "http://" + ln.Addr().String() + "/geosite.dat"
}

func TestDownloadFileRejectsWhatIsNotAGeodataFile(t *testing.T) {
	full := geositeBytes(t)
	html := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<!DOCTYPE html><html><body>blocked</body></html>"))
	}))
	defer html.Close()
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer empty.Close()

	cases := map[string]string{
		"html page":                html.URL + "/geosite.dat",
		"empty body":               empty.URL + "/geosite.dat",
		"close-delimited cut-off":  closeDelimitedServer(t, full[:len(full)-5]),
		"close-delimited raw junk": closeDelimitedServer(t, []byte("\n<html>")),
	}
	for name, url := range cases {
		t.Run(name, func(t *testing.T) {
			dest, good := existingGeosite(t)
			_, err := downloadFile(context.Background(), url, dest, verifyGeodat(geodat.KindSite))
			if !errors.Is(err, geodat.ErrUnusable) {
				t.Fatalf("want ErrUnusable, got %v", err)
			}
			assertKept(t, dest, good)
		})
	}
}

func TestDownloadFileReplacesWithAValidFile(t *testing.T) {
	full := geositeBytes(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(full)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "geosite.dat")
	if err := os.WriteFile(dest, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	size, err := downloadFile(context.Background(), srv.URL, dest, verifyGeodat(geodat.KindSite))
	if err != nil || size != int64(len(full)) {
		t.Fatalf("size=%d err=%v", size, err)
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, full) {
		t.Fatal("the new file is not in place")
	}
}

func TestDownloadFileGivesUpOnAStalledTransfer(t *testing.T) {
	prev := downloadStallTimeout
	downloadStallTimeout = 300 * time.Millisecond
	t.Cleanup(func() { downloadStallTimeout = prev })

	full := geositeBytes(t)
	release := make(chan struct{})
	defer close(release)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100000")
		_, _ = w.Write(full[:10])
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	dest, good := existingGeosite(t)
	start := time.Now()
	_, err := downloadFile(context.Background(), srv.URL, dest, verifyGeodat(geodat.KindSite))
	if !errors.Is(err, errDownloadStalled) {
		t.Fatalf("want a stall error, got %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("the stall timeout did not fire")
	}
	assertKept(t, dest, good)
}

func TestDownloadFileKeepsGoingWhileDataFlows(t *testing.T) {
	prev := downloadStallTimeout
	downloadStallTimeout = 300 * time.Millisecond
	t.Cleanup(func() { downloadStallTimeout = prev })

	full := geositeBytes(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := range full {
			_, _ = w.Write(full[i : i+1])
			w.(http.Flusher).Flush()
			time.Sleep(20 * time.Millisecond)
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "geosite.dat")
	if _, err := downloadFile(context.Background(), srv.URL, dest, verifyGeodat(geodat.KindSite)); err != nil {
		t.Fatalf("a slow transfer that keeps sending must finish: %v", err)
	}
}

func TestDownloadFileStopsWhenCancelled(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100000")
		_, _ = w.Write([]byte{0x0A})
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	dest, good := existingGeosite(t)
	_, err := downloadFile(ctx, srv.URL, dest, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	assertKept(t, dest, good)
}

func TestRefreshGeodatRefusesAConcurrentDownload(t *testing.T) {
	geodatDownloadMu.Lock()
	defer geodatDownloadMu.Unlock()
	api := &API{}
	if _, _, _, err := api.RefreshGeodat(context.Background(), t.TempDir(), "http://127.0.0.1:1/geosite.dat", ""); !errors.Is(err, errGeodatBusy) {
		t.Fatalf("want errGeodatBusy, got %v", err)
	}
}

func TestGeodatUploadRejectsWhatIsNotAGeodataFile(t *testing.T) {
	cfg := config.NewConfig()
	api := &API{cfgPtr: testCfgPtr(&cfg), geodataManager: geodat.NewGeodataManager("", "")}
	api.mux = http.NewServeMux()
	api.RegisterGeodatApi()

	dest, good := existingGeosite(t)
	for name, body := range map[string][]byte{
		"html":           []byte("<!DOCTYPE html><html></html>"),
		"truncated":      good[:len(good)-5],
		"geoip as sites": mustGeoIP(t),
	} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			mw := multipart.NewWriter(&buf)
			fw, err := mw.CreateFormFile("file", "geosite.dat")
			if err != nil {
				t.Fatal(err)
			}
			_, _ = fw.Write(body)
			_ = mw.WriteField("type", "geosite")
			_ = mw.WriteField("destination_path", filepath.Dir(dest))
			_ = mw.Close()

			req := httptest.NewRequest(http.MethodPost, "/api/geodat/upload", &buf)
			req.Header.Set("Content-Type", mw.FormDataContentType())
			rec := httptest.NewRecorder()
			api.mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "not a usable") {
				t.Fatalf("want 400, got %d %s", rec.Code, rec.Body.String())
			}
			got, _ := os.ReadFile(dest)
			if !bytes.Equal(got, good) {
				t.Fatal("the previous file was replaced")
			}
			leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(dest), ".geodat-upload-*.tmp"))
			if len(leftovers) != 0 {
				t.Fatalf("temp files left behind: %v", leftovers)
			}
		})
	}
}

func mustGeoIP(t *testing.T) []byte {
	t.Helper()
	b, err := proto.Marshal(&v2data.GeoIPList{Entry: []*v2data.GeoIP{
		{CountryCode: "TELEGRAM", Cidr: []*v2data.CIDR{{Ip: []byte{91, 108, 4, 0}, Prefix: 22}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}
