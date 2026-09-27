package web

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4/sni"
	"github.com/daniellavrushin/b4hub/internal/store"
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
	Domains    []string
	IPs        []string
	GeoSite    []string
	GeoIP      []string
	ASNs       []string
	TLSVersion string
	IPVersion  string
	DomainOnly bool
}

func TargetsOf(projection map[string]interface{}) Targets {
	t := Targets{
		Domains: store.TargetList(projection, "sni_domains"),
		IPs:     store.TargetList(projection, "ip"),
		GeoSite: store.TargetList(projection, "geosite_categories"),
		GeoIP:   store.TargetList(projection, "geoip_categories"),
		ASNs:    store.TargetList(projection, "asns"),
	}
	if targets, ok := projection["targets"].(map[string]interface{}); ok {
		t.TLSVersion, _ = targets["tls"].(string)
		t.IPVersion, _ = targets["ip_version"].(string)
		t.DomainOnly, _ = targets["domain_only"].(bool)
	}
	return t
}

func (t Targets) Empty() bool {
	return len(t.Domains) == 0 && len(t.IPs) == 0 && len(t.GeoSite) == 0 && len(t.GeoIP) == 0 && len(t.ASNs) == 0
}

type EmittedName struct {
	Name       string
	Source     string
	Unreadable bool
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
			add(builtinName(config.FakeSNI1), EmitBuiltin)
		case config.FakePayloadDefault2:
			add(builtinName(config.FakeSNI2), EmitBuiltin)
		case config.FakePayloadCapture:
			for _, ref := range payloads {
				if ref.Protocol == hubwire.ProtocolTLS {
					add(ref.Domain, EmitCapture)
				}
			}
		case config.FakePayloadDomain:
			add(set.Faking.PayloadDomain, EmitPayloadDomain)
		case config.FakePayloadCustom:
			if name := builtinName([]byte(set.Faking.CustomPayload)); name != "" {
				add(name, EmitCustom)
			} else if set.Faking.CustomPayload != "" {
				names = append(names, EmittedName{Source: EmitCustom, Unreadable: true})
			}
		}
		switch strings.ToLower(set.Faking.SNIMutation.Mode) {
		case "duplicate", "full":
			for _, fake := range set.Faking.SNIMutation.FakeSNIs {
				add(fake, EmitFakeSNIs)
			}
		}
	}
	if strings.ToLower(set.UDP.Mode) == config.UDPModeFake {
		for _, ref := range payloads {
			if ref.Protocol == hubwire.ProtocolQUIC {
				add(ref.Domain, EmitQUICCapture)
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
