import { Stack } from "@mui/material";
import { TcpIcon, UdpIcon, BlockIcon, ProxyIcon, DuplicateIcon } from "@b4.icons";
import { B4Badge } from "@b4.elements";

interface ProtocolChipProps {
  protocol: "TCP" | "UDP";
  flags?: string;
}

interface FlagBadgesProps {
  flags?: string;
}

export const FlagBadges = ({ flags }: FlagBadgesProps) => {
  const isBlocked = flags?.startsWith("ipblock");
  const isBlackhole = flags === "block";
  const isSocks5 = flags === "socks5";
  const isDuplicate = flags === "tcp-dup";

  if (!isBlocked && !isBlackhole && !isSocks5 && !isDuplicate) return null;

  return (
    <Stack direction="row" spacing={0.5} alignItems="center">
      {isBlackhole && (
        <B4Badge
          icon={<BlockIcon />}
          label="block"
          title="Blocked (blackhole)"
          variant="filled"
          color="error"
        />
      )}
      {isSocks5 && (
        <B4Badge
          icon={<ProxyIcon />}
          label="proxy"
          title="SOCKS5 Proxy"
          variant="outlined"
          color="info"
        />
      )}
      {isDuplicate && (
        <B4Badge
          icon={<DuplicateIcon />}
          label="dup"
          title="Duplicated packet"
          variant="outlined"
          color="secondary"
        />
      )}
      {isBlocked && (
        <B4Badge
          icon={<BlockIcon />}
          label="ip"
          title="Blocked by IP"
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
