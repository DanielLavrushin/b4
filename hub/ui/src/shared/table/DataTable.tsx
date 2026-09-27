import { useMemo, type KeyboardEvent, type MouseEvent, type ReactNode } from "react";
import {
  Box,
  Checkbox,
  LinearProgress,
  Paper,
  Skeleton,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TablePagination,
  TableRow,
  TableSortLabel,
  Typography,
  type SxProps,
  type Theme,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import { EmptyState, ErrorState } from "@/shared/components/States";
import { PAGE_SIZES } from "./useTableState";
import { sortRows, type SortDir, type SortValue } from "./sort";

export interface Column<T> {
  id: string;
  header: ReactNode;
  cell: (row: T) => ReactNode;
  sortValue?: (row: T) => SortValue;
  serverSort?: boolean;
  align?: "left" | "right" | "center";
  width?: number | string;
  minWidth?: number;
  nowrap?: boolean;
  hideBelow?: "sm" | "md" | "lg";
}

export interface Paging {
  mode: "client" | "server";
  page: number;
  pageSize: number;
  total?: number;
  onPage: (page: number) => void;
  onPageSize?: (size: number) => void;
}

export interface Selection<T> {
  selected: ReadonlySet<string>;
  onChange: (next: Set<string>) => void;
  selectable?: (row: T) => boolean;
}

interface DataTableProps<T> {
  columns: Column<T>[];
  rows: T[] | undefined;
  rowKey: (row: T) => string;
  loading?: boolean;
  fetching?: boolean;
  error?: unknown;
  onRetry?: () => void;
  emptyText: string;
  noMatchText?: string;
  filtered?: boolean;
  sort?: { by: string; dir: SortDir };
  onSort?: (by: string, dir: SortDir) => void;
  paging?: Paging;
  onRowClick?: (row: T) => void;
  activeKey?: string | null;
  selection?: Selection<T>;
  bulkBar?: (keys: string[]) => ReactNode;
  toolbar?: ReactNode;
  maxHeight?: number | string;
  rowSx?: (row: T) => SxProps<Theme>;
}

const interactive = (target: EventTarget | null): boolean => {
  let el = target as HTMLElement | null;
  while (el && el.tagName !== "TR") {
    if (["A", "BUTTON", "INPUT", "LABEL", "TEXTAREA", "SELECT"].includes(el.tagName) || el.getAttribute("role") === "button") return true;
    el = el.parentElement;
  }
  return false;
};

const hideSx = (hideBelow?: "sm" | "md" | "lg") => (hideBelow ? { display: { xs: "none", [hideBelow]: "table-cell" } } : {});

export function DataTable<T>({
  columns,
  rows,
  rowKey,
  loading,
  fetching,
  error,
  onRetry,
  emptyText,
  noMatchText,
  filtered,
  sort,
  onSort,
  paging,
  onRowClick,
  activeKey,
  selection,
  bulkBar,
  toolbar,
  maxHeight = "calc(100vh - 240px)",
  rowSx,
}: Readonly<DataTableProps<T>>) {
  const { t, i18n } = useTranslation();
  const sortColumn = columns.find((c) => c.id === sort?.by);
  const sorted = useMemo(() => {
    if (!rows) return [];
    if (!sort || !sortColumn?.sortValue) return rows;
    return sortRows(rows, sortColumn.sortValue, sort.dir, i18n.language);
  }, [rows, sort, sortColumn, i18n.language]);
  const visible = useMemo(() => {
    if (!paging || paging.mode === "server") return sorted;
    const start = paging.page * paging.pageSize;
    return sorted.slice(start, start + paging.pageSize);
  }, [sorted, paging]);
  const total = paging?.mode === "server" ? (paging.total ?? 0) : sorted.length;

  const selectableRows = selection ? visible.filter((r) => selection.selectable?.(r) ?? true) : [];
  const selectedOnPage = selection ? selectableRows.filter((r) => selection.selected.has(rowKey(r))).length : 0;

  const togglePage = () => {
    if (!selection) return;
    const next = new Set(selection.selected);
    if (selectedOnPage === selectableRows.length) selectableRows.forEach((r) => next.delete(rowKey(r)));
    else selectableRows.forEach((r) => next.add(rowKey(r)));
    selection.onChange(next);
  };

  const toggleRow = (key: string) => {
    if (!selection) return;
    const next = new Set(selection.selected);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    selection.onChange(next);
  };

  const click = (row: T) => (e: MouseEvent) => {
    if (!onRowClick || interactive(e.target)) return;
    onRowClick(row);
  };

  const keydown = (row: T) => (e: KeyboardEvent) => {
    if (!onRowClick || e.key !== "Enter" || interactive(e.target)) return;
    onRowClick(row);
  };

  const sortBy = (c: Column<T>) => {
    if (!onSort) return;
    const dir: SortDir = sort?.by === c.id && sort.dir === "desc" ? "asc" : "desc";
    onSort(c.id, dir);
  };

  const hasRows = rows !== undefined && rows.length > 0;
  const selectedKeys = selection ? [...selection.selected] : [];

  return (
    <Paper variant="outlined" sx={{ bgcolor: colors.background.paper, border: `1px solid ${colors.border.default}`, overflow: "hidden", minWidth: 0 }}>
      {toolbar && <Box sx={{ p: 1.5, display: "flex", gap: 1.5, alignItems: "center", flexWrap: "wrap", borderBottom: `1px solid ${colors.border.light}` }}>{toolbar}</Box>}
      {selection && bulkBar && selectedKeys.length > 0 && (
        <Box sx={{ p: 1, px: 1.5, display: "flex", gap: 1, alignItems: "center", flexWrap: "wrap", bgcolor: colors.accent.primary, borderBottom: `1px solid ${colors.border.light}` }}>
          <Typography variant="body2" sx={{ flex: 1, minWidth: 120 }}>
            {t("table.selected", { count: selectedKeys.length })}
          </Typography>
          {bulkBar(selectedKeys)}
        </Box>
      )}
      <Box sx={{ height: 3 }}>{fetching && !loading && <LinearProgress sx={{ height: 3 }} />}</Box>
      {error !== undefined && error !== null && (
        <Box sx={{ p: 1.5 }}>
          <ErrorState error={error} onRetry={onRetry} compact={hasRows} />
        </Box>
      )}
      <TableContainer sx={{ maxHeight }}>
        <Table stickyHeader size="small">
          <TableHead>
            <TableRow>
              {selection && (
                <TableCell padding="checkbox">
                  <Checkbox
                    size="small"
                    indeterminate={selectedOnPage > 0 && selectedOnPage < selectableRows.length}
                    checked={selectableRows.length > 0 && selectedOnPage === selectableRows.length}
                    onChange={togglePage}
                    disabled={selectableRows.length === 0}
                  />
                </TableCell>
              )}
              {columns.map((c) => {
                const sortable = onSort !== undefined && (c.sortValue !== undefined || c.serverSort === true);
                return (
                  <TableCell
                    key={c.id}
                    align={c.align}
                    sx={{ width: c.width, minWidth: c.minWidth, whiteSpace: "nowrap", bgcolor: colors.background.paper, ...hideSx(c.hideBelow) }}
                    sortDirection={sort?.by === c.id ? sort.dir : false}
                  >
                    {sortable ? (
                      <TableSortLabel active={sort?.by === c.id} direction={sort?.by === c.id ? sort.dir : "desc"} onClick={() => sortBy(c)}>
                        {c.header}
                      </TableSortLabel>
                    ) : (
                      c.header
                    )}
                  </TableCell>
                );
              })}
            </TableRow>
          </TableHead>
          <TableBody>
            {loading &&
              !hasRows &&
              [0, 1, 2, 3, 4].map((i) => (
                <TableRow key={`skeleton-${String(i)}`}>
                  {selection && <TableCell padding="checkbox" />}
                  {columns.map((c) => (
                    <TableCell key={c.id} sx={hideSx(c.hideBelow)}>
                      <Skeleton variant="text" />
                    </TableCell>
                  ))}
                </TableRow>
              ))}
            {visible.map((row) => {
              const key = rowKey(row);
              const canSelect = selection ? (selection.selectable?.(row) ?? true) : false;
              return (
                <TableRow
                  key={key}
                  hover
                  selected={activeKey === key || (selection?.selected.has(key) ?? false)}
                  tabIndex={onRowClick ? 0 : undefined}
                  onClick={click(row)}
                  onKeyDown={keydown(row)}
                  sx={{ cursor: onRowClick ? "pointer" : "default", verticalAlign: "top", ...(rowSx ? (rowSx(row)) : {}) }}
                >
                  {selection && (
                    <TableCell padding="checkbox">
                      <Checkbox size="small" checked={selection.selected.has(key)} disabled={!canSelect} onChange={() => toggleRow(key)} />
                    </TableCell>
                  )}
                  {columns.map((c) => (
                    <TableCell key={c.id} align={c.align} sx={{ whiteSpace: c.nowrap ? "nowrap" : undefined, minWidth: c.minWidth, ...hideSx(c.hideBelow) }}>
                      {c.cell(row)}
                    </TableCell>
                  ))}
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </TableContainer>
      {!loading && (error === undefined || error === null || hasRows) && rows !== undefined && visible.length === 0 && (
        <Box sx={{ px: 2 }}>
          <EmptyState text={filtered && noMatchText ? noMatchText : emptyText} />
        </Box>
      )}
      {paging && total > 0 && (
        <TablePagination
          component="div"
          count={total}
          page={Math.min(paging.page, Math.max(0, Math.ceil(total / paging.pageSize) - 1))}
          rowsPerPage={paging.pageSize}
          rowsPerPageOptions={paging.onPageSize ? PAGE_SIZES : [paging.pageSize]}
          onPageChange={(_e, p) => paging.onPage(p)}
          onRowsPerPageChange={(e) => paging.onPageSize?.(Number(e.target.value))}
          labelRowsPerPage={t("table.rowsPerPage")}
          labelDisplayedRows={({ from, to, count }) => t("table.displayedRows", { from, to, count })}
        />
      )}
    </Paper>
  );
}
