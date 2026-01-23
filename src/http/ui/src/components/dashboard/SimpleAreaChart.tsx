"use client";

import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@primitives/chart";
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from "recharts";

interface SimpleChartProps {
  data: { timestamp: number; value: number }[];
}

export const SimpleAreaChart = ({ data }: SimpleChartProps) => {
  if (data.length === 0) {
    return <p className="text-muted-foreground">No data</p>;
  }

  const chartData = data.map((d, index) => ({
    index,
    value: d.value,
    timestamp: new Date(d.timestamp).toLocaleTimeString(),
  }));

  const chartConfig = {
    value: {
      label: "conn/s",
    },
  } satisfies ChartConfig;

  return (
    <ChartContainer config={chartConfig} className="h-50 w-full">
      <AreaChart
        data={chartData}
        margin={{ left: 12, right: 12, top: 12, bottom: 12 }}
      >
        <CartesianGrid strokeDasharray="3 3" vertical={false} />
        <XAxis dataKey="index" tickLine={false} axisLine={false} tick={false} />
        <YAxis
          dataKey="value"
          tickLine={false}
          axisLine={false}
          tickMargin={8}
          tickFormatter={(value) => value.toFixed(1)}
        />
        <ChartTooltip
          cursor={false}
          content={<ChartTooltipContent hideLabel />}
        />
        <Area
          dataKey="value"
          strokeWidth={2}
          dot={false}
          stroke="var(--chart-1)"
          fill="var(--chart-1)"
          fillOpacity={0.2}
        />
      </AreaChart>
    </ChartContainer>
  );
};
