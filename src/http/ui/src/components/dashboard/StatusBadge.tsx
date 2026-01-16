import { WarningIcon, CheckIcon, CloseIcon } from "@b4.icons";
import { colors } from "@design";
import { Badge } from "@primitives/badge";

interface StatusBadgeProps {
  label: string;
  status: "active" | "inactive" | "warning" | "error";
}

export const StatusBadge = ({ label, status }: StatusBadgeProps) => {
  const statusConfig = {
    active: {
      color: "#4caf50",
      icon: <CheckIcon />,
    },
    inactive: {
      color: colors.text.secondary,
      icon: <CloseIcon />,
    },
    warning: {
      color: "#ff9800",
      icon: <WarningIcon />,
    },
    error: { color: "#f44336", icon: <CloseIcon /> },
  };

  const config = statusConfig[status];

  return (
    <Badge
      className="inline-flex items-center gap-1 font-semibold"
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
