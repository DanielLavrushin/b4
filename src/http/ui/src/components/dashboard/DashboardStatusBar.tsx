import { Card } from "@design/components/ui/card";
import { formatNumber } from "@utils";
import { StatusBadge } from "./StatusBadge";

interface DashboardStatusBarProps {
  metrics: {
    nfqueue_status: string;
    tables_status: string;
    worker_status: Array<unknown>;
    tcp_connections: number;
    udp_connections: number;
  };
}

export const DashboardStatusBar = ({ metrics }: DashboardStatusBarProps) => {
  return (
    <Card className="p-4 mb-6 border border-border">
      <div className="flex flex-row gap-4 items-center flex-wrap">
        <p className="text-sm font-medium text-muted-foreground">
          System Status:
        </p>
        <StatusBadge
          label={`NFQueue: ${metrics.nfqueue_status}`}
          status="active"
        />
        <StatusBadge
          label={`firewall: ${metrics.tables_status}`}
          status="active"
        />
        <StatusBadge
          label={`${metrics.worker_status.length} threads`}
          status={metrics.worker_status.length > 0 ? "active" : "error"}
        />
        <StatusBadge
          label={`TCP: ${formatNumber(metrics.tcp_connections)}`}
          status="active"
        />
        <StatusBadge
          label={`UDP: ${formatNumber(metrics.udp_connections)}`}
          status="active"
        />
      </div>
    </Card>
  );
};
