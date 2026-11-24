package utils

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

var (
	ErrURLSyntax   = errors.New("the address is not a valid URL")
	ErrURLRedirect = errors.New("the server sent a redirect that is not a valid URL")
	ErrURLReply    = errors.New("the server sent a reply that is not valid HTTP")
)

func URLErrorCause(asked string, err error) error {
	var failed *url.Error
	if !errors.As(err, &failed) {
		return err
	}
	u, parseErr := url.Parse(asked)
	if failed.Op == "parse" || parseErr != nil || !ValidURLPort(u) {
		return ErrURLSyntax
	}
	reason := failed.Err.Error()
	switch {
	case strings.HasPrefix(reason, "failed to parse Location header"):
		return ErrURLRedirect
	case quotesRequest(u, reason):
		return ErrURLReply
	}
	return failed.Err
}

func quotesRequest(asked *url.URL, reason string) bool {
	_, quoted, _ := strings.Cut(reason, `"`)
	for _, part := range []string{asked.Path, asked.EscapedPath(), asked.RawQuery} {
		if len(part) > 1 && strings.Contains(quoted, part) {
			return true
		}
	}
	return false
}

func RedirectedHost(asked string, err error) string {
	var failed *url.Error
	if !errors.As(err, &failed) {
		return ""
	}
	from, errFrom := url.Parse(asked)
	to, errTo := url.Parse(failed.URL)
	if errFrom != nil || errTo != nil || to.Host == from.Host || !ValidURLPort(to) {
		return ""
	}
	return to.Host
}

func ValidURLPort(u *url.URL) bool {
	if u.Port() == "" {
		return true
	}
	_, err := strconv.ParseUint(u.Port(), 10, 16)
	return err == nil
}
