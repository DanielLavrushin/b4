package utils

import (
	"crypto/tls"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

var (
	ErrURLSyntax   = errors.New("the address is not a valid URL")
	ErrURLRedirect = errors.New("the server sent a redirect that is not a valid URL")
)

func URLErrorCause(asked string, err error) error {
	if err == nil {
		return nil
	}
	var failed *url.Error
	if errors.As(err, &failed) {
		u, parseErr := url.Parse(asked)
		if failed.Op == "parse" || parseErr != nil || !ValidURLPort(u) {
			return ErrURLSyntax
		}
		err = failed.Err
	}
	var certificate *tls.CertificateVerificationError
	reason, _, quoted := strings.Cut(err.Error(), `"`)
	switch {
	case !quoted, errors.As(err, &certificate):
		return err
	case strings.HasPrefix(reason, "failed to parse Location header"):
		return ErrURLRedirect
	}
	return errors.New(reason + `"..."`)
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
