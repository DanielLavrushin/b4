import { memo } from "react";
import {
  Box,
  Divider,
  IconButton,
  Stack,
  Typography,
  Tooltip,
} from "@mui/material";
import { CloseIcon, AddIcon, NetworkIcon } from "@b4.icons";
import { B4Badge } from "@common/B4Badge";
import { ProtocolChip } from "@common/ProtocolChip";
import { colors } from "@design";
import { stripPort } from "@utils";
import { Sparkline } from "./Sparkline";
import type { EnrichedGroup } from "@hooks/useConnectionGroups";
import { useTranslation } from "react-i18next";

interface Props {
  group: EnrichedGroup | null;
  onClose: () => void;
  onAddDomain: (domain: string) => void;
  onAddIp: (ip: string) => void;
  onEnrichAsn: (ip: string) => void;
  onDeleteAsn: (asnId: string) => void;
  enrichingIps: Set<string>;
  width?: number;
}

export const DetailPane = memo<Props>(
  ({
    group,
    onClose,
    onAddDomain,
    onAddIp,
    onEnrichAsn,
    onDeleteAsn,
    enrichingIps,
    width = 340,
  }) => {
    const { t } = useTranslation();

    if (!group) return null;

    const deviceLabel =
      group.deviceName ||
      group.mac ||
      t("connections.aggregated.unknownDevice");

    const duration = Math.max(
      0,
      Math.floor((group.lastSeen - group.firstSeen) / 1000),
    );

    return (
      <Box
        sx={{
          width,
          maxWidth: "100vw",
          flexShrink: 0,
          borderLeft: `1px solid ${colors.border.light}`,
          bgcolor: colors.background.paper,
          overflow: "auto",
          display: "flex",
          flexDirection: "column",
        }}
      >
        <Stack
          direction="row"
          alignItems="center"
          sx={{
            px: 2,
            height: 32,
            borderBottom: `2px solid ${colors.border.default}`,
            bgcolor: colors.background.paper,
          }}
        >
          <Typography
            sx={{
              color: colors.secondary,
              fontWeight: 600,
              fontSize: 14,
              flex: 1,
            }}
          >
            {t("connections.aggregated.detail")}
          </Typography>
          <IconButton size="small" onClick={onClose}>
            <CloseIcon sx={{ fontSize: 16, color: colors.text.secondary }} />
          </IconButton>
        </Stack>

        <Stack spacing={2} sx={{ p: 2 }}>
          <Box>
            <Stack
              direction="row"
              spacing={1}
              alignItems="center"
              flexWrap="wrap"
              useFlexGap
            >
              <ProtocolChip protocol={group.protocol} flags={group.flags} />
              {group.tls && (
                <B4Badge variant="outlined" color="primary" label={group.tls} />
              )}
            </Stack>
            <Typography
              sx={{
                mt: 1,
                fontSize: 16,
                fontWeight: 500,
                color: group.domain
                  ? colors.text.primary
                  : colors.text.disabled,
                wordBreak: "break-all",
              }}
            >
              {group.domain || t("connections.aggregated.noDomain")}
            </Typography>
          </Box>

          <Box>
            <Typography
              variant="overline"
              sx={{ fontSize: 10, color: colors.text.disabled }}
            >
              {t("connections.aggregated.activity")}
            </Typography>
            <Box sx={{ mt: 0.5 }}>
              <Sparkline data={group.buckets} width={300} height={40} />
            </Box>
            <Stack direction="row" spacing={2} sx={{ mt: 0.5 }}>
              <Typography sx={{ fontSize: 11, color: colors.text.disabled }}>
                {t("connections.aggregated.packets")}:{" "}
                <b style={{ color: colors.text.secondary }}>{group.packets}</b>
              </Typography>
              <Typography sx={{ fontSize: 11, color: colors.text.disabled }}>
                {t("connections.aggregated.duration")}:{" "}
                <b style={{ color: colors.text.secondary }}>{duration}s</b>
              </Typography>
            </Stack>
          </Box>

          <Divider sx={{ borderColor: colors.border.light }} />

          <Box>
            <Typography
              variant="overline"
              sx={{ fontSize: 10, color: colors.text.disabled }}
            >
              {t("connections.aggregated.source")}
            </Typography>
            <Typography
              sx={{ fontSize: 13, color: colors.text.primary, mt: 0.5 }}
            >
              {deviceLabel}
            </Typography>
            {group.mac && group.deviceName && (
              <Typography
                sx={{
                  fontSize: 11,
                  color: colors.text.disabled,
                  fontFamily: "monospace",
                }}
              >
                {group.mac}
              </Typography>
            )}
          </Box>

          <Box>
            <Stack direction="row" alignItems="center" sx={{ mb: 0.5 }}>
              <Typography
                variant="overline"
                sx={{ fontSize: 10, color: colors.text.disabled, flex: 1 }}
              >
                {t("connections.aggregated.destinations")} (
                {group.destIps.length})
              </Typography>
            </Stack>
            <Stack spacing={0.5}>
              {group.destIps.map((ip) => {
                const base = stripPort(ip);
                const isEnriching = enrichingIps.has(base);
                const showAsn = group.asnName && ip === group.destIp;
                return (
                  <Stack
                    key={ip}
                    spacing={0.5}
                    sx={{
                      px: 1,
                      py: 0.5,
                      borderRadius: 0.5,
                      bgcolor: colors.background.dark,
                      "&:hover": { bgcolor: colors.accent.primaryStrong },
                    }}
                  >
                    <Stack direction="row" spacing={1} alignItems="center">
                      <Typography
                        sx={{
                          fontFamily: "monospace",
                          fontSize: 12,
                          flex: 1,
                          minWidth: 0,
                          color: colors.text.secondary,
                          overflow: "hidden",
                          textOverflow: "ellipsis",
                          whiteSpace: "nowrap",
                        }}
                      >
                        {ip}
                      </Typography>
                      {!showAsn && (
                        <Tooltip
                          title={t("connections.table.enrichAsn")}
                          arrow
                          placement="top"
                        >
                          <NetworkIcon
                            onClick={() => !isEnriching && onEnrichAsn(ip)}
                            sx={{
                              fontSize: 14,
                              cursor: "pointer",
                              color: isEnriching
                                ? colors.text.disabled
                                : `${colors.secondary}88`,
                              "&:hover": { color: colors.secondary },
                            }}
                          />
                        </Tooltip>
                      )}
                      <Tooltip
                        title={t("connections.aggregated.addIp")}
                        arrow
                        placement="top"
                      >
                        <AddIcon
                          onClick={() => onAddIp(ip)}
                          sx={{
                            fontSize: 14,
                            cursor: "pointer",
                            bgcolor: `${colors.secondary}88`,
                            color: colors.background.default,
                            borderRadius: "50%",
                            "&:hover": { bgcolor: colors.secondary },
                          }}
                        />
                      </Tooltip>
                    </Stack>
                    {showAsn && (
                      <Box sx={{ display: "flex" }}>
                        <B4Badge
                          variant="outlined"
                          color="primary"
                          label={group.asnName}
                          onDelete={
                            group.asnId
                              ? () => onDeleteAsn(group.asnId!)
                              : undefined
                          }
                          sx={{
                            maxWidth: "100%",
                            "& .MuiChip-label": {
                              overflow: "hidden",
                              textOverflow: "ellipsis",
                            },
                          }}
                        />
                      </Box>
                    )}
                  </Stack>
                );
              })}
            </Stack>
          </Box>

          {(group.hostSet || group.ipSet) && (
            <Box>
              <Typography
                variant="overline"
                sx={{ fontSize: 10, color: colors.text.disabled }}
              >
                {t("connections.table.set")}
              </Typography>
              <Stack
                direction="row"
                spacing={1}
                sx={{ mt: 0.5 }}
                flexWrap="wrap"
                useFlexGap
              >
                {group.hostSet && (
                  <B4Badge color="secondary" label={group.hostSet} />
                )}
                {group.ipSet && group.ipSet !== group.hostSet && (
                  <B4Badge color="secondary" label={group.ipSet} />
                )}
              </Stack>
            </Box>
          )}

          {group.domain && !group.hostSet && (
            <Box>
              <Stack
                direction="row"
                spacing={1}
                alignItems="center"
                onClick={() => onAddDomain(group.domain)}
                sx={{
                  p: 1,
                  border: `1px solid ${colors.border.default}`,
                  borderRadius: 1,
                  cursor: "pointer",
                  "&:hover": { bgcolor: colors.accent.secondaryHover },
                }}
              >
                <AddIcon sx={{ fontSize: 16, color: colors.secondary }} />
                <Typography sx={{ fontSize: 13, color: colors.text.primary }}>
                  {t("connections.aggregated.addDomainToSet")}
                </Typography>
              </Stack>
            </Box>
          )}
        </Stack>
      </Box>
    );
  },
);

DetailPane.displayName = "DetailPane";
