interface DiagSystem {
  hostname: string;
  distro?: string;
  os: string;
  arch: string;
  kernel: string;
  cpu_cores: number;
  mem_total_mb: number;
  mem_avail_mb: number;
  is_docker: boolean;
}

interface DiagB4 {
  version: string;
  commit: string;
  build_date: string;
  service_manager: string;
  config_path: string;
  running: boolean;
  pid?: number;
  memory_mb?: string;
  uptime?: string;
}

interface DiagModule {
  name: string;
  status: string;
}

interface DiagCapability {
  name: string;
  available: boolean;
  missing?: string[];
  packages?: string[];
  detail?: string;
}

interface DiagTool {
  name: string;
  found: boolean;
  detail?: string;
}

interface DiagMount {
  path: string;
  available: string;
  writable: boolean;
}

interface DiagInterface {
  name: string;
  mac?: string;
  addrs?: string[];
  up: boolean;
  mtu: number;
}

interface DiagRuleGroup {
  title: string;
  rules: string[];
}

interface DiagFirewall {
  backend: string;
  nfqueue_works: boolean;
  flow_offload: string;
  rule_groups?: DiagRuleGroup[];
}

interface DiagTUN {
  running: boolean;
  device_name: string;
  device_up: boolean;
  mtu?: number;
  address?: string;
  address_v6?: string;
  out_interface?: string;
  out_gateway?: string;
  resolved_src?: string;
  capture?: string;
  route_table?: number;
  reply_capture: boolean;
  packets_forwarded: number;
  forward_errors: number;
  ipv6_dropped: number;
}

interface DiagEngine {
  mode: string;
  tun?: DiagTUN;
}

interface DiagGeodata {
  geosite_configured: boolean;
  geosite_path?: string;
  geosite_size?: string;
  geoip_configured: boolean;
  geoip_path?: string;
  geoip_size?: string;
  total_domains: number;
  total_ips: number;
}

interface DiagPaths {
  binary: string;
  config: string;
  error_log?: string;
  geosite?: string;
  geoip?: string;
  data_dir?: string;
}

export interface SystemInfoDialogProps {
  open: boolean;
  onClose: () => void;
}

export interface Diagnostics {
  system: DiagSystem;
  b4: DiagB4;
  kernel: { modules: DiagModule[]; capabilities?: DiagCapability[] };
  tools: { firewall: DiagTool[]; required: DiagTool[]; optional: DiagTool[] };
  network: { interfaces: DiagInterface[] };
  engine: DiagEngine;
  firewall: DiagFirewall;
  geodata: DiagGeodata;
  storage: DiagMount[];
  paths: DiagPaths;
}
