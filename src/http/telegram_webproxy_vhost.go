package http

import (
	stdhttp "net/http"

	"github.com/daniellavrushin/b4/http/handler"
)

func telegramWebProxyVhost(next stdhttp.Handler) stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if relay := handler.MTProtoWebProxyServer(); relay != nil && relay.ServeWebProxy(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}
