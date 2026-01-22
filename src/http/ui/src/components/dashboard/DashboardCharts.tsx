import { SimpleLineChart } from "./SimpleLineChart";
import { colors } from "@design";
import { Card, CardHeader, CardTitle, CardContent } from "@primitives/card";

interface DashboardChartsProps {
  connectionRate: { timestamp: number; value: number }[];
  protocolDist: Record<string, number>;
}

export const DashboardCharts = ({ connectionRate }: DashboardChartsProps) => {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Connection Rate (last 60s)</CardTitle>
      </CardHeader>
      <CardContent>
        <SimpleLineChart data={connectionRate} />
      </CardContent>
    </Card>
  );
};
