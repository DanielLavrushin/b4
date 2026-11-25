package dns

import (
	"errors"
	"net/url"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/utils"
)

func withoutResolverPath(serverURL string, err error) error {
	cause := utils.URLErrorCause(serverURL, err)
	var failed *url.Error
	if !errors.As(err, &failed) {
		return cause
	}
	label := config.UpstreamLabel(serverURL)
	if host := utils.RedirectedHost(serverURL, failed); host != "" {
		label += " redirected to " + host
	}
	return &url.Error{Op: failed.Op, URL: label, Err: cause}
}
