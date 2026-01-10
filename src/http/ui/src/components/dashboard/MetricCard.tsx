import { ImprovementIcon } from "@b4.icons";
import { Card } from "@design/components/ui/card";
import { colors } from "@design";
import { cn } from "@design/lib/utils";

interface MetricCardProps {
  title: string;
  value: string | number;
  subtitle?: string;
  icon: React.ReactNode;
  color?: string;
  trend?: number;
}

export const MetricCard = ({
  title,
  value,
  subtitle,
  icon,
  color = colors.primary,
  trend,
}: MetricCardProps) => {
  const colorHex = color || colors.primary;
  const borderColor = `${colorHex}33`;
  const hoverBorderColor = `${colorHex}66`;
  const shadowColor = `${colorHex}22`;
  const bgColor = `${colorHex}22`;

  return (
    <Card
      className={cn(
        "relative overflow-visible transition-all hover:shadow-lg border border-border hover:border-border"
      )}
    >
      <div className="p-6">
        <div className="flex flex-row justify-between items-start">
          <div>
            <p className="text-xs uppercase text-secondary mb-1">{title}</p>
            <h4 className="text-2xl font-semibold text-primary mt-1">{value}</h4>
            {subtitle && (
              <p className="text-xs text-secondary mt-1">{subtitle}</p>
            )}
            {trend !== undefined && (
              <div className="flex items-center mt-1">
                <ImprovementIcon
                  className={cn(
                    "h-4 w-4 mr-1",
                    trend > 0 ? "text-green-500" : "text-red-500"
                  )}
                />
                <p
                  className={cn(
                    "text-xs",
                    trend > 0 ? "text-green-500" : "text-red-500"
                  )}
                >
                  {trend > 0 ? "+" : ""}
                  {trend.toFixed(1)}%
                </p>
              </div>
            )}
          </div>
          <div
            className="p-2 rounded-lg flex items-center justify-center"
            style={{
              backgroundColor: bgColor,
              color: colorHex,
            }}
          >
            {icon}
          </div>
        </div>
      </div>
    </Card>
  );
};
