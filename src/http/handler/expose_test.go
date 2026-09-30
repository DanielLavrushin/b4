package handler

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func exposeTestAPI(t *testing.T, mutate func(*config.Config)) *API {
	t.Helper()
	cfg := config.NewConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")
	cfg.System.Socks5.BindAddress = "127.0.0.1"
	if mutate != nil {
		mutate(&cfg)
	}
	return &API{cfgPtr: testCfgPtr(&cfg)}
}

func requireRefusal(t *testing.T, err error, path, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s to be refused", path)
	}
	var ae *APIError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *APIError, got %T (%v)", err, err)
	}
	if findAPIField(ae, path, code) == nil {
		t.Fatalf("expected field %s with code %s, got %+v", path, code, ae.Fields)
	}
}

func TestSaveRefusesExposingTheWebInterfaceWithoutCredentials(t *testing.T) {
	api := exposeTestAPI(t, nil)

	next := api.getCfg().Clone()
	next.System.WebServer.Expose = true
	requireRefusal(t, api.saveAndPushConfig(next), "system.web_server.expose", "expose_requires_auth")
	if api.getCfg().System.WebServer.Expose {
		t.Fatal("a refused save must not be applied")
	}

	next = api.getCfg().Clone()
	next.System.WebServer.Expose = true
	next.System.WebServer.Username = "admin"
	next.System.WebServer.Password = "$2a$12$hash"
	if err := api.saveAndPushConfig(next); err != nil {
		t.Fatalf("exposing an authenticated web interface must be accepted: %v", err)
	}
}

func TestSaveRefusesDroppingCredentialsWhileTheWebInterfaceIsExposed(t *testing.T) {
	api := exposeTestAPI(t, func(c *config.Config) {
		c.System.WebServer.Expose = true
		c.System.WebServer.Username = "admin"
		c.System.WebServer.Password = "$2a$12$hash"
	})

	next := api.getCfg().Clone()
	next.System.WebServer.Username = ""
	next.System.WebServer.Password = ""
	requireRefusal(t, api.saveAndPushConfig(next), "system.web_server.expose", "expose_requires_auth")
}

func TestSaveRefusesExposingAnOpenSOCKS5Relay(t *testing.T) {
	api := exposeTestAPI(t, func(c *config.Config) {
		c.System.Socks5.Enabled = true
		c.System.WebServer.Username = "admin"
		c.System.WebServer.Password = "$2a$12$hash"
	})

	next := api.getCfg().Clone()
	next.System.Socks5.Expose = true
	requireRefusal(t, api.saveAndPushConfig(next), "system.socks5.expose", "expose_open_relay")

	next = api.getCfg().Clone()
	next.System.Socks5.Expose = true
	next.System.Socks5.AllowedSources = []string{"203.0.113.0/24"}
	if err := api.saveAndPushConfig(next); err != nil {
		t.Fatalf("an allowed sources list keeps SOCKS5 from being an open relay: %v", err)
	}
}

func TestSaveKeepsAHandEditedExposureWritable(t *testing.T) {
	api := exposeTestAPI(t, func(c *config.Config) { c.System.WebServer.Expose = true })

	next := api.getCfg().Clone()
	next.System.Logging.Instaflush = !next.System.Logging.Instaflush
	if err := api.saveAndPushConfig(next); err != nil {
		t.Fatalf("a violation made by editing the file must not block unrelated saves: %v", err)
	}
}

func TestSaveHandsTheNewConfigToTheExposureSync(t *testing.T) {
	api := exposeTestAPI(t, nil)
	var synced *config.Config
	prev := exposureSyncFunc
	exposureSyncFunc = func(c *config.Config) { synced = c }
	t.Cleanup(func() { exposureSyncFunc = prev })

	next := api.getCfg().Clone()
	next.System.MTProto.Expose = true
	if err := api.saveAndPushConfig(next); err != nil {
		t.Fatalf("save: %v", err)
	}
	if synced == nil || !synced.System.MTProto.Expose {
		t.Fatal("every save must reach the firewall exposure, so a switch flipped in the UI takes effect at once")
	}
}

func TestSaveRefusesExposingSOCKS5InFrontOfAnOpenWebInterface(t *testing.T) {
	api := exposeTestAPI(t, func(c *config.Config) {
		c.System.Socks5.Enabled = true
		c.System.Socks5.Username = "u"
		c.System.Socks5.Password = "p"
	})

	next := api.getCfg().Clone()
	next.System.Socks5.Expose = true
	requireRefusal(t, api.saveAndPushConfig(next), "system.socks5.expose", "expose_requires_web_auth")

	withAuth := exposeTestAPI(t, func(c *config.Config) {
		c.System.Socks5.Enabled = true
		c.System.Socks5.Username = "u"
		c.System.Socks5.Password = "p"
		c.System.Socks5.Expose = true
		c.System.WebServer.Username = "admin"
		c.System.WebServer.Password = "$2a$12$hash"
	})
	next = withAuth.getCfg().Clone()
	next.System.WebServer.Username = ""
	next.System.WebServer.Password = ""
	requireRefusal(t, withAuth.saveAndPushConfig(next), "system.socks5.expose", "expose_requires_web_auth")
}

func TestSaveClosesDroppedPortsBeforeTheListenersRelax(t *testing.T) {
	api := exposeTestAPI(t, nil)
	var order []string
	prevShrink, prevSync := exposureShrinkFunc, exposureSyncFunc
	exposureShrinkFunc = func(*config.Config) { order = append(order, "shrink") }
	exposureSyncFunc = func(*config.Config) { order = append(order, "sync") }
	t.Cleanup(func() { exposureShrinkFunc, exposureSyncFunc = prevShrink, prevSync })
	prevSocks := globalSocks5Server
	globalSocks5Server = refresherFunc(func(*config.Config) { order = append(order, "socks5") })
	t.Cleanup(func() { globalSocks5Server = prevSocks })

	next := api.getCfg().Clone()
	next.System.Logging.Instaflush = !next.System.Logging.Instaflush
	if err := api.saveAndPushConfig(next); err != nil {
		t.Fatalf("save: %v", err)
	}
	if want := []string{"shrink", "socks5", "sync"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("closing must happen before a listener takes the relaxed settings, opening after: got %v, want %v", order, want)
	}
}

type refresherFunc func(*config.Config)

func (f refresherFunc) UpdateConfig(c *config.Config) { f(c) }
