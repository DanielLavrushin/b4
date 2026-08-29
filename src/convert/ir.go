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
	AnchorSNIExt Anchor = "sniext"
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
	Token   int    `json:"-"`
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
	SplitIPFrag   SplitKind = "ipfrag"
	SplitExt      SplitKind = "extsplit"
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
	Protos    []string `json:"protos"`
	Hosts     []string `json:"hosts"`
	HostsRef  string   `json:"hosts_ref"`
	Excluded  []string `json:"excluded"`
	IPs       []string `json:"ips"`
	IPsRef    string   `json:"ips_ref"`
	PortMin   int      `json:"port_min"`
	PortMax   int      `json:"port_max"`
	TCPPorts  []string `json:"tcp_ports"`
	UDPPorts  []string `json:"udp_ports"`
	IPVersion string   `json:"ip_version"`
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
		len(f.IPs) == 0 && f.IPsRef == "" && f.PortMin == 0 &&
		len(f.TCPPorts) == 0 && len(f.UDPPorts) == 0 && f.IPVersion == ""
}

type FakeOp struct {
	Present      bool     `json:"present"`
	Pos          Pos      `json:"pos"`
	TTL          int      `json:"ttl"`
	TTLSet       bool     `json:"ttl_set"`
	Fooling      []string `json:"fooling"`
	Repeats      int      `json:"repeats"`
	SeqIncrement int      `json:"seq_increment"`
	TSIncrement  int      `json:"ts_increment"`
	QUICRef      string   `json:"quic_ref"`
	MD5Sig       bool     `json:"md5sig"`
	IPOpt        bool     `json:"ip_opt"`
	DataInline   string   `json:"data_inline"`
	DataRef      string   `json:"data_ref"`
	SNIs         []string `json:"snis"`
	TLSMod       []string `json:"tls_mod"`
	TLSSize      int      `json:"tls_size"`
	TLSSizeSet   bool     `json:"tls_size_set"`
	Offset       int      `json:"offset"`
	OffsetSet    bool     `json:"offset_set"`
}

type UDPOp struct {
	FakeCount int      `json:"fake_count"`
	Present   bool     `json:"present"`
	Repeats   int      `json:"repeats"`
	QUICRef   string   `json:"quic_ref"`
	TTL       int      `json:"ttl"`
	TTLSet    bool     `json:"ttl_set"`
	Ports     []string `json:"ports"`
}

func (u UDPOp) Empty() bool {
	return u.FakeCount == 0 && !u.Present && u.Repeats == 0 &&
		u.QUICRef == "" && !u.TTLSet && len(u.Ports) == 0
}

type DesyncOp struct {
	Mode    string `json:"mode"`
	Repeats int    `json:"repeats"`
}

type SynFakeOp struct {
	Enabled bool `json:"enabled"`
	Len     int  `json:"len"`
}

type SeqOvlOp struct {
	Length  int    `json:"length"`
	Pattern string `json:"pattern"`
}

func (p *Profile) UDPOnly() bool {
	if len(p.Filters.UDPPorts) > 0 && len(p.Filters.TCPPorts) == 0 {
		return true
	}
	if p.Filters.HasProto("quic") || p.Filters.HasProto("stun") {
		return !p.Filters.HasProto("tls") && !p.Filters.HasProto("http")
	}
	return len(p.Filters.Protos) > 0 && p.Filters.HasProto("udp") &&
		!p.Filters.HasProto("tls") && !p.Filters.HasProto("http")
}

func (p *Profile) IsEntry() bool { return p.Trigger.Kind != TriggerDetect }

func (p *Profile) carriesNothing() bool {
	return len(p.Splits) == 0 && !p.Fake.Present && p.UDP.Empty() &&
		!p.DropSACK && !p.OOBSet && len(p.HTTPMod) == 0 &&
		len(p.DesyncModes) == 0 && p.Desync.Mode == "" && !p.SynFake.Enabled &&
		p.Duplicate == 0 && p.WinSize == 0 && p.SeqOvl.Length == 0 && !p.Skip
}

func (p *Profile) carriesStrategy() bool {
	return len(p.Splits) > 0 || p.Fake.Present || p.DropSACK || p.OOBSet ||
		len(p.HTTPMod) > 0 || len(p.DesyncModes) > 0 || p.Desync.Mode != "" ||
		p.SynFake.Enabled || p.Duplicate > 0 || p.WinSize > 0 || p.SeqOvl.Length > 0 ||
		p.UDP.Present || p.UDP.FakeCount > 0 || p.UDP.QUICRef != "" || p.UDP.Repeats > 0
}

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

	Desync    DesyncOp  `json:"desync"`
	SynFake   SynFakeOp `json:"syn_fake"`
	SeqOvl    SeqOvlOp  `json:"seq_ovl"`
	Duplicate int       `json:"duplicate"`
	WinSize   int       `json:"win_size"`
	Skip      bool      `json:"skip"`

	DesyncModes    []string `json:"desync_modes"`
	SplitPositions []Pos    `json:"split_positions"`

	ProtoTokens       []int `json:"-"`
	FoldedProtoTokens []int `json:"-"`
	FoldedTokens      []int `json:"-"`
	DesyncToken       int   `json:"-"`
	SplitPosToken     int   `json:"-"`
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
