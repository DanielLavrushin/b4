import { useState, useMemo } from "react";
import { Box, Collapse, Tooltip } from "@mui/material";
import {
  ExpandMore as ExpandMoreIcon,
  Check as CheckIcon,
} from "@mui/icons-material";
import { colors, fonts } from "@design";
import { formatNumber } from "@utils";
import { B4SetConfig } from "@models/config";
import { B4ConfidencePill, B4CountPill } from "@b4.elements";
import { useTranslation } from "react-i18next";
import { useDeviceNames } from "@hooks/useDeviceNames";
import { useDomainTargeting } from "@hooks/useDomainTargeting";
import { DashboardPanel } from "./DashboardPanel";
import { DataRow } from "./DataRow";
import { DomainLabel } from "./DomainLabel";
import { AddToSetButton } from "./AddToSetButton";

interface DeviceActivityProps {
  deviceDomains: Record<string, Record<string, number>>;
  domainTLS: Record<string, string>;
  sets: B4SetConfig[];
  targetedDomains: Set<string>;
  onRefreshSets: () => void;
}

const ROW_GRID = "200px 1fr 100px 100px 24px";

export const DeviceActivity = ({
  deviceDomains,
  domainTLS,
  sets,
  targetedDomains,
  onRefreshSets,
}: DeviceActivityProps) => {
  const { t } = useTranslation();
  const { getDeviceName, getDeviceMeta } = useDeviceNames();
  const isDomainTargeted = useDomainTargeting(targetedDomains);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  const sortedDevices = useMemo(() => {
    return Object.entries(deviceDomains)
      .map(([mac, domains]) => ({
        mac,
        domains,
        total: Object.values(domains).reduce((s, c) => s + c, 0),
        domainCount: Object.keys(domains).length,
      }))
      .sort((a, b) => b.total - a.total);
  }, [deviceDomains]);

  const toggleExpand = (mac: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(mac)) next.delete(mac);
      else next.add(mac);
      return next;
    });
  };

  if (sortedDevices.length === 0) return null;

  return (
    <DashboardPanel eyebrow={t("dashboard.deviceActivity.title")}>
      {sortedDevices.map(({ mac, domains, total, domainCount }) => {
        const isExpanded = expanded.has(mac);
        const sortedDomains = Object.entries(domains).sort(
          (a, b) => b[1] - a[1],
        );
        return (
          <Box key={mac}>
            <Box
              role="button"
              tabIndex={0}
              aria-expanded={isExpanded}
              onClick={() => toggleExpand(mac)}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.preventDefault();
                  toggleExpand(mac);
                }
              }}
              sx={{
                display: "grid",
                gridTemplateColumns: ROW_GRID,
                alignItems: "center",
                gap: "18px",
                p: "12px 14px",
                borderBottom: `1px solid ${colors.border.light}`,
                cursor: "pointer",
                transition: "background-color 120ms ease",
                "&:hover": { bgcolor: colors.background.hover },
              }}
            >
              <Box
                sx={{
                  display: "flex",
                  flexDirection: "column",
                  lineHeight: 1.2,
                  minWidth: 0,
                }}
              >
                <Box
                  component="span"
                  sx={{
                    color: colors.text.primary,
                    fontSize: 13,
                    fontWeight: 600,
                    overflow: "hidden",
                    textOverflow: "ellipsis",
                    whiteSpace: "nowrap",
                  }}
                  title={getDeviceName(mac)}
                >
                  {getDeviceName(mac)}
                </Box>
                <Box
                  component="span"
                  sx={{
                    fontFamily: fonts.mono,
                    fontSize: 11,
                    color: colors.text.secondary,
                    overflow: "hidden",
                    textOverflow: "ellipsis",
                    whiteSpace: "nowrap",
                  }}
                  title={getDeviceMeta(mac)}
                >
                  {getDeviceMeta(mac)}
                </Box>
              </Box>
              <Box
                sx={{
                  display: "flex",
                  alignItems: "center",
                  gap: "6px",
                  minWidth: 0,
                  overflow: "hidden",
                }}
              >
                {sortedDomains.slice(0, 3).map(([domain]) => (
                  <Box
                    key={domain}
                    component="span"
                    title={domain}
                    sx={{
                      fontFamily: fonts.mono,
                      fontSize: 10,
                      lineHeight: 1.4,
                      color: colors.text.secondary,
                      bgcolor: colors.background.default,
                      border: `1px solid ${colors.border.light}`,
                      borderRadius: "4px",
                      px: "6px",
                      py: "1px",
                      maxWidth: 150,
                      overflow: "hidden",
                      textOverflow: "ellipsis",
                      whiteSpace: "nowrap",
                      minWidth: 0,
                    }}
                  >
                    {domain}
                  </Box>
                ))}
                {domainCount > 3 && (
                  <Box
                    component="span"
                    sx={{
                      fontFamily: fonts.mono,
                      fontSize: 10,
                      color: colors.text.disabled,
                      flexShrink: 0,
                    }}
                  >
                    +{domainCount - 3}
                  </Box>
                )}
              </Box>
              <Box
                sx={{
                  fontFamily: fonts.mono,
                  fontSize: 12,
                  color: colors.text.primary,
                  textAlign: "right",
                }}
              >
                {domainCount} {t("core.domains")}
              </Box>
              <Box
                sx={{
                  fontFamily: fonts.mono,
                  fontSize: 12,
                  color: colors.text.primary,
                  textAlign: "right",
                }}
              >
                {formatNumber(total)}
              </Box>
              <ExpandMoreIcon
                sx={{
                  color: colors.text.secondary,
                  fontSize: 18,
                  transition: "transform 150ms ease",
                  transform: isExpanded ? "rotate(180deg)" : "rotate(0)",
                  justifySelf: "center",
                }}
              />
            </Box>
            <Collapse in={isExpanded} unmountOnExit>
              <Box sx={{ bgcolor: colors.background.default }}>
                {sortedDomains.map(([domain, count]) => {
                  const targeted = isDomainTargeted(domain);
                  return (
                    <DataRow
                      key={domain}
                      indent
                      leading={
                        domainTLS[domain] ? (
                          <B4ConfidencePill score={domainTLS[domain]} />
                        ) : undefined
                      }
                      right={
                        <>
                          <B4CountPill value={formatNumber(count)} />
                          {targeted ? (
                            <Tooltip
                              title={t("dashboard.deviceActivity.alreadyInSet")}
                            >
                              <CheckIcon
                                sx={{
                                  color: colors.state.success,
                                  fontSize: 16,
                                }}
                              />
                            </Tooltip>
                          ) : (
                            <AddToSetButton
                              domain={domain}
                              sets={sets}
                              onAdded={onRefreshSets}
                            />
                          )}
                        </>
                      }
                    >
                      <DomainLabel value={domain} />
                    </DataRow>
                  );
                })}
              </Box>
            </Collapse>
          </Box>
        );
      })}
    </DashboardPanel>
  );
};
