import type { EngineFailure } from "./settings";

export type FrameType = "hello" | "tick";
export type EngineState = "starting" | "running" | "failed" | "stopping";
export type EngineMode = "nfqueue" | "tun";
export type SetKind = "bypass" | "route" | "proxy" | "block";
export type EventLevel = "info" | "warning" | "error";

export type EventCode =
  | "started"
  | "engine_failed"
  | "settings_applied"
  | "targets_warning"
  | "socks5_failed"
  | "mtproto_failed"
  | "web_failed"
  | "web_tls_unusable"
  | "rules_restored"
  | "watchdog_healed"
  | "watchdog_gave_up"
  | "mcp_write";

export const MINUTE_BUCKETS = 61;
export const TEN_MINUTE_BUCKETS = 145;
export const RSS_BUCKETS = 145;
export const MINUTE_MS = 60_000;
export const TEN_MINUTE_MS = 600_000;

export interface EngineInfo {
  state: EngineState;
  mode: EngineMode;
  threads: number;
  firewall: string;
}

export interface RulesInfo {
  monitored: boolean;
  interval_s: number;
  last_check: number;
  restores: number;
  last_restore: number;
}

export interface ProcessInfo {
  rss_bytes: number;
  mem_total_bytes: number;
  cpu_percent: number;
  cpu_peak_percent: number;
  cpu_peak_at: number;
  cpu_cores: number;
  os_threads: number;
  thread_warn: number;
  thread_limit: number;
}

export interface ActivityBucket {
  t: number;
  in_sets: number;
  not_in_set: number;
}

export interface Activity {
  minute: ActivityBucket[];
  ten_minute: ActivityBucket[];
}

export interface RSSBucket {
  t: number;
  max: number;
}

export interface SetActivity {
  id: string;
  name: string;
  kind: SetKind;
  watched: boolean;
  conns_60m: number;
  last_match: number;
  open?: number;
  dns_blocked: number;
}

export interface Totals {
  conns: number;
  conns_in_sets: number;
  rst_dropped: number;
  escalations: number;
  blocked_dns: number;
  blocked_conns: number;
}

export interface BlockedEntry {
  key: string;
  count: number;
  last: number;
}

export interface BlockedLists {
  rev: number;
  domains: BlockedEntry[];
  devices: BlockedEntry[];
}

export interface TopEntry {
  key: string;
  count: number;
  last: number;
  sets: string[];
}

export interface TopList {
  rev: number;
  items: TopEntry[];
}

export interface EscalationEntry {
  host: string;
  to_set: string;
  hops: number;
  set_at: string;
  expires_at: string;
}

export interface EscalationList {
  rev: number;
  items: EscalationEntry[];
}

export interface B4Event {
  id: number;
  t: number;
  level: EventLevel;
  code: EventCode;
  args?: Record<string, string>;
  message: string;
}

export interface EventLog {
  rev: number;
  items: B4Event[];
  errors: B4Event[];
}

export interface UpstreamAttention {
  set_id: string;
  set_name: string;
  upstream: string;
  fail_open: boolean;
  failures: number;
  last_error: string;
  last_failure: number;
}

export interface Overload {
  window_s: number;
  inject_skipped: number;
  raw_send_dropped: number;
  queue_overflow: number;
}

export interface Conntrack {
  count: number;
  max: number;
}

export interface Attention {
  upstreams: UpstreamAttention[];
  binary_replaced: boolean;
  overload: Overload;
  conntrack?: Conntrack;
}

export interface MTProtoSecretStat {
  name: string;
  active: number;
  total: number;
  bytes_up: number;
  bytes_down: number;
  networks: number;
  network_addrs: string[];
}

export interface MTProtoStats {
  enabled: boolean;
  port: number;
  networks: number;
  active_connections: number;
  total_connections: number;
  bytes_up: number;
  bytes_down: number;
  secrets: MTProtoSecretStat[];
}

export interface MetricsFrame {
  type: FrameType;
  now: number;
  uptime_s: number;
  stats_since: number;
  engine: EngineInfo;
  rules: RulesInfo;
  last_packet_at: number;
  process: ProcessInfo;
  activity: Activity;
  rss_history: RSSBucket[];
  sets: SetActivity[];
  sets_disabled: number;
  totals: Totals;
  blocked?: BlockedLists;
  top_domains?: TopList;
  top_addresses?: TopList;
  escalations?: EscalationList;
  events?: EventLog;
  attention: Attention;
  mtproto?: MTProtoStats;
  engine_failure?: EngineFailure;
}
