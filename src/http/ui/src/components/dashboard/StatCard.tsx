import { colors } from "@design";
import { Card } from "@design/components/ui/card";
import { cn } from "@design/lib/utils";

interface StatCardProps {
  title: string;
  value: string | number;
  subtitle?: string;
  icon: React.ReactNode;
  color?: string;
  variant?: "default" | "outlined" | "elevated";
  onClick?: () => void;
  trend?: {
    value: number;
    label?: string;
  };
}

export const StatCard = ({
  title,
  value,
  subtitle,
  icon,
  color = colors.primary,
  variant = "outlined",
  onClick,
  trend,
}: StatCardProps) => {
  const colorStyle = color.startsWith("#")
    ? color
    : color.startsWith("rgb")
    ? color
    : `var(--${color})`;

  return (
    <Card
      className={cn(
        "w-full flex flex-col transition-all duration-200 border border-border",
        variant === "outlined"
          ? "border border-border"
          : variant === "elevated"
          ? "border-none shadow-lg"
          : "border-none",
        onClick && "cursor-pointer hover:-translate-y-0.5",
        "hover:border-border hover:shadow-lg"
      )}
      onClick={onClick}
      onMouseEnter={(e) => {
        if (onClick) {
          e.currentTarget.style.transform = "translateY(-2px)";
        }
      }}
      onMouseLeave={(e) => {
        e.currentTarget.style.transform = "";
      }}
    >
      <div className="p-4 flex-1">
        <div className="flex flex-row justify-between items-start">
          <div className="flex-1">
            <p className="text-xs text-muted-foreground uppercase tracking-wider">
              {title}
            </p>
            <h4 className="text-2xl font-semibold text-foreground mt-1 mb-1">
              {value}
            </h4>
            {subtitle && (
              <p className="text-xs text-muted-foreground">{subtitle}</p>
            )}
            {trend && (
              <div className="flex items-center gap-1 mt-1">
                <p
                  className={cn(
                    "text-xs font-semibold",
                    trend.value > 0 ? "text-green-500" : "text-red-500"
                  )}
                >
                  {trend.value > 0 ? "+" : ""}
                  {trend.value.toFixed(1)}%
                </p>
                {trend.label && (
                  <p className="text-xs text-muted-foreground">{trend.label}</p>
                )}
              </div>
            )}
          </div>
          <div
            className="p-3 flex items-center justify-center min-w-14 min-h-14"
            style={{
              backgroundColor: `${colorStyle}22`,
              color: colorStyle,
            }}
          >
            {icon}
          </div>
        </div>
      </div>
    </Card>
  );
};
