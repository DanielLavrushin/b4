import { B4Badge } from "@b4.elements";
import type {
  DNSHonesty,
  DNSProbeStatus,
  FetchStatus,
  HostingStatus,
  SiteOutcome,
  TelegramVerdict,
} from "@models/detector";
import { useTranslation } from "react-i18next";

type BadgeColor = "default" | "info" | "success" | "warning" | "error";

export const BLOCKED_STATUSES: FetchStatus[] = [
  "TLS_DPI",
  "TLS_MITM",
  "TLS_SPOOF",
  "TLS_ALERT",
  "TLS_RST",
  "TLS_DROP",
  "SYN_DROP",
  "TCP16",
  "ISP_PAGE",
  "BLOCKED",
  "DNS_FAKE",
  "TIMEOUT",
];

export function fetchStatusColor(status?: FetchStatus): BadgeColor {
  if (!status) return "default";
  if (status === "OK") return "success";
  if (status === "CHECKING") return "info";
  if (status === "SERVER_ERROR" || status === "MTLS") return "warning";
  if (BLOCKED_STATUSES.includes(status)) return "error";
  return "default";
}

export function outcomeColor(outcome: SiteOutcome): BadgeColor {
  switch (outcome) {
    case "ok":
      return "success";
    case "fixed":
      return "success";
    case "still_blocked":
    case "blocked":
      return "error";
    case "broken_by_b4":
      return "error";
    case "server":
      return "warning";
    case "pending":
      return "info";
    default:
      return "default";
  }
}

export function hostingColor(status: HostingStatus): BadgeColor {
  switch (status) {
    case "ok":
      return "success";
    case "dropped":
      return "error";
    case "mixed":
      return "warning";
    case "timeout":
    case "error":
      return "default";
    default:
      return "info";
  }
}

export function honestyColor(h?: DNSHonesty): BadgeColor {
  switch (h) {
    case "honest":
      return "success";
    case "substituted":
      return "error";
    case "filtered":
    case "differs":
      return "warning";
    default:
      return "default";
  }
}

export function dnsProbeColor(s?: DNSProbeStatus): BadgeColor {
  switch (s) {
    case "ok":
      return "success";
    case "blocked":
      return "error";
    case "timeout":
      return "warning";
    default:
      return "default";
  }
}

export function telegramColor(v?: TelegramVerdict): BadgeColor {
  switch (v) {
    case "ok":
      return "success";
    case "slow":
    case "stalled":
    case "partial":
      return "warning";
    case "blocked":
    case "error":
      return "error";
    default:
      return "default";
  }
}

interface ChipProps {
  label: string;
  color: BadgeColor;
  title?: string;
}

export const StatusChip = ({ label, color, title }: ChipProps) => (
  <B4Badge label={label} color={color} size="small" title={title} sx={{ fontWeight: 600 }} />
);

export const FetchChip = ({ fetch }: { fetch?: { status: FetchStatus; detail?: string; blocked_ips?: string[] } }) => {
  const { t } = useTranslation();
  if (!fetch) return <StatusChip label={t("detector.status.PENDING")} color="default" />;
  if (fetch.status === "OK" && fetch.blocked_ips && fetch.blocked_ips.length > 0) {
    return <StatusChip label={t("detector.status.OK_PARTIAL", { count: fetch.blocked_ips.length })} color="warning" title={fetch.detail} />;
  }
  return (
    <StatusChip
      label={t(`detector.status.${fetch.status}`, { defaultValue: fetch.status })}
      color={fetchStatusColor(fetch.status)}
      title={fetch.detail}
    />
  );
};
