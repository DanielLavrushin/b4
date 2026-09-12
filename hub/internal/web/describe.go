package web

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4/sni"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	SourceBuiltinPayload = "built-in payload"
	SourceCapture        = "attached TLS capture"
	SourcePayloadDomain  = "faking.payload_domain"
	SourceCustomPayload  = "faking.custom_payload"
	SourceFakeSNIs       = "faking.sni_mutation.fake_snis"
	SourceQUICCapture    = "attached QUIC capture"

	targetsPreview = 4
)

func DecodeSet(projection map[string]interface{}) (config.SetConfig, error) {
	set := config.NewSetConfig()
	raw, err := json.Marshal(projection)
	if err != nil {
		return set, err
	}
	if err := json.Unmarshal(raw, &set); err != nil {
		return set, err
	}
	config.ApplySetDefaults(&set)
	return set, nil
}

type Targets struct {
	Domains []string
	IPs     []string
	GeoSite []string
	GeoIP   []string
}

func TargetsOf(projection map[string]interface{}) Targets {
	return Targets{
		Domains: store.TargetList(projection, "sni_domains"),
		IPs:     store.TargetList(projection, "ip"),
		GeoSite: store.TargetList(projection, "geosite_categories"),
		GeoIP:   store.TargetList(projection, "geoip_categories"),
	}
}

func (t Targets) Empty() bool {
	return len(t.Domains) == 0 && len(t.IPs) == 0 && len(t.GeoSite) == 0 && len(t.GeoIP) == 0
}

func preview(items []string, label string) string {
	if len(items) == 0 {
		return ""
	}
	shown := items
	if len(shown) > targetsPreview {
		shown = shown[:targetsPreview]
	}
	out := strings.Join(shown, ", ")
	if len(items) > targetsPreview {
		out += fmt.Sprintf(" and %d more", len(items)-targetsPreview)
	}
	if label != "" {
		out = label + ": " + out
	}
	return out
}

func (t Targets) Summary() string {
	parts := make([]string, 0, 4)
	if s := preview(t.Domains, ""); s != "" {
		parts = append(parts, s)
	}
	if s := preview(t.IPs, "addresses"); s != "" {
		parts = append(parts, s)
	}
	if s := preview(t.GeoSite, "geosite"); s != "" {
		parts = append(parts, s)
	}
	if s := preview(t.GeoIP, "geoip"); s != "" {
		parts = append(parts, s)
	}
	if len(parts) == 0 {
		return "no targets"
	}
	return strings.Join(parts, "; ")
}

func payloadKind(set *config.SetConfig, payloads []hubwire.BlobRef) string {
	switch set.Faking.SNIType {
	case config.FakePayloadDefault1:
		return "built-in payload 1"
	case config.FakePayloadDefault2:
		return "built-in payload 2"
	case config.FakePayloadCapture:
		for _, ref := range payloads {
			if ref.Protocol == hubwire.ProtocolTLS && ref.Domain != "" {
				return "attached capture for " + ref.Domain
			}
		}
		return "attached capture"
	case config.FakePayloadDomain:
		if set.Faking.PayloadDomain != "" {
			return "generated ClientHello for " + set.Faking.PayloadDomain
		}
		return "generated ClientHello"
	case config.FakePayloadCustom:
		return "custom payload"
	case config.FakePayloadRandom:
		return "random bytes"
	case config.FakePayloadZero:
		return "zero bytes"
	case config.FakePayloadInverted:
		return "inverted original"
	case config.FakePayloadSTUN:
		return "STUN payload"
	}
	return fmt.Sprintf("payload type %d", set.Faking.SNIType)
}

func StrategyWords(set *config.SetConfig, payloads []hubwire.BlobRef) []string {
	words := make([]string, 0, 12)
	if set.Faking.SNI {
		fake := fmt.Sprintf("fake ClientHello (%s, %s", payloadKind(set, payloads), set.Faking.Strategy)
		if set.Faking.TTL > 0 {
			fake += fmt.Sprintf(", TTL %d", set.Faking.TTL)
		}
		if set.Faking.SNISeqLength > 1 {
			fake += fmt.Sprintf(", %d copies", set.Faking.SNISeqLength)
		}
		words = append(words, fake+")")
		if mode := strings.ToLower(set.Faking.SNIMutation.Mode); mode != "" && mode != config.ConfigOff {
			words = append(words, "SNI mutation "+mode)
		}
		if len(set.Faking.TLSMod) > 0 {
			words = append(words, "TLS modifications "+strings.Join(set.Faking.TLSMod, ", "))
		}
	}
	if strategy := strings.ToLower(set.Fragmentation.Strategy); strategy != "" && strategy != config.ConfigNone {
		frag := strategy + " fragmentation"
		if len(set.Fragmentation.StrategyPool) > 0 {
			frag += " (pool " + strings.Join(set.Fragmentation.StrategyPool, ", ") + ")"
		}
		if set.Fragmentation.MiddleSNI {
			frag += ", split inside the SNI"
		}
		if set.Fragmentation.ReverseOrder {
			frag += ", reversed order"
		}
		words = append(words, frag)
	}
	if set.TCP.SynFake {
		words = append(words, "fake data in SYN")
	}
	if mode := strings.ToLower(set.TCP.Desync.Mode); mode != "" && mode != config.ConfigOff {
		desync := "desync " + mode
		if set.TCP.Desync.Count > 1 {
			desync += fmt.Sprintf(" x%d", set.TCP.Desync.Count)
		}
		words = append(words, desync)
	}
	if mode := strings.ToLower(set.TCP.Incoming.Mode); mode != "" && mode != config.ConfigOff {
		words = append(words, "incoming "+mode)
	}
	if mode := strings.ToLower(set.TCP.Win.Mode); mode != "" && mode != config.ConfigOff {
		words = append(words, "window "+mode)
	}
	if set.TCP.Duplicate.Enabled {
		words = append(words, fmt.Sprintf("duplicate segments x%d", set.TCP.Duplicate.Count))
	}
	if set.TCP.RSTProtection.Enabled {
		words = append(words, "RST protection")
	}
	if set.TCP.DropSACK {
		words = append(words, "drop SACK")
	}
	if set.TCP.HTTPMethodEOL {
		words = append(words, "HTTP method EOL")
	}
	if set.TCP.Seg2Delay > 0 {
		words = append(words, fmt.Sprintf("second segment delayed %d ms", set.TCP.Seg2Delay))
	}
	if mode := strings.ToLower(set.UDP.Mode); mode != "" && mode != config.ConfigNone {
		udp := "UDP " + mode
		if mode == config.UDPModeFake {
			switch set.UDP.FakePayloadFile {
			case "":
				udp += " (zero fill)"
			case config.FakePayloadAutoQUIC:
				udp += " (generated QUIC Initial)"
			case config.FakePayloadPreset1, config.FakePayloadPreset2:
				udp += " (bundled QUIC preset)"
			default:
				udp += " (attached QUIC capture)"
			}
		}
		words = append(words, udp)
	}
	if set.DNS.Enabled {
		dns := "DNS redirect"
		if host, _ := hubwire.DoHHost(set.DNS.DoHURL); host != "" {
			dns += " via " + host
		}
		if set.DNS.Strict {
			dns += ", strict"
		}
		if set.DNS.FragmentQuery {
			dns += ", fragmented queries"
		}
		if n := len(set.DNS.Pins); n > 0 {
			dns += fmt.Sprintf(", %d pinned domain(s)", n)
		}
		words = append(words, dns)
	}
	if set.Routing.Enabled && set.Routing.Mode == config.RoutingModeBlock {
		block := "blocks matched traffic"
		if set.Routing.BlockAction != "" {
			block += " (" + set.Routing.BlockAction + ")"
		}
		words = append(words, block)
	}
	if set.MSSClamp.Enabled {
		words = append(words, fmt.Sprintf("MSS clamp %d", set.MSSClamp.Size))
	}
	if set.Targets.TLSVersion != "" {
		words = append(words, "TLS "+set.Targets.TLSVersion+" only")
	}
	if set.Targets.IPVersion != "" {
		words = append(words, "IPv"+set.Targets.IPVersion+" only")
	}
	if set.Targets.DomainOnly {
		words = append(words, "domain-only matching")
	}
	if len(words) == 0 {
		words = append(words, "no bypass, matched traffic passes unchanged")
	}
	return words
}

type EmittedName struct {
	Name   string
	Source string
}

func builtinName(payload []byte) string {
	name, _, ok := sni.ParseTLSClientHelloSNI(payload)
	if !ok {
		return ""
	}
	return name
}

func EmittedNames(set *config.SetConfig, payloads []hubwire.BlobRef) []EmittedName {
	names := make([]EmittedName, 0, 4)
	add := func(name, source string) {
		if name == "" {
			return
		}
		names = append(names, EmittedName{Name: name, Source: source})
	}
	if set.Faking.SNI {
		switch set.Faking.SNIType {
		case config.FakePayloadDefault1:
			add(builtinName(config.FakeSNI1), SourceBuiltinPayload)
		case config.FakePayloadDefault2:
			add(builtinName(config.FakeSNI2), SourceBuiltinPayload)
		case config.FakePayloadCapture:
			for _, ref := range payloads {
				if ref.Protocol == hubwire.ProtocolTLS {
					add(ref.Domain, SourceCapture)
				}
			}
		case config.FakePayloadDomain:
			add(set.Faking.PayloadDomain, SourcePayloadDomain)
		case config.FakePayloadCustom:
			if name := builtinName([]byte(set.Faking.CustomPayload)); name != "" {
				add(name, SourceCustomPayload)
			} else if set.Faking.CustomPayload != "" {
				add("(custom payload without a readable server name)", SourceCustomPayload)
			}
		}
		switch strings.ToLower(set.Faking.SNIMutation.Mode) {
		case "duplicate", "full":
			for _, fake := range set.Faking.SNIMutation.FakeSNIs {
				add(fake, SourceFakeSNIs)
			}
		}
	}
	if strings.ToLower(set.UDP.Mode) == config.UDPModeFake {
		for _, ref := range payloads {
			if ref.Protocol == hubwire.ProtocolQUIC {
				add(ref.Domain, SourceQUICCapture)
			}
		}
	}
	return names
}

type Pin struct {
	Domain    string
	Addresses []string
}

func PinsOf(set *config.SetConfig) []Pin {
	if len(set.DNS.Pins) == 0 {
		return nil
	}
	domains := make([]string, 0, len(set.DNS.Pins))
	for domain := range set.DNS.Pins {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	out := make([]Pin, 0, len(domains))
	for _, domain := range domains {
		out = append(out, Pin{Domain: domain, Addresses: set.DNS.Pins[domain]})
	}
	return out
}

func DoHHostOf(set *config.SetConfig) string {
	if !set.DNS.Enabled {
		return ""
	}
	host, _ := hubwire.DoHHost(set.DNS.DoHURL)
	return host
}

type Rating struct {
	Bucket  string
	Where   string
	Percent int
	N       float64
	Devices int
	Newest  string
}

func (r Rating) Usable() bool {
	return r.Bucket != hubwire.BucketNone && r.Bucket != ""
}

func RatingOf(scores hubwire.Scores, asnNames map[string]string, viewerASN, viewerCC string) Rating {
	d := hubwire.PickScore(scores, viewerASN, viewerCC)
	r := Rating{Bucket: d.Bucket, Percent: int(d.Score*100 + 0.5), N: d.N, Devices: d.Devices, Newest: d.Newest}
	switch d.Bucket {
	case hubwire.BucketASN:
		if name := asnNames[viewerASN]; name != "" {
			r.Where = name
		} else {
			r.Where = "AS" + viewerASN
		}
	case hubwire.BucketCountry:
		r.Where = strings.ToUpper(viewerCC)
	case hubwire.BucketGlobal:
		r.Where = "all networks"
	}
	return r
}
