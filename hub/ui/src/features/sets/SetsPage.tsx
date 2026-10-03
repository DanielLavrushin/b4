import { useEffect, useMemo, useState, type ReactNode } from "react";
import {
  Box,
  Button,
  Grid,
  IconButton,
  LinearProgress,
  MenuItem,
  Paper,
  Stack,
  Tab,
  TablePagination,
  Tabs,
  TextField,
  ToggleButton,
  Tooltip,
  Typography,
} from "@mui/material";
import ArrowDownwardIcon from "@mui/icons-material/ArrowDownward";
import ArrowUpwardIcon from "@mui/icons-material/ArrowUpward";
import { useTranslation } from "react-i18next";
import { FacetCompareBar, colors, radius } from "@design";
import type { SetAction, SetGroupName, SetRowView } from "@/models/api";
import { PAGE_SIZES, useTableState } from "@/shared/table/useTableState";
import type { SortDir } from "@/shared/table/sort";
import { SearchField, fieldSx } from "@/shared/components/SearchField";
import { EmptyState, ErrorState } from "@/shared/components/States";
import { BulkDialog } from "@/features/moderation/BulkDialog";
import { useSetRows } from "./api";
import { RowCard } from "./card/row";
import { HUB_FACETS } from "./card/SetCard";
import { SetCardSkeleton } from "./card/SetCardSkeleton";
import { useCardPanels } from "./card/useCardPanels";

const groups: SetGroupName[] = ["listed", "superseded", "withheld", "hidden", "rejected"];

const bulkActions: Partial<Record<SetGroupName, SetAction[]>> = {
  listed: ["hide"],
  superseded: ["hide"],
  hidden: ["restore"],
  rejected: ["approve"],
};

const allSorts = ["updated", "created", "title", "author", "score", "n", "devices", "reports"];
const plainSorts = ["updated", "created", "title", "author", "reports"];
const unscored = new Set<SetGroupName>(["hidden", "rejected"]);

const filterFor = (group: SetGroupName, filter: string) => (filter === "attention" && group !== "listed" ? "" : filter);

const gridItem = { xs: 12, sm: 6, lg: 4, xl: 3 } as const;
const skeletons = [0, 1, 2, 3, 4, 5, 6, 7];

const rowKey = (r: SetRowView) => `${r.set_id}/${String(r.version)}`;

const tabsSx = {
  minHeight: 38,
  borderBottom: `1px solid ${colors.border.light}`,
  "& .MuiTabs-list": { gap: "4px" },
  "& .MuiTab-root": {
    minHeight: 38,
    px: 1.5,
    py: 1.25,
    fontSize: 13,
    textTransform: "none",
    color: colors.text.secondary,
    "&.Mui-selected": { color: colors.secondary },
  },
  "& .MuiTabs-indicator": { height: 2, bgcolor: colors.secondary },
} as const;

const progressSx = { height: 3, bgcolor: colors.accent.secondary, "& .MuiLinearProgress-bar": { bgcolor: colors.secondary } } as const;

interface SortControlProps {
  value: string;
  options: string[];
  dir: SortDir;
  onChange: (by: string, dir: SortDir) => void;
}

function SortControl({ value, options, dir, onChange }: Readonly<SortControlProps>) {
  const { t } = useTranslation();
  const dirLabel = t(dir === "asc" ? "sets.sort.asc" : "sets.sort.desc");
  return (
    <Stack direction="row" alignItems="center" spacing={0.5}>
      <TextField select size="small" label={t("sets.sort.label")} value={value} onChange={(e) => onChange(e.target.value, dir)} sx={{ ...fieldSx, minWidth: 190 }}>
        {options.map((key) => (
          <MenuItem key={key} value={key}>
            {t(`sets.sort.${key}`)}
          </MenuItem>
        ))}
      </TextField>
      <Tooltip title={dirLabel}>
        <IconButton size="small" aria-label={dirLabel} onClick={() => onChange(value, dir === "asc" ? "desc" : "asc")} sx={{ color: colors.text.secondary }}>
          {dir === "asc" ? <ArrowUpwardIcon fontSize="small" /> : <ArrowDownwardIcon fontSize="small" />}
        </IconButton>
      </Tooltip>
    </Stack>
  );
}

function FilterToggle({ value, active, label, onToggle }: Readonly<{ value: string; active: boolean; label: string; onToggle: () => void }>) {
  return (
    <ToggleButton size="small" value={value} selected={active} onChange={onToggle} sx={{ textTransform: "none" }}>
      {label}
    </ToggleButton>
  );
}

export function SetsPage() {
  const { t } = useTranslation();
  const table = useTableState({ sort: "updated", dir: "desc", pageSize: 50 });
  const group = (groups.includes(table.param("group") as SetGroupName) ? table.param("group") : "listed") as SetGroupName;
  const filter = filterFor(group, table.param("filter"));
  const sortOptions = unscored.has(group) ? plainSorts : allSorts;
  const sort = sortOptions.includes(table.sort) ? table.sort : "updated";
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [bulk, setBulk] = useState<SetAction | null>(null);
  const view = `${group}|${String(table.page)}|${String(table.pageSize)}|${table.query}|${filter}|${sort}|${table.dir}`;
  useEffect(() => {
    setSelected(new Set());
  }, [view]);
  const rows = useSetRows({ group, q: table.query, sort, dir: table.dir, offset: table.page * table.pageSize, limit: table.pageSize, filter });
  const data = rows.data;
  const shown = data?.group ?? group;
  const list = useMemo(() => data?.rows ?? [], [data]);
  const keys = useMemo(() => list.map(rowKey), [list]);
  const panels = useCardPanels(keys);
  const total = data?.total ?? 0;
  const lastPage = Math.max(0, Math.ceil(total / table.pageSize) - 1);
  const outOfRange = data !== undefined && total > 0 && table.page > lastPage;
  const { setPage } = table;

  useEffect(() => {
    const present = new Set(keys);
    setSelected((prev) => {
      const next = new Set([...prev].filter((k) => present.has(k)));
      return next.size === prev.size ? prev : next;
    });
  }, [keys]);

  useEffect(() => {
    if (outOfRange) setPage(lastPage);
  }, [outOfRange, lastPage, setPage]);

  const actions = bulkActions[shown] ?? [];
  const targets = list.filter((r) => selected.has(rowKey(r))).map((r) => ({ set_id: r.set_id, version: r.version, title: r.title, status: r.status }));
  const allSelected = list.length > 0 && list.every((r) => selected.has(rowKey(r)));
  const filtered = table.query !== "" || filter !== "";
  const tabLabel = (g: SetGroupName) => (data ? t("sets.tabCount", { label: t(`sets.${g}`), count: data.groups[g] ?? 0 }) : t(`sets.${g}`));
  const toggleFilter = (name: string) => table.setParam("filter", filter === name ? null : name);
  const toggleRow = (key: string) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });

  let content: ReactNode;
  if (data === undefined) {
    content = rows.error ? null : (
      <Grid container spacing={3}>
        {skeletons.map((i) => (
          <Grid key={i} size={gridItem}>
            <SetCardSkeleton />
          </Grid>
        ))}
      </Grid>
    );
  } else if (list.length === 0) {
    content = <EmptyState text={filtered ? t("sets.noMatch") : t("sets.empty")} />;
  } else {
    content = (
      <Grid container spacing={3}>
        {list.map((row) => {
          const key = rowKey(row);
          return (
            <Grid key={key} size={gridItem}>
              <RowCard
                row={row}
                group={shown}
                selection={actions.length > 0 ? { selected: selected.has(key), onToggle: () => toggleRow(key), label: t("card.select", { title: row.title }) } : undefined}
                panel={panels.panelOf(key)}
                onPanelChange={(next) => panels.setPanel(key, next)}
              />
            </Grid>
          );
        })}
      </Grid>
    );
  }

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
      <Tabs
        value={group}
        variant="scrollable"
        allowScrollButtonsMobile
        onChange={(_e, v: SetGroupName) => {
          setSelected(new Set());
          table.setParam("group", v === "listed" ? null : v);
        }}
        sx={tabsSx}
      >
        {groups.map((g) => (
          <Tab key={g} value={g} label={tabLabel(g)} />
        ))}
      </Tabs>
      <Box sx={{ display: "flex", gap: 1.5, alignItems: "center", flexWrap: "wrap" }}>
        <SearchField value={table.q} onChange={table.setQ} placeholder={t("sets.searchPlaceholder")} />
        <Box sx={{ display: "flex", alignItems: "center", flexWrap: "wrap", gap: 1.5 }}>
          {group === "listed" && (
            <FilterToggle value="attention" active={filter === "attention"} label={t("sets.needsAttention", { count: data?.attention ?? 0 })} onToggle={() => toggleFilter("attention")} />
          )}
          <FilterToggle value="reports" active={filter === "reports"} label={t("sets.withReports")} onToggle={() => toggleFilter("reports")} />
          <FilterToggle value="edited" active={filter === "edited"} label={t("sets.edited")} onToggle={() => toggleFilter("edited")} />
        </Box>
        <Box sx={{ display: "flex", alignItems: "center", gap: 1.5, ml: "auto" }}>
          <SortControl value={sort} options={sortOptions} dir={table.dir} onChange={table.setSort} />
          {actions.length > 0 && list.length > 0 && (
            <Button size="small" onClick={() => setSelected(allSelected ? new Set() : new Set(keys))}>
              {allSelected ? t("table.clearSelection") : t("queue.selectAll")}
            </Button>
          )}
        </Box>
      </Box>
      <Box sx={{ display: "flex", flexDirection: "column", minWidth: 0 }}>
        <Box sx={{ height: 3, mb: 1.5 }}>{rows.isFetching && data !== undefined && <LinearProgress sx={progressSx} />}</Box>
        {rows.error ? (
          <Box sx={{ mb: 2 }}>
            <ErrorState error={rows.error} onRetry={() => void rows.refetch()} compact={data !== undefined} />
          </Box>
        ) : null}
        {selected.size > 0 && actions.length > 0 && (
          <Paper
            variant="outlined"
            sx={{
              p: 1.5,
              mb: 2,
              display: "flex",
              gap: 1,
              alignItems: "center",
              flexWrap: "wrap",
              position: "sticky",
              top: { xs: -16, md: -24 },
              zIndex: 3,
              borderRadius: radius.md,
              bgcolor: colors.background.paper,
            }}
          >
            <Typography variant="body2" sx={{ flex: 1 }}>
              {t("table.selected", { count: selected.size })}
            </Typography>
            <Stack direction="row" spacing={1} useFlexGap flexWrap="wrap">
              {actions.map((a) => (
                <Button key={a} size="small" variant="contained" onClick={() => setBulk(a)}>
                  {t(`moderation.${a}.confirm`)}
                </Button>
              ))}
              <Button size="small" onClick={() => setSelected(new Set())} sx={{ color: colors.text.secondary }}>
                {t("table.clearSelection")}
              </Button>
            </Stack>
          </Paper>
        )}
        {list.length > 1 && (
          <FacetCompareBar active={panels.compare} onPick={panels.pickCompare} toggle={panels.toggle} onToggle={panels.toggleAll} keys={HUB_FACETS} t={t} />
        )}
        {content}
        {total > 0 && (
          <TablePagination
            component="div"
            count={total}
            page={Math.min(table.page, lastPage)}
            rowsPerPage={table.pageSize}
            rowsPerPageOptions={PAGE_SIZES}
            onPageChange={(_e, p) => table.setPage(p)}
            onRowsPerPageChange={(e) => table.setPageSize(Number(e.target.value))}
            labelRowsPerPage={t("sets.perPage")}
            labelDisplayedRows={({ from, to, count }) => t("table.displayedRows", { from, to, count })}
            sx={{ mt: 2, borderTop: `1px solid ${colors.border.light}` }}
          />
        )}
      </Box>
      <BulkDialog action={bulk ?? "hide"} targets={targets} open={bulk !== null} onClose={() => setBulk(null)} onDone={() => setSelected(new Set())} />
    </Box>
  );
}
