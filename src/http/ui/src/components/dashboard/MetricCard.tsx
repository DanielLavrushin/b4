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
        "border-border hover:border-border relative overflow-visible border transition-all hover:shadow-lg",
      )}
    >
      <div className="p-6">
        <div className="flex flex-row items-start justify-between">
          <div>
            <p className="text-secondary mb-1 text-xs uppercase">{title}</p>
            <h4 className="text-primary mt-1 text-2xl font-semibold">
              {value}
            </h4>
            {subtitle && (
              <p className="text-secondary mt-1 text-xs">{subtitle}</p>
            )}
            {trend !== undefined && (
              <div className="mt-1 flex items-center">
                <ImprovementIcon
                  className={cn(
                    "mr-1 size-4",
                    trend > 0 ? "text-green-500" : "text-red-500",
                  )}
                />
                <p
                  className={cn(
                    "text-xs",
                    trend > 0 ? "text-green-500" : "text-red-500",
                  )}
                >
                  {trend > 0 ? "+" : ""}
                  {trend.toFixed(1)}%
                </p>
              </div>
            )}
          </div>
          <div
            className="flex items-center justify-center rounded-lg p-2"
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
