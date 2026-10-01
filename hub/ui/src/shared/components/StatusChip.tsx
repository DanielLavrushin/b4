import { Chip } from "@mui/material";

type Tone = "default" | "primary" | "secondary" | "success" | "warning" | "error" | "info";

const tones: Record<string, Tone> = {
  pending: "warning",
  active: "success",
  listed: "success",
  hidden: "default",
  rejected: "error",
  approved: "success",
  banned: "error",
  trusted: "info",
  ok: "success",
  healthy: "success",
  unhealthy: "error",
};

export function StatusChip({ status, label }: { status: string; label?: string }) {
  return (
    <Chip
      size="small"
      variant="outlined"
      color={tones[status] ?? "default"}
      label={label ?? status}
      sx={{ textTransform: "uppercase", letterSpacing: "0.06em", fontSize: 10.5 }}
    />
  );
}
