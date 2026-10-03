import { StatusDot, type StatusTone } from "./StatusDot";

const tones: Record<string, StatusTone> = {
  pending: "warning",
  active: "success",
  listed: "success",
  hidden: "neutral",
  rejected: "error",
  approved: "success",
  banned: "error",
  trusted: "info",
  ok: "success",
  healthy: "success",
  unhealthy: "error",
};

export const statusTone = (status: string): StatusTone => tones[status] ?? "neutral";

export function StatusChip({ status, label }: Readonly<{ status: string; label?: string }>) {
  return <StatusDot tone={statusTone(status)} label={label ?? status} />;
}
