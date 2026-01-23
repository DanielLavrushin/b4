import { formatBytes, formatNumber } from "@utils";
import { MetricCard } from "./MetricCard";

interface DashboardMetricsGridProps {
  metrics: {
    total_connections: number;
    active_flows: number;
    packets_processed: number;
    bytes_processed: number;
    targeted_connections: number;
    current_cps: number;
    current_pps: number;
    memory_usage: {
      percent: number;
    };
  };
}

export const DashboardMetricsGrid = ({
  metrics,
}: DashboardMetricsGridProps) => {
  return (
    <div className="grid grid-cols-2 gap-6 md:grid-cols-4">
      <MetricCard
        title="Total Connections"
        value={formatNumber(metrics.total_connections)}
        subtitle={`${metrics.targeted_connections} targeted`}
      />

      <MetricCard
        title="Active Flows"
        value={formatNumber(metrics.active_flows)}
        subtitle={`${metrics.current_cps.toFixed(1)} conn/s`}
      />

      <MetricCard
        title="Packets Processed"
        value={formatNumber(metrics.packets_processed)}
        subtitle={`${metrics.current_pps.toFixed(1)} pkt/s`}
      />

      <MetricCard
        title="Data Processed"
        value={formatBytes(metrics.bytes_processed)}
        subtitle={`Memory: ${metrics.memory_usage.percent.toFixed(1)}%`}
      />
    </div>
  );
};
