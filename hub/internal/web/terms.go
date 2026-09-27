package web

import (
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hubwire"
)

const (
	TechFake          = "fake"
	TechSNIMutation   = "sni_mutation"
	TechTLSMod        = "tls_mod"
	TechFrag          = "frag"
	TechSynFake       = "syn_fake"
	TechDesync        = "desync"
	TechIncoming      = "incoming"
	TechWindow        = "window"
	TechDuplicate     = "duplicate"
	TechRSTProtection = "rst_protection"
	TechDropSACK      = "drop_sack"
	TechHTTPMethodEOL = "http_method_eol"
	TechSeg2Delay     = "seg2_delay"
	TechUDP           = "udp"
	TechDNS           = "dns"
	TechBlock         = "block"
	TechMSSClamp      = "mss_clamp"
	TechPassthrough   = "passthrough"

	FilterTLSOnly    = "tls_only"
	FilterIPOnly     = "ip_only"
	FilterDomainOnly = "domain_only"

	EmitBuiltin       = "builtin_payload"
	EmitCapture       = "capture"
	EmitPayloadDomain = "payload_domain"
	EmitCustom        = "custom_payload"
	EmitFakeSNIs      = "fake_snis"
	EmitQUICCapture   = "quic_capture"
)

var techniqueCodes = []string{TechFake, TechSNIMutation, TechTLSMod, TechFrag, TechSynFake, TechDesync, TechIncoming, TechWindow, TechDuplicate,
	TechRSTProtection, TechDropSACK, TechHTTPMethodEOL, TechSeg2Delay, TechUDP, TechDNS, TechBlock, TechMSSClamp, TechPassthrough}

var filterCodes = []string{FilterTLSOnly, FilterIPOnly, FilterDomainOnly}

var emitCodes = []string{EmitBuiltin, EmitCapture, EmitPayloadDomain, EmitCustom, EmitFakeSNIs, EmitQUICCapture}

type Term struct {
	Code   string                 `json:"code"`
	Params map[string]interface{} `json:"params,omitempty"`
}

func term(code string, params map[string]interface{}) Term {
	if len(params) == 0 {
		params = nil
	}
	return Term{Code: code, Params: params}
}

func payloadCode(set *config.SetConfig, payloads []hubwire.BlobRef) (string, string) {
	switch set.Faking.SNIType {
	case config.FakePayloadDefault1:
		return "builtin1", ""
	case config.FakePayloadDefault2:
		return "builtin2", ""
	case config.FakePayloadCapture:
		for _, ref := range payloads {
			if ref.Protocol == hubwire.ProtocolTLS && ref.Domain != "" {
				return "capture", ref.Domain
			}
		}
		return "capture", ""
	case config.FakePayloadDomain:
		return "generated", set.Faking.PayloadDomain
	case config.FakePayloadCustom:
		return "custom", ""
	case config.FakePayloadRandom:
		return "random", ""
	case config.FakePayloadZero:
		return "zero", ""
	case config.FakePayloadInverted:
		return "inverted", ""
	case config.FakePayloadSTUN:
		return "stun", ""
	}
	return "other", ""
}

func active(mode, off string) (string, bool) {
	m := strings.ToLower(mode)
	return m, m != "" && m != off
}

func Techniques(set *config.SetConfig, payloads []hubwire.BlobRef) []Term {
	out := make([]Term, 0, 8)
	if set.Faking.SNI {
		payload, domain := payloadCode(set, payloads)
		p := map[string]interface{}{"payload": payload, "strategy": set.Faking.Strategy}
		if domain != "" {
			p["domain"] = domain
		}
		if set.Faking.TTL > 0 {
			p["ttl"] = set.Faking.TTL
		}
		if set.Faking.SNISeqLength > 1 {
			p["copies"] = set.Faking.SNISeqLength
		}
		out = append(out, term(TechFake, p))
		if mode, ok := active(set.Faking.SNIMutation.Mode, config.ConfigOff); ok {
			out = append(out, term(TechSNIMutation, map[string]interface{}{"mode": mode}))
		}
		if len(set.Faking.TLSMod) > 0 {
			out = append(out, term(TechTLSMod, map[string]interface{}{"mods": strings.Join(set.Faking.TLSMod, ", ")}))
		}
	}
	if strategy, ok := active(set.Fragmentation.Strategy, config.ConfigNone); ok {
		p := map[string]interface{}{"strategy": strategy}
		if len(set.Fragmentation.StrategyPool) > 0 {
			p["pool"] = strings.Join(set.Fragmentation.StrategyPool, ", ")
		}
		if set.Fragmentation.MiddleSNI {
			p["middle_sni"] = true
		}
		if set.Fragmentation.ReverseOrder {
			p["reverse"] = true
		}
		out = append(out, term(TechFrag, p))
	}
	if set.TCP.SynFake {
		out = append(out, term(TechSynFake, nil))
	}
	if mode, ok := active(set.TCP.Desync.Mode, config.ConfigOff); ok {
		p := map[string]interface{}{"mode": mode}
		if set.TCP.Desync.Count > 1 {
			p["count"] = set.TCP.Desync.Count
		}
		out = append(out, term(TechDesync, p))
	}
	if mode, ok := active(set.TCP.Incoming.Mode, config.ConfigOff); ok {
		out = append(out, term(TechIncoming, map[string]interface{}{"mode": mode}))
	}
	if mode, ok := active(set.TCP.Win.Mode, config.ConfigOff); ok {
		out = append(out, term(TechWindow, map[string]interface{}{"mode": mode}))
	}
	if set.TCP.Duplicate.Enabled {
		out = append(out, term(TechDuplicate, map[string]interface{}{"count": set.TCP.Duplicate.Count}))
	}
	if set.TCP.RSTProtection.Enabled {
		out = append(out, term(TechRSTProtection, nil))
	}
	if set.TCP.DropSACK {
		out = append(out, term(TechDropSACK, nil))
	}
	if set.TCP.HTTPMethodEOL {
		out = append(out, term(TechHTTPMethodEOL, nil))
	}
	if set.TCP.Seg2Delay > 0 {
		out = append(out, term(TechSeg2Delay, map[string]interface{}{"ms": set.TCP.Seg2Delay}))
	}
	if mode, ok := active(set.UDP.Mode, config.ConfigNone); ok {
		p := map[string]interface{}{"mode": mode}
		if mode == config.UDPModeFake {
			switch set.UDP.FakePayloadFile {
			case "":
				p["payload"] = "zero"
			case config.FakePayloadAutoQUIC:
				p["payload"] = "quic_initial"
			case config.FakePayloadPreset1, config.FakePayloadPreset2:
				p["payload"] = "quic_preset"
			default:
				p["payload"] = "quic_capture"
			}
		}
		out = append(out, term(TechUDP, p))
	}
	if set.DNS.Enabled {
		p := map[string]interface{}{}
		if host, _ := hubwire.DoHHost(set.DNS.DoHURL); host != "" {
			p["host"] = host
		}
		if set.DNS.Strict {
			p["strict"] = true
		}
		if set.DNS.FragmentQuery {
			p["fragment"] = true
		}
		if n := len(set.DNS.Pins); n > 0 {
			p["pins"] = n
		}
		out = append(out, term(TechDNS, p))
	}
	if set.Routing.Enabled && set.Routing.Mode == config.RoutingModeBlock {
		p := map[string]interface{}{}
		if set.Routing.BlockAction != "" {
			p["action"] = set.Routing.BlockAction
		}
		out = append(out, term(TechBlock, p))
	}
	if set.MSSClamp.Enabled {
		out = append(out, term(TechMSSClamp, map[string]interface{}{"size": set.MSSClamp.Size}))
	}
	if len(out) == 0 {
		out = append(out, term(TechPassthrough, nil))
	}
	return out
}

func (t Targets) FilterTerms() []Term {
	out := make([]Term, 0, 3)
	if t.TLSVersion != "" {
		out = append(out, term(FilterTLSOnly, map[string]interface{}{"version": t.TLSVersion}))
	}
	if t.IPVersion != "" {
		out = append(out, term(FilterIPOnly, map[string]interface{}{"version": t.IPVersion}))
	}
	if t.DomainOnly {
		out = append(out, term(FilterDomainOnly, nil))
	}
	return out
}
