import { Stack } from "@mui/material";
import { useTranslation } from "react-i18next";
import {
  TcpIcon,
  UdpIcon,
  BlockIcon,
  ProxyIcon,
  DuplicateIcon,
  TelegramIcon,
  DnsIcon,
  WarningIcon,
} from "@b4.icons";
import { B4Badge } from "@b4.elements";

interface ProtocolChipProps {
  protocol: "TCP" | "UDP";
  flags?: string;
}

interface FlagBadgesProps {
  flags?: string;
}

type BadgeColor =
  | "default"
  | "primary"
  | "info"
  | "success"
  | "warning"
  | "error";

interface DnsVerdictStyle {
  label: string;
  color: BadgeColor;
  variant: "filled" | "outlined";
  icon: "dns" | "block" | "warning";
}

const DNS_PREFIX = "dns-";

const DNS_VERDICT_STYLES: Record<string, DnsVerdictStyle> = {
  doh: { label: "doh", color: "success", variant: "outlined", icon: "dns" },
  forward: {
    label: "forward",
    color: "info",
    variant: "outlined",
    icon: "dns",
  },
  pin: { label: "pin", color: "primary", variant: "outlined", icon: "dns" },
  heal: { label: "heal", color: "warning", variant: "outlined", icon: "dns" },
  passthrough: {
    label: "passthrough",
    color: "default",
    variant: "outlined",
    icon: "dns",
  },
  "ipv6-disabled": {
    label: "ipv6 off",
    color: "default",
    variant: "outlined",
    icon: "dns",
  },
  "ipv6-stripped": {
    label: "ipv4 only",
    color: "warning",
    variant: "outlined",
    icon: "dns",
  },
  "heal+ipv6-stripped": {
    label: "heal + ipv4 only",
    color: "warning",
    variant: "outlined",
    icon: "dns",
  },
  sinkhole: {
    label: "sinkhole",
    color: "error",
    variant: "filled",
    icon: "block",
  },
  block: { label: "block", color: "error", variant: "filled", icon: "block" },
  servfail: {
    label: "servfail",
    color: "warning",
    variant: "filled",
    icon: "warning",
  },
  "bad-target": {
    label: "bad target",
    color: "warning",
    variant: "filled",
    icon: "warning",
  },
};

interface DnsVerdict {
  action: string;
  target: string;
  style: DnsVerdictStyle;
}

const DNS_CLASSIFY_REASONS = new Set(["hint"]);

export function parseDnsVerdict(flags?: string): DnsVerdict | null {
  if (!flags?.startsWith(DNS_PREFIX)) return null;
  const body = flags.slice(DNS_PREFIX.length);
  if (!body) return null;
  const arrow = body.indexOf("->");
  const action = arrow === -1 ? body : body.slice(0, arrow);
  const target = arrow === -1 ? "" : body.slice(arrow + 2).trim();
  if (!action || DNS_CLASSIFY_REASONS.has(action)) return null;
  return {
    action,
    target,
    style: DNS_VERDICT_STYLES[action] ?? {
      label: action,
      color: "default",
      variant: "outlined",
      icon: "dns",
    },
  };
}

const dnsIcons = {
  dns: <DnsIcon />,
  block: <BlockIcon />,
  warning: <WarningIcon />,
};

const DnsBadge = ({ verdict }: { verdict: DnsVerdict }) => {
  const { t } = useTranslation();
  const { action, target, style } = verdict;
  const known = action in DNS_VERDICT_STYLES;
  const title = known
    ? t(`connections.flags.dns.${action}`, { target })
    : DNS_PREFIX + action;

  return (
    <B4Badge
      icon={dnsIcons[style.icon]}
      label={target ? `${style.label} ${target}` : style.label}
      title={title}
      variant={style.variant}
      color={style.color}
      sx={{ maxWidth: 240 }}
    />
  );
};

export const FlagBadges = ({ flags }: FlagBadgesProps) => {
  const { t } = useTranslation();
  const dnsVerdict = parseDnsVerdict(flags);
  const isBlocked = flags?.startsWith("ipblock");
  const isBlackhole = flags === "block";
  const isSocks5 = flags === "socks5";
  const isDuplicate = flags === "tcp-dup";
  const isMtproto = flags?.startsWith("mtproto");
  const mtprotoName = isMtproto
    ? (flags as string).slice("mtproto".length).replace(/^:/, "").trim()
    : "";

  if (
    !isBlocked &&
    !isBlackhole &&
    !isSocks5 &&
    !isDuplicate &&
    !isMtproto &&
    !dnsVerdict
  )
    return null;

  return (
    <Stack direction="row" spacing={0.5} alignItems="center">
      {dnsVerdict && <DnsBadge verdict={dnsVerdict} />}
      {isMtproto && (
        <B4Badge
          icon={<TelegramIcon />}
          label={mtprotoName || "mtproto"}
          title={
            mtprotoName
              ? t("connections.flags.mtprotoNamed", { name: mtprotoName })
              : t("connections.flags.mtproto")
          }
          variant="outlined"
          color="primary"
        />
      )}
      {isBlackhole && (
        <B4Badge
          icon={<BlockIcon />}
          label="block"
          title={t("connections.flags.blackhole")}
          variant="filled"
          color="error"
        />
      )}
      {isSocks5 && (
        <B4Badge
          icon={<ProxyIcon />}
          label="proxy"
          title={t("connections.flags.socks5")}
          variant="outlined"
          color="info"
        />
      )}
      {isDuplicate && (
        <B4Badge
          icon={<DuplicateIcon />}
          label="dup"
          title={t("connections.flags.duplicate")}
          variant="outlined"
          color="secondary"
        />
      )}
      {isBlocked && (
        <B4Badge
          icon={<BlockIcon />}
          label="ip"
          title={t("connections.flags.ipBlocked")}
          variant={flags === "ipblock-cached" ? "outlined" : "filled"}
          color="error"
        />
      )}
    </Stack>
  );
};

export const ProtocolChip = ({ protocol, flags }: ProtocolChipProps) => {
  const icon = protocol === "TCP" ? <TcpIcon /> : <UdpIcon />;

  return (
    <Stack direction="row" spacing={0.5} alignItems="center">
      <B4Badge
        icon={icon}
        label={protocol}
        variant="outlined"
        color={protocol === "TCP" ? "primary" : "secondary"}
      />
      <FlagBadges flags={flags} />
    </Stack>
  );
};
