"use client";

import { Line, LineChart, CartesianGrid, XAxis, YAxis } from "recharts";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@primitives/chart";
import { colors } from "@design";

interface SimpleChartProps {
  data: { timestamp: number; value: number }[];
  color?: string;
}

export const SimpleLineChart = ({
  data,
  color = colors.secondary,
}: SimpleChartProps) => {
  if (data.length === 0) {
    return <p className="text-muted-foreground">No data</p>;
  }

  // Преобразуем данные для recharts
  const chartData = data.map((d, index) => ({
    index,
    value: d.value,
    timestamp: new Date(d.timestamp).toLocaleTimeString(),
  }));

  const chartConfig = {
    value: {
      label: "Value",
      color: color,
    },
  } satisfies ChartConfig;

  return (
    <ChartContainer config={chartConfig} className="h-50 w-full">
      <LineChart
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
        <Line
          type="monotone"
          dataKey="value"
          stroke={`var(--color-value)`}
          strokeWidth={2}
          dot={false}
        />
      </LineChart>
    </ChartContainer>
  );
};
