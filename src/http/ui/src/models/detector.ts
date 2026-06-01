export type DetectorTestType =
  | "dns"
  | "dns-availability"
  | "domains"
  | "tcp"
  | "sni"
  | "telegram";
export type SuiteStatus = "pending" | "running" | "complete" | "failed" | "canceled";

// DNS types
export type DNSStatus =
  | "OK"
  | "DNS_SPOOFING"
  | "DNS_INTERCEPTION"
  | "FAKE_IP"
  | "FAKE_NXDOMAIN"
  | "FAKE_EMPTY"
  | "DOH_BLOCKED"
  | "BOTH_UNAVAILABLE"
  | "TIMEOUT"
  | "BLOCKED";

export interface DNSDomainResult {
  domain: string;
  doh_ip: string;
  udp_ip: string;
  status: DNSStatus;
  is_stub_ip?: boolean;
}

export interface DNSResult {
  status: DNSStatus;
  doh_server: string;
  udp_server: string;
  doh_blocked: boolean;
  udp_blocked: boolean;
  stub_ips?: string[];
  domains: DNSDomainResult[];
  summary: string;
  spoof_count: number;
  intercept_count: number;
  fakeip_count: number;
  ok_count: number;
}

// DNS availability types
export type DNSAvailKind = "doh" | "udp";

export interface DNSAvailProviderResult {
  provider: string;
  kind: DNSAvailKind;
  address: string;
  avg_ms: number;
  ok: boolean;
  ok_count: number;
  total: number;
}

export interface DNSAvailResult {
  providers: DNSAvailProviderResult[];
  doh_ok: number;
  doh_total: number;
  udp_ok: number;
  udp_total: number;
  summary: string;
}

// Domain accessibility types
export type DomainStatus =
  | "OK"
  | "TLS_DPI"
  | "TLS_MITM"
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
  | "ERROR";

export interface TLSProbeResult {
  status: DomainStatus;
  detail?: string;
  latency_ms: number;
}

export interface HTTPProbeResult {
  status: DomainStatus;
  detail?: string;
  status_code?: number;
  redirect_to?: string;
}

export interface DomainCheckResult {
  domain: string;
  ip: string;
  tls13?: TLSProbeResult;
  tls12?: TLSProbeResult;
  http?: HTTPProbeResult;
  is_fake_ip?: boolean;
  overall: DomainStatus;
}

export interface DomainsResult {
  domains: DomainCheckResult[];
  blocked_count: number;
  ok_count: number;
  dpi_count: number;
  summary: string;
}

// TCP fat probe types
export type TCPStatus = "OK" | "DETECTED" | "MIXED" | "TIMEOUT" | "ERROR";

export interface TCPTarget {
  id: string;
  ip: string;
  port: number;
  asn: string;
  provider: string;
  sni?: string;
}

export interface TCPTargetResult {
  target: TCPTarget;
  status: TCPStatus;
  alive: boolean;
  drop_at_kb?: number;
  rtt_ms?: number;
  detail?: string;
}

export interface TCPResult {
  targets: TCPTargetResult[];
  detected_count: number;
  ok_count: number;
  summary: string;
}

// SNI whitelist brute-force types
export type SNIStatus = "FOUND" | "NOT_FOUND" | "NOT_BLOCKED";

export interface SNIASNResult {
  asn: string;
  provider: string;
  ip: string;
  found_sni?: string;
  status: SNIStatus;
}

export interface SNIResult {
  asn_results: SNIASNResult[];
  found_count: number;
  tested_count: number;
  summary: string;
}

// Telegram types
export type TelegramVerdict = "ok" | "slow" | "stalled" | "blocked" | "partial" | "error";

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
  dc_pings: TelegramDCPing[];
  dc_reachable: number;
  dc_total: number;
  verdict: TelegramVerdict;
  summary: string;
}

// Overall suite
export interface DetectorSuite {
  id: string;
  status: SuiteStatus;
  start_time: string;
  end_time?: string;
  tests: DetectorTestType[];
  current_test?: DetectorTestType;
  total_checks: number;
  completed_checks: number;
  dns_result?: DNSResult;
  dnsavail_result?: DNSAvailResult;
  domains_result?: DomainsResult;
  tcp_result?: TCPResult;
  sni_result?: SNIResult;
  telegram_result?: TelegramResult;
}

export interface DetectorResponse {
  id: string;
  tests: string[];
  estimated_tests: number;
  message: string;
}

export interface DetectorHistoryEntry {
  id: string;
  status: SuiteStatus;
  tests: DetectorTestType[];
  start_time: string;
  end_time: string;
  dns_result?: DNSResult;
  dnsavail_result?: DNSAvailResult;
  domains_result?: DomainsResult;
  tcp_result?: TCPResult;
  sni_result?: SNIResult;
  telegram_result?: TelegramResult;
}
