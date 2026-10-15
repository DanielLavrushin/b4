package discovery

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"math/rand/v2"
	"net"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/netprobe"
)

const (
	echLookupTimeout = 3 * time.Second
	echLookupWorkers = 8
	presetECHStrip   = "ech-strip"
)

var (
	echRejectionRoots = netprobe.TLSRoots
	echNameservers    = netprobe.SystemNameservers
)

type echKey struct {
	configList []byte
	coverName  string
}

var lookupECHConfigList = func(ctx context.Context, mark uint, domain string) []byte {
	r := &netprobe.Resolver{Mark: int(mark), Timeout: echLookupTimeout}
	for _, server := range echNameservers() {
		id := uint16(rand.N(65535)) + 1
		resp, err := r.Exchange(ctx, server, dns.BuildQuery(domain, id, dns.TypeHTTPS))
		if err != nil || !finalAnswer(resp, id) {
			continue
		}
		return dns.ParseResponseECH(resp)
	}
	return nil
}

func finalAnswer(resp []byte, id uint16) bool {
	rcode, ok := dns.ResponseRcode(resp)
	return ok && resp[2]&0x80 != 0 && binary.BigEndian.Uint16(resp) == id &&
		(rcode == dns.RcodeNoError || rcode == dns.RcodeNXDomain)
}

func (ds *DiscoverySuite) probeECH() {
	if ds.tlsVersion == "tls12" {
		return
	}
	var domains []string
	for _, di := range ds.Domains {
		if u, err := url.Parse(di.CheckURL); err == nil && u.Scheme == "https" {
			domains = append(domains, di.Domain)
		}
	}
	lists := ds.lookupECHConfigLists(domains)
	for i, domain := range domains {
		if ds.interrupted() {
			return
		}
		cover := dns.ECHPublicName(lists[i])
		if cover == "" {
			if len(lists[i]) > 0 {
				log.DiscoveryLogf("  [%s] publishes an ECH key the probes cannot use, so they connect without ECH", domain)
			}
			continue
		}
		ds.storeECH(domain, echKey{configList: lists[i], coverName: cover})
		ds.CheckSuite.mu.Lock()
		if dr := ds.domainResults[domain]; dr != nil {
			dr.ECHCoverName = cover
		}
		ds.CheckSuite.mu.Unlock()
		log.DiscoveryLogf("  [%s] publishes an ECH key: browsers that use it name %s in the ClientHello instead of the site, and so do the probes", domain, cover)
	}
}

func (ds *DiscoverySuite) lookupECHConfigLists(domains []string) [][]byte {
	lists := make([][]byte, len(domains))
	slots := make(chan struct{}, echLookupWorkers)
	var wg sync.WaitGroup
	for i, domain := range domains {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			if !ds.interrupted() {
				lists[i] = lookupECHConfigList(ds.ctx, ds.flowMark, domain)
			}
		}()
	}
	wg.Wait()
	return lists
}

func (ds *DiscoverySuite) storeECH(host string, key echKey) {
	ds.echMu.Lock()
	defer ds.echMu.Unlock()
	if ds.ech == nil {
		ds.ech = make(map[string]echKey)
	}
	ds.ech[strings.ToLower(host)] = key
}

func (ds *DiscoverySuite) echFor(host string) (echKey, bool) {
	ds.echMu.Lock()
	defer ds.echMu.Unlock()
	key, ok := ds.ech[strings.ToLower(host)]
	return key, ok
}

func (ds *DiscoverySuite) renewECH(host string, configList []byte, coverName string) {
	host = strings.ToLower(host)
	ds.echMu.Lock()
	key, ok := ds.ech[host]
	previous := key.coverName
	if ok {
		key.configList, key.coverName = configList, coverName
		ds.ech[host] = key
	}
	ds.echMu.Unlock()
	if !ok || previous == coverName {
		return
	}

	ds.CheckSuite.mu.Lock()
	if dr := ds.domainResults[host]; dr != nil {
		dr.ECHCoverName = coverName
	}
	ds.CheckSuite.mu.Unlock()
	log.DiscoveryLogf("  [%s] its new ECH key names %s instead of %s; the probes and the sets follow it", host, coverName, previous)
}

func (ds *DiscoverySuite) withECHCoverNames(domains []string) []string {
	out := append([]string(nil), domains...)
	ds.echMu.Lock()
	defer ds.echMu.Unlock()
	for _, domain := range domains {
		if key, ok := ds.ech[strings.ToLower(domain)]; ok && !slices.Contains(out, key.coverName) {
			out = append(out, key.coverName)
		}
	}
	return out
}

func (ds *DiscoverySuite) echDialTLS(dial func(context.Context, string, string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			host = addr
		}
		plain := ds.tlsConfig()
		plain.ServerName = host
		key, ok := ds.echFor(host)
		if !ok {
			return tlsHandshake(ctx, dial, network, addr, plain)
		}

		conf := plain.Clone()
		conf.MinVersion = tls.VersionTLS13
		conf.EncryptedClientHelloConfigList = key.configList
		if roots, verify := echRejectionRoots(); verify {
			conf.RootCAs = roots
		} else {
			conf.EncryptedClientHelloRejectionVerify = func(tls.ConnectionState) error { return nil }
		}
		conn, err := tlsHandshake(ctx, dial, network, addr, conf)
		var rejected *tls.ECHRejectionError
		if !errors.As(err, &rejected) {
			return conn, err
		}
		cover := dns.ECHPublicName(rejected.RetryConfigList)
		if cover == "" {
			log.Tracef("Discovery: %s turned ECH down without a key this client can use, retrying without ECH as browsers do", host)
			return tlsHandshake(ctx, dial, network, addr, plain)
		}
		log.Tracef("Discovery: %s rotated its ECH key, retrying with the key it sent", host)
		ds.renewECH(host, rejected.RetryConfigList, cover)
		conf.EncryptedClientHelloConfigList = rejected.RetryConfigList
		return tlsHandshake(ctx, dial, network, addr, conf)
	}
}

func tlsHandshake(ctx context.Context, dial func(context.Context, string, string) (net.Conn, error), network, addr string, conf *tls.Config) (net.Conn, error) {
	raw, err := dial(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	conn := tls.Client(raw, conf)
	if err := conn.HandshakeContext(ctx); err != nil {
		raw.Close()
		return nil, err
	}
	return conn, nil
}

func (ds *DiscoverySuite) noteECHOnlyBlocks(baseline map[string]CheckResult) {
	timeout := time.Duration(ds.cfg.System.Checker.DiscoveryTimeoutSec) * time.Second
	for _, di := range ds.Domains {
		key, ok := ds.echFor(di.Domain)
		if !ok || ds.interrupted() {
			continue
		}
		if r, tested := baseline[di.Domain]; !tested || r.Status == CheckStatusComplete || r.untried {
			continue
		}
		plain := di
		plain.plainTLS = true
		result := ds.fetchForDomain(plain, timeout)
		if result.Status != CheckStatusComplete {
			continue
		}
		if !ds.echKeepsFailing(di, timeout) {
			if !ds.interrupted() {
				log.DiscoveryLogf("  [%s] loaded with ECH on a later try, so its first failure was not this network dropping ECH", di.Domain)
			}
			continue
		}
		ds.recordECHStrip(di.Domain, result)
		log.DiscoveryLogf("  [%s] loads without b4 when the ClientHello carries no ECH, and failed %d of %d tries with it: this network drops ECH ClientHellos naming %s, not the site", di.Domain, confirmTries, confirmTries, key.coverName)
		log.DiscoveryLogf("  ★ [%s] loads without any packet change once DNS answers carry no ECH key", di.Domain)
	}
}

func (ds *DiscoverySuite) echKeepsFailing(di DomainInput, timeout time.Duration) bool {
	for range confirmTries - 1 {
		if ds.interrupted() || ds.fetchForDomain(di, timeout).Status == CheckStatusComplete {
			return false
		}
	}
	return true
}

func (ds *DiscoverySuite) recordECHStrip(domain string, result CheckResult) {
	ds.CheckSuite.mu.Lock()
	defer ds.CheckSuite.mu.Unlock()

	dr := ds.domainResults[domain]
	if dr == nil {
		return
	}
	dr.ECHOnlyBlock = true
	if _, exists := dr.Results[presetECHStrip]; exists {
		return
	}
	dr.Results[presetECHStrip] = &DomainPresetResult{
		PresetName: presetECHStrip,
		Family:     FamilyECHStrip,
		Phase:      PhaseBaseline,
		Status:     CheckStatusComplete,
		Duration:   result.Duration,
		Speed:      result.Speed,
		BytesRead:  result.BytesRead,
		StatusCode: result.StatusCode,
		Set:        ds.plainFixSet(presetECHStrip, FamilyECHStrip),
	}
}
