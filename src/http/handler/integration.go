package handler

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/asnprefix"
	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/netprobe"
)

const (
	ipinfoTimeout = 10 * time.Second
	ipinfoMaxBody = 1 << 20
)

var (
	ipinfoBaseURL = "https://ipinfo.io"
	ipinfoClient  = sharedIPInfoClient

	ipinfoClientOnce sync.Once
	ipinfoHTTP       *http.Client
)

func sharedIPInfoClient() *http.Client {
	ipinfoClientOnce.Do(func() {
		ipinfoHTTP = netprobe.HTTPClient(int(config.SelfDialMark), ipinfoTimeout)
	})
	return ipinfoHTTP
}

func (api *API) RegisterIntegrationApi() {
	api.mux.HandleFunc("/api/integration/ipinfo", api.getIpInfo)
}

func scrubIPInfoError(err error, secret string) error {
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		err = ue.Err
	}
	msg := err.Error()
	if secret == "" || !strings.Contains(msg, secret) && !strings.Contains(msg, url.QueryEscape(secret)) {
		return err
	}
	msg = strings.ReplaceAll(msg, url.QueryEscape(secret), "[token]")
	return errors.New(strings.ReplaceAll(msg, secret, "[token]"))
}

// @Summary Query IPInfo API for IP details
// @Description Relays the IPInfo record of one address using the token under Settings. The token never appears in the response or in the log.
// @Tags Integration
// @Produce json
// @Param ip query string true "IP address, optionally with a port"
// @Success 200 {object} object
// @Failure 400 {object} APIError "code: ip_invalid or ipinfo_token_missing"
// @Failure 502 {object} APIError "code: ipinfo_failed"
// @Security BearerAuth
// @Router /integration/ipinfo [get]
func (a *API) getIpInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	raw := r.URL.Query().Get("ip")
	addr, ok := asnprefix.ParseIP(raw)
	if !ok {
		writeAPIError(w, &APIError{Status: http.StatusBadRequest, Code: "ip_invalid", Message: fmt.Sprintf("%q is not an IP address", strings.TrimSpace(raw))})
		return
	}

	token := strings.TrimSpace(a.getCfg().System.API.IPInfoToken)
	if token == "" {
		writeAPIError(w, &APIError{Status: http.StatusBadRequest, Code: "ipinfo_token_missing", Message: "IPInfo token not configured"})
		return
	}

	ip := addr.String()
	target := ipinfoBaseURL + "/" + url.PathEscape(ip) + "?token=" + url.QueryEscape(token)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		writeAPIError(w, ErrInternal("Failed to build the IPInfo request"))
		return
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "b4")

	resp, err := ipinfoClient().Do(req)
	if err != nil {
		cause := scrubIPInfoError(err, token)
		log.Errorf("IPInfo lookup of %s failed: %v", ip, cause)
		writeAPIError(w, &APIError{Status: http.StatusBadGateway, Code: "ipinfo_failed", Message: fmt.Sprintf("IPInfo lookup failed: %v", cause)})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, ipinfoMaxBody))
		msg := fmt.Sprintf("IPInfo answered %d", resp.StatusCode)
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			msg += ": the token was rejected"
		}
		log.Warnf("IPInfo lookup of %s: %s", ip, msg)
		writeAPIError(w, &APIError{Status: http.StatusBadGateway, Code: "ipinfo_failed", Message: msg})
		return
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, ipinfoMaxBody+1))
	if err != nil {
		cause := scrubIPInfoError(err, token)
		log.Errorf("IPInfo lookup of %s: reading the answer failed: %v", ip, cause)
		writeAPIError(w, &APIError{Status: http.StatusBadGateway, Code: "ipinfo_failed", Message: fmt.Sprintf("IPInfo lookup failed: %v", cause)})
		return
	}
	if len(body) > ipinfoMaxBody {
		writeAPIError(w, &APIError{Status: http.StatusBadGateway, Code: "ipinfo_failed", Message: "IPInfo answer is too large"})
		return
	}

	setJsonHeader(w)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
