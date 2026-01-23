import { cn } from "@design/lib/utils";
import { Card, CardContent } from "@primitives/card";

interface MetricCardProps {
  title: string;
  value: string | number;
  subtitle?: string;

  trend?: {
    value: number;
    label?: string;
  };
}

export const MetricCard = ({
  title,
  value,
  subtitle,

  trend,
}: MetricCardProps) => {
  return (
    <Card>
      <CardContent>
        <p className="text-muted-foreground text-xs tracking-wider uppercase">
          {title}
        </p>
        <h4 className="text-foreground mt-1 mb-1 text-2xl font-semibold">
          {value}
        </h4>
        {subtitle && (
          <p className="text-muted-foreground text-xs">{subtitle}</p>
        )}
        {trend && (
          <div className="mt-1 flex items-center gap-1">
            <p
              className={cn(
                "text-xs font-semibold",
                trend.value > 0 ? "text-green-500" : "text-red-500",
              )}
            >
              {trend.value > 0 ? "+" : ""}
              {trend.value.toFixed(1)}%
            </p>
            {trend.label && (
              <p className="text-muted-foreground text-xs">{trend.label}</p>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  );
};
