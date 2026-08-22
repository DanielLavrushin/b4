package mtproto

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/gorilla/websocket"
)

func TestWebBridgeCapabilityVectors(t *testing.T) {
	cases := []struct {
		host   string
		secret string
		want   string
	}{
		{"proxy.example.com", "000102030405060708090a0b0c0d0e0f", "MHLEY5PmW1GWqJkSrlmJpvJUiLhBH_QKy6yKg8a0JPk"},
		{"proxy.example.com", "dd000102030405060708090a0b0c0d0e0f", "IpJrt3e7sKtzPyoXy6w-Zj6GGEvsvclN66JzQEfPYLA"},
	}
	for _, c := range cases {
		raw, err := hex.DecodeString(c.secret)
		if err != nil {
			t.Fatalf("decode %s: %v", c.secret, err)
		}
		if got := WebBridgeCapability(c.host, raw); got != c.want {
			t.Errorf("capability(%s, %s) = %s, want %s", c.host, c.secret, got, c.want)
		}
	}
}

func TestWebSecretFormsAndLink(t *testing.T) {
	var key [16]byte
	copy(key[:], mustHex(t, "000102030405060708090a0b0c0d0e0f"))

	plain, padded := WebSecretForms(key)
	if plain != "000102030405060708090a0b0c0d0e0f" {
		t.Errorf("plain form = %s", plain)
	}
	if padded != "dd000102030405060708090a0b0c0d0e0f" {
		t.Errorf("padded form = %s", padded)
	}
	want := "https://t.me/webproxy?server=relay.example.org&secret=" + padded
	if got := WebProxyLink("relay.example.org", key); got != want {
		t.Errorf("link = %s, want %s", got, want)
	}
}

func TestValidateWebProxyHost(t *testing.T) {
	ok := []struct{ in, want string }{
		{"relay.example.org", "relay.example.org"},
		{"Relay.Example.ORG", "relay.example.org"},
		{"relay.example.org.", "relay.example.org"},
		{"xn--bcher-kva.de", "xn--bcher-kva.de"},
		{"a.b.c.example.com", "a.b.c.example.com"},
	}
	for _, c := range ok {
		got, err := ValidateWebProxyHost(c.in)
		if err != nil {
			t.Errorf("ValidateWebProxyHost(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ValidateWebProxyHost(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	bad := []string{
		"", "   ",
		"https://relay.example.org",
		"relay.example.org:443",
		"relay.example.org/path",
		"relay.example.org?a=b",
		"relay.example.org#frag",
		"user@relay.example.org",
		"localhost",
		"127.0.0.1",
		"127.1",
		"1.2.3",
		"0x7f.1",
		"0177.0.0.1",
		"::1",
		"bücher.de",
		"relay..example.org",
		"-relay.example.org",
		"relay-.example.org",
		"relay.example.4",
		"relay.example.c0m",
		"relay.example.o",
		strings.Repeat("a", 64) + ".example.org",
	}
	for _, in := range bad {
		if got, err := ValidateWebProxyHost(in); err == nil {
			t.Errorf("ValidateWebProxyHost(%q) accepted as %q, want rejection", in, got)
		}
	}
}

func TestParseWebFrames(t *testing.T) {
	msg := appendWebFrame(nil, webFrameHello, 0, []byte{1})
	msg = appendWebFrame(msg, webFrameData, 0x0a0b0c, []byte("payload"))

	frames, err := parseWebFrames(msg)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(frames) != 2 {
		t.Fatalf("got %d frames, want 2", len(frames))
	}
	if frames[0].typ != webFrameHello || frames[0].stream != 0 || !bytes.Equal(frames[0].payload, []byte{1}) {
		t.Errorf("frame 0 = %+v", frames[0])
	}
	if frames[1].typ != webFrameData || frames[1].stream != 0x0a0b0c || string(frames[1].payload) != "payload" {
		t.Errorf("frame 1 = %+v", frames[1])
	}
}

func TestParseWebFramesRejects(t *testing.T) {
	cases := map[string][]byte{
		"empty":            {},
		"truncated header": {0x02, 0x00, 0x00},
		"unknown type":     {0x77, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00},
		"truncated body":   {0x02, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x04, 0x01},
		"payload over cap": {0x02, 0x00, 0x00, 0x01, 0x00, 0x20, 0x00, 0x01},
	}
	for name, msg := range cases {
		if _, err := parseWebFrames(msg); err == nil {
			t.Errorf("%s: accepted, want error", name)
		}
	}
}

func TestWebStreamReadWriteCredit(t *testing.T) {
	sess := newTestSession(t, nil)
	st := newWebStream(sess, 7, webAddr{network: "tcp", value: "1.2.3.4:1"})

	st.deliver([]byte("hello"))
	buf := make([]byte, 16)
	n, err := st.Read(buf)
	if err != nil || string(buf[:n]) != "hello" {
		t.Fatalf("read = %q, %v", buf[:n], err)
	}

	st.mu.Lock()
	st.sendCred = 4
	st.mu.Unlock()

	done := make(chan int, 1)
	go func() {
		n, _ := st.Write([]byte("abcdefgh"))
		done <- n
	}()

	select {
	case n := <-done:
		t.Fatalf("write completed with %d bytes before credit was granted", n)
	case <-time.After(50 * time.Millisecond):
	}

	st.addSendCredit(4)
	select {
	case n := <-done:
		if n != 8 {
			t.Fatalf("wrote %d bytes, want 8", n)
		}
	case <-time.After(time.Second):
		t.Fatal("write did not complete after credit was granted")
	}
}

func TestWebStreamReadDeadline(t *testing.T) {
	sess := newTestSession(t, nil)
	st := newWebStream(sess, 1, webAddr{network: "tcp", value: "1.2.3.4:1"})

	if err := st.SetReadDeadline(time.Now().Add(30 * time.Millisecond)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	if _, err := st.Read(make([]byte, 4)); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("read error = %v, want deadline exceeded", err)
	}

	if err := st.SetReadDeadline(time.Time{}); err != nil {
		t.Fatalf("clear deadline: %v", err)
	}
	st.deliver([]byte("x"))
	if _, err := st.Read(make([]byte, 4)); err != nil {
		t.Fatalf("read after cleared deadline: %v", err)
	}
}

func TestWebStreamReadAfterShutdown(t *testing.T) {
	sess := newTestSession(t, nil)
	st := newWebStream(sess, 1, webAddr{network: "tcp", value: "1.2.3.4:1"})
	st.deliver([]byte("tail"))
	st.shutdown(nil)

	got := make([]byte, 8)
	n, err := st.Read(got)
	if err != nil || string(got[:n]) != "tail" {
		t.Fatalf("buffered read = %q, %v", got[:n], err)
	}
	if _, err := st.Read(got); !errors.Is(err, io.EOF) {
		t.Fatalf("read after drain = %v, want EOF", err)
	}
}

func newTestSession(t *testing.T, serve func(*webStream)) *webSession {
	t.Helper()
	sess := &webSession{
		srv:       &Server{},
		secret:    &Secret{Name: "test"},
		host:      "relay.example.org",
		remote:    webAddr{network: "tcp", value: "1.2.3.4:1"},
		tag:       "[test]",
		outCh:     make(chan []byte, 512),
		ctrlCh:    make(chan []byte, 64),
		done:      make(chan struct{}),
		streams:   make(map[uint32]*webStream),
		recvCred:  make(map[uint32]int64),
		pendWin:   make(map[uint32]int),
		closedIDs: make(map[uint32]struct{}),
	}
	if serve == nil {
		serve = func(*webStream) {}
	}
	sess.serve = serve
	t.Cleanup(func() { sess.fail(errors.New("test over")) })
	return sess
}

type carrierHarness struct {
	client  *websocket.Conn
	server  *httptest.Server
	streams chan *webStream
}

func newCarrierHarness(t *testing.T) *carrierHarness {
	t.Helper()
	h := &carrierHarness{streams: make(chan *webStream, 8)}

	var wg sync.WaitGroup
	h.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := webUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		sess := newWebSession(&Server{}, &Secret{Name: "test"}, conn, "relay.example.org",
			webAddr{network: "tcp", value: "1.2.3.4:1"}, "[test]")
		sess.serve = func(st *webStream) {
			h.streams <- st
			<-sess.done
		}
		wg.Add(1)
		go func() { defer wg.Done(); sess.run() }()
	}))

	url := "ws" + strings.TrimPrefix(h.server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial carrier: %v", err)
	}
	h.client = conn
	t.Cleanup(func() {
		_ = conn.Close()
		h.server.Close()
		wg.Wait()
	})
	return h
}

func (h *carrierHarness) send(t *testing.T, frames ...[]byte) {
	t.Helper()
	var msg []byte
	for _, f := range frames {
		msg = append(msg, f...)
	}
	if err := h.client.WriteMessage(websocket.BinaryMessage, msg); err != nil {
		t.Fatalf("write carrier message: %v", err)
	}
}

func (h *carrierHarness) recv(t *testing.T) []webFrame {
	t.Helper()
	_ = h.client.SetReadDeadline(time.Now().Add(3 * time.Second))
	typ, msg, err := h.client.ReadMessage()
	if err != nil {
		t.Fatalf("read carrier message: %v", err)
	}
	if typ != websocket.BinaryMessage {
		t.Fatalf("carrier sent message type %d", typ)
	}
	frames, err := parseWebFrames(msg)
	if err != nil {
		t.Fatalf("parse carrier message: %v", err)
	}
	return frames
}

func (h *carrierHarness) expectClosed(t *testing.T) {
	t.Helper()
	_ = h.client.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		if _, _, err := h.client.ReadMessage(); err != nil {
			return
		}
	}
}

func helloFrame() []byte { return webFrameBytes(webFrameHello, 0, []byte{1}) }

func openFrame(id uint32) []byte { return webFrameBytes(webFrameOpen, id, nil) }

func TestCarrierWelcomeIsAloneInFirstMessage(t *testing.T) {
	h := newCarrierHarness(t)
	h.send(t, helloFrame())

	frames := h.recv(t)
	if len(frames) != 1 {
		t.Fatalf("first message carried %d frames, want exactly 1", len(frames))
	}
	if frames[0].typ != webFrameWelcome || frames[0].stream != 0 || len(frames[0].payload) != 0 {
		t.Fatalf("first frame = %+v, want empty WELCOME on stream 0", frames[0])
	}
}

func TestCarrierRejectsTrafficBeforeHello(t *testing.T) {
	h := newCarrierHarness(t)
	h.send(t, openFrame(1))
	h.expectClosed(t)
}

func TestCarrierRejectsSessionFrameOnStream(t *testing.T) {
	h := newCarrierHarness(t)
	h.send(t, webFrameBytes(webFrameHello, 5, []byte{1}))
	h.expectClosed(t)
}

func TestCarrierRejectsStreamFrameOnZero(t *testing.T) {
	h := newCarrierHarness(t)
	h.send(t, helloFrame())
	h.recv(t)
	h.send(t, webFrameBytes(webFrameData, 0, []byte("x")))
	h.expectClosed(t)
}

func TestCarrierRejectsDataBeyondCredit(t *testing.T) {
	h := newCarrierHarness(t)
	h.send(t, helloFrame())
	h.recv(t)
	h.send(t, openFrame(1))

	select {
	case <-h.streams:
	case <-time.After(3 * time.Second):
		t.Fatal("stream was never opened")
	}

	payload := make([]byte, webMaxFramePayload)
	for i := 0; i < webInitialWindow/webMaxFramePayload+1; i++ {
		if err := h.client.WriteMessage(websocket.BinaryMessage,
			webFrameBytes(webFrameData, 1, payload)); err != nil {
			return
		}
	}
	h.expectClosed(t)
}

func TestCarrierDeliversDataAndGrantsWindow(t *testing.T) {
	h := newCarrierHarness(t)
	h.send(t, helloFrame())
	h.recv(t)
	h.send(t, openFrame(1))

	var st *webStream
	select {
	case st = <-h.streams:
	case <-time.After(3 * time.Second):
		t.Fatal("stream was never opened")
	}

	body := bytes.Repeat([]byte("z"), webWindowFlushBytes+1)
	h.send(t, webFrameBytes(webFrameData, 1, body))

	got := make([]byte, 0, len(body))
	buf := make([]byte, 32<<10)
	for len(got) < len(body) {
		n, err := st.Read(buf)
		if err != nil {
			t.Fatalf("stream read: %v", err)
		}
		got = append(got, buf[:n]...)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("stream received %d bytes, want %d", len(got), len(body))
	}

	total := 0
	deadline := time.Now().Add(3 * time.Second)
	for total < len(body) && time.Now().Before(deadline) {
		for _, f := range h.recv(t) {
			if f.typ == webFrameWindow && f.stream == 1 {
				total += int(binary.BigEndian.Uint32(f.payload))
			}
		}
	}
	if total != len(body) {
		t.Fatalf("carrier granted %d bytes of window, want %d", total, len(body))
	}
}

func TestCarrierStreamWriteBecomesDataFrames(t *testing.T) {
	h := newCarrierHarness(t)
	h.send(t, helloFrame())
	h.recv(t)
	h.send(t, openFrame(3))

	var st *webStream
	select {
	case st = <-h.streams:
	case <-time.After(3 * time.Second):
		t.Fatal("stream was never opened")
	}

	body := bytes.Repeat([]byte("q"), webMaxDataChunk+11)
	go func() { _, _ = st.Write(body) }()

	var got []byte
	for len(got) < len(body) {
		for _, f := range h.recv(t) {
			if f.typ != webFrameData || f.stream != 3 {
				continue
			}
			if len(f.payload) > webMaxDataChunk {
				t.Errorf("DATA frame carried %d bytes, over the %d chunk cap", len(f.payload), webMaxDataChunk)
			}
			got = append(got, f.payload...)
		}
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("carrier delivered %d bytes, want %d", len(got), len(body))
	}
}

func TestCarrierCloseRaceIsTolerated(t *testing.T) {
	h := newCarrierHarness(t)
	h.send(t, helloFrame())
	h.recv(t)
	h.send(t, openFrame(9))

	select {
	case st := <-h.streams:
		st.Close()
	case <-time.After(3 * time.Second):
		t.Fatal("stream was never opened")
	}

	h.send(t, webFrameBytes(webFrameData, 9, []byte("late")))
	h.send(t, webFrameBytes(webFrameClose, 9, nil))
	h.send(t, webFrameBytes(webFramePing, 0, nil))

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, f := range h.recv(t) {
			if f.typ == webFramePong {
				return
			}
		}
	}
	t.Fatal("carrier did not survive a close race")
}

func TestServeWebProxyRoutesByHostAndCapability(t *testing.T) {
	var key [16]byte
	copy(key[:], mustHex(t, "000102030405060708090a0b0c0d0e0f"))
	secret := &Secret{Key: key, Name: "web", RawBytes: []byte{0xee}}
	secrets := []*Secret{secret}

	srv := &Server{}
	cfg := &config.Config{}
	cfg.System.MTProto.Enabled = true
	cfg.System.MTProto.WebProxy.Enabled = true
	cfg.System.MTProto.WebProxy.Hostname = "relay.example.org"
	srv.cfg.Store(cfg)
	srv.secrets.Store(&secrets)

	capability := WebBridgeCapability("relay.example.org", []byte{0xdd, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f})

	bridge := serveWebProxyTo(t, srv, "relay.example.org", "/?bridge="+capability)
	if !strings.Contains(bridge.body, "TelegramWebProxy") {
		t.Error("a valid capability did not get the bridge page")
	}
	if got := bridge.header.Get("Content-Security-Policy"); !strings.Contains(got, "http://127.0.0.1:*") {
		t.Errorf("bridge CSP = %q, must admit the client's loopback page", got)
	}
	if bridge.header.Get("X-Frame-Options") != "" {
		t.Error("bridge response must not set X-Frame-Options")
	}

	plain := serveWebProxyTo(t, srv, "relay.example.org", "/")
	invalid := serveWebProxyTo(t, srv, "relay.example.org", "/?bridge=notacapability")
	if plain.body != invalid.body || plain.status != invalid.status {
		t.Error("an invalid capability must be indistinguishable from an ordinary visit")
	}
	if strings.Contains(plain.body, "TelegramWebProxy") {
		t.Error("the ordinary site leaked the bridge page")
	}

	other := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "http://b4.example.net/", nil)
	if srv.ServeWebProxy(other, r) {
		t.Error("the relay took a request for another hostname")
	}
}

type webProxyResponse struct {
	status int
	body   string
	header http.Header
}

func serveWebProxyTo(t *testing.T, srv *Server, host, target string) webProxyResponse {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "http://"+host+target, nil)
	if !srv.ServeWebProxy(w, r) {
		t.Fatalf("relay did not take %s%s", host, target)
	}
	res := w.Result()
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return webProxyResponse{status: res.StatusCode, body: string(body), header: res.Header}
}

func TestWebProxyHostRequiresBothSwitches(t *testing.T) {
	srv := &Server{}
	cfg := &config.Config{}
	cfg.System.MTProto.WebProxy.Enabled = true
	cfg.System.MTProto.WebProxy.Hostname = "relay.example.org"
	srv.cfg.Store(cfg)
	if got := srv.WebProxyHost(); got != "" {
		t.Errorf("WebProxyHost = %q while the MTProto proxy is disabled", got)
	}

	cfg2 := &config.Config{}
	cfg2.System.MTProto.Enabled = true
	cfg2.System.MTProto.WebProxy.Hostname = "relay.example.org"
	srv.cfg.Store(cfg2)
	if got := srv.WebProxyHost(); got != "" {
		t.Errorf("WebProxyHost = %q while the WEB carrier is disabled", got)
	}
}

func TestWebRequestHost(t *testing.T) {
	cases := []struct{ host, forwarded, want string }{
		{"relay.example.org", "", "relay.example.org"},
		{"relay.example.org:8443", "", "relay.example.org"},
		{"Relay.Example.ORG.", "", "relay.example.org"},
		{"b4.internal:7000", "relay.example.org", "relay.example.org"},
		{"b4.internal:7000", "relay.example.org, proxy.internal", "relay.example.org"},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "http://"+c.host+"/", nil)
		r.Host = c.host
		if c.forwarded != "" {
			r.Header.Set("X-Forwarded-Host", c.forwarded)
		}
		if got := webRequestHost(r); got != c.want {
			t.Errorf("webRequestHost(%q, %q) = %q, want %q", c.host, c.forwarded, got, c.want)
		}
	}
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("decode %s: %v", s, err)
	}
	return b
}

var _ net.Conn = (*webStream)(nil)
