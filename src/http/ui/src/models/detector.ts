export type DetectorScope = "sites" | "dns" | "hosting" | "telegram";
export type SuiteStatus = "pending" | "running" | "complete" | "failed" | "canceled";
export type FetchMode = "both" | "direct";
export type IPVersion = "ipv4" | "ipv6" | "both";

export interface DetectorOptions {
  sites: string[];
  scopes: DetectorScope[];
  ip_version?: IPVersion;
  parallel?: number;
  fetch_mode?: FetchMode;
  skip_tls12?: boolean;
  sni_search?: boolean;
}

export interface DetectorProgress {
  phase?: DetectorScope | "";
  done: number;
  total: number;
  current?: string;
}

export interface NetworkInfo {
  wan_ip?: string;
  asn?: string;
  org?: string;
  country?: string;
  ipv6: boolean;
}

export type FetchStatus =
  | "OK"
  | "TLS_DPI"
  | "TLS_MITM"
  | "MTLS"
  | "TLS_SPOOF"
  | "TLS_ALERT"
  | "TLS_RST"
  | "TLS_DROP"
  | "SYN_DROP"
  | "TCP16"
  | "ISP_PAGE"
  | "BLOCKED"
  | "DNS_FAKE"
  | "TIMEOUT"
  | "ERROR"
  | "SERVER_ERROR"
  | "PENDING"
  | "CHECKING"
  | "SKIPPED";

export interface Fetch {
  status: FetchStatus;
  detail?: string;
  latency_ms?: number;
  bytes?: number;
  status_code?: number;
  redirect_to?: string;
  tls12?: FetchStatus;
  http?: FetchStatus;
  http_detail?: string;
}

export type SiteOutcome =
  | "pending"
  | "ok"
  | "fixed"
  | "still_blocked"
  | "blocked"
  | "broken_by_b4"
  | "server"
  | "error";

export interface SiteResult {
  input: string;
  domain: string;
  url: string;
  family?: "ipv4" | "ipv6";
  ip?: string;
  honest_ip?: string;
  fake_dns?: boolean;
  alt_works?: boolean;
  direct?: Fetch;
  through_b4?: Fetch;
  outcome: SiteOutcome;
  set_id?: string;
  set_name?: string;
  set_enabled?: boolean;
  set_dns?: boolean;
  done: boolean;
}

export interface SitesResult {
  sites: SiteResult[];
  ok: number;
  blocked: number;
  fixed: number;
  still_blocked: number;
  broken_by_b4: number;
  server: number;
  errors: number;
  stub_ips?: string[];
}

export type DNSProbeStatus = "ok" | "timeout" | "blocked" | "error";
export type DNSHonesty = "honest" | "substituted" | "filtered" | "differs" | "unknown";

export interface DNSProbe {
  address: string;
  status: DNSProbeStatus;
  latency_ms?: number;
  honesty?: DNSHonesty;
  substituted?: number;
  checked?: number;
  answered_by?: string;
  answered_by_asn?: string;
  answered_by_org?: string;
  hijacked?: boolean;
  detail?: string;
}

export interface DNSProvider {
  name: string;
  router?: boolean;
  udp?: DNSProbe;
  doh?: DNSProbe;
  dot?: DNSProbe;
}

export interface DNSResult {
  providers: DNSProvider[];
  udp_ok: number;
  udp_total: number;
  doh_ok: number;
  doh_total: number;
  dot_ok: number;
  dot_total: number;
  hijacked: number;
  hijacked_by?: string;
  hijacked_by_asn?: string;
  substituting: number;
  honest_doh?: string[];
  stub_ips?: string[];
  truth_available: boolean;
  router_servers?: string[];
}

export type HostingStatus = "" | "ok" | "dropped" | "mixed" | "timeout" | "error";

export interface TCPTarget {
  id: string;
  ip: string;
  port: number;
  asn: string;
  provider: string;
  sni?: string;
  reference?: boolean;
}

export interface TargetResult {
  target: TCPTarget;
  status: HostingStatus;
  drop_at_kb?: number;
  rtt_ms?: number;
  detail?: string;
  done: boolean;
}

export interface HostingGroup {
  asn: string;
  provider: string;
  reference?: boolean;
  status: HostingStatus;
  total: number;
  dropped: number;
  ok: number;
  timeouts: number;
  drop_min_kb?: number;
  drop_max_kb?: number;
  working_snis?: string[];
  sni_searched?: boolean;
  targets: TargetResult[];
}

export interface HostingResult {
  groups: HostingGroup[];
  dropped_groups: number;
  ok_groups: number;
  total: number;
  dropped: number;
  ok: number;
}

export type TelegramVerdict = "ok" | "slow" | "stalled" | "blocked" | "partial" | "error" | "";

export interface TelegramThroughput {
  verdict: TelegramVerdict;
  bytes: number;
  expected: number;
  pct_ok: number;
  duration_ms: number;
  mbps_avg: number;
  mbps_peak: number;
  drop_at_sec?: number;
  detail?: string;
}

export interface TelegramDCPing {
  dc: number;
  address: string;
  ok: boolean;
  rtt_ms?: number;
}

export interface TelegramResult {
  download: TelegramThroughput;
  upload: TelegramThroughput;
  dc_pings: TelegramDCPing[];
  dc_reachable: number;
  dc_total: number;
  verdict: TelegramVerdict;
}

export interface DetectorVerdict {
  blocked_by_isp: number;
  fixed_by_b4: number;
  still_blocked: number;
  broken_by_b4: number;
  not_blocked: number;
  sites: number;
  block_kinds?: Record<string, number>;
  still_blocked_sites?: string[];
  dns_hijacked: boolean;
  dns_substituted: boolean;
  doh_works: boolean;
  dot_works: boolean;
  dropped_networks?: string[];
  telegram?: string;
}

export interface DetectorSuite {
  id: string;
  status: SuiteStatus;
  start_time: string;
  end_time?: string;
  options: DetectorOptions;
  progress: DetectorProgress;
  lists_date?: string;
  network?: NetworkInfo;
  sites?: SitesResult;
  dns?: DNSResult;
  hosting?: HostingResult;
  telegram?: TelegramResult;
  verdict: DetectorVerdict;
}

export interface DetectorStartResponse {
  id: string;
  total: number;
  message: string;
}

export interface DetectorLists {
  lists_date: string;
  lists_source: string;
  embedded_date: string;
  custom: boolean;
  sites: string[];
  site_count: number;
  dns_servers: number;
  tcp_targets: number;
  whitelist_sni: number;
}
