import { useEffect, useMemo, useRef, useState } from "react";
import { Box, Button, IconButton, Paper, Stack, Tooltip, Typography } from "@mui/material";
import KeyboardIcon from "@mui/icons-material/KeyboardOutlined";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import { useQueue } from "@/features/sets/api";
import { setPath } from "@/features/sets/SetDrawerHost";
import type { EntryView, SetAction } from "@/models/api";
import { EmptyState, ErrorState, Loading } from "@/shared/components/States";
import { useOverlay } from "@/shared/hooks/useOverlay";
import { useHotkeys, type Hotkey } from "@/shared/hooks/useHotkeys";
import { setRef } from "@/shared/utils/format";
import { useModerationContext } from "@/features/moderation/ModerationProvider";
import { BulkDialog } from "@/features/moderation/BulkDialog";
import { QueueCard } from "./QueueCard";
import { ShortcutsDialog } from "./ShortcutsDialog";
import { SimilarSets } from "./SimilarSets";

const keyOf = (e: EntryView) => setRef(e.set_id, e.version);

export function QueuePage() {
  const { t } = useTranslation();
  const sets = useQueue();
  const moderation = useModerationContext();
  const overlay = useOverlay();
  const pending = useMemo(() => sets.data?.items ?? [], [sets.data]);
  const [focus, setFocus] = useState(0);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [compare, setCompare] = useState<Set<string>>(new Set());
  const [bulk, setBulk] = useState<SetAction | null>(null);
  const [help, setHelp] = useState(false);
  const cards = useRef(new Map<string, HTMLDivElement>());

  useEffect(() => {
    if (focus >= pending.length && pending.length > 0) setFocus(pending.length - 1);
  }, [focus, pending.length]);

  useEffect(() => {
    const present = new Set(pending.map(keyOf));
    setSelected((prev) => {
      const next = new Set([...prev].filter((k) => present.has(k)));
      return next.size === prev.size ? prev : next;
    });
  }, [pending]);

  const current = pending[Math.min(focus, pending.length - 1)] as EntryView | undefined;

  const move = (delta: number) => {
    if (pending.length === 0) return;
    const next = Math.max(0, Math.min(pending.length - 1, focus + delta));
    setFocus(next);
    const el = cards.current.get(keyOf(pending[next]));
    el?.scrollIntoView({ block: "nearest", behavior: "smooth" });
  };

  const toggle = (set: Set<string>, key: string) => {
    const next = new Set(set);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    return next;
  };

  const targets = pending.filter((e) => selected.has(keyOf(e))).map((e) => ({ set_id: e.set_id, version: e.version, title: e.title, status: e.status }));

  const bindings: Hotkey[] = [
    { keys: "j|ArrowDown", labelKey: "shortcuts.next", run: () => move(1) },
    { keys: "k|ArrowUp", labelKey: "shortcuts.prev", run: () => move(-1) },
    { keys: "o|Enter", labelKey: "shortcuts.open", run: () => current && overlay.open(setPath(current.set_id, current.version)) },
    { keys: "a", labelKey: "shortcuts.approve", run: () => current && moderation.approve(current) },
    { keys: "r", labelKey: "shortcuts.reject", run: () => current && moderation.reject(current) },
    { keys: "e", labelKey: "shortcuts.edit", run: () => current && moderation.edit(current) },
    { keys: "h", labelKey: "shortcuts.hide", run: () => current && moderation.hide(current) },
    { keys: "c", labelKey: "shortcuts.compare", run: () => current && setCompare((s) => toggle(s, keyOf(current))) },
    { keys: "b", labelKey: "shortcuts.ban", run: () => current && !current.author_banned && moderation.ban(current.uploader_hmac, current.author) },
    { keys: "x", labelKey: "shortcuts.select", run: () => current && setSelected((s) => toggle(s, keyOf(current))) },
    { keys: "shift+a", labelKey: "shortcuts.bulkApprove", run: () => selected.size > 0 && setBulk("approve") },
    { keys: "shift+r", labelKey: "shortcuts.bulkReject", run: () => selected.size > 0 && setBulk("reject") },
    { keys: "?", labelKey: "shortcuts.help", run: () => setHelp(true) },
  ];
  useHotkeys(bindings, !moderation.open && bulk === null && !help);

  if (sets.data === undefined) {
    return sets.error ? <ErrorState error={sets.error} onRetry={() => void sets.refetch()} /> : <Loading />;
  }

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
      <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
        <Typography variant="sectionHeader" sx={{ flex: 1 }}>
          {t("queue.title", { count: pending.length })}
        </Typography>
        {pending.length > 0 && (
          <Button size="small" onClick={() => setSelected(selected.size === pending.length ? new Set() : new Set(pending.map(keyOf)))}>
            {selected.size === pending.length ? t("table.clearSelection") : t("queue.selectAll")}
          </Button>
        )}
        <Tooltip title={t("shortcuts.title")}>
          <IconButton size="small" onClick={() => setHelp(true)}>
            <KeyboardIcon fontSize="small" />
          </IconButton>
        </Tooltip>
      </Box>
      {selected.size > 0 && (
        <Paper variant="outlined" sx={{ p: 1.5, display: "flex", gap: 1, alignItems: "center", flexWrap: "wrap", position: "sticky", top: 0, zIndex: 3, bgcolor: colors.background.paper }}>
          <Typography variant="body2" sx={{ flex: 1 }}>
            {t("table.selected", { count: selected.size })}
          </Typography>
          <Stack direction="row" spacing={1}>
            <Button size="small" variant="contained" color="success" onClick={() => setBulk("approve")}>
              {t("queue.approve")}
            </Button>
            <Button size="small" variant="outlined" color="error" onClick={() => setBulk("reject")}>
              {t("queue.reject")}
            </Button>
            <Button size="small" variant="outlined" color="inherit" onClick={() => setBulk("hide")}>
              {t("queue.hide")}
            </Button>
            <Button size="small" color="inherit" onClick={() => setSelected(new Set())}>
              {t("table.clearSelection")}
            </Button>
          </Stack>
        </Paper>
      )}
      {pending.length === 0 ? (
        <EmptyState text={t("queue.empty")} />
      ) : (
        pending.map((entry, index) => {
          const key = keyOf(entry);
          return (
            <QueueCard
              key={key}
              ref={(el) => {
                if (el) cards.current.set(key, el);
                else cards.current.delete(key);
              }}
              entry={entry}
              moderation={moderation}
              focused={index === focus}
              selected={selected.has(key)}
              compare={compare.has(key)}
              onToggleSelect={() => setSelected((s) => toggle(s, key))}
              onToggleCompare={() => setCompare((s) => toggle(s, key))}
              onOpen={() => overlay.open(setPath(entry.set_id, entry.version))}
              onFocus={() => setFocus(index)}
              similar={<SimilarSets entry={entry} onReject={moderation.reject} />}
            />
          );
        })
      )}
      <BulkDialog action={bulk ?? "approve"} targets={targets} open={bulk !== null} onClose={() => setBulk(null)} onDone={() => setSelected(new Set())} />
      <ShortcutsDialog open={help} onClose={() => setHelp(false)} bindings={bindings} />
    </Box>
  );
}
