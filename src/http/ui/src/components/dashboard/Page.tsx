import { CheckIcon, CloseIcon } from "@b4.icons";
import { Badge } from "@design/components/ui/badge";
import { Progress } from "@design/components/ui/progress";
import { useEffect, useRef, useState } from "react";
import { DashboardActivityPanels } from "./DashboardActivityPanels";
import { DashboardCharts } from "./DashboardCharts";
import { DashboardMetricsGrid } from "./DashboardMetricsGrid";
import { DashboardStatusBar } from "./DashboardStatusBar";

export interface Metrics {
  total_connections: number;
  active_flows: number;
  packets_processed: number;
  bytes_processed: number;
  tcp_connections: number;
  udp_connections: number;
  targeted_connections: number;
  connection_rate: { timestamp: number; value: number }[];
  packet_rate: { timestamp: number; value: number }[];
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
  }>;
  recent_events: Array<{
    timestamp: string;
    level: string;
    message: string;
  }>;
  current_cps: number;
  current_pps: number;
}

const safeNumber = (val: number, defaultValue: number = 0): number => {
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
      connection_rate: [],
      packet_rate: [],
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
        percent: 0,
      },
      worker_status: [],
      nfqueue_status: "unknown",
      tables_status: "unknown",
      recent_connections: [],
      recent_events: [],
      current_cps: 0,
      current_pps: 0,
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
    connection_rate: Array.isArray(data.connection_rate)
      ? data.connection_rate.map(
          (item: { timestamp: number; value: number }) => ({
            timestamp: safeNumber(item?.timestamp),
            value: safeNumber(item?.value),
          })
        )
      : [],
    packet_rate: Array.isArray(data.packet_rate)
      ? data.packet_rate.map((item: { timestamp: number; value: number }) => ({
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
            ])
          )
        : {},
    protocol_dist:
      data.protocol_dist && typeof data.protocol_dist === "object"
        ? Object.fromEntries(
            Object.entries(data.protocol_dist).map(([k, v]) => [
              String(k),
              safeNumber(v),
            ])
          )
        : {},
    geo_dist:
      data.geo_dist && typeof data.geo_dist === "object"
        ? Object.fromEntries(
            Object.entries(data.geo_dist).map(([k, v]) => [
              String(k),
              safeNumber(v),
            ])
          )
        : {},
    start_time: String(data.start_time || new Date().toISOString()),
    uptime: String(data.uptime || "0s"),
    cpu_usage: safeNumber(data.cpu_usage),
    memory_usage: {
      allocated: safeNumber(data?.memory_usage?.allocated),
      total_allocated: safeNumber(data?.memory_usage?.total_allocated),
      system: safeNumber(data?.memory_usage?.system),
      num_gc: safeNumber(data?.memory_usage?.num_gc),
      heap_alloc: safeNumber(data?.memory_usage?.heap_alloc),
      heap_inuse: safeNumber(data?.memory_usage?.heap_inuse),
      percent: safeNumber(data?.memory_usage?.percent),
    },
    worker_status: Array.isArray(data.worker_status)
      ? data.worker_status.map(
          (w: { id: number; status: string; processed: number }) => ({
            id: safeNumber(w?.id),
            status: String(w?.status || "unknown"),
            processed: safeNumber(w?.processed),
          })
        )
      : [],
    nfqueue_status: String(data.nfqueue_status || "unknown"),
    tables_status: String(data.tables_status || "unknown"),
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
            timestamp: String(conn?.timestamp || ""),
            protocol:
              conn?.protocol === "TCP" || conn?.protocol === "UDP"
                ? conn.protocol
                : ("TCP" as "TCP" | "UDP"),
            domain: String(conn?.domain || ""),
            source: String(conn?.source || ""),
            destination: String(conn?.destination || ""),
            is_target: Boolean(conn?.is_target),
          })
        )
      : [],
    recent_events: Array.isArray(data.recent_events)
      ? data.recent_events.map(
          (evt: { timestamp?: string; level?: string; message?: string }) => ({
            timestamp: String(evt?.timestamp || ""),
            level: String(evt?.level || ""),
            message: String(evt?.message || ""),
          })
        )
      : [],
    current_cps: safeNumber(data.current_cps),
    current_pps: safeNumber(data.current_pps),
  };
};

export function DashboardPage() {
  const [metrics, setMetrics] = useState<Metrics | null>(null);
  const [connected, setConnected] = useState(false);
  const wsRef = useRef<WebSocket | null>(null);

  useEffect(() => {
    // Connect to WebSocket for real-time metrics
    const connectWebSocket = () => {
      const ws = new WebSocket(
        (location.protocol === "https:" ? "wss://" : "ws://") +
          location.host +
          "/api/ws/metrics"
      );

      ws.onopen = () => {
        setConnected(true);
        console.log("Metrics WebSocket connected");
      };

      ws.onmessage = (event) => {
        try {
          const data =
            typeof event.data === "string"
              ? (JSON.parse(event.data) as Metrics)
              : normalizeMetrics(null);
          const normalizedData = normalizeMetrics(data);
          setMetrics(normalizedData);
        } catch (error) {
          console.error("Failed to parse metrics:", error);
          console.error("Raw data:", event.data);
          setMetrics(normalizeMetrics(null));
        }
      };

      ws.onerror = (error) => {
        console.error("Metrics WebSocket error:", error);
        setConnected(false);
      };

      ws.onclose = () => {
        setConnected(false);
        console.log("Metrics WebSocket disconnected, reconnecting...");
        setTimeout(connectWebSocket, 3000);
      };

      wsRef.current = ws;
    };

    connectWebSocket();

    // Cleanup on unmount
    return () => {
      if (wsRef.current) {
        wsRef.current.close();
      }
    };
  }, []);

  if (!metrics) {
    return (
      <div className="py-6 px-4">
        <div className="text-center py-16">
          <Progress className="mb-4" />
          <p>Loading dashboard metrics...</p>
        </div>
      </div>
    );
  }

  return (
    <div className="h-full">
      <div className="mb-6 flex justify-between items-center">
        <div className="flex flex-row gap-4 items-center">
          <Badge
            variant={connected ? "default" : "destructive"}
            className="inline-flex items-center gap-1"
          >
            {connected ? (
              <CheckIcon className="h-3 w-3" />
            ) : (
              <CloseIcon className="h-3 w-3" />
            )}
            {connected ? "Connected" : "Disconnected"}
          </Badge>
          <p className="text-sm text-muted-foreground">
            Uptime: {metrics.uptime}
          </p>
        </div>
      </div>

      {/* Key Metrics Cards */}
      <div className="mb-6">
        <DashboardMetricsGrid metrics={metrics} />
      </div>

      {/* Status Bar */}
      <DashboardStatusBar metrics={metrics} />

      {/* Charts Row */}
      <div className="mb-6">
        <DashboardCharts
          connectionRate={metrics.connection_rate}
          protocolDist={metrics.protocol_dist}
        />
      </div>

      {/* Activity Panels */}
      <DashboardActivityPanels
        topDomains={metrics.top_domains}
        recentConnections={metrics.recent_connections}
      />
    </div>
  );
}
