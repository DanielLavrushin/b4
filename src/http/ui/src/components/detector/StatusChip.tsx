import { B4Badge } from "@b4.elements";
import { CheckCircleIcon, WarningIcon } from "@b4.icons";
import { statusColors } from "./constants";

export function StatusChip({
  status,
  label,
}: Readonly<{ status: string; label?: string }>) {
  const display = label || status;
  let color: "primary" | "error" | "info" | "secondary" | "default" = "default";

  switch (status) {
    case "OK":
    case "FOUND":
    case "ok":
      color = "primary";
      break;
    case "DNS_SPOOFING":
    case "FAKE_IP":
    case "FAKE_NXDOMAIN":
    case "FAKE_EMPTY":
    case "DOH_BLOCKED":
    case "BOTH_UNAVAILABLE":
    case "TLS_DPI":
    case "TLS_SPOOF":
    case "TLS_RST":
    case "TLS_DROP":
    case "SYN_DROP":
    case "TCP16":
    case "DETECTED":
    case "BLOCKED":
    case "blocked":
      color = "error";
      break;
    case "DNS_INTERCEPTION":
    case "TLS_MITM":
    case "TLS_ALERT":
    case "ISP_PAGE":
    case "DNS_FAKE":
    case "MIXED":
    case "NOT_FOUND":
    case "slow":
    case "stalled":
    case "partial":
      color = "secondary";
      break;
    case "NOT_BLOCKED":
    case "TIMEOUT":
    case "ERROR":
    case "error":
      color = "info";
      break;
  }

  return <B4Badge label={display} size="small" color={color} />;
}

export function SummaryIcon({ ok }: { ok: boolean }) {
  return ok ? (
    <CheckCircleIcon sx={{ color: statusColors.ok, fontSize: 20 }} />
  ) : (
    <WarningIcon sx={{ color: statusColors.error, fontSize: 20 }} />
  );
}
