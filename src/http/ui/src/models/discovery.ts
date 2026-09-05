import { B4SetConfig } from "@models/config";

export type StrategyFamily =
  | "none"
  | "tcp_frag"
  | "tls_record"
  | "oob"
  | "ip_frag"
  | "fake_sni"
  | "sack"
  | "syn_fake"
  | "desync"
  | "delay"
  | "disorder"
  | "overlap"
  | "extsplit"
  | "firstbyte"
  | "combo"
  | "hybrid"
  | "window"
  | "mutation"
  | "incoming"
  | "tcpmd5"
  | "alt_address"
  | "dns_redirect";

export type DiscoveryPhase =
  | "baseline"
  | "cached"
  | "strategy_detection"
  | "optimization"
  | "dns_detection"
  | "combination"
  | "confirmation";

export type DiscoveryOutcome =
  | "found"
  | "works_without_bypass"
  | "address_blocked"
  | "not_found";

export type DiscoverySource = "web" | "watchdog" | "mcp";

export type DiscoveryStatus =
  | "pending"
  | "running"
  | "complete"
  | "failed"
  | "canceled";

export interface DomainPresetResult {
  preset_name: string;
  family?: StrategyFamily;
  phase?: DiscoveryPhase;
  priority?: number;
  status: "complete" | "failed";
  duration: number;
  speed: number;
  bytes_read: number;
  error?: string;
  status_code: number;
  confirmed?: number;
  confirm_tries?: number;
  set?: B4SetConfig;
}

export interface BackendStrategyGroup {
  winner_preset: string;
  family: StrategyFamily;
  domains: string[];
  set?: B4SetConfig;
  median_speed?: number;
}

export interface AltScanSummary {
  resolver: string;
  regions: number;
  answered: number;
  addresses: number;
  reachable: number;
}

export interface DNSDiscoveryResult {
  is_poisoned: boolean;
  transport_blocked?: boolean;
  expected_ips?: string[];
  best_server?: string;
  best_doh_url?: string;
  needs_fragment: boolean;
  alternative_ips?: string[];
  alt_scan?: AltScanSummary;
}

export interface DiscoveryResult {
  domain: string;
  url?: string;
  best_preset: string;
  best_speed: number;
  best_success: boolean;
  results: Record<string, DomainPresetResult>;
  baseline_speed?: number;
  baseline_works?: boolean;
  confirmed?: number;
  confirm_tries?: number;
  final_host?: string;
  dns_result?: DNSDiscoveryResult;
  outcome?: DiscoveryOutcome;
  unconfirmed?: boolean;
}

export interface DiscoverySuite {
  id: string;
  status: DiscoveryStatus;
  start_time: string;
  end_time: string;
  total_checks: number;
  completed_checks: number;
  current_phase?: DiscoveryPhase;
  current_domain?: string;
  domains?: { domain: string; check_url: string }[];
  domain_discovery_results?: Record<string, DiscoveryResult>;
  strategy_groups?: BackendStrategyGroup[];
  source?: DiscoverySource;
  runtime_active?: boolean;
}

export interface DiscoveryRuntimeState {
  runtime_active: boolean;
}

export type DiscoveryCurrent = DiscoverySuite | DiscoveryRuntimeState | null;

export const isSuite = (
  current: DiscoveryCurrent,
): current is DiscoverySuite => !!current && "id" in current;

export interface DiscoveryResponse {
  id: string;
  estimated_tests: number;
  message: string;
  domain: string;
  domains?: string[];
  check_url: string;
}

export interface HistoryEntry {
  domain: string;
  url: string;
  best_preset: string;
  best_speed: number;
  best_success: boolean;
  best_family?: StrategyFamily;
  status: "complete" | "failed" | "canceled";
  start_time: string;
  end_time: string;
  results?: Record<string, DomainPresetResult>;
  dns_result?: DNSDiscoveryResult;
  baseline_speed?: number;
  baseline_works?: boolean;
  confirmed?: number;
  confirm_tries?: number;
  final_host?: string;
  suite_id?: string;
  set?: B4SetConfig;
  outcome?: DiscoveryOutcome;
  unconfirmed?: boolean;
}

export interface SimilarSet {
  id: string;
  name: string;
  domains: string[];
}
