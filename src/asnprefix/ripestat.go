package asnprefix

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/netprobe"
)

const (
	fetchTimeout = 30 * time.Second
	maxBodyBytes = 4 << 20
	userAgent    = "b4"
	sourceApp    = "b4"
)

var (
	ErrInvalidASN = errors.New("not a public AS number")
	ErrInvalidIP  = errors.New("not an IP address")
	ErrNoPrefixes = errors.New("announces no public prefixes")
)

var (
	baseURL    = "https://stat.ripe.net"
	httpClient = sharedClient
	nowFn      = time.Now

	clientOnce sync.Once
	client     *http.Client
)

func sharedClient() *http.Client {
	clientOnce.Do(func() {
		client = netprobe.HTTPClient(int(config.SelfDialMark), fetchTimeout)
	})
	return client
}

type ripeResponse[T any] struct {
	Status     string          `json:"status"`
	StatusCode int             `json:"status_code"`
	Messages   json.RawMessage `json:"messages"`
	Data       T               `json:"data"`
}

type originatingPrefixes struct {
	Originating []string `json:"originating"`
}

type risPrefixesData struct {
	Resource string `json:"resource"`
	Prefixes struct {
		V4 originatingPrefixes `json:"v4"`
		V6 originatingPrefixes `json:"v6"`
	} `json:"prefixes"`
}

type asOverviewData struct {
	Resource  string `json:"resource"`
	Holder    string `json:"holder"`
	Announced bool   `json:"announced"`
}

type networkInfoData struct {
	ASNs   []string `json:"asns"`
	Prefix string   `json:"prefix"`
}

func (r *ripeResponse[T]) problem() string {
	var msgs [][]string
	if len(r.Messages) == 0 || json.Unmarshal(r.Messages, &msgs) != nil {
		return ""
	}
	var serious, all []string
	for _, m := range msgs {
		if len(m) < 2 {
			continue
		}
		text := strings.TrimSpace(m[1])
		if text == "" {
			continue
		}
		all = append(all, text)
		if m[0] == "error" || m[0] == "warning" {
			serious = append(serious, text)
		}
	}
	if len(serious) > 0 {
		return strings.Join(serious, "; ")
	}
	return strings.Join(all, "; ")
}

func unwrapURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		return ue.Err
	}
	return err
}

func query[T any](ctx context.Context, call string, params url.Values) (T, error) {
	var zero T
	params.Set("sourceapp", sourceApp)
	target := baseURL + "/data/" + call + "/data.json?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return zero, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient().Do(req)
	if err != nil {
		return zero, fmt.Errorf("RIPEstat %s: %w", call, unwrapURLError(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return zero, fmt.Errorf("RIPEstat %s: reading the response: %w", call, unwrapURLError(err))
	}
	if len(body) > maxBodyBytes {
		return zero, fmt.Errorf("RIPEstat %s: the response is larger than %d MiB", call, maxBodyBytes>>20)
	}
	var out ripeResponse[T]
	decodeErr := json.Unmarshal(body, &out)
	if resp.StatusCode != http.StatusOK {
		if decodeErr == nil {
			if msg := out.problem(); msg != "" {
				return zero, fmt.Errorf("RIPEstat %s answered %d: %s", call, resp.StatusCode, msg)
			}
		}
		return zero, fmt.Errorf("RIPEstat %s answered %s", call, resp.Status)
	}
	if decodeErr != nil {
		return zero, fmt.Errorf("RIPEstat %s: unreadable response: %v", call, decodeErr)
	}
	if out.Status != "ok" {
		if msg := out.problem(); msg != "" {
			return zero, fmt.Errorf("RIPEstat %s: status %q: %s", call, out.Status, msg)
		}
		return zero, fmt.Errorf("RIPEstat %s: status %q", call, out.Status)
	}
	return out.Data, nil
}

func Fetch(ctx context.Context, id string) (*config.AsnInfo, error) {
	norm, ok := config.NormalizeASN(id)
	if !ok {
		return nil, fmt.Errorf("%q: %w", strings.TrimSpace(id), ErrInvalidASN)
	}
	data, err := query[risPrefixesData](ctx, "ris-prefixes", url.Values{
		"resource":      {"AS" + norm},
		"list_prefixes": {"true"},
		"types":         {"o"},
		"noise":         {"filter"},
	})
	if err != nil {
		return nil, err
	}
	if res := strings.TrimSpace(data.Resource); res != "" {
		if got, ok := config.NormalizeASN(res); !ok || got != norm {
			return nil, fmt.Errorf("RIPEstat ris-prefixes answered for %q instead of AS%s", res, norm)
		}
	}
	raw := make([]string, 0, len(data.Prefixes.V4.Originating)+len(data.Prefixes.V6.Originating))
	raw = append(raw, data.Prefixes.V4.Originating...)
	raw = append(raw, data.Prefixes.V6.Originating...)
	prefixes := config.SanitizeASNPrefixes(raw)
	if len(prefixes) == 0 {
		return nil, fmt.Errorf("AS%s %w", norm, ErrNoPrefixes)
	}
	name, err := holderName(ctx, norm)
	if err != nil {
		log.Debugf("ASN AS%s: holder name unavailable: %v", norm, err)
	}
	return &config.AsnInfo{
		ID:        norm,
		Name:      name,
		Prefixes:  prefixes,
		UpdatedAt: nowFn().Unix(),
		Source:    config.AsnSourceRIPEstat,
	}, nil
}

func holderName(ctx context.Context, id string) (string, error) {
	data, err := query[asOverviewData](ctx, "as-overview", url.Values{"resource": {"AS" + id}})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(data.Holder), nil
}

type Lookup struct {
	IP     string      `json:"ip"`
	Prefix string      `json:"prefix"`
	ASNs   []LookupASN `json:"asns"`
}

type LookupASN struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Cached bool   `json:"cached"`
}

func ParseIP(raw string) (netip.Addr, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return netip.Addr{}, false
	}
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return ap.Addr().WithZone("").Unmap(), true
	}
	addr, err := netip.ParseAddr(strings.TrimSuffix(strings.TrimPrefix(s, "["), "]"))
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.WithZone("").Unmap(), true
}

func IsPublicAddr(addr netip.Addr) bool {
	return addr.IsValid() && !config.IsBogonPrefix(netip.PrefixFrom(addr, addr.BitLen()))
}

func LookupIP(ctx context.Context, ip string) (*Lookup, error) {
	addr, ok := ParseIP(ip)
	if !ok {
		return nil, fmt.Errorf("%q: %w", strings.TrimSpace(ip), ErrInvalidIP)
	}
	out := &Lookup{IP: addr.String(), ASNs: []LookupASN{}}
	if !IsPublicAddr(addr) {
		return out, nil
	}
	data, err := query[networkInfoData](ctx, "network-info", url.Values{"resource": {addr.String()}})
	if err != nil {
		return nil, err
	}
	if p, err := netip.ParsePrefix(strings.TrimSpace(data.Prefix)); err == nil {
		out.Prefix = p.Masked().String()
	}
	seen := make(map[string]bool, len(data.ASNs))
	for _, raw := range data.ASNs {
		id, ok := config.NormalizeASN(raw)
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		entry := LookupASN{ID: id}
		if info := config.Asns().Get(id); info != nil {
			entry.Name = info.Name
			entry.Cached = len(info.Prefixes) > 0
		}
		if entry.Name == "" {
			if name, err := holderName(ctx, id); err == nil {
				entry.Name = name
			}
		}
		out.ASNs = append(out.ASNs, entry)
	}
	return out, nil
}
