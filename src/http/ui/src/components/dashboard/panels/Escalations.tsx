import { memo, useId, useState } from "react";
import { Box, Button, Stack, Tooltip, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors, fonts } from "@design";
import { ClearIcon } from "@b4.icons";
import { B4Dialog } from "@common/B4Dialog";
import { formatInteger } from "@common/charts";
import { apiPost } from "@api/apiClient";
import { useSnackbar } from "@context/SnackbarProvider";
import { describeApiError } from "@utils";
import type { EscalationEntry } from "@models/metrics";
import { useMetricsFrame, useServerNow } from "@/stores/useMetrics";
import { AGO_TICK_MS } from "./Ago";
import { PanelCard } from "./PanelCard";
import { ShowMore, visibleRows } from "./ShowMore";
import { formatSince, parseTimestamp, splitRemaining } from "./format";
import { emptySx, listSx, numeric, rowSx } from "./styles";

export interface EscalationsClearResult {
  success: boolean;
  cleared: number;
}

export const clearEscalations = () =>
  apiPost<EscalationsClearResult>("/api/escalations/clear");

const EMPTY_ITEMS: readonly EscalationEntry[] = [];

function TimeLeft({ expiresAt }: { expiresAt: string }) {
  const { t, i18n } = useTranslation();
  const now = useServerNow(AGO_TICK_MS);
  const expiry = parseTimestamp(expiresAt);
  if (!expiry) return null;
  const parts = splitRemaining(expiry - now);
  let text: string;
  if (!parts) {
    text = t("dashboard.escalations.expiring");
  } else {
    let duration: string;
    if (parts.hours > 0 && parts.minutes > 0) {
      duration = t("dashboard.escalations.hoursMinutes", {
        hours: t("charts.hours", { n: parts.hours }),
        minutes: t("charts.minutes", { n: parts.minutes }),
      });
    } else if (parts.hours > 0) {
      duration = t("charts.hours", { n: parts.hours });
    } else if (parts.minutes > 0) {
      duration = t("charts.minutes", { n: parts.minutes });
    } else {
      duration = t("charts.seconds", { n: parts.seconds });
    }
    text = t("dashboard.escalations.left", { duration });
  }
  return (
    <Tooltip
      title={t("dashboard.escalations.expiresAt", {
        time: formatSince(expiry, now, i18n.language),
      })}
      describeChild
    >
      <Box component="span" sx={{ ...numeric, whiteSpace: "nowrap" }}>
        {text}
      </Box>
    </Tooltip>
  );
}

const EscalationRow = memo(function EscalationRow({
  entry,
}: {
  entry: EscalationEntry;
}) {
  const { t, i18n } = useTranslation();
  return (
    <Box
      component="li"
      sx={{
        ...rowSx,
        display: "flex",
        flexWrap: "wrap",
        alignItems: "baseline",
        columnGap: "14px",
        rowGap: "2px",
      }}
    >
      <Box
        component="span"
        title={entry.host}
        sx={{
          flex: "1 1 200px",
          minWidth: 0,
          overflow: "hidden",
          textOverflow: "ellipsis",
          whiteSpace: "nowrap",
          fontFamily: fonts.mono,
          fontSize: 12,
          color: colors.text.primary,
        }}
      >
        {entry.host}
      </Box>
      <Box
        sx={{
          display: "flex",
          flexWrap: "wrap",
          alignItems: "baseline",
          columnGap: "14px",
          rowGap: "2px",
          fontSize: 12,
          color: colors.text.secondary,
        }}
      >
        <Tooltip title={t("dashboard.escalations.movedHint")} describeChild>
          <Box component="span" sx={{ whiteSpace: "nowrap" }}>
            {t("dashboard.escalations.movedTo", { set: entry.to_set || "-" })}
          </Box>
        </Tooltip>
        {entry.hops > 1 && (
          <Tooltip title={t("dashboard.escalations.hopsHint")} describeChild>
            <Box component="span" sx={{ ...numeric, whiteSpace: "nowrap" }}>
              {t("dashboard.escalations.hopsCount", {
                count: entry.hops,
                value: formatInteger(entry.hops, i18n.language),
              })}
            </Box>
          </Tooltip>
        )}
        <TimeLeft expiresAt={entry.expires_at} />
      </Box>
    </Box>
  );
});

function EscalationsPanelView() {
  const { t, i18n } = useTranslation();
  const locale = i18n.language;
  const listId = useId();
  const { showSuccess, showError } = useSnackbar();
  const [expanded, setExpanded] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const hasFrame = useMetricsFrame(() => true) ?? false;
  const items = useMetricsFrame((f) => f.escalations?.items) ?? EMPTY_ITEMS;
  const total = useMetricsFrame((f) => f.totals.escalations) ?? 0;
  const statsSince = useMetricsFrame((f) => f.stats_since) ?? 0;
  const now = useMetricsFrame((f) => Math.floor(f.now / 60_000) * 60_000) ?? 0;

  const runClear = async () => {
    setBusy(true);
    try {
      const result = await clearEscalations();
      const cleared = typeof result.cleared === "number" ? result.cleared : 0;
      showSuccess(
        t("dashboard.escalations.cleared", {
          count: cleared,
          value: formatInteger(cleared, locale),
        }),
      );
      setConfirmOpen(false);
    } catch (error) {
      showError(
        t("dashboard.escalations.clearFailed", { error: describeApiError(error) }),
      );
    } finally {
      setBusy(false);
    }
  };

  const actions = (
    <>
      <Box component="span" sx={{ ...numeric, fontSize: 12, color: colors.text.secondary }}>
        {t("dashboard.escalations.since", {
          count: total,
          value: formatInteger(total, locale),
          time: formatSince(statsSince, now, locale),
        })}
      </Box>
      {items.length > 0 && (
        <Button
          size="small"
          variant="outlined"
          startIcon={<ClearIcon sx={{ fontSize: 16 }} />}
          onClick={() => setConfirmOpen(true)}
          disabled={busy}
          sx={{ py: "2px", px: "10px", fontSize: 12 }}
        >
          {t("dashboard.escalations.clear")}
        </Button>
      )}
    </>
  );

  return (
    <>
      <PanelCard
        title={t("dashboard.escalations.title")}
        actions={hasFrame ? actions : undefined}
        waiting={!hasFrame}
      >
        {items.length === 0 ? (
          <Box sx={emptySx}>{t("dashboard.escalations.empty")}</Box>
        ) : (
          <>
            <Box component="ul" id={listId} sx={listSx}>
              {visibleRows(items, expanded).map((entry) => (
                <EscalationRow key={entry.host} entry={entry} />
              ))}
            </Box>
            <ShowMore
              total={items.length}
              expanded={expanded}
              onToggle={() => setExpanded((value) => !value)}
              controls={listId}
            />
          </>
        )}
      </PanelCard>
      <B4Dialog
        open={confirmOpen}
        onClose={() => {
          if (!busy) setConfirmOpen(false);
        }}
        title={t("dashboard.escalations.confirmTitle")}
        maxWidth="sm"
        fullWidth
        actions={
          <Stack direction="row" spacing={1}>
            <Button
              onClick={() => setConfirmOpen(false)}
              disabled={busy}
              sx={{ color: colors.text.secondary }}
            >
              {t("core.cancel")}
            </Button>
            <Button
              onClick={() => void runClear()}
              disabled={busy}
              variant="contained"
              color="warning"
            >
              {t("dashboard.escalations.clear")}
            </Button>
          </Stack>
        }
      >
        <Typography sx={{ color: colors.text.primary, mt: 1, fontSize: 14 }}>
          {t("dashboard.escalations.confirmBody", {
            count: items.length,
            value: formatInteger(items.length, locale),
          })}
        </Typography>
        <Typography sx={{ color: colors.text.secondary, mt: 1, fontSize: 13 }}>
          {t("dashboard.escalations.confirmDetail")}
        </Typography>
      </B4Dialog>
    </>
  );
}

export const EscalationsPanel = memo(EscalationsPanelView);
