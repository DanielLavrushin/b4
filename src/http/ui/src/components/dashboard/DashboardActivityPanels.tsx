import { ProtocolChip } from "@common/ProtocolChip";
import { Badge } from "@design/components/ui/badge";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@design/components/ui/card";
import { formatNumber } from "@utils";

interface Connection {
  timestamp: string;
  protocol: "TCP" | "UDP";
  domain: string;
  source: string;
  destination: string;
  is_target: boolean;
}

interface DashboardActivityPanelsProps {
  topDomains: Record<string, number>;
  recentConnections: Connection[];
}

export const DashboardActivityPanels = ({
  topDomains,
  recentConnections,
}: DashboardActivityPanelsProps) => {
  const topDomainsData = Object.entries(topDomains)
    .sort((a, b) => b[1] - a[1])
    .slice(0, 10);

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
      <Card>
        <CardHeader>
          <CardTitle>Top Domains</CardTitle>
        </CardHeader>
        <CardContent>
          {topDomainsData.length > 0 ? (
            <ul>
              {topDomainsData.map(([domain, count], index) => (
                <li
                  key={domain}
                  className="flex justify-between items-center py-2"
                >
                  <span className="text-sm">
                    {index + 1}. {domain}
                  </span>
                  <Badge variant="default">{formatNumber(count)}</Badge>
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-muted-foreground text-center py-8">
              No domain data available yet
            </p>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Recent Activity</CardTitle>
        </CardHeader>
        <CardContent>
          {recentConnections.length > 0 ? (
            <ul className="max-h-100 overflow-auto">
              {recentConnections.map((conn) => (
                <li key={conn.timestamp} className="py-2">
                  <div className="flex gap-2 items-center">
                    <ProtocolChip protocol={conn.protocol} />
                    <span className="text-sm">{conn.domain}</span>
                    {conn.is_target && (
                      <Badge
                        variant="default"
                        className="font-semibold bg-green-500/20 text-green-500"
                      >
                        TARGET
                      </Badge>
                    )}
                  </div>
                  <span className="text-xs text-muted-foreground">
                    {conn.source} → {conn.destination} •{" "}
                    {new Date(conn.timestamp).toLocaleTimeString()}
                  </span>
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-muted-foreground text-center py-8">
              No recent connections
            </p>
          )}
        </CardContent>
      </Card>
    </div>
  );
};
