import { WarningIcon, CheckIcon, CloseIcon } from "@b4.icons";
import { colors } from "@design";
import { Badge } from "@design/components/ui/badge";

interface StatusBadgeProps {
  label: string;
  status: "active" | "inactive" | "warning" | "error";
}

export const StatusBadge = ({ label, status }: StatusBadgeProps) => {
  const statusConfig = {
    active: {
      color: "#4caf50",
      icon: <CheckIcon className="h-4 w-4" />,
    },
    inactive: {
      color: colors.text.secondary,
      icon: <CloseIcon className="h-4 w-4" />,
    },
    warning: {
      color: "#ff9800",
      icon: <WarningIcon className="h-4 w-4" />,
    },
    error: { color: "#f44336", icon: <CloseIcon className="h-4 w-4" /> },
  };

  const config = statusConfig[status];

  return (
    <Badge
      className="font-semibold inline-flex items-center gap-1"
      style={{
        backgroundColor: `${config.color}22`,
        color: config.color,
        borderColor: `${config.color}44`,
      }}
    >
      {config.icon}
      {label}
    </Badge>
  );
};
