export interface Metrics {
  total_connections: number;
  active_flows: number;
  packets_processed: number;
  bytes_processed: number;
  tcp_connections: number;
  udp_connections: number;
  targeted_connections: number;
  rst_dropped: number;
  blocked_total: number;
  blocked_domains: Record<string, number>;
  blocked_devices: Record<string, number>;
  connection_rate: { timestamp: number; value: number }[];
  packet_rate: { timestamp: number; value: number }[];
  byte_rate: { timestamp: number; value: number }[];
  top_domains: Record<string, number>;
  protocol_dist: Record<string, number>;
  geo_dist: Record<string, number>;
  start_time: string;
  uptime: string;
  cpu_usage: number;
  memory_usage: {
    allocated: number;
    total_allocated: number;
    system: number;
    num_gc: number;
    heap_alloc: number;
    heap_inuse: number;
    heap_sys: number;
    rss: number;
    goroutines: number;
    threads: number;
    open_fds: number;
    percent: number;
  };
  worker_status: Array<{
    id: number;
    status: string;
    processed: number;
  }>;
  nfqueue_status: string;
  tables_status: string;
  recent_connections: Array<{
    timestamp: string;
    protocol: "TCP" | "UDP";
    domain: string;
    source: string;
    destination: string;
    is_target: boolean;
    source_mac?: string;
    host_set?: string;
  }>;
  recent_events: Array<{
    timestamp: string;
    level: string;
    message: string;
  }>;
  device_domains: Record<string, Record<string, number>>;
  domain_tls: Record<string, string>;
  current_cps: number;
  current_pps: number;
  current_bps: number;
  escalations: EscalationEntry[];
  total_escalations: number;
  mtproto?: MTProtoStats;
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

export interface EscalationEntry {
  host: string;
  to_set: string;
  hops: number;
  set_at: string;
  expires_at: string;
}
