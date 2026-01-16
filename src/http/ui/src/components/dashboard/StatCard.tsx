import { colors } from "@design";
import { Card } from "@primitives/card";
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
        "border-border flex w-full flex-col border transition-all duration-200",
        variant === "outlined"
          ? "border-border border"
          : variant === "elevated"
            ? "border-none shadow-lg"
            : "border-none",
        onClick && "cursor-pointer hover:-translate-y-0.5",
        "hover:border-border hover:shadow-lg",
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
      <div className="flex-1 p-4">
        <div className="flex flex-row items-start justify-between">
          <div className="flex-1">
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
          </div>
          <div
            className="flex min-h-14 min-w-14 items-center justify-center p-3"
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
