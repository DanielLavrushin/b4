package handler

import (
	"sync/atomic"

	"github.com/daniellavrushin/b4/config"
)

var (
	runningWebListener atomic.Pointer[config.WebListener]
	loginDroppedFunc   func()
)

func SetLoginDroppedFunc(fn func()) {
	loginDroppedFunc = fn
}

func SetRunningWebListener(web config.WebListener) {
	runningWebListener.Store(&web)
}

func currentWebListener() config.WebListener {
	if web := runningWebListener.Load(); web != nil {
		return *web
	}
	return config.WebListener{}
}

var exposureRules = []struct {
	unsafe func(*config.Config) bool
	field  FieldError
}{
	{
		unsafe: func(c *config.Config) bool {
			ws := c.System.WebServer
			return ws.Expose && (ws.Username == "" || ws.Password == "")
		},
		field: FieldError{
			Path:    "system.web_server.expose",
			Code:    "expose_requires_auth",
			Message: "exposing the web interface to the internet needs a username and password",
		},
	},
	{
		unsafe: func(c *config.Config) bool {
			s5 := c.System.Socks5
			return s5.Expose && s5.Enabled && s5.Username == "" && !mcpHasSourceEntries(s5.AllowedSources)
		},
		field: FieldError{
			Path:    "system.socks5.expose",
			Code:    "expose_open_relay",
			Message: "exposing SOCKS5 to the internet needs a username and password or an allowed sources list",
		},
	},
	{
		unsafe: func(c *config.Config) bool {
			s5 := c.System.Socks5
			return s5.Expose && s5.Enabled && c.WebInterfaceUnprotectedWith(currentWebListener())
		},
		field: FieldError{
			Path:    "system.socks5.expose",
			Code:    "expose_requires_web_auth",
			Message: "exposing SOCKS5 to the internet needs a username and password on the web interface, which SOCKS5 clients can otherwise reach through the proxy",
		},
	},
}

func exposureRefusal(oldCfg, newCfg *config.Config) error {
	for _, rule := range exposureRules {
		if rule.unsafe(newCfg) && !rule.unsafe(oldCfg) {
			return ErrValidation(rule.field.Message, rule.field)
		}
	}
	return nil
}
