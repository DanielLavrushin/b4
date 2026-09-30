package handler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPRevertRefusesToReopenAnExposedPort(t *testing.T) {
	cfg := writableCfg(t)
	cfg.System.Socks5.Enabled = true
	cfg.System.Socks5.Expose = true
	t.Cleanup(mcpResetHistory)
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	if res := callSetValue(t, session, ctx, "system.socks5.enabled", "false"); res.IsError {
		t.Fatalf("closing a port over MCP is allowed: %s", mcpErrorText(res))
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "b4_revert_last_change"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(mcpErrorText(res), "system.socks5.expose") {
		t.Fatalf("undoing it would open the SOCKS5 port to the internet again and must be refused like the direct write: %s", mcpErrorText(res))
	}
	if api.getCfg().System.Socks5.Enabled {
		t.Error("the refused revert must change nothing")
	}
	if len(mcpHistory) != 1 {
		t.Errorf("the refused change stays on the undo list, got %d", len(mcpHistory))
	}
}

func TestAFailedSaveLeavesTheFirewallAndListenersAlone(t *testing.T) {
	api := exposeTestAPI(t, nil)
	var touched []string
	prevShrink, prevSync, prevSocks := exposureShrinkFunc, exposureSyncFunc, globalSocks5Server
	exposureShrinkFunc = func(*config.Config) { touched = append(touched, "shrink") }
	exposureSyncFunc = func(*config.Config) { touched = append(touched, "sync") }
	globalSocks5Server = refresherFunc(func(*config.Config) { touched = append(touched, "socks5") })
	t.Cleanup(func() { exposureShrinkFunc, exposureSyncFunc, globalSocks5Server = prevShrink, prevSync, prevSocks })

	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	next := api.getCfg().Clone()
	next.ConfigPath = filepath.Join(blocker, "b4.json")
	next.System.Logging.Instaflush = !next.System.Logging.Instaflush
	if err := api.saveAndPushConfig(next); err == nil {
		t.Fatal("a save below a regular file must fail")
	}
	if len(touched) != 0 {
		t.Fatalf("a save that could not be written must not reach the firewall or the listeners, touched %v", touched)
	}
}

func TestSaveSignalsWhenLoginIsDropped(t *testing.T) {
	api := exposeTestAPI(t, func(c *config.Config) {
		c.System.WebServer.Username = "admin"
		c.System.WebServer.Password = "$2a$12$hash"
	})
	dropped := 0
	prev := loginDroppedFunc
	loginDroppedFunc = func() { dropped++ }
	t.Cleanup(func() { loginDroppedFunc = prev })

	next := api.getCfg().Clone()
	next.System.Logging.Instaflush = !next.System.Logging.Instaflush
	if err := api.saveAndPushConfig(next); err != nil {
		t.Fatalf("save: %v", err)
	}
	next = api.getCfg().Clone()
	next.System.WebServer.Username = ""
	next.System.WebServer.Password = ""
	if err := api.saveAndPushConfig(next); err != nil {
		t.Fatalf("save: %v", err)
	}
	if dropped != 1 {
		t.Fatalf("only the save that turns login off must be signalled, got %d", dropped)
	}
}

func TestSaveRefusesExposingSOCKS5WhileTheRunningWebInterfaceIsOpen(t *testing.T) {
	api := exposeTestAPI(t, func(c *config.Config) {
		c.System.Socks5.Enabled = true
		c.System.Socks5.Username = "u"
		c.System.Socks5.Password = "p"
		c.System.WebServer.Port = 0
	})
	prev := runningWebListener.Load()
	SetRunningWebListener(config.WebListener{Port: 7000})
	t.Cleanup(func() { runningWebListener.Store(prev) })

	next := api.getCfg().Clone()
	next.System.Socks5.Expose = true
	requireRefusal(t, api.saveAndPushConfig(next), "system.socks5.expose", "expose_requires_web_auth")
}
