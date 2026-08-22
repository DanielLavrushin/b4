package handler

import (
	"fmt"
	"net"
	"strconv"

	"github.com/daniellavrushin/b4/config"
)

const traceDomainSampleLimit = 8

type traceSet struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Targets  traceSetTargets   `json:"targets"`
	TCP      traceSetTCP       `json:"tcp"`
	UDP      traceSetUDP       `json:"udp"`
	Frag     traceSetFrag      `json:"fragmentation"`
	Faking   traceSetFaking    `json:"faking"`
	DNS      *traceSetDNS      `json:"dns,omitempty"`
	Routing  *traceSetRouting  `json:"routing,omitempty"`
	Escalate *traceSetEscalate `json:"escalate,omitempty"`
	MSSClamp int               `json:"mss_clamp,omitempty"`
}

type traceSetTargets struct {
	SNIDomains      int      `json:"sni_domains"`
	IPs             int      `json:"ips"`
	ResolvedDomains int      `json:"resolved_domains"`
	ResolvedIPs     int      `json:"resolved_ips"`
	DomainSample    []string `json:"domain_sample,omitempty"`
	GeoSite         []string `json:"geosite_categories,omitempty"`
	GeoIP           []string `json:"geoip_categories,omitempty"`
	SourceDevices   int      `json:"source_devices,omitempty"`
	SourceExclude   bool     `json:"source_devices_exclude,omitempty"`
	DomainOnly      bool     `json:"domain_only,omitempty"`
	TLSVersion      string   `json:"tls,omitempty"`
	IPVersion       string   `json:"ip_version,omitempty"`
}

type traceSetTCP struct {
	ConnBytesLimit int    `json:"conn_bytes_limit"`
	DPortFilter    string `json:"dport_filter,omitempty"`
	Seg2Delay      int    `json:"seg2delay,omitempty"`
	SynFake        bool   `json:"syn_fake,omitempty"`
	DropSACK       bool   `json:"drop_sack,omitempty"`
	Desync         string `json:"desync,omitempty"`
	Win            string `json:"win,omitempty"`
	Incoming       string `json:"incoming,omitempty"`
	Duplicate      int    `json:"duplicate,omitempty"`
	IPBlockDetect  bool   `json:"ip_block_detect,omitempty"`
	RSTProtection  bool   `json:"rst_protection,omitempty"`
}

type traceSetUDP struct {
	Mode            string `json:"mode,omitempty"`
	ConnBytesLimit  int    `json:"conn_bytes_limit"`
	DPortFilter     string `json:"dport_filter,omitempty"`
	FilterQUIC      string `json:"filter_quic,omitempty"`
	FilterSTUN      bool   `json:"filter_stun,omitempty"`
	FakeLen         int    `json:"fake_len,omitempty"`
	FakingStrategy  string `json:"faking_strategy,omitempty"`
	FakePayloadFile string `json:"fake_payload_file,omitempty"`
	Seg2Delay       int    `json:"seg2delay,omitempty"`
}

type traceSetFrag struct {
	Strategy     string   `json:"strategy,omitempty"`
	StrategyPool []string `json:"strategy_pool,omitempty"`
	ReverseOrder bool     `json:"reverse_order,omitempty"`
	MiddleSNI    bool     `json:"middle_sni,omitempty"`
	SNIPosition  int      `json:"sni_position,omitempty"`
	TLSRecordPos int      `json:"tlsrec_pos,omitempty"`
}

type traceSetFaking struct {
	SNI           bool   `json:"sni"`
	TTL           uint8  `json:"ttl,omitempty"`
	Strategy      string `json:"strategy,omitempty"`
	SNISeqLength  int    `json:"sni_seq_length,omitempty"`
	SNIType       int    `json:"sni_type"`
	PayloadFile   string `json:"payload_file,omitempty"`
	PayloadDomain string `json:"payload_domain,omitempty"`
	SNIMutation   string `json:"sni_mutation,omitempty"`
	TCPMD5        bool   `json:"tcp_md5,omitempty"`
}

type traceSetDNS struct {
	TargetDNS     string `json:"target_dns,omitempty"`
	DoHURL        string `json:"doh_url,omitempty"`
	FragmentQuery bool   `json:"fragment_query,omitempty"`
	Pins          int    `json:"pins,omitempty"`
}

type traceSetRouting struct {
	Mode              string   `json:"mode"`
	EgressInterface   string   `json:"egress_interface,omitempty"`
	Upstream          string   `json:"upstream,omitempty"`
	UpstreamAuth      bool     `json:"upstream_auth,omitempty"`
	UpstreamUDP       bool     `json:"upstream_udp,omitempty"`
	UpstreamUseDomain bool     `json:"upstream_use_domain,omitempty"`
	FailOpen          bool     `json:"fail_open,omitempty"`
	FWMark            string   `json:"fwmark,omitempty"`
	Table             int      `json:"table,omitempty"`
	SourceInterfaces  []string `json:"source_interfaces,omitempty"`
	IPTTLSeconds      int      `json:"ip_ttl_seconds,omitempty"`
	BlockAction       string   `json:"block_action,omitempty"`
}

type traceSetEscalate struct {
	To     string `json:"to"`
	ToName string `json:"to_name,omitempty"`
}

func collectTraceSets(cfg *config.Config) []traceSet {
	if cfg == nil {
		return nil
	}

	sets := make([]traceSet, 0, len(cfg.Sets))
	for _, set := range cfg.Sets {
		if set == nil || !set.Enabled {
			continue
		}

		ts := traceSet{
			ID:      set.Id,
			Name:    set.Name,
			Targets: traceTargets(set.Targets),
			TCP:     traceTCP(set.TCP),
			UDP:     traceUDP(set.UDP),
			Frag:    traceFrag(set.Fragmentation),
			Faking:  traceFaking(set.Faking),
		}

		if set.DNS.Enabled {
			ts.DNS = &traceSetDNS{
				TargetDNS:     set.DNS.TargetDNS,
				DoHURL:        set.DNS.DoHURL,
				FragmentQuery: set.DNS.FragmentQuery,
				Pins:          len(set.DNS.Pins),
			}
		}

		if set.Routing.Enabled {
			ts.Routing = traceRouting(set.Routing)
		}

		if set.Escalate.Active() {
			e := &traceSetEscalate{To: set.Escalate.To}
			if target := cfg.GetSetById(set.Escalate.To); target != nil {
				e.ToName = target.Name
			}
			ts.Escalate = e
		}

		if set.MSSClamp.Enabled {
			ts.MSSClamp = set.MSSClamp.Size
		}

		sets = append(sets, ts)
	}

	return sets
}

func traceTargets(t config.TargetsConfig) traceSetTargets {
	ts := traceSetTargets{
		SNIDomains:      len(t.SNIDomains),
		IPs:             len(t.IPs),
		ResolvedDomains: len(t.DomainsToMatch),
		ResolvedIPs:     len(t.IpsToMatch),
		GeoSite:         t.GeoSiteCategories,
		GeoIP:           t.GeoIpCategories,
		SourceDevices:   len(t.SourceDevices),
		SourceExclude:   t.SourceDevicesExclude,
		DomainOnly:      t.DomainOnly,
		TLSVersion:      t.TLSVersion,
		IPVersion:       t.IPVersion,
	}

	if n := len(t.SNIDomains); n > 0 {
		if n > traceDomainSampleLimit {
			n = traceDomainSampleLimit
		}
		ts.DomainSample = append(ts.DomainSample, t.SNIDomains[:n]...)
	}

	return ts
}

func traceTCP(c config.TCPConfig) traceSetTCP {
	ts := traceSetTCP{
		ConnBytesLimit: c.ConnBytesLimit,
		DPortFilter:    c.DPortFilter,
		Seg2Delay:      c.Seg2Delay,
		SynFake:        c.SynFake,
		DropSACK:       c.DropSACK,
		Desync:         traceActiveMode(c.Desync.Mode),
		Win:            traceActiveMode(c.Win.Mode),
		Incoming:       traceActiveMode(c.Incoming.Mode),
		IPBlockDetect:  c.IPBlockDetect.Enabled,
		RSTProtection:  c.RSTProtection.Enabled,
	}
	if c.Duplicate.Enabled {
		ts.Duplicate = c.Duplicate.Count
	}
	return ts
}

func traceUDP(c config.UDPConfig) traceSetUDP {
	return traceSetUDP{
		Mode:            traceActiveMode(c.Mode),
		ConnBytesLimit:  c.ConnBytesLimit,
		DPortFilter:     c.DPortFilter,
		FilterQUIC:      c.FilterQUIC,
		FilterSTUN:      c.FilterSTUN,
		FakeLen:         c.FakeLen,
		FakingStrategy:  traceActiveMode(c.FakingStrategy),
		FakePayloadFile: c.FakePayloadFile,
		Seg2Delay:       c.Seg2Delay,
	}
}

func traceFrag(c config.FragmentationConfig) traceSetFrag {
	return traceSetFrag{
		Strategy:     traceActiveMode(c.Strategy),
		StrategyPool: c.StrategyPool,
		ReverseOrder: c.ReverseOrder,
		MiddleSNI:    c.MiddleSNI,
		SNIPosition:  c.SNIPosition,
		TLSRecordPos: c.TLSRecordPosition,
	}
}

func traceFaking(c config.FakingConfig) traceSetFaking {
	return traceSetFaking{
		SNI:           c.SNI,
		TTL:           c.TTL,
		Strategy:      traceActiveMode(c.Strategy),
		SNISeqLength:  c.SNISeqLength,
		SNIType:       c.SNIType,
		PayloadFile:   c.PayloadFile,
		PayloadDomain: c.PayloadDomain,
		SNIMutation:   traceActiveMode(c.SNIMutation.Mode),
		TCPMD5:        c.TCPMD5,
	}
}

func traceRouting(c config.RoutingConfig) *traceSetRouting {
	ts := &traceSetRouting{
		Mode:             c.Mode,
		EgressInterface:  c.EgressInterface,
		SourceInterfaces: c.SourceInterfaces,
		IPTTLSeconds:     c.IPTTLSeconds,
		Table:            c.Table,
	}

	if ts.Mode == "" {
		ts.Mode = config.RoutingModeInterface
	}
	if config.RoutingIsBlock(ts.Mode) {
		ts.BlockAction = config.NormalizeBlockAction(c.BlockAction)
	}
	if c.Upstream.Host != "" {
		ts.Upstream = net.JoinHostPort(c.Upstream.Host, strconv.Itoa(c.Upstream.Port))
		ts.UpstreamAuth = c.Upstream.Username != "" || c.Upstream.Password != ""
		ts.UpstreamUDP = c.Upstream.UDP
		ts.UpstreamUseDomain = c.Upstream.UseDomain
		ts.FailOpen = c.Upstream.FailOpen
	}
	if c.FWMark != 0 {
		ts.FWMark = fmt.Sprintf("0x%x", c.FWMark)
	}

	return ts
}

func traceActiveMode(mode string) string {
	if mode == config.ConfigOff || mode == config.ConfigNone {
		return ""
	}
	return mode
}
