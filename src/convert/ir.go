package convert

type Status string

const (
	StatusMapped        Status = "mapped"
	StatusApproximated  Status = "approximated"
	StatusUnsupported   Status = "unsupported"
	StatusNotApplicable Status = "not_applicable"
	StatusDegenerate    Status = "degenerate"
	StatusUnknown       Status = "unknown"
	StatusInvalid       Status = "invalid"
)

type Anchor string

const (
	AnchorAbs    Anchor = "abs"
	AnchorSNI    Anchor = "sni"
	AnchorHost   Anchor = "host"
	AnchorPacket Anchor = "packet"
)

type Rel string

const (
	RelStart Rel = "start"
	RelEnd   Rel = "end"
	RelMid   Rel = "mid"
	RelRand  Rel = "rand"
)

type Pos struct {
	Raw     string `json:"raw"`
	Offset  int    `json:"offset"`
	Repeats int    `json:"repeats"`
	Skip    int    `json:"skip"`
	Anchor  Anchor `json:"anchor"`
	Rel     Rel    `json:"rel"`
}

func (p Pos) IsAbs() bool { return p.Anchor == AnchorAbs }

type SplitKind string

const (
	SplitPlain    SplitKind = "split"
	SplitDisorder SplitKind = "disorder"
	SplitOOB      SplitKind = "oob"
	SplitDisOOB   SplitKind = "disoob"
	SplitFake     SplitKind = "fake"
	SplitTLSRec   SplitKind = "tlsrec"
)

type SplitOp struct {
	Kind  SplitKind `json:"kind"`
	Pos   Pos       `json:"pos"`
	Token int       `json:"token"`
}

type TriggerKind string

const (
	TriggerDefault TriggerKind = "default"
	TriggerNone    TriggerKind = "none"
	TriggerDetect  TriggerKind = "detect"
)

type Trigger struct {
	Kind       TriggerKind `json:"kind"`
	OnRST      bool        `json:"on_rst"`
	OnRedirect bool        `json:"on_redirect"`
	OnTLSErr   bool        `json:"on_tls_err"`
}

type Filters struct {
	Protos   []string `json:"protos"`
	Hosts    []string `json:"hosts"`
	HostsRef string   `json:"hosts_ref"`
	IPs      []string `json:"ips"`
	IPsRef   string   `json:"ips_ref"`
	PortMin  int      `json:"port_min"`
	PortMax  int      `json:"port_max"`
}

func (f Filters) HasProto(p string) bool {
	for _, v := range f.Protos {
		if v == p {
			return true
		}
	}
	return false
}

func (f Filters) Empty() bool {
	return len(f.Protos) == 0 && len(f.Hosts) == 0 && f.HostsRef == "" &&
		len(f.IPs) == 0 && f.IPsRef == "" && f.PortMin == 0
}

type FakeOp struct {
	Present    bool     `json:"present"`
	Pos        Pos      `json:"pos"`
	TTL        int      `json:"ttl"`
	TTLSet     bool     `json:"ttl_set"`
	MD5Sig     bool     `json:"md5sig"`
	IPOpt      bool     `json:"ip_opt"`
	DataInline string   `json:"data_inline"`
	DataRef    string   `json:"data_ref"`
	SNIs       []string `json:"snis"`
	TLSMod     []string `json:"tls_mod"`
	TLSSize    int      `json:"tls_size"`
	TLSSizeSet bool     `json:"tls_size_set"`
	Offset     int      `json:"offset"`
	OffsetSet  bool     `json:"offset_set"`
}

type UDPOp struct {
	FakeCount int `json:"fake_count"`
}

func (p *Profile) UDPOnly() bool {
	return len(p.Filters.Protos) > 0 && p.Filters.HasProto("udp") &&
		!p.Filters.HasProto("tls") && !p.Filters.HasProto("http")
}

func (p *Profile) IsEntry() bool { return p.Trigger.Kind != TriggerDetect }

func (p *Profile) hasOwnTargets() bool {
	return len(p.Filters.Hosts) > 0 || p.Filters.HostsRef != "" ||
		len(p.Filters.IPs) > 0 || p.Filters.IPsRef != ""
}

type Profile struct {
	Index    int       `json:"index"`
	Trigger  Trigger   `json:"trigger"`
	Filters  Filters   `json:"filters"`
	Splits   []SplitOp `json:"splits"`
	Fake     FakeOp    `json:"fake"`
	UDP      UDPOp     `json:"udp"`
	HTTPMod  []string  `json:"http_mod"`
	OOBByte  byte      `json:"oob_byte"`
	OOBSet   bool      `json:"oob_set"`
	DropSACK bool      `json:"drop_sack"`
	RoundMin int       `json:"round_min"`
	RoundMax int       `json:"round_max"`
	TLSMinor int       `json:"tls_minor"`
	Tokens   []int     `json:"tokens"`

	ProtoTokens       []int `json:"-"`
	FoldedProtoTokens []int `json:"-"`
}

type Globals struct {
	NoDomain  bool   `json:"no_domain"`
	NoIPv6    bool   `json:"no_ipv6"`
	NoUDP     bool   `json:"no_udp"`
	DefTTL    int    `json:"def_ttl"`
	TimeoutMs int    `json:"timeout_ms"`
	DelayMs   int    `json:"delay_ms"`
	FakeSNI   string `json:"fake_sni"`
	AutoMode  string `json:"auto_mode"`
	Tokens    []int  `json:"tokens"`
}

type Program struct {
	Tool     string     `json:"tool"`
	Version  string     `json:"version"`
	Globals  Globals    `json:"globals"`
	Profiles []*Profile `json:"profiles"`
}

func (p *Program) current() *Profile {
	if len(p.Profiles) == 0 {
		p.Profiles = append(p.Profiles, &Profile{Index: 0, Trigger: Trigger{Kind: TriggerDefault}})
	}
	return p.Profiles[len(p.Profiles)-1]
}

func (p *Program) newProfile(t Trigger) *Profile {
	p.current()
	prof := &Profile{Index: len(p.Profiles), Trigger: t}
	p.Profiles = append(p.Profiles, prof)
	return prof
}
