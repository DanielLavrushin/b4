import { useEffect, useMemo, useState } from "react";
import { Box, Button, Grid, Paper, Stack, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { FacetCompareBar, colors } from "@design";
import { useQueue } from "@/features/sets/api";
import { useCardPanels } from "@/features/sets/card/useCardPanels";
import type { EntryView, SetAction } from "@/models/api";
import { EmptyState, ErrorState, Loading } from "@/shared/components/States";
import { setRef } from "@/shared/utils/format";
import { useModerationContext } from "@/features/moderation/ModerationProvider";
import { BulkDialog } from "@/features/moderation/BulkDialog";
import { QueueCard } from "./QueueCard";

const keyOf = (e: EntryView) => setRef(e.set_id, e.version);

const toggle = (set: Set<string>, key: string) => {
  const next = new Set(set);
  if (next.has(key)) next.delete(key);
  else next.add(key);
  return next;
};

export function QueuePage() {
  const { t } = useTranslation();
  const sets = useQueue();
  const moderation = useModerationContext();
  const pending = useMemo(() => sets.data?.items ?? [], [sets.data]);
  const keys = useMemo(() => pending.map(keyOf), [pending]);
  const panels = useCardPanels(keys);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [bulk, setBulk] = useState<SetAction | null>(null);

  useEffect(() => {
    const present = new Set(keys);
    setSelected((prev) => {
      const next = new Set([...prev].filter((k) => present.has(k)));
      return next.size === prev.size ? prev : next;
    });
  }, [keys]);

  if (sets.data === undefined) {
    return sets.error ? <ErrorState error={sets.error} onRetry={() => void sets.refetch()} /> : <Loading />;
  }

  const targets = pending.filter((e) => selected.has(keyOf(e))).map((e) => ({ set_id: e.set_id, version: e.version, title: e.title, status: e.status }));
  const allSelected = pending.length > 0 && selected.size === pending.length;

  return (
    <Box sx={{ display: "flex", flexDirection: "column" }}>
      <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap", mb: 2 }}>
        <Typography variant="sectionHeader" sx={{ flex: 1 }}>
          {t("queue.title", { count: pending.length })}
        </Typography>
        {pending.length > 0 && (
          <Button size="small" onClick={() => setSelected(allSelected ? new Set() : new Set(keys))}>
            {allSelected ? t("table.clearSelection") : t("queue.selectAll")}
          </Button>
        )}
      </Box>
      {selected.size > 0 && (
        <Paper
          variant="outlined"
          sx={{ p: 1.5, mb: 2, display: "flex", gap: 1, alignItems: "center", flexWrap: "wrap", position: "sticky", top: 0, zIndex: 3, bgcolor: colors.background.paper }}
        >
          <Typography variant="body2" sx={{ flex: 1 }}>
            {t("table.selected", { count: selected.size })}
          </Typography>
          <Stack direction="row" spacing={1} useFlexGap flexWrap="wrap">
            <Button size="small" variant="contained" onClick={() => setBulk("approve")}>
              {t("queue.approve")}
            </Button>
            <Button size="small" variant="outlined" onClick={() => setBulk("reject")}>
              {t("queue.reject")}
            </Button>
            <Button size="small" variant="outlined" onClick={() => setBulk("hide")}>
              {t("queue.hide")}
            </Button>
            <Button size="small" onClick={() => setSelected(new Set())} sx={{ color: colors.text.secondary }}>
              {t("table.clearSelection")}
            </Button>
          </Stack>
        </Paper>
      )}
      {pending.length > 1 && (
        <FacetCompareBar active={panels.compare} onPick={panels.pickCompare} toggle={panels.toggle} onToggle={panels.toggleAll} t={t} />
      )}
      {pending.length === 0 ? (
        <EmptyState text={t("queue.empty")} />
      ) : (
        <Grid container spacing={3}>
          {pending.map((entry) => {
            const key = keyOf(entry);
            return (
              <Grid key={key} size={{ xs: 12, md: 6, xl: 4 }}>
                <QueueCard
                  entry={entry}
                  moderation={moderation}
                  selected={selected.has(key)}
                  onToggleSelect={() => setSelected((s) => toggle(s, key))}
                  panel={panels.panelOf(key)}
                  onPanelChange={(next) => panels.setPanel(key, next)}
                />
              </Grid>
            );
          })}
        </Grid>
      )}
      <BulkDialog action={bulk ?? "approve"} targets={targets} open={bulk !== null} onClose={() => setBulk(null)} onDone={() => setSelected(new Set())} />
    </Box>
  );
}
