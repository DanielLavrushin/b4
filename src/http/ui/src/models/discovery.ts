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
  | "dns_redirect"
  | "current";

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
  | "gateway_intercepted"
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
  gateway_ips?: string[];
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
  stopped_early?: boolean;
  stopped_phase?: DiscoveryPhase;
  runtime_active?: boolean;
  set_id?: string;
  set_verdict?: SetVerdict;
  stopped_covered?: boolean;
}

export type SetVerdictStatus =
  | "covered"
  | "current_works"
  | "partial"
  | "not_needed"
  | "none"
  | "incomplete";

export interface SetVerdict {
  status: SetVerdictStatus;
  winner_preset?: string;
  family?: StrategyFamily;
  set?: B4SetConfig;
  covered?: string[];
  uncovered?: string[];
  no_bypass?: string[];
  confirmed?: boolean;
}

export interface SetRunRecord {
  set_id: string;
  suite_id: string;
  start_time: string;
  end_time: string;
  urls?: string[];
  verdict: SetVerdict;
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
  set_id?: string;
}

export interface AppliedMark {
  set_id?: string;
  at: string;
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
  stopped_early?: boolean;
  order?: number;
  applied?: Record<string, AppliedMark>;
  size_bytes?: number;
  set_id?: string;
}

export interface SimilarSet {
  id: string;
  name: string;
  domains: string[];
}

export type ProbeSuggestionSource =
  | "stored"
  | "history"
  | "detector"
  | "domain"
  | "service";

export interface ProbeSuggestion {
  url: string;
  host: string;
  source: ProbeSuggestionSource;
  owner_set_id?: string;
  owner_set_name?: string;
}

export interface ProbeSuggestions {
  set_id: string;
  urls: ProbeSuggestion[] | null;
}
