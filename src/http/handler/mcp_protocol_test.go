package handler

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpTestStatelessVersion = "2026-07-28"

const mcpTestMeta = `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}`

func mcpStatelessRequest(t *testing.T, ctx context.Context, url, method, name, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", mcpTestStatelessVersion)
	req.Header.Set("Mcp-Method", method)
	if name != "" {
		req.Header.Set("Mcp-Name", name)
	}
	return req
}

func mcpStatelessPost(t *testing.T, url, method, name, body string) (*http.Response, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := http.DefaultClient.Do(mcpStatelessRequest(t, ctx, url, method, name, body))
	if err != nil {
		t.Fatalf("post %s: %v", method, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp, string(raw)
}

func TestMCPAcceptsNotificationsWithoutAProtocolVersionInMeta(t *testing.T) {
	srv := newMCPTestServer(t, mcpTestCfg())
	url := srv.URL + mcpEndpoint

	cases := []struct {
		name, method, body string
	}{
		{"roots changed, empty params", "notifications/roots/list_changed", `{"jsonrpc":"2.0","method":"notifications/roots/list_changed","params":{}}`},
		{"roots changed, no params", "notifications/roots/list_changed", `{"jsonrpc":"2.0","method":"notifications/roots/list_changed"}`},
		{"roots changed, null params", "notifications/roots/list_changed", `{"jsonrpc":"2.0","method":"notifications/roots/list_changed","params":null}`},
		{"roots changed, meta without version", "notifications/roots/list_changed", `{"jsonrpc":"2.0","method":"notifications/roots/list_changed","params":{"_meta":{"progressToken":7}}}`},
		{"cancelled", "notifications/cancelled", `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":3,"reason":"client exiting"}}`},
		{"initialized", "notifications/initialized", `{"jsonrpc":"2.0","method":"notifications/initialized"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := mcpStatelessPost(t, url, tc.method, "", tc.body)
			if resp.StatusCode != http.StatusAccepted {
				t.Fatalf("status = %d, want 202; body %q", resp.StatusCode, body)
			}
		})
	}

	resp, body := mcpStatelessPost(t, url, "tools/list", "", `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{`+mcpTestMeta+`}}`)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"b4_status"`) {
		t.Fatalf("tools/list after the notifications: status %d, body %q", resp.StatusCode, body)
	}
}

func TestMCPLeavesProtocolVersionErrorsOfRequestsAndStatedVersionsToTheSDK(t *testing.T) {
	srv := newMCPTestServer(t, mcpTestCfg())
	url := srv.URL + mcpEndpoint

	resp, body := mcpStatelessPost(t, url, "tools/list", "", `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(body, "protocolVersion") {
		t.Fatalf("a request without _meta: status %d, body %q; want go-sdk's 400", resp.StatusCode, body)
	}

	resp, body = mcpStatelessPost(t, url, "notifications/cancelled", "", `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":3,"_meta":{"io.modelcontextprotocol/protocolVersion":"2025-11-25"}}}`)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(body, "does not match") {
		t.Fatalf("a notification stating another version: status %d, body %q; want go-sdk's mismatch 400", resp.StatusCode, body)
	}

	huge := `{"jsonrpc":"2.0","method":"notifications/roots/list_changed","params":{"pad":"` + strings.Repeat("a", mcpMaxBodyBytes) + `"}}`
	resp, body = mcpStatelessPost(t, url, "notifications/roots/list_changed", "", huge)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("an oversized notification: status %d, body %q; want go-sdk's 413", resp.StatusCode, body)
	}
}

func TestMCPGateKeepsABodyReadErrorForTheSDK(t *testing.T) {
	srv := newMCPTestServer(t, mcpTestCfg())
	host := strings.TrimPrefix(srv.URL, "http://")
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{` + mcpTestMeta + `}}`

	for _, version := range []string{"2025-11-25", mcpTestStatelessVersion} {
		t.Run(version, func(t *testing.T) {
			conn, err := net.Dial("tcp", host)
			if err != nil {
				t.Fatalf("dial: %v", err)
			}
			defer conn.Close()
			fmt.Fprintf(conn, "POST %s HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\nAccept: application/json, text/event-stream\r\nMcp-Protocol-Version: %s\r\nMcp-Method: tools/list\r\nContent-Length: %d\r\n\r\n%s",
				mcpEndpoint, host, version, len(body)+10, body)
			if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
				t.Fatalf("close write: %v", err)
			}
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
			if err != nil {
				t.Fatalf("read response: %v", err)
			}
			defer resp.Body.Close()
			raw, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(raw), "failed to read body") {
				t.Fatalf("a body cut short of its Content-Length: status %d, body %q; want go-sdk's 400", resp.StatusCode, raw)
			}
		})
	}
}

func TestMCPClosingTheStreamCancelsTheToolCall(t *testing.T) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	started := make(chan struct{})
	cancelled := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	mcp.AddTool(srv, &mcp.Tool{Name: "wait"}, func(ctx context.Context, _ *mcp.CallToolRequest, _ mcpEmpty) (*mcp.CallToolResult, mcpEmpty, error) {
		close(started)
		select {
		case <-ctx.Done():
			close(cancelled)
		case <-release:
		}
		return nil, mcpEmpty{}, ctx.Err()
	})

	api := &API{cfgPtr: testCfgPtr(mcpTestCfg())}
	ts := httptest.NewServer(api.mcpHandler(func(*http.Request) *mcp.Server { return srv }))
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := mcpStatelessRequest(t, ctx, ts.URL+mcpEndpoint, "tools/call", "wait",
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"wait","arguments":{},`+mcpTestMeta+`}}`)
	go func() {
		if resp, err := http.DefaultClient.Do(req); err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the tool call never started")
	}
	cancel()
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("closing the response stream did not cancel the tool call")
	}
}

func TestMCPEventStreamsAskProxiesNotToBuffer(t *testing.T) {
	srv := newMCPTestServer(t, mcpTestCfg())
	url := srv.URL + mcpEndpoint

	resp, _ := mcpStatelessPost(t, url, "tools/list", "", `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{`+mcpTestMeta+`}}`)
	if got := resp.Header.Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("tools/list: X-Accel-Buffering = %q, want no", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	listen, err := http.DefaultClient.Do(mcpStatelessRequest(t, ctx, url, "subscriptions/listen", "",
		`{"jsonrpc":"2.0","id":2,"method":"subscriptions/listen","params":{"notifications":{"toolsListChanged":true},`+mcpTestMeta+`}}`))
	if err != nil {
		t.Fatalf("subscriptions/listen: %v", err)
	}
	defer listen.Body.Close()
	if listen.StatusCode != http.StatusOK {
		t.Fatalf("subscriptions/listen: status %d", listen.StatusCode)
	}
	if got := listen.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("subscriptions/listen: Content-Type = %q, want an event stream", got)
	}
	if got := listen.Header.Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("subscriptions/listen: X-Accel-Buffering = %q, want no", got)
	}

	acked := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(listen.Body)
		for sc.Scan() {
			if line := sc.Text(); strings.HasPrefix(line, "data:") {
				acked <- line
				return
			}
		}
		close(acked)
	}()
	select {
	case line, ok := <-acked:
		if !ok || !strings.Contains(line, "notifications/subscriptions/acknowledged") {
			t.Fatalf("first event on the listen stream = %q, want the acknowledgment", line)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the listen stream sent no acknowledgment")
	}
}
