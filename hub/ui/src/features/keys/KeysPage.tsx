import { useMemo } from "react";
import { Box, Chip, Stack, ToggleButton, ToggleButtonGroup, Tooltip, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import type { KeyRowView } from "@/models/api";
import { DataTable, type Column } from "@/shared/table/DataTable";
import { useTableState } from "@/shared/table/useTableState";
import { SearchField } from "@/shared/components/SearchField";
import { KeyRef, keyHref } from "@/shared/components/KeyRef";
import { StatusChip } from "@/shared/components/StatusChip";
import { useOverlay } from "@/shared/hooks/useOverlay";
import { formatAgo, formatStamp } from "@/shared/utils/format";
import { useKeyList } from "./api";

const statuses = ["", "banned", "trusted", "ok"];
const tags = ["", "staff", "test", "any"];
const setFilters = ["", "any", "listed", "pending", "none"];

export function KeyStatus({ k }: Readonly<{ k: KeyRowView }>) {
  const { t } = useTranslation();
  if (k.banned) {
    return (
      <Tooltip title={k.ban_reason ?? ""}>
        <span>
          <StatusChip status="banned" label={t("keys.banned", { when: formatAgo(t, k.banned_at) })} />
        </span>
      </Tooltip>
    );
  }
  if (k.trusted) return <StatusChip status="trusted" label={t("keys.trusted", { when: formatAgo(t, k.trusted_at ?? k.first_seen) })} />;
  return <StatusChip status="ok" label={t("keys.ok")} />;
}

export function KeysPage() {
  const { t } = useTranslation();
  const overlay = useOverlay();
  const table = useTableState({ sort: "last_seen", dir: "desc", pageSize: 50 });
  const status = table.param("status");
  const tag = table.param("tag");
  const sets = table.param("sets");
  const list = useKeyList({
    q: table.query,
    status,
    tag,
    sets,
    active: table.param("active"),
    sort: table.sort,
    dir: table.dir,
    offset: table.page * table.pageSize,
    limit: table.pageSize,
  });
  const data = list.data;
  const counts = data?.counts;

  const columns = useMemo<Column<KeyRowView>[]>(
    () => [
      {
        id: "name",
        header: t("keys.columns.key"),
        serverSort: true,
        minWidth: 200,
        cell: (k) => (
          <Box>
            <KeyRef hmac={k.key_hmac} label={k.label} name={k.name} tag={k.tag} banned={k.banned} trusted={k.trusted} />
            {k.note && (
              <Typography variant="caption" sx={{ display: "block", color: colors.text.secondary, maxWidth: 260, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                {k.note}
              </Typography>
            )}
          </Box>
        ),
      },
      { id: "status", header: t("keys.columns.status"), cell: (k) => <KeyStatus k={k} /> },
      {
        id: "last_seen",
        header: t("keys.columns.lastSeen"),
        serverSort: true,
        nowrap: true,
        cell: (k) => (k.last_seen ? <span title={formatStamp(k.last_seen)}>{formatAgo(t, k.last_seen)}</span> : <Typography variant="caption" sx={{ color: colors.text.disabled }}>{t("keys.neverSent")}</Typography>),
      },
      { id: "first_seen", header: t("keys.columns.firstSeen"), serverSort: true, nowrap: true, hideBelow: "md", cell: (k) => <span title={formatStamp(k.first_seen)}>{formatAgo(t, k.first_seen)}</span> },
      {
        id: "sets",
        header: t("keys.columns.sets"),
        serverSort: true,
        align: "right",
        cell: (k) => (
          <Tooltip title={t("keys.setsTip", { listed: k.listed_sets, total: k.sets, pending: k.pending_versions })}>
            <span>
              {k.listed_sets}/{k.sets}
              {k.pending_versions > 0 && <Chip size="small" color="warning" label={k.pending_versions} sx={{ ml: 0.5, height: 18, fontSize: 11 }} />}
            </span>
          </Tooltip>
        ),
      },
      {
        id: "votes",
        header: t("keys.columns.votes"),
        serverSort: true,
        align: "right",
        cell: (k) => (
          <Tooltip title={t("keys.votesTip", { manual: k.manual_votes, uploads: k.votes - k.manual_votes })}>
            <span>{k.manual_votes}</span>
          </Tooltip>
        ),
      },
      { id: "reports", header: t("keys.columns.reports"), serverSort: true, align: "right", hideBelow: "sm", cell: (k) => (k.reports > 0 ? k.reports : "") },
      {
        id: "origin",
        header: t("keys.columns.origin"),
        hideBelow: "md",
        cell: (k) => (k.last_asn ? `AS${k.last_asn} ${k.last_country ?? ""}` : ""),
      },
    ],
    [t],
  );

  const chipGroup = (name: string, values: string[], labels: (v: string) => string) => (
    <ToggleButtonGroup
      size="small"
      exclusive
      value={table.param(name)}
      onChange={(_e, v: string | null) => table.setParam(name, v === null || v === "" ? null : v)}
      sx={{ "& .MuiToggleButton-root": { textTransform: "none", py: 0.25 } }}
    >
      {values.map((v) => (
        <ToggleButton key={v || "all"} value={v}>
          {labels(v)}
        </ToggleButton>
      ))}
    </ToggleButtonGroup>
  );

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
      <Typography variant="sectionHeader">{t("keys.title", { count: counts?.all ?? 0 })}</Typography>
      {data?.match_by === "key_id" && (
        <Typography variant="body2" sx={{ color: colors.state.info }}>
          {t("keys.matchedByKeyId")}
        </Typography>
      )}
      <DataTable
        columns={columns}
        rows={data?.items}
        rowKey={(k) => k.key_hmac}
        loading={list.isLoading}
        fetching={list.isFetching}
        error={list.error}
        onRetry={() => void list.refetch()}
        emptyText={t("keys.empty")}
        noMatchText={t("keys.noMatch")}
        filtered={table.query !== "" || status !== "" || tag !== "" || sets !== ""}
        sort={{ by: table.sort, dir: table.dir }}
        onSort={table.setSort}
        paging={{ mode: "server", page: table.page, pageSize: table.pageSize, total: data?.total ?? 0, onPage: table.setPage, onPageSize: table.setPageSize }}
        onRowClick={(k) => overlay.open(keyHref(k.key_hmac))}
        toolbar={
          <Stack direction="row" spacing={1.5} useFlexGap flexWrap="wrap" alignItems="center" sx={{ width: "100%" }}>
            <SearchField value={table.q} onChange={table.setQ} placeholder={t("keys.searchPlaceholder")} />
            {chipGroup("status", statuses, (v) => (v ? `${t(`keys.filters.status.${v}`)}${counts && v !== "ok" ? ` (${String(v === "banned" ? counts.banned : counts.trusted)})` : ""}` : t("keys.filters.all")))}
            {chipGroup("tag", tags, (v) => (v ? `${t(`keys.filters.tag.${v}`)}${counts && (v === "staff" || v === "test") ? ` (${String(v === "staff" ? counts.staff : counts.test)})` : ""}` : t("keys.filters.anyTag")))}
            {chipGroup("sets", setFilters, (v) => (v ? t(`keys.filters.sets.${v}`) : t("keys.filters.anySets")))}
          </Stack>
        }
      />
    </Box>
  );
}
