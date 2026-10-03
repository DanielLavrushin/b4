import { Fragment, memo, useId, useMemo, useState, type ReactNode } from "react";
import { Box, Link } from "@mui/material";
import { Link as RouterLink } from "react-router";
import { useTranslation } from "react-i18next";
import { colors, fonts } from "@design";
import { ErrorIcon, InfoIcon, WarningIcon } from "@b4.icons";
import type { B4Event, EventLevel } from "@models/metrics";
import { useMetricsFrame } from "@/stores/useMetrics";
import { Ago } from "./Ago";
import { PanelCard } from "./PanelCard";
import { ShowMore, visibleRows } from "./ShowMore";
import { describeEvent, type EventLinkKind } from "./eventText";
import { emptySx, linkSx, listSx, rowSx, srOnlySx } from "./styles";

const LEVEL_VISUAL: Record<EventLevel, { icon: ReactNode; color: string }> = {
  info: { icon: <InfoIcon />, color: colors.text.secondary },
  warning: { icon: <WarningIcon />, color: colors.state.warning },
  error: { icon: <ErrorIcon />, color: colors.state.error },
};

const LINK_KEY: Record<EventLinkKind, string> = {
  logs: "dashboard.events.link.logs",
  set: "dashboard.events.link.set",
  sets: "dashboard.events.link.sets",
  mcp: "dashboard.events.link.mcp",
};

const EMPTY_EVENTS: readonly B4Event[] = [];

const breakAfterDots = (value: string): ReactNode[] =>
  value.split(".").map((part, i, all) => (
    <Fragment key={i}>
      {part}
      {i < all.length - 1 && (
        <>
          .<wbr />
        </>
      )}
    </Fragment>
  ));

const EventRow = memo(function EventRow({ event }: { event: B4Event }) {
  const { t, i18n } = useTranslation();
  const view = describeEvent(event, t, i18n.language);
  const level: EventLevel = LEVEL_VISUAL[event.level] ? event.level : "info";
  const visual = LEVEL_VISUAL[level];
  const levelLabel = t(`dashboard.events.level.${level}`);
  return (
    <Box
      component="li"
      sx={{
        ...rowSx,
        display: "flex",
        alignItems: "flex-start",
        gap: "10px",
      }}
    >
      <Box
        component="span"
        aria-hidden
        sx={{
          display: "inline-flex",
          mt: "1px",
          color: visual.color,
          "& svg": { fontSize: 16 },
        }}
      >
        {visual.icon}
      </Box>
      <Box sx={{ flex: "1 1 auto", minWidth: 0 }}>
        <Box
          sx={{
            fontSize: 13,
            lineHeight: 1.45,
            color: colors.text.primary,
            overflowWrap: "anywhere",
          }}
        >
          {level === "info" ? (
            <Box component="span" sx={srOnlySx}>
              {`${levelLabel}: `}
            </Box>
          ) : (
            <Box
              component="span"
              sx={{
                mr: "6px",
                fontSize: 11,
                fontWeight: 700,
                letterSpacing: "0.08em",
                textTransform: "uppercase",
                color: colors.text.secondary,
              }}
            >
              {levelLabel}
            </Box>
          )}
          {view.text}
        </Box>
        {(view.detail || view.link) && (
          <Box
            sx={{
              display: "flex",
              flexWrap: "wrap",
              alignItems: "baseline",
              columnGap: "12px",
              rowGap: "2px",
              mt: "2px",
            }}
          >
            {view.detail && (
              <Box
                component="span"
                sx={{
                  fontFamily: view.detailIsText ? fonts.sans : fonts.mono,
                  fontSize: view.detailIsText ? 12 : 11,
                  lineHeight: 1.5,
                  color: colors.text.secondary,
                  overflowWrap: "anywhere",
                  minWidth: 0,
                }}
              >
                {view.detailIsText ? view.detail : breakAfterDots(view.detail)}
              </Box>
            )}
            {view.link && (
              <Link component={RouterLink} underline="hover" to={view.link.to} sx={linkSx}>
                {t(LINK_KEY[view.link.kind])}
              </Link>
            )}
          </Box>
        )}
      </Box>
      <Box
        sx={{
          flexShrink: 0,
          fontSize: 12,
          lineHeight: 1.6,
          color: colors.text.secondary,
        }}
      >
        <Ago at={event.t} />
      </Box>
    </Box>
  );
});

function EventsPanelView() {
  const { t } = useTranslation();
  const listId = useId();
  const [expanded, setExpanded] = useState(false);
  const hasFrame = useMetricsFrame(() => true) ?? false;
  const items = useMetricsFrame((f) => f.events?.items) ?? EMPTY_EVENTS;

  const sorted = useMemo(
    () => [...items].sort((a, b) => b.t - a.t || b.id - a.id),
    [items],
  );
  const shown = visibleRows(sorted, expanded);

  return (
    <PanelCard title={t("dashboard.events.title")} waiting={!hasFrame}>
      {sorted.length === 0 ? (
        <Box sx={emptySx}>{t("dashboard.events.empty")}</Box>
      ) : (
        <>
          <Box component="ul" id={listId} sx={listSx}>
            {shown.map((event) => (
              <EventRow key={event.id} event={event} />
            ))}
          </Box>
          <ShowMore
            total={sorted.length}
            expanded={expanded}
            onToggle={() => setExpanded((value) => !value)}
            controls={listId}
          />
        </>
      )}
    </PanelCard>
  );
}

export const EventsPanel = memo(EventsPanelView);
