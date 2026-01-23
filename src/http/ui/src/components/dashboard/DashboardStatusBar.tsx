import { CheckIcon, CloseIcon } from "@b4.icons";
import { Badge } from "@primitives/badge";
import { Card, CardContent } from "@primitives/card";
import { formatNumber } from "@utils";

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
  const isActive = metrics.worker_status.length > 0;

  return (
    <Card>
      <CardContent className="flex flex-row flex-wrap gap-4">
        <p className="text-muted-foreground text-sm font-medium">
          System Status:
        </p>
        <Badge className="bg-green-50 text-green-700 dark:bg-green-950 dark:text-green-300">
          <CheckIcon />
          NFQueue: {metrics.nfqueue_status}
        </Badge>
        <Badge className="bg-green-50 text-green-700 dark:bg-green-950 dark:text-green-300">
          <CheckIcon />
          firewall: {metrics.tables_status}
        </Badge>
        {isActive ? (
          <Badge className="bg-green-50 text-green-700 dark:bg-green-950 dark:text-green-300">
            <CheckIcon />
            {metrics.worker_status.length} threads
          </Badge>
        ) : (
          <Badge variant="destructive">
            <CloseIcon />
            {metrics.worker_status.length} threads
          </Badge>
        )}
        <Badge className="bg-green-50 text-green-700 dark:bg-green-950 dark:text-green-300">
          <CheckIcon />
          TCP: {formatNumber(metrics.tcp_connections)}
        </Badge>
        <Badge className="bg-green-50 text-green-700 dark:bg-green-950 dark:text-green-300">
          <CheckIcon />
          UDP: {formatNumber(metrics.udp_connections)}
        </Badge>
      </CardContent>
    </Card>
  );
};
