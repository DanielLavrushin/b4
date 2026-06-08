package detector

import (
	"context"
	"fmt"
	"net/http"

	"github.com/daniellavrushin/b4/dns"
)

func resolveDoHWire(ctx context.Context, client *http.Client, serverURL, domain string) (string, error) {
	query := dns.BuildAQuery(domain, 0)

	body, err := dns.ResolveDoH(ctx, client, serverURL, query)
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
