package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hubwire"
)

const payloadFile = "captures/tls_www_google_com.bin"

func readPayload(name string) ([]byte, error) {
	if name == payloadFile {
		return config.FakeSNI1, nil
	}
	return nil, errors.New("no such payload")
}

type network struct {
	ASN     string
	Country string
	Name    string
	Prefix  string
	Weight  int
}

var networks = []*network{
	{ASN: "12389", Country: "RU", Name: "ROSTELECOM-AS, RU", Prefix: "95.24", Weight: 14},
	{ASN: "8359", Country: "RU", Name: "MTS, RU", Prefix: "85.140", Weight: 9},
	{ASN: "31213", Country: "RU", Name: "MEGAFON-RU, RU", Prefix: "83.149", Weight: 7},
	{ASN: "3216", Country: "RU", Name: "SOVAM-AS, RU", Prefix: "89.178", Weight: 6},
	{ASN: "25513", Country: "RU", Name: "ASN-MGTS-USPD, RU", Prefix: "93.80", Weight: 5},
	{ASN: "8402", Country: "RU", Name: "CORBINA-AS, RU", Prefix: "95.27", Weight: 4},
	{ASN: "58224", Country: "IR", Name: "TCI, IR", Prefix: "5.200", Weight: 5},
	{ASN: "197207", Country: "IR", Name: "MCCI-AS, IR", Prefix: "5.112", Weight: 4},
	{ASN: "9198", Country: "KZ", Name: "KAZTELECOM-AS, KZ", Prefix: "92.46", Weight: 3},
	{ASN: "25106", Country: "BY", Name: "MTSBY-AS, BY", Prefix: "178.168", Weight: 2},
	{ASN: "9121", Country: "TR", Name: "TTNET, TR", Prefix: "88.230", Weight: 3},
	{ASN: "4134", Country: "CN", Name: "CHINANET-BACKBONE, CN", Prefix: "113.98", Weight: 2},
	{ASN: "15895", Country: "UA", Name: "KSNET-AS, UA", Prefix: "46.211", Weight: 2},
	{ASN: "3320", Country: "DE", Name: "DTAG, DE", Prefix: "87.178", Weight: 1},
	{ASN: "24940", Country: "DE", Name: "HETZNER-AS, DE", Prefix: "65.21"},
	{Prefix: "45.155", Weight: 2},
}

var b4Versions = []string{"1.81.2", "1.82.0", "1.82.1", "1.83.0", "1.83.0", "1.83.1", "1.83.1", "1.83.1"}

type router struct {
	id      *hubwire.Identity
	ip      net.IP
	net     *network
	version string
	engine  string
}

type world struct {
	rng      *rand.Rand
	byPrefix map[string]*network
	byASN    map[string]*network
	subnets  int
}

func newWorld(rng *rand.Rand) *world {
	w := &world{rng: rng, byPrefix: make(map[string]*network), byASN: make(map[string]*network)}
	for _, n := range networks {
		if n.ASN != "" {
			w.byPrefix[n.Prefix] = n
			w.byASN[n.ASN] = n
		}
	}
	return w
}

func (w *world) on(asn string) *router {
	for _, n := range networks {
		if n.ASN == asn {
			return w.router(n)
		}
	}
	panic("unknown network AS" + asn)
}

func (w *world) random() *router {
	total := 0
	for _, n := range networks {
		total += n.Weight
	}
	pick := w.rng.IntN(total)
	for _, n := range networks {
		if pick < n.Weight {
			return w.router(n)
		}
		pick -= n.Weight
	}
	return w.router(networks[0])
}

func (w *world) router(n *network) *router {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(w.rng.Uint32())
	}
	id, err := hubwire.IdentityFromSeed(seed)
	if err != nil {
		panic(err)
	}
	w.subnets++
	engine := "nfqueue"
	if w.rng.IntN(7) == 0 {
		engine = "tun"
	}
	return &router{
		id:      id,
		ip:      net.ParseIP(fmt.Sprintf("%s.%d.%d", n.Prefix, 1+w.subnets%254, 2+w.rng.IntN(250))).To4(),
		net:     n,
		version: b4Versions[w.rng.IntN(len(b4Versions))],
		engine:  engine,
	}
}

func (w *world) lookup(_ context.Context, name string) ([]string, error) {
	if strings.HasSuffix(name, ".origin.asn.cymru.com") {
		labels := strings.Split(strings.TrimSuffix(name, ".origin.asn.cymru.com"), ".")
		if len(labels) == 4 {
			if n, ok := w.byPrefix[labels[3]+"."+labels[2]]; ok {
				prefix := labels[3] + "." + labels[2] + "." + labels[1] + ".0/24"
				return []string{n.ASN + " | " + prefix + " | " + n.Country + " | ripencc | 2012-04-17"}, nil
			}
		}
	}
	if strings.HasPrefix(name, "AS") && strings.HasSuffix(name, ".asn.cymru.com") {
		if n, ok := w.byASN[strings.TrimSuffix(strings.TrimPrefix(name, "AS"), ".asn.cymru.com")]; ok {
			return []string{n.ASN + " | " + n.Country + " | ripencc | 2002-08-21 | " + n.Name}, nil
		}
	}
	return nil, errors.New("no such record")
}

type setSpec struct {
	title       string
	description string
	domains     []string
	ips         []string
	asns        []string
	geosite     []string
	strategy    string
	ttl         uint8
	capture     bool
	tune        func(*config.SetConfig)
	pub         *published
}

type published struct {
	author  *router
	setID   string
	version int
	fp      string
}

func (p *setSpec) config() config.SetConfig {
	set := config.NewSetConfig()
	set.Name = p.title
	set.Targets.SNIDomains = append([]string{}, p.domains...)
	set.Targets.IPs = append([]string{}, p.ips...)
	set.Targets.ASNs = append([]string{}, p.asns...)
	set.Targets.GeoSiteCategories = append([]string{}, p.geosite...)
	set.Faking.SNI = true
	set.Faking.TTL = p.ttl
	set.Faking.SNIType = config.FakePayloadDefault1
	if p.capture {
		set.Faking.SNIType = config.FakePayloadCapture
		set.Faking.PayloadFile = payloadFile
	}
	set.Fragmentation.Strategy = p.strategy
	if p.tune != nil {
		p.tune(&set)
	}
	return set
}

func (p *setSpec) next(change func(*setSpec)) *setSpec {
	n := *p
	n.pub = nil
	change(&n)
	return &n
}

func (p *setSpec) ref() string {
	if p.pub == nil {
		return p.title
	}
	return fmt.Sprintf("%s v%d", p.pub.setID, p.pub.version)
}
