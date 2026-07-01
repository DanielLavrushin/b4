import { useEffect, useState, useRef } from "react";
import {
  Box,
  Container,
  Grid,
  Typography,
  LinearProgress,
} from "@mui/material";
import { HealthBanner } from "./HealthBanner";
import { LiveSignal } from "./LiveSignal";
import { ActiveSets } from "./ActiveSets";
import { DeviceActivity } from "./DeviceActivity";
import { Escalations } from "./Escalations";
import { Blackhole } from "./Blackhole";
import { UnmatchedDomains } from "./UnmatchedDomains";
import { MTProtoActivity } from "./MTProtoActivity";
import { RuntimeHealth } from "./RuntimeHealth";
import { useTranslation } from "react-i18next";
import { useDashboardSets } from "@hooks/useDashboardSets";
import { wsUrl } from "@utils";

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
}

export interface MTProtoStats {
  enabled: boolean;
  port: number;
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

const normalizeMetrics = (data: null | Metrics): Metrics => {
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

export function DashboardPage() {
  const { t } = useTranslation();
  const [metrics, setMetrics] = useState<Metrics | null>(null);
  const [connected, setConnected] = useState(false);
  const wsRef = useRef<WebSocket | null>(null);
  const { sets, targetedDomains, refresh: refreshSets } = useDashboardSets();

  useEffect(() => {
    const connectWebSocket = () => {
      const ws = new WebSocket(wsUrl("/api/ws/metrics"));

      ws.onopen = () => {
        setConnected(true);
      };

      ws.onmessage = (event) => {
        try {
          const data =
            typeof event.data === "string"
              ? (JSON.parse(event.data) as Metrics)
              : normalizeMetrics(null);
          setMetrics(normalizeMetrics(data));
        } catch {
          setMetrics(normalizeMetrics(null));
        }
      };

      ws.onerror = () => {
        setConnected(false);
      };

      ws.onclose = () => {
        setConnected(false);
        setTimeout(connectWebSocket, 3000);
      };

      wsRef.current = ws;
    };

    connectWebSocket();

    return () => {
      if (wsRef.current) {
        wsRef.current.close();
      }
    };
  }, []);

  if (!metrics) {
    return (
      <Container maxWidth={false} sx={{ py: 3 }}>
        <Box sx={{ textAlign: "center", py: 8 }}>
          <LinearProgress sx={{ mb: 2 }} />
          <Typography>{t("dashboard.loading")}</Typography>
        </Box>
      </Container>
    );
  }

  const hasDevices = Object.keys(metrics.device_domains).length > 0;

  return (
    <Container maxWidth={false} sx={{ p: 2 }}>
      <HealthBanner metrics={metrics} connected={connected} />

      <Box sx={{ mb: 1.5 }}>
        <RuntimeHealth metrics={metrics} />
      </Box>

      <Box sx={{ mb: 1.5 }}>
        <LiveSignal metrics={metrics} />
      </Box>

      {sets.length > 0 && (
        <Box sx={{ mb: 1.5 }}>
          <ActiveSets sets={sets} />
        </Box>
      )}

      <Grid container spacing={1.5} sx={{ mb: 1.5 }} alignItems="flex-start">
        {hasDevices && (
          <Grid size={{ xs: 12, xl: 6 }} sx={{ display: "flex" }}>
            <Box sx={{ width: "100%" }}>
              <DeviceActivity
                deviceDomains={metrics.device_domains}
                domainTLS={metrics.domain_tls}
                sets={sets}
                targetedDomains={targetedDomains}
                onRefreshSets={refreshSets}
              />
            </Box>
          </Grid>
        )}
        <Grid
          size={{ xs: 12, xl: hasDevices ? 6 : 12 }}
          sx={{ display: "flex" }}
        >
          <Box sx={{ width: "100%" }}>
            <UnmatchedDomains
              topDomains={metrics.top_domains}
              domainTLS={metrics.domain_tls}
              sets={sets}
              targetedDomains={targetedDomains}
              onRefreshSets={refreshSets}
            />
          </Box>
        </Grid>
      </Grid>

      {metrics.mtproto?.enabled && (
        <Box sx={{ mb: 1.5 }}>
          <MTProtoActivity stats={metrics.mtproto} />
        </Box>
      )}

      {metrics.escalations.length > 0 && (
        <Box sx={{ mb: 1.5 }}>
          <Escalations
            escalations={metrics.escalations}
            total={metrics.total_escalations}
          />
        </Box>
      )}

      {metrics.blocked_total > 0 && (
        <Box sx={{ mb: 1.5 }}>
          <Blackhole
            total={metrics.blocked_total}
            blockedDomains={metrics.blocked_domains}
            blockedDevices={metrics.blocked_devices}
          />
        </Box>
      )}
    </Container>
  );
}
