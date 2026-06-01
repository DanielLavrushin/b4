package detector

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/daniellavrushin/b4/dns"
)

const dohContentType = "application/dns-message"

var errDoHMethodNotAllowed = errors.New("doh method not allowed")

func resolveDoHWire(ctx context.Context, client *http.Client, serverURL, domain string) (string, error) {
	query := dns.BuildAQuery(domain, 0)

	body, err := dohWirePOST(ctx, client, serverURL, query)
	if errors.Is(err, errDoHMethodNotAllowed) {
		body, err = dohWireGET(ctx, client, serverURL, query)
	}
	if err != nil {
		return "", err
	}

	ips := dns.ParseResponseIPs(body)
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			return v4.String(), nil
		}
	}
	return "", fmt.Errorf("no A record")
}

func dohWirePOST(ctx context.Context, client *http.Client, serverURL string, query []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL, bytes.NewReader(query))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", dohContentType)
	req.Header.Set("Content-Type", dohContentType)
	return dohWireDo(client, req)
}

func dohWireGET(ctx context.Context, client *http.Client, serverURL string, query []byte) ([]byte, error) {
	enc := base64.RawURLEncoding.EncodeToString(query)
	sep := "?"
	if strings.ContainsRune(serverURL, '?') {
		sep = "&"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serverURL+sep+"dns="+enc, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", dohContentType)
	return dohWireDo(client, req)
}

func dohWireDo(client *http.Client, req *http.Request) ([]byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotImplemented {
		io.Copy(io.Discard, resp.Body)
		return nil, errDoHMethodNotAllowed
	}
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("doh status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 65536))
}
