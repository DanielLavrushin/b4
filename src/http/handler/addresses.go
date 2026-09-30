package handler

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/detector"
	"github.com/daniellavrushin/b4/netprobe"
	"github.com/daniellavrushin/b4/tables"
)

type HostAddress struct {
	Iface string `json:"iface"`
	IP    string `json:"ip"`
	Scope string `json:"scope"`
}

type HostAddressesResponse struct {
	Success     bool                `json:"success"`
	WANv4       *HostAddress        `json:"wan_v4,omitempty"`
	WANv6       *HostAddress        `json:"wan_v6,omitempty"`
	LAN         []HostAddress       `json:"lan"`
	PublicV4    string              `json:"public_v4,omitempty"`
	PublicError string              `json:"public_error,omitempty"`
	Exposure    tables.ExposeStatus `json:"exposure"`
}

type hostIface struct {
	name     string
	up       bool
	loopback bool
	addrs    []netip.Addr
}

const (
	ifaFlagTemporary  = 0x01
	ifaFlagDadFailed  = 0x08
	ifaFlagDeprecated = 0x20
	ifaFlagTentative  = 0x40
	routeFlagUp       = 0x0001
	routeFlagReject   = 0x0200
	ipv6AnyRouteHex   = "00000000000000000000000000000000"
)

var (
	procNetRouteFile     = "/proc/net/route"
	procNetIPv6RouteFile = "/proc/net/ipv6_route"
	procNetIfInet6File   = "/proc/net/if_inet6"
	readHostFile         = os.ReadFile
	hostInterfaces       = listHostInterfaces
	publicIPv4Lookup     = lookupPublicIPv4

	publicIPv4TTL      = 5 * time.Minute
	publicIPv4Mu       sync.Mutex
	publicIPv4Cached   string
	publicIPv4CachedAt time.Time

	cgnatPrefix          = netip.MustParsePrefix("100.64.0.0/10")
	lanIfaceSkipPrefixes = []string{"docker", "veth", "virbr"}
)

// @Summary Host addresses and port exposure
// @Description The default-route (WAN) IPv4 and IPv6 addresses with their scope (public, private, cgnat, ula, other), the private LAN addresses, and which listener ports b4 opens to the internet and where. public=1 also asks the detector's IP lookup services for the public IPv4, cached for five minutes.
// @Tags System
// @Produce json
// @Param public query string false "1 to look up the public IPv4"
// @Success 200 {object} HostAddressesResponse
// @Security BearerAuth
// @Router /system/addresses [get]
func (api *API) handleHostAddresses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	skip, wanOverride := "", ""
	if globalTUNEngine != nil {
		di := globalTUNEngine.DiagInfo()
		skip, wanOverride = di.DeviceName, di.OutInterface
	}
	resp := collectHostAddresses(hostInterfaces(), skip, wanOverride)
	if r.URL.Query().Get("public") == "1" {
		ip, err := cachedPublicIPv4(r.Context(), uint(api.getCfg().MainInjectedMark()))
		if err != nil {
			resp.PublicError = err.Error()
		}
		resp.PublicV4 = ip
	}
	resp.Exposure = tables.ExposureStatus()
	resp.Success = true
	sendResponse(w, resp)
}

func collectHostAddresses(ifaces []hostIface, skip, wanOverride string) HostAddressesResponse {
	resp := HostAddressesResponse{LAN: []HostAddress{}}

	wan4 := wanOverride
	if wan4 == "" {
		wan4 = defaultRouteIfaceV4(skip)
	}
	wan6 := defaultRouteIfaceV6(skip)
	flags := ipv6AddressFlags()

	for _, ifc := range ifaces {
		if ifc.name == wan4 && resp.WANv4 == nil {
			if ip, ok := pickWANv4(ifc.addrs); ok {
				resp.WANv4 = &HostAddress{Iface: ifc.name, IP: ip.String(), Scope: addressScope(ip)}
			}
		}
		if ifc.name == wan6 && resp.WANv6 == nil {
			if ip, ok := pickWANv6(ifc.addrs, flags); ok {
				resp.WANv6 = &HostAddress{Iface: ifc.name, IP: ip.String(), Scope: addressScope(ip)}
			}
		}
	}

	for _, ifc := range ifaces {
		if !ifc.up || ifc.loopback || ifc.name == wan4 || ifc.name == skip || lanIfaceSkipped(ifc.name) {
			continue
		}
		for _, ip := range ifc.addrs {
			if ip.Is4() && addressScope(ip) == "private" {
				resp.LAN = append(resp.LAN, HostAddress{Iface: ifc.name, IP: ip.String(), Scope: "private"})
			}
		}
	}
	return resp
}

func lanIfaceSkipped(name string) bool {
	for _, p := range lanIfaceSkipPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func addressScope(ip netip.Addr) string {
	ip = ip.Unmap()
	switch {
	case ip.Is4() && cgnatPrefix.Contains(ip):
		return "cgnat"
	case ip.IsPrivate() && ip.Is4():
		return "private"
	case ip.IsPrivate():
		return "ula"
	case netprobe.PublicAddress(net.IP(ip.AsSlice())):
		return "public"
	}
	return "other"
}

func pickWANv4(addrs []netip.Addr) (netip.Addr, bool) {
	for _, ip := range addrs {
		if ip.Is4() && !ip.IsLinkLocalUnicast() && !ip.IsLoopback() {
			return ip, true
		}
	}
	return netip.Addr{}, false
}

func pickWANv6(addrs []netip.Addr, flags map[netip.Addr]uint64) (netip.Addr, bool) {
	var ula netip.Addr
	for _, ip := range addrs {
		if !ip.Is6() || ip.Is4In6() || !ip.IsGlobalUnicast() {
			continue
		}
		if f, known := flags[ip]; known && f&(ifaFlagTemporary|ifaFlagDeprecated|ifaFlagTentative|ifaFlagDadFailed) != 0 {
			continue
		}
		if ip.IsPrivate() {
			if !ula.IsValid() {
				ula = ip
			}
			continue
		}
		return ip, true
	}
	return ula, ula.IsValid()
}

func defaultRouteIfaceV4(skip string) string {
	raw, err := readHostFile(procNetRouteFile)
	if err != nil {
		return ""
	}
	return parseDefaultRouteV4(string(raw), skip)
}

func parseDefaultRouteV4(data, skip string) string {
	best, bestMetric := "", uint64(0)
	for _, line := range strings.Split(data, "\n") {
		f := strings.Fields(line)
		if len(f) < 8 || f[1] != "00000000" || f[7] != "00000000" || f[0] == skip {
			continue
		}
		flags, err1 := strconv.ParseUint(f[3], 16, 32)
		metric, err2 := strconv.ParseUint(f[6], 10, 32)
		if err1 != nil || err2 != nil || flags&routeFlagUp == 0 || flags&routeFlagReject != 0 {
			continue
		}
		if best == "" || metric < bestMetric {
			best, bestMetric = f[0], metric
		}
	}
	return best
}

func defaultRouteIfaceV6(skip string) string {
	raw, err := readHostFile(procNetIPv6RouteFile)
	if err != nil {
		return ""
	}
	return parseDefaultRouteV6(string(raw), skip)
}

func parseDefaultRouteV6(data, skip string) string {
	best, bestMetric := "", uint64(0)
	for _, line := range strings.Split(data, "\n") {
		f := strings.Fields(line)
		if len(f) < 10 || f[0] != ipv6AnyRouteHex || f[1] != "00" || f[9] == "lo" || f[9] == skip {
			continue
		}
		flags, err1 := strconv.ParseUint(f[8], 16, 32)
		metric, err2 := strconv.ParseUint(f[5], 16, 32)
		if err1 != nil || err2 != nil || flags&routeFlagUp == 0 || flags&routeFlagReject != 0 {
			continue
		}
		if best == "" || metric < bestMetric {
			best, bestMetric = f[9], metric
		}
	}
	return best
}

func ipv6AddressFlags() map[netip.Addr]uint64 {
	raw, err := readHostFile(procNetIfInet6File)
	if err != nil {
		return nil
	}
	return parseIfInet6(string(raw))
}

func parseIfInet6(data string) map[netip.Addr]uint64 {
	out := make(map[netip.Addr]uint64)
	for _, line := range strings.Split(data, "\n") {
		f := strings.Fields(line)
		if len(f) < 6 || len(f[0]) != 32 {
			continue
		}
		var b [16]byte
		ok := true
		for i := 0; i < 16; i++ {
			v, err := strconv.ParseUint(f[0][2*i:2*i+2], 16, 8)
			if err != nil {
				ok = false
				break
			}
			b[i] = byte(v)
		}
		flags, err := strconv.ParseUint(f[4], 16, 32)
		if !ok || err != nil {
			continue
		}
		out[netip.AddrFrom16(b)] = flags
	}
	return out
}

func listHostInterfaces() []hostIface {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	out := make([]hostIface, 0, len(ifaces))
	for _, ifc := range ifaces {
		h := hostIface{
			name:     ifc.Name,
			up:       ifc.Flags&net.FlagUp != 0,
			loopback: ifc.Flags&net.FlagLoopback != 0,
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if ip, ok := netip.AddrFromSlice(ipn.IP); ok {
				h.addrs = append(h.addrs, ip.Unmap())
			}
		}
		out = append(out, h)
	}
	return out
}

func cachedPublicIPv4(ctx context.Context, mark uint) (string, error) {
	publicIPv4Mu.Lock()
	defer publicIPv4Mu.Unlock()
	if publicIPv4Cached != "" && time.Since(publicIPv4CachedAt) < publicIPv4TTL {
		return publicIPv4Cached, nil
	}
	ip, err := publicIPv4Lookup(ctx, mark)
	if err != nil {
		return "", err
	}
	publicIPv4Cached, publicIPv4CachedAt = ip, time.Now()
	return ip, nil
}

func lookupPublicIPv4(ctx context.Context, mark uint) (string, error) {
	urls := detector.Lists().IPLookupURLs
	if len(urls) == 0 {
		return "", errors.New("no IP lookup service is configured")
	}
	client := netprobe.HTTPClient(int(mark), 4*time.Second)
	defer client.CloseIdleConnections()
	for _, u := range urls {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "curl/8.0")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			continue
		}
		if ip, err := netip.ParseAddr(strings.TrimSpace(string(body))); err == nil && ip.Is4() {
			return ip.String(), nil
		}
	}
	return "", errors.New("none of the IP lookup services answered")
}
