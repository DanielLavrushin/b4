import { Card, CardContent, CardHeader, CardTitle } from "@primitives/card";
import { SimpleAreaChart } from "./SimpleAreaChart";

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
        <SimpleAreaChart data={connectionRate} />
      </CardContent>
    </Card>
  );
};
