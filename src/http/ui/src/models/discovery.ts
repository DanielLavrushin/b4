import { B4SetConfig } from "@b4.sets";
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
  | "extsplit"
  | "firstbyte"
  | "combo"
  | "hybrid"
  | "window"
  | "mutation"
  | "incoming";

export type DiscoveryPhase =
  | "baseline"
  | "cached"
  | "strategy_detection"
  | "optimization"
  | "dns_detection"
  | "combination"
  | "confirmation";

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

export interface DNSDiscoveryResult {
  is_poisoned: boolean;
  transport_blocked?: boolean;
  expected_ips?: string[];
  best_server?: string;
  needs_fragment: boolean;
}

export interface DiscoveryResult {
  domain: string;
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
}

export interface DiscoverySuite {
  id: string;
  status: "pending" | "running" | "complete" | "failed" | "canceled";
  start_time: string;
  end_time: string;
  total_checks: number;
  completed_checks: number;
  current_phase?: DiscoveryPhase;
  current_domain?: string;
  domains?: { domain: string; check_url: string }[];
  domain_discovery_results?: Record<string, DiscoveryResult>;
  strategy_groups?: BackendStrategyGroup[];
}

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
}
