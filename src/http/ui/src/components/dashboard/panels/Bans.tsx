import { memo, useCallback, useId, useState } from "react";
import { Box, Button, Stack, Tooltip, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors, fonts } from "@design";
import { ClearIcon } from "@b4.icons";
import { B4Dialog } from "@common/B4Dialog";
import { formatInteger } from "@common/charts";
import { apiPost } from "@api/apiClient";
import { useSnackbar } from "@context/SnackbarProvider";
import { describeApiError } from "@utils";
import type { BanEntry } from "@models/metrics";
import { useMetricsFrame, useServerNow } from "@/stores/useMetrics";
import { formatUptime } from "../statusFormat";
import { AGO_TICK_MS } from "./Ago";
import { PanelCard } from "./PanelCard";
import { ShowMore, visibleRows } from "./ShowMore";
import { Tag } from "./Tag";
import { formatSince, validTime } from "./format";
import { emptySx, listSx, numeric, rowSx } from "./styles";

export interface BansClearResult {
  success: boolean;
  cleared: number;
}

export const clearBans = (key?: string) =>
  apiPost<BansClearResult>("/api/ipbans/clear", key ? { key } : {});

const EMPTY_ITEMS: readonly BanEntry[] = [];

interface HiddenKeys {
  rev: number;
  keys: readonly string[];
}

const NO_HIDDEN: HiddenKeys = { rev: -1, keys: [] };

function TimeLeft({ until }: { until: number }) {
  const { t, i18n } = useTranslation();
  const now = useServerNow(AGO_TICK_MS);
  if (!validTime(until)) return null;
  const left = until - now;
  const text =
    left > 0
      ? t("dashboard.bans.left", {
          duration: formatUptime(Math.ceil(left / 1000), t),
        })
      : t("dashboard.bans.expiring");
  return (
    <Tooltip
      title={t("dashboard.bans.endsAt", {
        time: formatSince(until, now, i18n.language),
      })}
      describeChild
    >
      <Box component="span" sx={{ ...numeric, whiteSpace: "nowrap" }}>
        {text}
      </Box>
    </Tooltip>
  );
}

interface BanRowProps {
  entry: BanEntry;
  disabled: boolean;
  onUnban: (key: string) => Promise<void>;
}

const BanRow = memo(function BanRow({ entry, disabled, onUnban }: BanRowProps) {
  const { t, i18n } = useTranslation();
  return (
    <Box
      component="li"
      sx={{ ...rowSx, display: "flex", alignItems: "center", gap: "12px" }}
    >
      <Box
        sx={{
          flex: "1 1 auto",
          minWidth: 0,
          display: "flex",
          flexWrap: "wrap",
          alignItems: "baseline",
          columnGap: "14px",
          rowGap: "2px",
        }}
      >
        <Box
          sx={{
            flex: "1 1 200px",
            minWidth: 0,
            display: "flex",
            alignItems: "baseline",
            columnGap: "8px",
          }}
        >
          <Box
            component="span"
            title={entry.key}
            sx={{
              flexShrink: 0,
              maxWidth: "100%",
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: "nowrap",
              fontFamily: fonts.mono,
              fontSize: 12,
              color: colors.text.primary,
            }}
          >
            {entry.key}
          </Box>
          {entry.host && (
            <Box
              component="span"
              title={entry.host}
              sx={{
                minWidth: 0,
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
                fontSize: 12,
                color: colors.text.secondary,
              }}
            >
              {entry.host}
            </Box>
          )}
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
          {entry.set && (
            <Tooltip title={t("dashboard.bans.setHint")} describeChild>
              <Box component="span" sx={{ whiteSpace: "nowrap" }}>
                {t("dashboard.bans.set", { set: entry.set })}
              </Box>
            </Tooltip>
          )}
          <Tooltip title={t("dashboard.bans.strikesHint")} describeChild>
            <Box component="span" sx={{ ...numeric, whiteSpace: "nowrap" }}>
              {t("dashboard.bans.strikes", {
                count: entry.strikes,
                value: formatInteger(entry.strikes, i18n.language),
              })}
            </Box>
          </Tooltip>
          {entry.repeat && (
            <Tooltip title={t("dashboard.bans.repeatHint")} describeChild>
              <Tag label={t("dashboard.bans.repeat")} />
            </Tooltip>
          )}
          <TimeLeft until={entry.until} />
        </Box>
      </Box>
      <Button
        size="small"
        onClick={() => void onUnban(entry.key)}
        disabled={disabled}
        aria-label={t("dashboard.bans.unbanLabel", { address: entry.key })}
        sx={{ flexShrink: 0, minWidth: 0, py: "2px", px: "8px", fontSize: 12 }}
      >
        {t("dashboard.bans.unban")}
      </Button>
    </Box>
  );
});

function BansPanelView() {
  const { t, i18n } = useTranslation();
  const locale = i18n.language;
  const listId = useId();
  const { showSuccess, showError } = useSnackbar();
  const [expanded, setExpanded] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [hidden, setHidden] = useState<HiddenKeys>(NO_HIDDEN);
  const hasFrame = useMetricsFrame(() => true) ?? false;
  const rev = useMetricsFrame((f) => f.bans?.rev) ?? 0;
  const items = useMetricsFrame((f) => f.bans?.items) ?? EMPTY_ITEMS;
  const rows =
    hidden.rev === rev
      ? items.filter((entry) => !hidden.keys.includes(entry.key))
      : items;

  const hide = useCallback(
    (keys: readonly string[]) =>
      setHidden((prev) => ({
        rev,
        keys: prev.rev === rev ? [...prev.keys, ...keys] : keys,
      })),
    [rev],
  );

  const unban = useCallback(
    async (key: string) => {
      setBusy(true);
      try {
        await clearBans(key);
        hide([key]);
        showSuccess(t("dashboard.bans.unbanned", { address: key }));
      } catch (error) {
        showError(
          t("dashboard.bans.unbanFailed", {
            address: key,
            error: describeApiError(error),
          }),
        );
      } finally {
        setBusy(false);
      }
    },
    [hide, showSuccess, showError, t],
  );

  const runClear = async () => {
    setBusy(true);
    try {
      const result = await clearBans();
      const cleared = typeof result.cleared === "number" ? result.cleared : 0;
      hide(rows.map((entry) => entry.key));
      showSuccess(
        t("dashboard.bans.cleared", {
          count: cleared,
          value: formatInteger(cleared, locale),
        }),
      );
      setConfirmOpen(false);
    } catch (error) {
      showError(
        t("dashboard.bans.clearFailed", { error: describeApiError(error) }),
      );
    } finally {
      setBusy(false);
    }
  };

  const actions =
    rows.length > 0 ? (
      <Button
        size="small"
        variant="outlined"
        startIcon={<ClearIcon sx={{ fontSize: 16 }} />}
        onClick={() => setConfirmOpen(true)}
        disabled={busy}
        sx={{ py: "2px", px: "10px", fontSize: 12 }}
      >
        {t("dashboard.bans.clear")}
      </Button>
    ) : undefined;

  return (
    <>
      <PanelCard
        title={t("dashboard.bans.title")}
        subtitle={t("dashboard.bans.subtitle")}
        actions={hasFrame ? actions : undefined}
        settings={{
          to: "/settings/general/engine",
          label: t("dashboard.bans.settings"),
        }}
        waiting={!hasFrame}
      >
        {rows.length === 0 ? (
          <Box sx={emptySx}>{t("dashboard.bans.empty")}</Box>
        ) : (
          <>
            <Box component="ul" id={listId} sx={listSx}>
              {visibleRows(rows, expanded).map((entry) => (
                <BanRow
                  key={entry.key}
                  entry={entry}
                  disabled={busy}
                  onUnban={unban}
                />
              ))}
            </Box>
            <ShowMore
              total={rows.length}
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
        title={t("dashboard.bans.confirmTitle")}
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
              {t("dashboard.bans.clear")}
            </Button>
          </Stack>
        }
      >
        <Typography sx={{ color: colors.text.primary, mt: 1, fontSize: 14 }}>
          {t("dashboard.bans.confirmBody", {
            count: rows.length,
            value: formatInteger(rows.length, locale),
          })}
        </Typography>
        <Typography sx={{ color: colors.text.secondary, mt: 1, fontSize: 13 }}>
          {t("dashboard.bans.confirmDetail")}
        </Typography>
      </B4Dialog>
    </>
  );
}

export const BansPanel = memo(BansPanelView);
