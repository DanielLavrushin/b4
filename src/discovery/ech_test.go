package discovery

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sni"
)

func echKeyPair(t *testing.T, configID byte, publicName string) (tls.EncryptedClientHelloKey, []byte) {
	t.Helper()
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pk := priv.PublicKey().Bytes()
	contents := []byte{configID}
	contents = binary.BigEndian.AppendUint16(contents, 0x0020)
	contents = binary.BigEndian.AppendUint16(contents, uint16(len(pk)))
	contents = append(contents, pk...)
	contents = binary.BigEndian.AppendUint16(contents, 4)
	contents = binary.BigEndian.AppendUint16(contents, 0x0001)
	contents = binary.BigEndian.AppendUint16(contents, 0x0001)
	contents = append(contents, 0, byte(len(publicName)))
	contents = append(contents, publicName...)
	contents = binary.BigEndian.AppendUint16(contents, 0)

	cfg := binary.BigEndian.AppendUint16(nil, 0xfe0d)
	cfg = binary.BigEndian.AppendUint16(cfg, uint16(len(contents)))
	cfg = append(cfg, contents...)
	list := binary.BigEndian.AppendUint16(nil, uint16(len(cfg)))
	list = append(list, cfg...)
	return tls.EncryptedClientHelloKey{Config: cfg, PrivateKey: priv.Bytes(), SendAsRetry: true}, list
}

type echServer struct {
	*httptest.Server
	mu    sync.Mutex
	seen  []tls.ConnectionState
	hello []string
	drop  string
}

type helloListener struct {
	net.Listener
	s *echServer
}

func (l helloListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &helloConn{Conn: c, s: l.s}, nil
}

type helloConn struct {
	net.Conn
	s    *echServer
	buf  []byte
	done bool
}

func (c *helloConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if !c.done && n > 0 {
		c.buf = append(c.buf, p[:n]...)
		if name, _, ok := sni.ParseTLSClientHelloSNI(c.buf); ok {
			c.done = true
			c.s.mu.Lock()
			c.s.hello = append(c.s.hello, name)
			drop := c.s.drop != "" && name == c.s.drop
			c.s.mu.Unlock()
			if drop {
				c.Conn.Close()
				return 0, net.ErrClosed
			}
		}
	}
	return n, err
}

func startECHServer(t *testing.T, keys ...tls.EncryptedClientHelloKey) *echServer {
	t.Helper()
	page := htmlOf(4096)
	s := &echServer{}
	s.Server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.seen = append(s.seen, *r.TLS)
		s.mu.Unlock()
		_, _ = w.Write(page)
	}))
	s.Listener = helloListener{Listener: s.Listener, s: s}
	s.TLS = &tls.Config{MinVersion: tls.VersionTLS13, EncryptedClientHelloKeys: keys}
	s.StartTLS()
	t.Cleanup(s.Close)
	return s
}

func (s *echServer) states() []tls.ConnectionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]tls.ConnectionState(nil), s.seen...)
}

func (s *echServer) helloNames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.hello...)
}

func echProbeSuite(t *testing.T, srv *echServer, domain string) (*DiscoverySuite, DomainInput) {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	di := DomainInput{Domain: domain, CheckURL: "https://" + domain + ":" + u.Port() + "/"}
	ds := &DiscoverySuite{
		CheckSuite: &CheckSuite{Domains: []DomainInput{di}},
		ipVersion:  "ipv4",
		ctx:        context.Background(),
	}
	return ds, di
}

func TestProbeSendsECHWhenTheSitePublishesAKey(t *testing.T) {
	key, list := echKeyPair(t, 7, "cover.example")
	srv := startECHServer(t, key)
	ds, di := echProbeSuite(t, srv, "site.example")
	ds.storeECH("site.example", echKey{configList: list, coverName: "cover.example"})

	res := ds.fetchUsingIPForDomain(di, 5*time.Second, "127.0.0.1")
	if res.Status != CheckStatusComplete {
		t.Fatalf("fetch over ECH: %s (%s)", res.Status, res.Error)
	}
	states := srv.states()
	if len(states) != 1 || !states[0].ECHAccepted || states[0].ServerName != "site.example" {
		t.Fatalf("want one ECH request for site.example, got %+v", states)
	}
	if names := srv.helloNames(); len(names) == 0 || names[0] != "cover.example" {
		t.Fatalf("the ClientHello on the wire must name the cover, got %v", names)
	}
}

func TestProbeRetriesWithTheECHKeyTheServerSends(t *testing.T) {
	current, currentList := echKeyPair(t, 9, "cover.example")
	_, staleList := echKeyPair(t, 8, "cover.example")
	srv := startECHServer(t, current)
	ds, di := echProbeSuite(t, srv, "site.example")
	ds.storeECH("site.example", echKey{configList: staleList, coverName: "cover.example"})

	res := ds.fetchUsingIPForDomain(di, 5*time.Second, "127.0.0.1")
	if res.Status != CheckStatusComplete {
		t.Fatalf("a rotated key must be retried with the server's new one, got %s (%s)", res.Status, res.Error)
	}
	if states := srv.states(); len(states) != 1 || !states[0].ECHAccepted {
		t.Fatalf("want the request on the retried ECH connection, got %+v", states)
	}
	if key, _ := ds.echFor("site.example"); !bytes.Equal(key.configList, currentList) {
		t.Fatal("the suite keeps using the stale key after the server sent a new one")
	}
}

func TestProbeRetriesWithoutECHWhenTheServerHasNoKey(t *testing.T) {
	_, list := echKeyPair(t, 3, "cover.example")
	srv := startECHServer(t)
	ds, di := echProbeSuite(t, srv, "site.example")
	ds.storeECH("site.example", echKey{configList: list, coverName: "cover.example"})

	res := ds.fetchUsingIPForDomain(di, 5*time.Second, "127.0.0.1")
	if res.Status != CheckStatusComplete {
		t.Fatalf("a server without ECH turns it down with no key; browsers then retry without ECH, got %s (%s)", res.Status, res.Error)
	}
	if states := srv.states(); len(states) != 1 || states[0].ECHAccepted {
		t.Fatalf("want one request without ECH, got %+v", states)
	}
}

func TestPlainProbeLeavesECHOut(t *testing.T) {
	key, list := echKeyPair(t, 5, "cover.example")
	srv := startECHServer(t, key)
	ds, di := echProbeSuite(t, srv, "site.example")
	ds.storeECH("site.example", echKey{configList: list, coverName: "cover.example"})

	di.plainTLS = true
	res := ds.fetchUsingIPForDomain(di, 5*time.Second, "127.0.0.1")
	if res.Status != CheckStatusComplete {
		t.Fatalf("plain fetch: %s (%s)", res.Status, res.Error)
	}
	if states := srv.states(); len(states) != 1 || states[0].ECHAccepted {
		t.Fatalf("the plain fetch must not offer ECH, got %+v", states)
	}
	if names := srv.helloNames(); len(names) != 1 || names[0] != "site.example" {
		t.Fatalf("the plain ClientHello names the site, got %v", names)
	}
}

func TestProbeECHRecordsTheCoverName(t *testing.T) {
	_, list := echKeyPair(t, 1, "cloudflare-ech.com")
	orig := lookupECHConfigList
	t.Cleanup(func() { lookupECHConfigList = orig })
	var askedMu sync.Mutex
	var asked []string
	lookupECHConfigList = func(_ context.Context, _ uint, domain string) []byte {
		askedMu.Lock()
		asked = append(asked, domain)
		askedMu.Unlock()
		if domain == "steamdb.info" {
			return list
		}
		return nil
	}

	inputs := []DomainInput{
		{Domain: "steamdb.info", CheckURL: "https://steamdb.info/"},
		{Domain: "plain.example", CheckURL: "https://plain.example/"},
		{Domain: "port80.example", CheckURL: "http://port80.example/"},
	}
	newSuite := func(tlsVersion string) *DiscoverySuite {
		results := map[string]*DomainDiscoveryResult{}
		for _, di := range inputs {
			results[di.Domain] = &DomainDiscoveryResult{Domain: di.Domain}
		}
		return &DiscoverySuite{
			CheckSuite:    &CheckSuite{Domains: inputs},
			domainResults: results,
			tlsVersion:    tlsVersion,
			ctx:           context.Background(),
		}
	}

	ds := newSuite("auto")
	ds.probeECH()
	slices.Sort(asked)
	if !slices.Equal(asked, []string{"plain.example", "steamdb.info"}) {
		t.Fatalf("an http:// check URL never sends a ClientHello, asked %v", asked)
	}
	if got := ds.domainResults["steamdb.info"].ECHCoverName; got != "cloudflare-ech.com" {
		t.Fatalf("cover name %q", got)
	}
	if got := ds.domainResults["plain.example"].ECHCoverName; got != "" {
		t.Fatalf("a site without a key got cover name %q", got)
	}

	asked = nil
	tls12 := newSuite("tls12")
	tls12.probeECH()
	if len(asked) != 0 || tls12.domainResults["steamdb.info"].ECHCoverName != "" {
		t.Fatalf("ECH needs TLS 1.3; a TLS 1.2 run must not probe it, asked %v", asked)
	}
}

func TestSetsTargetTheECHCoverName(t *testing.T) {
	ds := &DiscoverySuite{
		CheckSuite: &CheckSuite{Domains: []DomainInput{
			{Domain: "steamdb.info", CheckURL: "https://steamdb.info/"},
			{Domain: "plain.example", CheckURL: "https://plain.example/"},
		}},
		cfg:        &config.Config{},
		dnsResults: map[string]*DNSDiscoveryResult{},
	}
	ds.storeECH("steamdb.info", echKey{coverName: "cloudflare-ech.com"})

	set := config.NewSetConfig()
	scoped := ds.scopeSetToDomains(&set, []string{"steamdb.info"})
	want := []string{"steamdb.info", "cloudflare-ech.com"}
	if !slices.Equal(scoped.Targets.SNIDomains, want) || !slices.Equal(scoped.Targets.DomainsToMatch, want) {
		t.Fatalf("the proposed set must also match the ECH cover name, got %v / %v", scoped.Targets.SNIDomains, scoped.Targets.DomainsToMatch)
	}
	if plain := ds.scopeSetToDomains(&set, []string{"plain.example"}); !slices.Equal(plain.Targets.SNIDomains, []string{"plain.example"}) {
		t.Fatalf("a site without ECH gets no cover name, got %v", plain.Targets.SNIDomains)
	}

	multi := ds.buildTestConfigMulti(ConfigPreset{Name: "combo-pastseq", Config: config.NewSetConfig()})
	if got := multi.Sets[0].Targets.SNIDomains; !slices.Equal(got, []string{"steamdb.info", "plain.example", "cloudflare-ech.com"}) {
		t.Fatalf("the probes' test set must match the ECH ClientHello by its cover name, got %v", got)
	}
}

func (s *echServer) dropHellosNaming(name string) {
	s.mu.Lock()
	s.drop = name
	s.mu.Unlock()
}

func echBlockedSuite(t *testing.T, srv *echServer, domain, cover string, list []byte) (*DiscoverySuite, DomainInput) {
	t.Helper()
	ds, di := echProbeSuite(t, srv, domain)
	ds.cfg = &config.Config{}
	ds.cfg.System.Checker.DiscoveryTimeoutSec = 5
	ds.dnsResults = map[string]*DNSDiscoveryResult{domain: {ExpectedIPs: []string{"127.0.0.1"}}}
	ds.domainResults = map[string]*DomainDiscoveryResult{domain: {Domain: domain, Results: map[string]*DomainPresetResult{}}}
	ds.storeECH(domain, echKey{configList: list, coverName: cover})
	return ds, di
}

func TestECHOnlyBlockProposesTheStripSet(t *testing.T) {
	key, list := echKeyPair(t, 4, "cover.example")
	srv := startECHServer(t, key)
	srv.dropHellosNaming("cover.example")
	ds, di := echBlockedSuite(t, srv, "site.example", "cover.example", list)

	if res := ds.fetchForDomain(di, 5*time.Second); res.Status == CheckStatusComplete {
		t.Fatal("rig: the ECH probe must fail where hellos naming the cover are dropped")
	}

	ds.noteECHOnlyBlocks(map[string]CheckResult{"site.example": {Domain: "site.example", Status: CheckStatusFailed}})
	ds.determineBest()

	dr := ds.domainResults["site.example"]
	if !dr.ECHOnlyBlock || dr.BestPreset != presetECHStrip || !dr.BestSuccess || dr.Outcome != OutcomeFound {
		t.Fatalf("ech_only_block %v, best %q (%v), outcome %q: want the ech-strip plain fix", dr.ECHOnlyBlock, dr.BestPreset, dr.BestSuccess, dr.Outcome)
	}
	if ds.needsBypass("site.example") {
		t.Fatal("the site still counts as needing a packet strategy")
	}
	r := dr.Results[presetECHStrip]
	if r.Family != FamilyECHStrip || r.Phase != PhaseBaseline || r.Set == nil {
		t.Fatalf("result %+v", r)
	}
	set := r.Set
	if !set.DNS.StripECH {
		t.Fatal("the proposed set does not strip ECH keys")
	}
	if set.Fragmentation.Strategy != config.ConfigNone || set.Faking.SNI || set.UDP.Mode != config.ConfigOff {
		t.Fatalf("the proposed set changes packets: frag %q, fake %v, udp %q", set.Fragmentation.Strategy, set.Faking.SNI, set.UDP.Mode)
	}
	if !slices.Equal(set.Targets.SNIDomains, []string{"site.example", "cover.example"}) {
		t.Fatalf("targets %v, want the site and the cover its key names", set.Targets.SNIDomains)
	}
	if !plainFixName(presetECHStrip) {
		t.Fatal("ech-strip must stay out of the strategy cache and the set verdict's strategy list")
	}
}

func TestECHOnlyBlockNeedsThePlainLoad(t *testing.T) {
	key, list := echKeyPair(t, 6, "cover.example")
	srv := startECHServer(t, key)
	srv.dropHellosNaming("site.example")
	ds, _ := echBlockedSuite(t, srv, "site.example", "cover.example", list)

	ds.noteECHOnlyBlocks(map[string]CheckResult{"site.example": {Domain: "site.example", Status: CheckStatusFailed}})
	dr := ds.domainResults["site.example"]
	if dr.ECHOnlyBlock || dr.Results[presetECHStrip] != nil {
		t.Fatal("a site whose plain hello is dropped too is not an ECH-only block")
	}

	passed, _ := echBlockedSuite(t, srv, "site.example", "cover.example", list)
	passed.noteECHOnlyBlocks(map[string]CheckResult{"site.example": {Domain: "site.example", Status: CheckStatusComplete}})
	if passed.domainResults["site.example"].ECHOnlyBlock {
		t.Fatal("a site that loads with ECH is not blocked at all")
	}
}

func TestECHStripConfirmsWithoutECH(t *testing.T) {
	ds := &DiscoverySuite{CheckSuite: &CheckSuite{Domains: []DomainInput{
		{Domain: "site.example", CheckURL: "https://site.example/"},
		{Domain: "other.example", CheckURL: "https://other.example/"},
	}}}
	for _, di := range ds.confirmInputs(presetECHStrip, []string{"site.example"}) {
		if !di.plainTLS {
			t.Fatalf("%s: the strip set is confirmed with ECH, the hello the set prevents", di.Domain)
		}
	}
	for _, di := range ds.confirmInputs("combo-pastseq", []string{"site.example", "other.example"}) {
		if di.plainTLS {
			t.Fatalf("%s: a strategy is confirmed without ECH", di.Domain)
		}
	}
}

func TestScopedSetKeepsStripECH(t *testing.T) {
	ds := &DiscoverySuite{
		CheckSuite: &CheckSuite{Domains: []DomainInput{{Domain: "site.example", CheckURL: "https://site.example/"}}},
		cfg:        &config.Config{},
		dnsResults: map[string]*DNSDiscoveryResult{},
	}
	set := config.NewSetConfig()
	set.DNS = config.DNSConfig{Enabled: true, TargetDNS: "9.9.9.9", StripECH: true}
	scoped := ds.scopeSetToDomains(&set, []string{"site.example"})
	if scoped.DNS.Enabled || !scoped.DNS.StripECH {
		t.Fatalf("dns %+v: want the redirect dropped for a clean name and the ECH switch kept", scoped.DNS)
	}

	multi := ds.buildTestConfigMulti(plainFixPreset(presetECHStrip, FamilyECHStrip))
	if !multi.Sets[0].DNS.StripECH {
		t.Fatal("the ech-strip test set lost its switch")
	}
	if strategy := ds.buildTestConfig(ConfigPreset{Name: "combo-pastseq", Config: config.NewSetConfig()}); strategy.Sets[0].DNS.StripECH {
		t.Fatal("a strategy preset turned the switch on")
	}
}

func TestRotatedECHKeyMovesTheCover(t *testing.T) {
	current, _ := echKeyPair(t, 9, "new-cover.example")
	_, staleList := echKeyPair(t, 8, "cover.example")
	srv := startECHServer(t, current)
	ds, di := echProbeSuite(t, srv, "site.example")
	ds.domainResults = map[string]*DomainDiscoveryResult{"site.example": {Domain: "site.example", ECHCoverName: "cover.example"}}
	ds.storeECH("site.example", echKey{configList: staleList, coverName: "cover.example"})

	if res := ds.fetchUsingIPForDomain(di, 5*time.Second, "127.0.0.1"); res.Status != CheckStatusComplete {
		t.Fatalf("fetch: %s (%s)", res.Status, res.Error)
	}
	if names := srv.helloNames(); len(names) != 2 || names[1] != "new-cover.example" {
		t.Fatalf("rig: the retry must name the new key's public name, got %v", names)
	}
	if key, _ := ds.echFor("site.example"); key.coverName != "new-cover.example" {
		t.Fatalf("cached cover %q, want the name of the key the server sent", key.coverName)
	}
	if got := ds.domainResults["site.example"].ECHCoverName; got != "new-cover.example" {
		t.Fatalf("reported cover %q", got)
	}
	if got := ds.withECHCoverNames([]string{"site.example"}); !slices.Equal(got, []string{"site.example", "new-cover.example"}) {
		t.Fatalf("sets target %v, want the name the probes now send", got)
	}
}

func TestUnusableECHKeyMeansNoECH(t *testing.T) {
	_, list := echKeyPair(t, 2, "cloudflare-ech.com")
	unusable := append([]byte(nil), list...)
	binary.BigEndian.PutUint16(unusable[7:9], 0x0010)
	orig := lookupECHConfigList
	t.Cleanup(func() { lookupECHConfigList = orig })
	lookupECHConfigList = func(context.Context, uint, string) []byte { return unusable }

	ds := &DiscoverySuite{
		CheckSuite:    &CheckSuite{Domains: []DomainInput{{Domain: "site.example", CheckURL: "https://site.example/"}}},
		domainResults: map[string]*DomainDiscoveryResult{"site.example": {Domain: "site.example"}},
		ctx:           context.Background(),
	}
	ds.probeECH()
	if _, ok := ds.echFor("site.example"); ok {
		t.Fatal("a key Go's client cannot use would fail every probe locally and read as a block")
	}
	if got := ds.domainResults["site.example"].ECHCoverName; got != "" {
		t.Fatalf("cover %q reported for a key nobody can use", got)
	}
}

func TestProbeECHLooksUpTheSitesTogether(t *testing.T) {
	_, list := echKeyPair(t, 3, "cloudflare-ech.com")
	orig := lookupECHConfigList
	t.Cleanup(func() { lookupECHConfigList = orig })

	inputs := []DomainInput{
		{Domain: "a.example", CheckURL: "https://a.example/"},
		{Domain: "b.example", CheckURL: "https://b.example/"},
		{Domain: "c.example", CheckURL: "https://c.example/"},
	}
	var arrived atomic.Int32
	everyone := make(chan struct{})
	lookupECHConfigList = func(context.Context, uint, string) []byte {
		if arrived.Add(1) == int32(len(inputs)) {
			close(everyone)
		}
		select {
		case <-everyone:
			return list
		case <-time.After(2 * time.Second):
			return nil
		}
	}

	results := map[string]*DomainDiscoveryResult{}
	for _, di := range inputs {
		results[di.Domain] = &DomainDiscoveryResult{Domain: di.Domain}
	}
	ds := &DiscoverySuite{CheckSuite: &CheckSuite{Domains: inputs}, domainResults: results, ctx: context.Background()}
	ds.probeECH()
	for _, di := range inputs {
		if got := results[di.Domain].ECHCoverName; got != "cloudflare-ech.com" {
			t.Fatalf("%s: cover %q: each lookup waited for the one before it, so a slow resolver costs its timeout once per site", di.Domain, got)
		}
	}
}
