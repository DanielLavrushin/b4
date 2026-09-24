import i18n from "@/i18n";
import { formatTimeAgo } from "@utils";

type TFn = (key: string, opts?: Record<string, unknown>) => string;

function parseTime(iso?: string): Date | null {
  if (!iso) return null;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 1970) return null;
  return d;
}

export function timeAgo(t: TFn, iso?: string): string {
  if (!parseTime(iso)) return "-";
  return formatTimeAgo(t, iso ?? "") || "-";
}

export function fullTime(iso?: string): string {
  return parseTime(iso)?.toLocaleString(i18n.language) ?? "";
}

export function clockTime(iso?: string): string {
  const d = parseTime(iso);
  if (!d) return "";
  const sameDay = d.toDateString() === new Date().toDateString();
  return sameDay
    ? d.toLocaleTimeString(i18n.language, { hour: "2-digit", minute: "2-digit" })
    : d.toLocaleString(i18n.language, {
        day: "numeric",
        month: "short",
        hour: "2-digit",
        minute: "2-digit",
      });
}

export function isFuture(iso?: string): boolean {
  const d = parseTime(iso);
  return !!d && d.getTime() > Date.now();
}
