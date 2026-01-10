import { StatCard } from "./StatCard";
import { formatBytes, formatNumber } from "@utils";
import { colors } from "@design";
import {
  DashboardIcon,
  IconDatabase,
  IconArrowsExchange,
  IconCpu,
} from "@b4.icons";

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
    <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-4 gap-6">
      <div className="flex">
        <StatCard
          title="Total Connections"
          value={formatNumber(metrics.total_connections)}
          subtitle={`${metrics.targeted_connections} targeted`}
          icon={<IconArrowsExchange />}
          color={colors.primary}
          variant="outlined"
        />
      </div>

      <div className="flex">
        <StatCard
          title="Active Flows"
          value={formatNumber(metrics.active_flows)}
          subtitle={`${metrics.current_cps.toFixed(1)} conn/s`}
          icon={<DashboardIcon />}
          color={colors.secondary}
          variant="outlined"
        />
      </div>

      <div className="flex">
        <StatCard
          title="Packets Processed"
          value={formatNumber(metrics.packets_processed)}
          subtitle={`${metrics.current_pps.toFixed(1)} pkt/s`}
          icon={<IconDatabase />}
          color={colors.tertiary}
          variant="outlined"
        />
      </div>

      <div className="flex">
        <StatCard
          title="Data Processed"
          value={formatBytes(metrics.bytes_processed)}
          subtitle={`Memory: ${metrics.memory_usage.percent.toFixed(1)}%`}
          icon={<IconCpu />}
          color={colors.quaternary}
          variant="outlined"
        />
      </div>
    </div>
  );
};
