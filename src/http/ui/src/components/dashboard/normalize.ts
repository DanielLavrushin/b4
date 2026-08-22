import type {
  EscalationEntry,
  Metrics,
  MTProtoSecretStat,
  MTProtoStats,
} from "./types";

const safeNumber = (
  val: number | null | undefined,
  defaultValue: number = 0,
): number => {
  if (val === null || val === undefined) return defaultValue;
  const num = Number(val);
  if (Number.isNaN(num) || !Number.isFinite(num)) return defaultValue;
  if (num > Number.MAX_SAFE_INTEGER) return Number.MAX_SAFE_INTEGER;
  if (num < Number.MIN_SAFE_INTEGER) return Number.MIN_SAFE_INTEGER;
  return num;
};

const normalizeMTProto = (raw: unknown): MTProtoStats | undefined => {
  if (!raw || typeof raw !== "object") return undefined;
  const m = raw as Partial<MTProtoStats>;
  return {
    enabled: Boolean(m.enabled),
    port: safeNumber(m.port),
    active_connections: safeNumber(m.active_connections),
    total_connections: safeNumber(m.total_connections),
    bytes_up: safeNumber(m.bytes_up),
    bytes_down: safeNumber(m.bytes_down),
    secrets: Array.isArray(m.secrets)
      ? m.secrets.map((s: Partial<MTProtoSecretStat>) => ({
          name: String(s?.name ?? ""),
          active: safeNumber(s?.active),
          total: safeNumber(s?.total),
          bytes_up: safeNumber(s?.bytes_up),
          bytes_down: safeNumber(s?.bytes_down),
        }))
      : [],
  };
};

const normalizeEscalations = (raw: unknown): EscalationEntry[] => {
  if (!Array.isArray(raw)) return [];
  return raw.map((e: Partial<EscalationEntry>) => ({
    host: String(e?.host ?? ""),
    to_set: String(e?.to_set ?? ""),
    hops: safeNumber(e?.hops ?? 0),
    set_at: String(e?.set_at ?? ""),
    expires_at: String(e?.expires_at ?? ""),
  }));
};

export const normalizeMetrics = (data: null | Metrics): Metrics => {
  if (!data || typeof data !== "object") {
    return {
      total_connections: 0,
      active_flows: 0,
      packets_processed: 0,
      bytes_processed: 0,
      tcp_connections: 0,
      udp_connections: 0,
      targeted_connections: 0,
      rst_dropped: 0,
      blocked_total: 0,
      blocked_domains: {},
      blocked_devices: {},
      connection_rate: [],
      packet_rate: [],
      byte_rate: [],
      top_domains: {},
      protocol_dist: {},
      geo_dist: {},
      start_time: new Date().toISOString(),
      uptime: "0s",
      cpu_usage: 0,
      memory_usage: {
        allocated: 0,
        total_allocated: 0,
        system: 0,
        num_gc: 0,
        heap_alloc: 0,
        heap_inuse: 0,
        heap_sys: 0,
        rss: 0,
        goroutines: 0,
        threads: 0,
        open_fds: 0,
        percent: 0,
      },
      worker_status: [],
      nfqueue_status: "unknown",
      tables_status: "unknown",
      recent_connections: [],
      recent_events: [],
      device_domains: {},
      domain_tls: {},
      current_cps: 0,
      current_pps: 0,
      current_bps: 0,
      escalations: [],
      total_escalations: 0,
    };
  }

  return {
    total_connections: safeNumber(data.total_connections),
    active_flows: safeNumber(data.active_flows),
    packets_processed: safeNumber(data.packets_processed),
    bytes_processed: safeNumber(data.bytes_processed),
    tcp_connections: safeNumber(data.tcp_connections),
    udp_connections: safeNumber(data.udp_connections),
    targeted_connections: safeNumber(data.targeted_connections),
    rst_dropped: safeNumber(data.rst_dropped),
    blocked_total: safeNumber(data.blocked_total),
    blocked_domains:
      data.blocked_domains && typeof data.blocked_domains === "object"
        ? Object.fromEntries(
            Object.entries(data.blocked_domains).map(([k, v]) => [
              String(k),
              safeNumber(v),
            ]),
          )
        : {},
    blocked_devices:
      data.blocked_devices && typeof data.blocked_devices === "object"
        ? Object.fromEntries(
            Object.entries(data.blocked_devices).map(([k, v]) => [
              String(k),
              safeNumber(v),
            ]),
          )
        : {},
    connection_rate: Array.isArray(data.connection_rate)
      ? data.connection_rate.map(
          (item: { timestamp: number; value: number }) => ({
            timestamp: safeNumber(item?.timestamp),
            value: safeNumber(item?.value),
          }),
        )
      : [],
    packet_rate: Array.isArray(data.packet_rate)
      ? data.packet_rate.map((item: { timestamp: number; value: number }) => ({
          timestamp: safeNumber(item?.timestamp),
          value: safeNumber(item?.value),
        }))
      : [],
    byte_rate: Array.isArray(data.byte_rate)
      ? data.byte_rate.map((item: { timestamp: number; value: number }) => ({
          timestamp: safeNumber(item?.timestamp),
          value: safeNumber(item?.value),
        }))
      : [],
    top_domains:
      data.top_domains && typeof data.top_domains === "object"
        ? Object.fromEntries(
            Object.entries(data.top_domains).map(([k, v]) => [
              String(k),
              safeNumber(v),
            ]),
          )
        : {},
    protocol_dist:
      data.protocol_dist && typeof data.protocol_dist === "object"
        ? Object.fromEntries(
            Object.entries(data.protocol_dist).map(([k, v]) => [
              String(k),
              safeNumber(v),
            ]),
          )
        : {},
    geo_dist:
      data.geo_dist && typeof data.geo_dist === "object"
        ? Object.fromEntries(
            Object.entries(data.geo_dist).map(([k, v]) => [
              String(k),
              safeNumber(v),
            ]),
          )
        : {},
    start_time: String(data.start_time ?? new Date().toISOString()),
    uptime: String(data.uptime ?? "0s"),
    cpu_usage: safeNumber(data.cpu_usage),
    memory_usage: {
      allocated: safeNumber(data?.memory_usage?.allocated),
      total_allocated: safeNumber(data?.memory_usage?.total_allocated),
      system: safeNumber(data?.memory_usage?.system),
      num_gc: safeNumber(data?.memory_usage?.num_gc),
      heap_alloc: safeNumber(data?.memory_usage?.heap_alloc),
      heap_inuse: safeNumber(data?.memory_usage?.heap_inuse),
      heap_sys: safeNumber(data?.memory_usage?.heap_sys),
      rss: safeNumber(data?.memory_usage?.rss),
      goroutines: safeNumber(data?.memory_usage?.goroutines),
      threads: safeNumber(data?.memory_usage?.threads),
      open_fds: safeNumber(data?.memory_usage?.open_fds),
      percent: safeNumber(data?.memory_usage?.percent),
    },
    worker_status: Array.isArray(data.worker_status)
      ? data.worker_status.map(
          (w: { id: number; status: string; processed: number }) => ({
            id: safeNumber(w.id),
            status: String(w.status ?? "unknown"),
            processed: safeNumber(w.processed),
          }),
        )
      : [],
    nfqueue_status: String(data.nfqueue_status ?? "unknown"),
    tables_status: String(data.tables_status ?? "unknown"),
    recent_connections: Array.isArray(data.recent_connections)
      ? data.recent_connections.map(
          (conn: {
            timestamp?: string;
            protocol?: "TCP" | "UDP";
            domain?: string;
            source?: string;
            destination?: string;
            is_target?: boolean;
          }) => ({
            timestamp: String(conn?.timestamp ?? ""),
            protocol:
              conn?.protocol === "TCP" || conn?.protocol === "UDP"
                ? conn.protocol
                : "TCP",
            domain: String(conn?.domain ?? ""),
            source: String(conn?.source ?? ""),
            destination: String(conn?.destination ?? ""),
            is_target: Boolean(conn?.is_target),
          }),
        )
      : [],
    recent_events: Array.isArray(data.recent_events)
      ? data.recent_events.map(
          (evt: { timestamp?: string; level?: string; message?: string }) => ({
            timestamp: String(evt?.timestamp ?? ""),
            level: String(evt?.level ?? ""),
            message: String(evt?.message ?? ""),
          }),
        )
      : [],
    device_domains:
      data.device_domains && typeof data.device_domains === "object"
        ? Object.fromEntries(
            Object.entries(data.device_domains).map(([mac, domains]) => [
              String(mac),
              domains && typeof domains === "object"
                ? Object.fromEntries(
                    Object.entries(domains).map(([d, c]) => [
                      String(d),
                      safeNumber(c),
                    ]),
                  )
                : {},
            ]),
          )
        : {},
    domain_tls:
      data.domain_tls && typeof data.domain_tls === "object"
        ? Object.fromEntries(
            Object.entries(data.domain_tls).map(([k, v]) => [
              String(k),
              String(v ?? ""),
            ]),
          )
        : {},
    current_cps: safeNumber(data.current_cps),
    current_pps: safeNumber(data.current_pps),
    current_bps: safeNumber(data.current_bps),
    escalations: normalizeEscalations(data.escalations),
    total_escalations: safeNumber(data.total_escalations),
    mtproto: normalizeMTProto(data.mtproto),
  };
};
