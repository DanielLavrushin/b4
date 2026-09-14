package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/daniellavrushin/b4/hubwire"
)

const MaxMirrorURLLength = 200

func privateHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
}

func ValidateMirrorURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("mirror url is empty")
	}
	if len(raw) > MaxMirrorURLLength {
		return "", fmt.Errorf("mirror url exceeds %d characters", MaxMirrorURLLength)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("mirror url does not parse: %w", err)
	}
	if u.User != nil {
		return "", errors.New("mirror url must not carry credentials")
	}
	if u.RawQuery != "" || u.ForceQuery {
		return "", errors.New("mirror url must not carry a query")
	}
	if u.Fragment != "" || u.RawFragment != "" {
		return "", errors.New("mirror url must not carry a fragment")
	}
	host := u.Hostname()
	if host == "" {
		return "", errors.New("mirror url has no host")
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !privateHost(host) {
			return "", errors.New("http mirrors are accepted only on private or loopback hosts")
		}
	default:
		return "", errors.New("mirror url must use https")
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u.String(), nil
}

func (s *Service) mirror(ctx context.Context, entry record) Response {
	var body hubwire.MirrorBody
	if err := json.Unmarshal(entry.rec.Body, &body); err != nil {
		return fail(http.StatusBadRequest, CodeBadRecord, "mirror body does not decode: "+err.Error())
	}
	base, err := ValidateMirrorURL(body.URL)
	if err != nil {
		return fail(http.StatusBadRequest, CodeBadMirrorURL, err.Error())
	}
	m, err := s.Store.AnnounceMirror(ctx, base, entry.keyHMAC, entry.now)
	if err != nil {
		return internalError(err)
	}
	if err := s.remember(ctx, entry, "", 0); err != nil {
		return internalError(err)
	}
	return Response{Status: http.StatusAccepted, Body: map[string]interface{}{"id": entry.id, "kind": entry.rec.Kind, "status": m.Status}}
}
