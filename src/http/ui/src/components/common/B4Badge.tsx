import { forwardRef } from "react";
import { Chip, ChipProps } from "@mui/material";

interface B4BadgeProps extends Omit<ChipProps, "color" | "variant"> {
  color?:
    | "default"
    | "primary"
    | "secondary"
    | "info"
    | "success"
    | "warning"
    | "error";
  variant?: "filled" | "outlined";
}

export const B4Badge = forwardRef<HTMLDivElement, B4BadgeProps>(
  ({ sx, ...props }, ref) => <Chip ref={ref} size="small" sx={sx} {...props} />,
);
