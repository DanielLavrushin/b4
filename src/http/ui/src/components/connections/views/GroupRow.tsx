import { memo } from "react";
import { Box, Stack, Typography, Tooltip } from "@mui/material";
import { AddIcon, NetworkIcon } from "@b4.icons";
import {
  B4Badge,
  B4ConfidencePill,
  B4CountPill,
  B4MiniBars,
} from "@b4.elements";
import { ProtocolChip, FlagBadges } from "@common/ProtocolChip";
import { colors, fonts } from "@design";
import { formatRelativeShort, stripPort } from "@utils";
import type { EnrichedGroup } from "@hooks/useConnectionGroups";
import { useTranslation } from "react-i18next";

export const ROW_HEIGHT = 48;

const sameBuckets = (a: number[], b: number[]): boolean => {
  if (a === b) return true;
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
  return true;
};

interface Props {
  group: EnrichedGroup;
  now: number;
  selected: boolean;
  onSelect: (key: string) => void;
  onAddDomain: (domain: string) => void;
  onAddIp: (ip: string) => void;
  onEnrichAsn: (ip: string) => void;
  enrichingIps: Set<string>;
}

export const GroupRow = memo<Props>(
  ({
    group,
    now,
    selected,
    onSelect,
    onAddDomain,
    onAddIp,
    onEnrichAsn,
    enrichingIps,
  }) => {
    const { t } = useTranslation();
    const ipBase = stripPort(group.destIp);
    const isEnriching = enrichingIps.has(ipBase);
    const hasDomain = !!group.domain;
    const deviceLabel = group.deviceName || group.mac;

    let asnSlot: React.ReactNode = null;
    if (group.asnName) {
      asnSlot = (
        <Typography
          sx={{
            fontFamily: fonts.mono,
            fontSize: 11,
            color: colors.text.secondary,
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
            minWidth: 0,
          }}
          title={group.asnName}
        >
          {group.asnName}
        </Typography>
      );
    } else if (group.destIp) {
      asnSlot = (
        <Tooltip
          title={t("connections.table.enrichAsn")}
          arrow
          placement="top"
        >
          <NetworkIcon
            onClick={(e) => {
              e.stopPropagation();
              if (!isEnriching) onEnrichAsn(group.destIp);
            }}
            sx={{
              fontSize: 14,
              color: isEnriching
                ? colors.text.disabled
                : `${colors.secondary}88`,
              "&:hover": { color: colors.secondary },
            }}
          />
        </Tooltip>
      );
    }

    return (
      <Box
        onClick={() => onSelect(group.key)}
        sx={{
          height: ROW_HEIGHT,
          display: "flex",
          alignItems: "center",
          gap: 1.5,
          px: 2,
          borderBottom: `1px solid ${colors.border.light}`,
          cursor: "pointer",
          bgcolor: selected ? colors.accent.primary : "transparent",
          "&:hover": {
            bgcolor: selected
              ? colors.accent.primaryHover
              : "rgba(255, 255, 255, 0.025)",
          },
          transition: "background-color 120ms ease",
        }}
      >
        <Box sx={{ width: 80, flexShrink: 0 }}>
          <ProtocolChip protocol={group.protocol} />
        </Box>

        <Box sx={{ flex: 2, minWidth: 0 }}>
          <Stack
            direction="row"
            spacing={1}
            alignItems="center"
            sx={{ minWidth: 0 }}
          >
            {group.tls && <B4ConfidencePill score={group.tls} />}
            <Typography
              sx={{
                fontFamily: fonts.mono,
                fontSize: 11,
                letterSpacing: "0.04em",
                color: hasDomain ? colors.text.primary : colors.text.disabled,
                textTransform: hasDomain ? "uppercase" : "none",
                fontStyle: hasDomain ? "normal" : "italic",
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
                minWidth: 0,
              }}
            >
              {group.domain || t("connections.aggregated.noDomain")}
            </Typography>
            <FlagBadges flags={group.flags} />
            {hasDomain && !group.hostSet && (
              <Tooltip
                title={t("connections.aggregated.addDomain")}
                arrow
                placement="top"
              >
                <AddIcon
                  onClick={(e) => {
                    e.stopPropagation();
                    onAddDomain(group.domain);
                  }}
                  sx={{
                    fontSize: 16,
                    bgcolor: `${colors.secondary}88`,
                    color: colors.background.default,
                    borderRadius: "50%",
                    "&:hover": { bgcolor: colors.secondary },
                  }}
                />
              </Tooltip>
            )}
          </Stack>
        </Box>

        <Box sx={{ flex: 1.5, minWidth: 0 }}>
          <Stack
            direction="row"
            spacing={1}
            alignItems="center"
            sx={{ minWidth: 0 }}
          >
            <Typography
              sx={{
                fontFamily: fonts.mono,
                fontSize: 11,
                color: colors.text.primary,
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
              }}
            >
              {group.destIp || "—"}
            </Typography>
            {asnSlot}
            {group.destIps.length > 1 && (
              <B4Badge
                label={`+${group.destIps.length - 1}`}
                variant="outlined"
              />
            )}
            {!group.ipSet && group.destIp && (
              <Tooltip
                title={t("connections.aggregated.addIp")}
                arrow
                placement="top"
              >
                <AddIcon
                  onClick={(e) => {
                    e.stopPropagation();
                    onAddIp(group.destIp);
                  }}
                  sx={{
                    fontSize: 14,
                    bgcolor: `${colors.secondary}88`,
                    color: colors.background.default,
                    borderRadius: "50%",
                    "&:hover": { bgcolor: colors.secondary },
                  }}
                />
              </Tooltip>
            )}
          </Stack>
        </Box>

        <Box sx={{ width: 130, flexShrink: 0 }}>
          <Stack direction="row" spacing={0.5} flexWrap="wrap" useFlexGap>
            {group.hostSet && <B4CountPill value={group.hostSet} />}
            {group.ipSet && group.ipSet !== group.hostSet && (
              <B4CountPill value={group.ipSet} />
            )}
          </Stack>
        </Box>

        <Box sx={{ width: 100, flexShrink: 0 }}>
          <Tooltip title={deviceLabel || ""} arrow placement="top">
            <Typography
              sx={{
                fontSize: 12,
                color: deviceLabel
                  ? colors.text.secondary
                  : colors.text.disabled,
                fontFamily: group.deviceName ? "inherit" : fonts.mono,
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
              }}
            >
              {deviceLabel || "—"}
            </Typography>
          </Tooltip>
        </Box>

        <Box sx={{ width: 120, flexShrink: 0 }}>
          <B4MiniBars data={group.buckets} height={20} />
        </Box>

        <Box sx={{ width: 60, flexShrink: 0, textAlign: "right" }}>
          <Typography
            sx={{
              fontFamily: fonts.mono,
              fontSize: 12,
              color: colors.text.primary,
            }}
          >
            {group.packets}
          </Typography>
        </Box>

        <Box sx={{ width: 48, flexShrink: 0, textAlign: "right" }}>
          <Typography
            sx={{
              fontFamily: fonts.mono,
              fontSize: 11,
              color: colors.text.secondary,
            }}
          >
            {formatRelativeShort(t, group.lastSeen, now)}
          </Typography>
        </Box>
      </Box>
    );
  },
  (prev, next) =>
    prev.selected === next.selected &&
    prev.enrichingIps === next.enrichingIps &&
    prev.now === next.now &&
    prev.group.packets === next.group.packets &&
    prev.group.lastSeen === next.group.lastSeen &&
    prev.group.asnName === next.group.asnName &&
    prev.group.destIp === next.group.destIp &&
    prev.group.destIps.length === next.group.destIps.length &&
    prev.group.hostSet === next.group.hostSet &&
    prev.group.ipSet === next.group.ipSet &&
    prev.group.domain === next.group.domain &&
    prev.group.deviceName === next.group.deviceName &&
    prev.group.mac === next.group.mac &&
    prev.group.protocol === next.group.protocol &&
    prev.group.tls === next.group.tls &&
    prev.group.flags === next.group.flags &&
    sameBuckets(prev.group.buckets, next.group.buckets),
);

GroupRow.displayName = "GroupRow";
