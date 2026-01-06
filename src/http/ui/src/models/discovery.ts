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
  | "strategy_detection"
  | "optimization"
  | "dns_detection"
  | "combination";

export interface DomainPresetResult {
  preset_name: string;
  family?: StrategyFamily;
  phase?: DiscoveryPhase;
  status: "complete" | "failed";
  duration: number;
  speed: number;
  bytes_read: number;
  error?: string;
  status_code: number;
  set?: B4SetConfig;
}

export interface DiscoveryResult {
  domain: string;
  best_preset: string;
  best_speed: number;
  best_success: boolean;
  results: Record<string, DomainPresetResult>;
  baseline_speed?: number;
  improvement?: number;
}

export interface DiscoverySuite {
  id: string;
  status: "pending" | "running" | "complete" | "failed" | "canceled";
  start_time: string;
  end_time: string;
  total_checks: number;
  completed_checks: number;
  current_phase?: DiscoveryPhase;
  domain_discovery_results?: Record<string, DiscoveryResult>;
}

export interface DiscoveryResponse {
  id: string;
  estimated_tests: number;
  message: string;
  domain: string;
  check_url: string;
}
