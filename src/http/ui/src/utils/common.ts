import type { DeviceInfo } from "@models/devices";
import { getAuthToken } from "@context/AuthProvider";

export function wsUrl(path: string): string {
  const protocol = location.protocol === "https:" ? "wss://" : "ws://";
  const token = getAuthToken();
  const query = token ? `?token=${encodeURIComponent(token)}` : "";
  return protocol + location.host + path + query;
}

export function resolveDeviceName(d: DeviceInfo): string {
  if (d.alias) return d.alias;
  if (d.hostname) return d.hostname;
  if (d.vendor && d.vendor !== "Private") return `${d.vendor} (${d.mac})`;
  return d.mac;
}

export function resolveDeviceMeta(d: DeviceInfo): string {
  const parts: string[] = [];
  if (d.ip) parts.push(d.ip);
  if (d.vendor && d.vendor !== "Private") parts.push(d.vendor);
  return parts.join(" · ");
}

export function sortDevices(
  devices: DeviceInfo[],
  isSelected: (mac: string) => boolean,
): DeviceInfo[] {
  return [...devices].sort((a, b) => {
    const aSelected = isSelected(a.mac);
    const bSelected = isSelected(b.mac);
    if (aSelected !== bSelected) return aSelected ? -1 : 1;
    const aName = (a.alias || a.vendor || a.hostname || "").toLowerCase();
    const bName = (b.alias || b.vendor || b.hostname || "").toLowerCase();
    return aName.localeCompare(bName);
  });
}

export function formatRelativeShort(
  t: (key: string) => string,
  ts: number,
  now: number,
): string {
  const diff = Math.max(0, Math.floor((now - ts) / 1000));
  if (diff < 2) return t("core.timeAgo.nowShort");
  if (diff < 60) return `${diff}s`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m`;
  return `${Math.floor(diff / 3600)}h`;
}

export const formatBytes = (
  bytes: number | string | null | undefined,
): string => {
  if (bytes === null || bytes === undefined) return "0 B";

  const num =
    typeof bytes === "string" ? Number.parseFloat(bytes) : Number(bytes);

  if (Number.isNaN(num) || !Number.isFinite(num) || num < 0) return "0 B";

  if (num === 0) return "0 B";

  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.min(Math.floor(Math.log(num) / Math.log(k)), sizes.length - 1);
  const value = num / Math.pow(k, i);

  return (Number.isFinite(value) ? value.toFixed(2) : "0") + " " + sizes[i];
};

export const formatNumber = (
  num: number | string | null | undefined,
): string => {
  if (num === null || num === undefined) return "0";

  const value = typeof num === "string" ? Number.parseFloat(num) : Number(num);

  if (Number.isNaN(value) || !Number.isFinite(value)) return "0";

  const absValue = Math.abs(value);
  const sign = value < 0 ? "-" : "";

  if (absValue >= 1000000) {
    const formatted = (absValue / 1000000).toFixed(1);
    return (
      sign +
      (Number.isFinite(Number.parseFloat(formatted)) ? formatted : "0") +
      "M"
    );
  }
  if (absValue >= 1000) {
    const formatted = (absValue / 1000).toFixed(1);
    return (
      sign +
      (Number.isFinite(Number.parseFloat(formatted)) ? formatted : "0") +
      "K"
    );
  }

  return sign + Math.floor(absValue).toString();
};
