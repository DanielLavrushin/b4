package mtproto

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const (
	liveEnvRelay    = "B4_WEB_RELAY"
	liveEnvSecret   = "B4_WEB_SECRET"
	liveEnvEndpoint = "B4_WEB_ENDPOINT"
	liveEnvInsecure = "B4_WEB_INSECURE"
	liveEnvDC       = "B4_WEB_DC"
)

type liveRelay struct {
	host     string
	endpoint string
	insecure bool
	secret   []byte
	cap      string
	dc       int
}

func liveRelayFromEnv(t *testing.T) *liveRelay {
	t.Helper()
	host := strings.TrimSpace(os.Getenv(liveEnvRelay))
	secretHex := strings.TrimSpace(os.Getenv(liveEnvSecret))
	if host == "" || secretHex == "" {
		t.Skipf("set %s and %s to run the live relay conformance test", liveEnvRelay, liveEnvSecret)
	}
	canonical, err := ValidateWebProxyHost(host)
	if err != nil {
		t.Fatalf("%s=%q: %v", liveEnvRelay, host, err)
	}
	raw, err := hex.DecodeString(secretHex)
	if err != nil {
		t.Fatalf("%s is not hex: %v", liveEnvSecret, err)
	}
	switch {
	case len(raw) == 16:
	case len(raw) == 17 && raw[0] == secretTagPadded:
	case len(raw) >= 17 && raw[0] == secretTagFakeTLS:
		key := make([]byte, 0, 17)
		key = append(key, secretTagPadded)
		key = append(key, raw[1:17]...)
		t.Logf("fake-TLS secret supplied; using its padded WEB form for a %d-byte key", len(raw[1:17]))
		raw = key
	default:
		t.Fatalf("%s must be a 16-byte, dd-prefixed or ee-prefixed secret", liveEnvSecret)
	}

	dc := 2
	if v := os.Getenv(liveEnvDC); v != "" {
		if _, err := fmt.Sscanf(v, "%d", &dc); err != nil {
			t.Fatalf("%s=%q: %v", liveEnvDC, v, err)
		}
	}
	return &liveRelay{
		host:     canonical,
		endpoint: strings.TrimSpace(os.Getenv(liveEnvEndpoint)),
		insecure: os.Getenv(liveEnvInsecure) != "",
		secret:   raw,
		cap:      WebBridgeCapability(canonical, raw),
		dc:       dc,
	}
}

func (l *liveRelay) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if l.endpoint != "" {
		addr = l.endpoint
	}
	var d net.Dialer
	return d.DialContext(ctx, network, addr)
}

func (l *liveRelay) httpClient(t *testing.T) *http.Client {
	t.Helper()
	tr := &http.Transport{
		DialContext:       l.dialContext,
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: l.insecure},
		DisableKeepAlives: true,
	}
	t.Cleanup(tr.CloseIdleConnections)
	return &http.Client{Timeout: 15 * time.Second, Transport: tr}
}

func (l *liveRelay) get(t *testing.T, target string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "https://"+l.host+target, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	res, err := l.httpClient(t).Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	return res.StatusCode, string(body)
}

func TestWebProxyLiveBridgeGating(t *testing.T) {
	l := liveRelayFromEnv(t)

	plainCode, plain := l.get(t, "/")
	invalidCode, invalid := l.get(t, "/?bridge=notacapability")
	bridgeCode, bridge := l.get(t, "/?bridge="+l.cap)

	if plainCode != invalidCode || plain != invalid {
		t.Errorf("an invalid capability is distinguishable from a plain visit (%d vs %d)", plainCode, invalidCode)
	}
	if strings.Contains(plain, "TelegramWebProxy") {
		t.Error("the ordinary site leaked the bridge page")
	}
	if bridgeCode != http.StatusOK || !strings.Contains(bridge, "TelegramWebProxy") {
		t.Fatalf("valid capability got %d and no bridge page; check that the relay carries this secret", bridgeCode)
	}
	t.Logf("relay %s serves the bridge page for capability %s", l.host, l.cap)
}

func TestWebProxyLiveCarrierReachesTelegram(t *testing.T) {
	l := liveRelayFromEnv(t)

	dialer := websocket.Dialer{
		NetDialContext:   l.dialContext,
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: l.insecure},
		HandshakeTimeout: 15 * time.Second,
	}
	_, page := l.get(t, "/?bridge="+l.cap)
	ticket := webTicketFromPage(t, page)

	dialer.Subprotocols = []string{webSubprotoPrefix + ticket}
	hdr := http.Header{}
	hdr.Set("Origin", "https://"+l.host)

	url := "wss://" + l.host + webCarrierPath
	conn, res, err := dialer.Dial(url, hdr)
	if err != nil {
		status := 0
		if res != nil {
			status = res.StatusCode
		}
		t.Fatalf("carrier dial %s: %v (http %d)", url, err, status)
	}
	defer func() {
		_ = conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(time.Second))
		_ = conn.Close()
	}()
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	client := &liveCarrier{t: t, conn: conn}
	client.write(webFrameBytes(webFrameHello, 0, []byte{1}))

	first := client.read()
	if len(first) != 1 || first[0].typ != webFrameWelcome || first[0].stream != 0 || len(first[0].payload) != 0 {
		t.Fatalf("first carrier message was %v, want exactly one empty WELCOME", first)
	}
	t.Log("carrier handshake complete")

	const stream = 1
	client.write(webFrameBytes(webFrameOpen, stream, nil))

	frame, encStream, decStream := buildClientObfuscation(t, l.secret, l.dc, connectionTagAbridged)
	client.write(webFrameBytes(webFrameData, stream, frame))

	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatalf("nonce: %v", err)
	}
	req := abridgedFrame(unencryptedMTProto(reqPQMulti(nonce)))
	encStream.XORKeyStream(req, req)
	client.write(webFrameBytes(webFrameData, stream, req))

	var down []byte
	deadline := time.Now().Add(25 * time.Second)
	for len(down) < 5 && time.Now().Before(deadline) {
		for _, f := range client.read() {
			switch f.typ {
			case webFrameData:
				if f.stream != stream {
					t.Fatalf("DATA on unexpected stream %d", f.stream)
				}
				plain := make([]byte, len(f.payload))
				copy(plain, f.payload)
				decStream.XORKeyStream(plain, plain)
				down = append(down, plain...)
			case webFrameClose:
				t.Fatalf("relay closed stream %d before Telegram answered; check the b4 log for the DC dial", f.stream)
			case webFramePing:
				client.write(webFrameBytes(webFramePong, 0, f.payload))
			case webFrameWindow:
			default:
				t.Fatalf("unexpected %s from relay", f.typ)
			}
		}
	}
	if len(down) < 5 {
		t.Fatalf("Telegram sent nothing back through the carrier (%d bytes)", len(down))
	}

	body := stripAbridged(t, down)
	if len(body) < 24 {
		t.Fatalf("downstream MTProto message too short: %d bytes", len(body))
	}
	if got := binary.LittleEndian.Uint64(body[0:8]); got != 0 {
		t.Fatalf("downstream auth_key_id = %d, want 0 for an unencrypted reply", got)
	}
	ctor := binary.LittleEndian.Uint32(body[20:24])
	if ctor != 0x05162463 {
		t.Fatalf("downstream constructor = 0x%08x, want resPQ 0x05162463", ctor)
	}
	if len(body) >= 40 && string(body[24:40]) != string(nonce) {
		t.Fatal("resPQ echoed a different nonce")
	}
	t.Logf("Telegram DC%d answered resPQ through the WEB carrier (%d bytes downstream)", l.dc, len(down))
}

func webTicketFromPage(t *testing.T, page string) string {
	t.Helper()
	const marker = "'" + webSubprotoPrefix
	i := strings.Index(page, marker)
	if i < 0 {
		t.Fatal("bridge page carried no carrier ticket")
	}
	rest := page[i+len(marker):]
	j := strings.IndexByte(rest, '\'')
	if j <= 0 {
		t.Fatal("bridge page ticket is not terminated")
	}
	return rest[:j]
}

type liveCarrier struct {
	t    *testing.T
	conn *websocket.Conn
}

func (c *liveCarrier) write(frame []byte) {
	c.t.Helper()
	_ = c.conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	if err := c.conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		c.t.Fatalf("carrier write: %v", err)
	}
}

func (c *liveCarrier) read() []webFrame {
	c.t.Helper()
	_ = c.conn.SetReadDeadline(time.Now().Add(25 * time.Second))
	typ, msg, err := c.conn.ReadMessage()
	if err != nil {
		c.t.Fatalf("carrier read: %v", err)
	}
	if typ != websocket.BinaryMessage {
		c.t.Fatalf("carrier sent message type %d", typ)
	}
	frames, err := parseWebFrames(msg)
	if err != nil {
		c.t.Fatalf("carrier sent an unparseable message: %v", err)
	}
	return frames
}

func buildClientObfuscation(t *testing.T, secret []byte, dc int, protoTag uint32) ([]byte, interface {
	XORKeyStream(dst, src []byte)
}, interface {
	XORKeyStream(dst, src []byte)
}) {
	t.Helper()
	key := secret
	if len(key) == 17 {
		key = key[1:]
	}

	frame := generateFrame(dc, protoTag)

	encStream, err := newAESCTR(deriveKey(frame[8:40], key), append([]byte(nil), frame[40:56]...))
	if err != nil {
		t.Fatalf("client encrypt stream: %v", err)
	}
	reversed := make([]byte, 48)
	for i := 0; i < 48; i++ {
		reversed[i] = frame[55-i]
	}
	decStream, err := newAESCTR(deriveKey(reversed[:32], key), append([]byte(nil), reversed[32:48]...))
	if err != nil {
		t.Fatalf("client decrypt stream: %v", err)
	}

	wire := make([]byte, len(frame))
	copy(wire, frame)
	encStream.XORKeyStream(wire, wire)
	copy(wire[0:56], frame[0:56])
	return wire, encStream, decStream
}

func reqPQMulti(nonce []byte) []byte {
	body := make([]byte, 0, 20)
	body = binary.LittleEndian.AppendUint32(body, 0xbe7e8ef1)
	return append(body, nonce...)
}

func unencryptedMTProto(body []byte) []byte {
	out := make([]byte, 0, 20+len(body))
	out = binary.LittleEndian.AppendUint64(out, 0)
	out = binary.LittleEndian.AppendUint64(out, uint64(time.Now().Unix())<<32)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(body)))
	return append(out, body...)
}

func abridgedFrame(payload []byte) []byte {
	words := len(payload) / 4
	if words < 127 {
		return append([]byte{byte(words)}, payload...)
	}
	hdr := []byte{0x7f, byte(words), byte(words >> 8), byte(words >> 16)}
	return append(hdr, payload...)
}

func stripAbridged(t *testing.T, data []byte) []byte {
	t.Helper()
	if len(data) == 0 {
		t.Fatal("empty downstream frame")
	}
	if data[0] == 0x7f {
		if len(data) < 4 {
			t.Fatal("truncated abridged length")
		}
		return data[4:]
	}
	return data[1:]
}
