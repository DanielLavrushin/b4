import { memo, useCallback, useId, useState } from "react";
import { Box, Button, Stack, Tooltip } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors, fonts } from "@design";
import { ClearIcon } from "@b4.icons";
import { B4Dialog } from "@common/B4Dialog";
import { apiPost } from "@api/apiClient";
import { useSnackbar } from "@context/SnackbarProvider";
import { describeApiError } from "@utils";
import type { CollectedEntry } from "@models/metrics";
import { useMetricsFrame } from "@/stores/useMetrics";
import { PanelCard } from "./PanelCard";
import { ShowMore, visibleRows } from "./ShowMore";
import { emptySx, listSx, rowSx } from "./styles";

interface ClearResult { success: boolean; cleared: number }
export const clearCollected = (ip?: string) =>
  apiPost<ClearResult>("/api/collected-ips/clear", ip ? { ip } : {});
const EMPTY: readonly CollectedEntry[] = [];

function CollectedPanelView() {
  const { t } = useTranslation();
  const { showSuccess, showError } = useSnackbar();
  const listId = useId();
  const rev = useMetricsFrame((f) => f.collected?.rev) ?? 0;
  const items = useMetricsFrame((f) => f.collected?.items) ?? EMPTY;
  const [expanded, setExpanded] = useState(false);
  const [busy, setBusy] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [hidden, setHidden] = useState<{ rev: number; ips: readonly string[] }>({ rev: -1, ips: [] });
  const rows = hidden.rev === rev ? items.filter((e) => !hidden.ips.includes(e.ip)) : items;
  const remove = useCallback(async (ip?: string) => {
    setBusy(true);
    try {
      const result = await clearCollected(ip);
      setHidden((previous) => ({
        rev,
        ips: ip ? [...(previous.rev === rev ? previous.ips : []), ip] : items.map((e) => e.ip),
      }));
      showSuccess(ip ? t("dashboard.collected.removed", { address: ip }) : t("dashboard.collected.cleared", { count: result.cleared }));
      setConfirmOpen(false);
    } catch (error) {
      showError(t("dashboard.collected.removeFailed", { error: describeApiError(error) }));
    } finally {
      setBusy(false);
    }
  }, [rev, items, showSuccess, showError, t]);
  return (
    <>
      <PanelCard
        title={t("dashboard.collected.title")}
        subtitle={t("dashboard.collected.subtitle")}
        actions={rows.length > 0 ? (
          <Button size="small" variant="outlined" startIcon={<ClearIcon sx={{ fontSize: 16 }} />}
            onClick={() => setConfirmOpen(true)} disabled={busy}
            sx={{ py: "2px", px: "10px", fontSize: 12 }}>
            {t("dashboard.collected.clear")}
          </Button>
        ) : undefined}
      >
        {rows.length === 0 ? <Box sx={emptySx}>{t("dashboard.collected.empty")}</Box> : (
          <>
            <Box component="ul" id={listId} sx={listSx}>
              {visibleRows(rows, expanded).map((entry) => (
                <Box key={entry.ip} component="li" sx={{ ...rowSx, display: "flex", alignItems: "center", gap: "12px" }}>
                  <Box sx={{ flex: "1 1 auto", minWidth: 0 }}>
                    <Box sx={{ display: "flex", flexWrap: "wrap", alignItems: "baseline", columnGap: "8px", rowGap: "2px", minWidth: 0 }}>
                      <Box component="span" sx={{ fontFamily: fonts.mono, fontSize: 12, color: colors.text.primary, overflowWrap: "anywhere" }}>{entry.ip}</Box>
                      {entry.host && <Box component="span" title={entry.host} sx={{ fontSize: 12, minWidth: 0, color: colors.text.secondary, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{entry.host}</Box>}
                    </Box>
                    {entry.set && <Box sx={{ fontSize: 12, color: colors.text.secondary, overflowWrap: "anywhere" }}>{t("dashboard.collected.set", { set: entry.set })}</Box>}
                  </Box>
                  <Tooltip title={t("dashboard.collected.removeHint")} describeChild>
                    <span><Button size="small" disabled={busy} onClick={() => void remove(entry.ip)}
                      aria-label={t("dashboard.collected.removeLabel", { address: entry.ip })}
                      sx={{ flexShrink: 0, minWidth: 0, py: "2px", px: "8px", fontSize: 12 }}>
                      {t("dashboard.collected.remove")}
                    </Button></span>
                  </Tooltip>
                </Box>
              ))}
            </Box>
            <ShowMore total={rows.length} expanded={expanded} onToggle={() => setExpanded((value) => !value)} controls={listId} />
          </>
        )}
      </PanelCard>
      <B4Dialog open={confirmOpen} onClose={() => { if (!busy) setConfirmOpen(false); }}
        title={t("dashboard.collected.confirmTitle")} maxWidth="sm" fullWidth
        actions={
          <Stack direction="row" spacing={1} sx={{ width: "100%" }}>
            <Button disabled={busy} onClick={() => setConfirmOpen(false)}>{t("core.cancel")}</Button>
            <Box sx={{ flex: 1 }} />
            <Button variant="contained" disabled={busy} onClick={() => void remove()}>{t("dashboard.collected.clear")}</Button>
          </Stack>
        }>
        {t("dashboard.collected.confirmBody")}
      </B4Dialog>
    </>
  );
}

export const CollectedPanel = memo(CollectedPanelView);
