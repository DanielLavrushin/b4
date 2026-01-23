import { Card, CardContent } from "@primitives/card";
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
    <Card>
      <CardContent className="flex flex-row flex-wrap gap-4">
        <p className="text-muted-foreground text-sm font-medium">
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
      </CardContent>
    </Card>
  );
};
