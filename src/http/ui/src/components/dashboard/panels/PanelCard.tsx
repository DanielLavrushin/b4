import { useEffect, useId, useState, type ReactNode } from "react";
import { Box, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors, radiusPx } from "@design";
import { HistoryIcon } from "@b4.icons";
import { formatClock } from "@common/charts";
import { useMetrics } from "@/stores/useMetrics";
import { PANEL_PAD_X, emptySx, tagSx } from "./styles";

const STALE_GRACE_MS = 1_500;
const STALE_OPACITY = 0.5;

export function useStaleSince(enabled = true): number {
  const since = useMetrics((s) =>
    enabled && s.stale && s.frame ? s.receivedAt : 0,
  );
  const connectingSince = useMetrics((s) =>
    enabled && s.stale && s.link.state === "connecting" && s.link.startup
      ? s.link.since
      : 0,
  );
  const [graceOver, setGraceOver] = useState(0);
  useEffect(() => {
    if (!connectingSince) return;
    const wait = connectingSince + STALE_GRACE_MS - Date.now();
    const id = setTimeout(() => setGraceOver(connectingSince), Math.max(0, wait));
    return () => clearTimeout(id);
  }, [connectingSince]);
  if (!since) return 0;
  if (connectingSince && graceOver !== connectingSince) return 0;
  return since;
}

function StaleBadge({ since }: { since: number }) {
  const { t, i18n } = useTranslation();
  return (
    <Box component="span" sx={tagSx}>
      <HistoryIcon sx={{ fontSize: 13 }} aria-hidden />
      {t("metricsLink.stale", { time: formatClock(since, i18n.language, true) })}
    </Box>
  );
}

interface PanelCardProps {
  title: string;
  subtitle?: ReactNode;
  icon?: ReactNode;
  actions?: ReactNode;
  footer?: ReactNode;
  live?: boolean;
  waiting?: boolean;
  children?: ReactNode;
}

export function PanelCard({
  title,
  subtitle,
  icon,
  actions,
  footer,
  live = true,
  waiting = false,
  children,
}: PanelCardProps) {
  const { t } = useTranslation();
  const headingId = useId();
  const staleSince = useStaleSince(live && !waiting);
  const dim = staleSince > 0 ? STALE_OPACITY : 1;

  return (
    <Box
      component="section"
      aria-labelledby={headingId}
      sx={{
        containerType: "inline-size",
        minWidth: 0,
        bgcolor: colors.background.paper,
        border: `1px solid ${colors.border.default}`,
        borderRadius: `${radiusPx.md}px`,
        color: colors.text.primary,
      }}
    >
      <Box
        sx={{
          display: "flex",
          flexWrap: "wrap",
          alignItems: "center",
          justifyContent: "space-between",
          columnGap: "12px",
          rowGap: "8px",
          px: PANEL_PAD_X,
          pt: "12px",
          pb: "10px",
        }}
      >
        <Box
          sx={{
            display: "flex",
            alignItems: "center",
            gap: "8px",
            minWidth: 0,
            flex: "1 1 220px",
          }}
        >
          {icon && (
            <Box
              aria-hidden
              sx={{
                display: "flex",
                color: colors.text.secondary,
                "& svg": { fontSize: 18 },
              }}
            >
              {icon}
            </Box>
          )}
          <Box sx={{ minWidth: 0 }}>
            <Typography
              component="h2"
              id={headingId}
              sx={{
                m: 0,
                fontSize: 12,
                fontWeight: 700,
                letterSpacing: "0.12em",
                textTransform: "uppercase",
                lineHeight: 1.35,
                color: colors.text.primary,
              }}
            >
              {title}
            </Typography>
            {subtitle && (
              <Typography
                component="div"
                sx={{
                  fontSize: 12,
                  lineHeight: 1.4,
                  color: colors.text.secondary,
                  mt: "2px",
                }}
              >
                {subtitle}
              </Typography>
            )}
          </Box>
        </Box>
        {(staleSince > 0 || actions) && (
          <Box
            sx={{
              display: "flex",
              flexWrap: "wrap",
              alignItems: "center",
              justifyContent: "flex-end",
              gap: "8px 12px",
              minWidth: 0,
            }}
          >
            {staleSince > 0 && <StaleBadge since={staleSince} />}
            {actions}
          </Box>
        )}
      </Box>
      <Box sx={{ opacity: dim }}>
        {waiting ? (
          <Box sx={emptySx}>{t("metricsLink.waiting")}</Box>
        ) : (
          children
        )}
      </Box>
      {footer && !waiting && (
        <Box
          sx={{
            opacity: dim,
            px: PANEL_PAD_X,
            py: "10px",
            borderTop: `1px solid ${colors.border.light}`,
            fontSize: 12,
            lineHeight: 1.5,
            color: colors.text.secondary,
            display: "flex",
            flexDirection: "column",
            gap: "4px",
          }}
        >
          {footer}
        </Box>
      )}
    </Box>
  );
}
