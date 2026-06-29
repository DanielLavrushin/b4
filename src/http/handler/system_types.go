package handler

type VersionInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
}

type RestartResponse struct {
	Success        bool   `json:"success"`
	Message        string `json:"message"`
	ServiceManager string `json:"service_manager"`
	RestartCommand string `json:"restart_command,omitempty"`
}

type SystemInfo struct {
	ServiceManager string `json:"service_manager"`
	OS             string `json:"os"`
	Arch           string `json:"arch"`
	CanRestart     bool   `json:"can_restart"`
	IsDocker       bool   `json:"is_docker"`
}

type UpdateRequest struct {
	Version string `json:"version,omitempty"`
}

type UpdateResponse struct {
	Success        bool   `json:"success"`
	Message        string `json:"message"`
	ServiceManager string `json:"service_manager"`
	UpdateCommand  string `json:"update_command,omitempty"`
}

type DiagnosticsResponse struct {
	Success bool        `json:"success"`
	Data    Diagnostics `json:"data"`
}

type Diagnostics struct {
	System     DiagSystem      `json:"system"`
	B4         DiagB4          `json:"b4"`
	Kernel     DiagKernel      `json:"kernel"`
	Tools      DiagTools       `json:"tools"`
	Network    DiagNetwork     `json:"network"`
	Engine     DiagEngine      `json:"engine"`
	Firewall   DiagFirewall    `json:"firewall"`
	Geodata    DiagGeodata     `json:"geodata"`
	Storage    []DiagMount     `json:"storage"`
	Paths      DiagPaths       `json:"paths"`
}

type DiagSystem struct {
	Hostname string `json:"hostname"`
	Distro   string `json:"distro,omitempty"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Kernel   string `json:"kernel"`
	CPUCores int    `json:"cpu_cores"`
	MemTotal uint64 `json:"mem_total_mb"`
	MemAvail uint64 `json:"mem_avail_mb"`
	IsDocker bool   `json:"is_docker"`
}

type DiagB4 struct {
	Version        string `json:"version"`
	Commit         string `json:"commit"`
	BuildDate      string `json:"build_date"`
	ServiceManager string `json:"service_manager"`
	ConfigPath     string `json:"config_path"`
	Running        bool   `json:"running"`
	PID            int    `json:"pid,omitempty"`
	MemoryMB       string `json:"memory_mb,omitempty"`
	Uptime         string `json:"uptime,omitempty"`
}

type DiagPaths struct {
	Binary   string `json:"binary"`
	Config   string `json:"config"`
	ErrorLog string `json:"error_log,omitempty"`
	Geosite  string `json:"geosite,omitempty"`
	Geoip    string `json:"geoip,omitempty"`
	DataDir  string `json:"data_dir,omitempty"`
}

type DiagNetwork struct {
	Interfaces []DiagInterface `json:"interfaces"`
}

type DiagInterface struct {
	Name  string   `json:"name"`
	MAC   string   `json:"mac,omitempty"`
	Addrs []string `json:"addrs,omitempty"`
	Up    bool     `json:"up"`
	MTU   int      `json:"mtu"`
}

type DiagEngine struct {
	Mode string   `json:"mode"`
	TUN  *DiagTUN `json:"tun,omitempty"`
}

type DiagTUN struct {
	Running          bool   `json:"running"`
	DeviceName       string `json:"device_name"`
	DeviceUp         bool   `json:"device_up"`
	MTU              int    `json:"mtu,omitempty"`
	Address          string `json:"address,omitempty"`
	AddressV6        string `json:"address_v6,omitempty"`
	OutInterface     string `json:"out_interface,omitempty"`
	OutGateway       string `json:"out_gateway,omitempty"`
	ResolvedSrc      string `json:"resolved_src,omitempty"`
	Capture          string `json:"capture,omitempty"`
	RouteTable       int    `json:"route_table,omitempty"`
	ReplyCapture     bool   `json:"reply_capture"`
	PacketsForwarded uint64 `json:"packets_forwarded"`
	ForwardErrors    uint64 `json:"forward_errors"`
	IPv6Dropped      uint64 `json:"ipv6_dropped"`
}

type DiagFirewall struct {
	Backend      string          `json:"backend"`
	NFQueueWorks bool            `json:"nfqueue_works"`
	FlowOffload  string          `json:"flow_offload"`
	RuleGroups   []DiagRuleGroup `json:"rule_groups,omitempty"`
}

type DiagRuleGroup struct {
	Title string   `json:"title"`
	Rules []string `json:"rules"`
}

type DiagGeodata struct {
	GeositeConfigured bool   `json:"geosite_configured"`
	GeositePath       string `json:"geosite_path,omitempty"`
	GeositeSize       string `json:"geosite_size,omitempty"`
	GeoipConfigured   bool   `json:"geoip_configured"`
	GeoipPath         string `json:"geoip_path,omitempty"`
	GeoipSize         string `json:"geoip_size,omitempty"`
	TotalDomains      int    `json:"total_domains"`
	TotalIPs          int    `json:"total_ips"`
}

type DiagKernel struct {
	Modules      []DiagModule     `json:"modules"`
	Capabilities []DiagCapability `json:"capabilities,omitempty"`
}

type DiagModule struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type DiagCapability struct {
	Name      string   `json:"name"`
	Available bool     `json:"available"`
	Missing   []string `json:"missing,omitempty"`
	Packages  []string `json:"packages,omitempty"`
	Detail    string   `json:"detail,omitempty"`
}

type DiagTools struct {
	Firewall []DiagTool `json:"firewall"`
	Required []DiagTool `json:"required"`
	Optional []DiagTool `json:"optional"`
}

type DiagTool struct {
	Name   string `json:"name"`
	Found  bool   `json:"found"`
	Detail string `json:"detail,omitempty"`
}

type DiagMount struct {
	Path      string `json:"path"`
	Available string `json:"available"`
	Writable  bool   `json:"writable"`
}
