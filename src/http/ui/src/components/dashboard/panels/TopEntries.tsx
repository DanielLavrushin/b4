import { memo, useId, useMemo, useState, type FocusEvent, type ReactNode } from "react";
import { Box, IconButton, Tooltip } from "@mui/material";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { colors, fonts, radiusPx } from "@design";
import { AddIcon } from "@b4.icons";
import { formatInteger } from "@common/charts";
import { apiGet } from "@api/apiClient";
import type { B4Config, B4SetConfig } from "@models/config";
import type { TopEntry } from "@models/metrics";
import { shallowEqual, useMetricsFrame } from "@/stores/useMetrics";
import { Ago } from "./Ago";
import { ShowMore, visibleRows } from "./ShowMore";
import { Tag } from "./Tag";
import { emptySx, listSx, numeric, rowSx } from "./styles";

const TOP_ROWS = 10;
const NO_NAMES: readonly string[] = [];
const NO_SETS: B4SetConfig[] = [];
const DIALOG_CONFIG_KEY = ["dashboard", "dialog-config"] as const;

export interface PendingSet {
  setId: string;
  setName: string;
}

interface HeldOrder {
  keys: string[];
  byKey: Map<string, TopEntry>;
}

const addSx = {
  width: 20,
  height: 20,
  p: 0,
  flexShrink: 0,
  border: `1px dashed ${colors.border.default}`,
  borderRadius: `${radiusPx.lg}px`,
  color: colors.text.secondary,
  "& svg": { fontSize: 14 },
  "&:hover": {
    color: colors.secondary,
    borderColor: colors.border.strong,
    bgcolor: colors.accent.secondaryHover,
  },
  "&:focus-visible": {
    outline: `2px solid ${colors.border.strong}`,
    outlineOffset: "2px",
  },
} as const;

const knownSets = (entry: TopEntry, names: ReadonlyMap<string, string>): string[] =>
  entry.sets.filter((id) => names.has(id));

export function useDialogConfig(enabled: boolean) {
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: DIALOG_CONFIG_KEY,
    queryFn: () => apiGet<B4Config>("/api/config"),
    enabled,
  });
  return {
    sets: query.data?.sets ?? NO_SETS,
    ipInfoToken: query.data?.system?.api?.ipinfo_token ?? "",
    refresh: () => {
      void queryClient.invalidateQueries({ queryKey: DIALOG_CONFIG_KEY });
    },
  };
}

function useSetNames(): ReadonlyMap<string, string> {
  const flat =
    useMetricsFrame((f) => f.sets.flatMap((s) => [s.id, s.name || s.id]), shallowEqual) ??
    NO_NAMES;
  return useMemo(() => {
    const map = new Map<string, string>();
    for (let i = 0; i + 1 < flat.length; i += 2) map.set(flat[i], flat[i + 1]);
    return map;
  }, [flat]);
}

interface TopEntryRowProps {
  entry: TopEntry;
  detail?: string;
  setNames: ReadonlyMap<string, string>;
  pending?: PendingSet;
  addTip: string;
  addLabel: string;
  onAdd: (key: string) => void;
}

const TopEntryRow = memo(function TopEntryRow({
  entry,
  detail,
  setNames,
  pending,
  addTip,
  addLabel,
  onAdd,
}: TopEntryRowProps) {
  const { i18n } = useTranslation();
  const sets = knownSets(entry, setNames);
  let membership: ReactNode;
  if (sets.length > 0) {
    membership = sets.map((id) => (
      <Tag key={id} label={setNames.get(id) ?? id} to={`/sets/${encodeURIComponent(id)}`} />
    ));
  } else if (pending) {
    membership = (
      <Tag
        label={pending.setName}
        to={pending.setId ? `/sets/${encodeURIComponent(pending.setId)}` : undefined}
      />
    );
  } else {
    membership = (
      <Tooltip title={addTip}>
        <IconButton aria-label={addLabel} onClick={() => onAdd(entry.key)} sx={addSx}>
          <AddIcon />
        </IconButton>
      </Tooltip>
    );
  }
  return (
    <Box component="li" sx={{ ...rowSx, display: "flex", alignItems: "center", gap: "12px" }}>
      <Box
        sx={{
          flex: "1 1 auto",
          minWidth: 0,
          display: "flex",
          flexWrap: "wrap",
          alignItems: "center",
          columnGap: "8px",
          rowGap: "4px",
        }}
      >
        <Box
          component="span"
          title={entry.key}
          sx={{
            minWidth: 0,
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
        {detail && (
          <Box
            component="span"
            title={detail}
            sx={{
              minWidth: 0,
              maxWidth: "100%",
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: "nowrap",
              fontSize: 12,
              color: colors.text.secondary,
            }}
          >
            {detail}
          </Box>
        )}
        {membership}
      </Box>
      <Box
        component="span"
        sx={{
          ...numeric,
          flexShrink: 0,
          fontSize: 13,
          fontWeight: 700,
          color: colors.text.primary,
        }}
      >
        {formatInteger(entry.count, i18n.language)}
      </Box>
      <Box
        component="span"
        sx={{
          flexShrink: 0,
          minWidth: 72,
          textAlign: "right",
          fontSize: 12,
          color: colors.text.secondary,
        }}
      >
        <Ago at={entry.last} />
      </Box>
    </Box>
  );
});

interface TopEntriesProps {
  entries: readonly TopEntry[];
  empty: string;
  addTip: string;
  addLabel: (key: string) => string;
  detail?: (key: string) => string | undefined;
  pendingFor: (entry: TopEntry) => PendingSet | undefined;
  onAdd: (key: string) => void;
}

export function TopEntries({
  entries,
  empty,
  addTip,
  addLabel,
  detail,
  pendingFor,
  onAdd,
}: TopEntriesProps) {
  const listId = useId();
  const setNames = useSetNames();
  const [expanded, setExpanded] = useState(false);
  const [held, setHeld] = useState<HeldOrder | null>(null);

  const rows = useMemo(() => {
    if (!held) return entries;
    const live = new Map(entries.map((entry) => [entry.key, entry]));
    return held.keys
      .map((key) => live.get(key) ?? held.byKey.get(key))
      .filter((entry): entry is TopEntry => entry !== undefined);
  }, [entries, held]);

  if (rows.length === 0) return <Box sx={emptySx}>{empty}</Box>;

  const hold = () =>
    setHeld(
      (prev) =>
        prev ?? {
          keys: entries.map((entry) => entry.key),
          byKey: new Map(entries.map((entry) => [entry.key, entry])),
        },
    );
  const release = () => setHeld(null);
  const releaseOnLeave = (e: FocusEvent<HTMLElement>) => {
    if (!e.currentTarget.contains(e.relatedTarget)) release();
  };

  return (
    <>
      <Box
        component="ul"
        id={listId}
        sx={listSx}
        onMouseEnter={hold}
        onMouseLeave={release}
        onFocus={hold}
        onBlur={releaseOnLeave}
      >
        {visibleRows(rows, expanded, TOP_ROWS).map((entry) => (
          <TopEntryRow
            key={entry.key}
            entry={entry}
            detail={detail?.(entry.key)}
            setNames={setNames}
            pending={knownSets(entry, setNames).length > 0 ? undefined : pendingFor(entry)}
            addTip={addTip}
            addLabel={addLabel(entry.key)}
            onAdd={onAdd}
          />
        ))}
      </Box>
      <ShowMore
        total={rows.length}
        expanded={expanded}
        onToggle={() => setExpanded((value) => !value)}
        limit={TOP_ROWS}
        controls={listId}
      />
    </>
  );
}
