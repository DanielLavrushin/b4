import { useEffect, useMemo, useState } from "react";
import { Box, Button, Chip, Stack, Tab, Tabs, ToggleButton, Tooltip, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import type { SetAction, SetGroupName, SetRowView } from "@/models/api";
import { DataTable, type Column } from "@/shared/table/DataTable";
import { useTableState } from "@/shared/table/useTableState";
import { SearchField } from "@/shared/components/SearchField";
import { KeyRef } from "@/shared/components/KeyRef";
import { useOverlay } from "@/shared/hooks/useOverlay";
import { formatAgo, formatStamp } from "@/shared/utils/format";
import { reasonText } from "@/shared/utils/reason";
import { filterText } from "@/shared/utils/terms";
import { BulkDialog } from "@/features/moderation/BulkDialog";
import { useSetRows } from "./api";
import { setPath } from "./SetDrawerHost";
import { FlagChips, TechniqueChips } from "./components/TechniqueChips";
import { EvidenceCell, ScoreCell } from "./components/ScoreCell";

const groups: SetGroupName[] = ["listed", "superseded", "withheld", "hidden", "rejected"];

const bulkActions: Partial<Record<SetGroupName, SetAction[]>> = {
  listed: ["hide"],
  superseded: ["hide"],
  hidden: ["restore"],
  rejected: ["approve"],
};

const rowKey = (r: SetRowView) => `${r.set_id}/${String(r.version)}`;

function Targets({ row }: Readonly<{ row: SetRowView }>) {
  const { t } = useTranslation();
  const tg = row.targets;
  const parts: string[] = [...tg.domains];
  const more = tg.domains_total - tg.domains.length;
  return (
    <Box sx={{ maxWidth: 260 }}>
      <Typography variant="body2" sx={{ overflowWrap: "anywhere" }}>
        {parts.join(", ")}
        {more > 0 && (
          <Typography component="span" variant="caption" sx={{ color: colors.text.secondary, ml: 0.5 }}>
            {t("targets.more", { count: more })}
          </Typography>
        )}
      </Typography>
      <Typography variant="caption" sx={{ color: colors.text.secondary, display: "block" }}>
        {[
          tg.ips ? t("entry.ips", { count: tg.ips }) : "",
          tg.geosite.length ? `geosite: ${tg.geosite.join(", ")}` : "",
          tg.geoip.length ? `geoip: ${tg.geoip.join(", ")}` : "",
          tg.asns.length ? tg.asns.map((a) => `AS${a}`).join(", ") : "",
          ...tg.filters.map((f) => filterText(t, f)),
        ]
          .filter(Boolean)
          .join(" · ")}
      </Typography>
    </Box>
  );
}

export function SetsPage() {
  const { t } = useTranslation();
  const overlay = useOverlay();
  const table = useTableState({ sort: "updated", dir: "desc", pageSize: 50 });
  const group = (groups.includes(table.param("group") as SetGroupName) ? table.param("group") : "listed") as SetGroupName;
  const filter = table.param("filter");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [bulk, setBulk] = useState<SetAction | null>(null);
  const view = `${group}|${String(table.page)}|${String(table.pageSize)}|${table.query}|${filter}|${table.sort}|${table.dir}`;
  useEffect(() => {
    setSelected(new Set());
  }, [view]);
  const rows = useSetRows({ group, q: table.query, sort: table.sort, dir: table.dir, offset: table.page * table.pageSize, limit: table.pageSize, filter });
  const data = rows.data;

  const columns = useMemo(() => {
    const cols: Column<SetRowView>[] = [
      {
        id: "title",
        header: t("sets.columns.title"),
        serverSort: true,
        minWidth: 220,
        cell: (r) => (
          <Box sx={{ display: "flex", flexDirection: "column", gap: 0.5 }}>
            <Typography variant="body2" sx={{ fontWeight: 600, overflowWrap: "anywhere" }}>
              {r.title}
            </Typography>
            <Typography variant="monoSmall" sx={{ color: colors.text.secondary }}>
              {r.set_id.slice(0, 10)}/v{r.version}
              {r.versions && r.versions.length > 1 ? ` · ${t("sets.versions", { list: r.versions.join(", ") })}` : ""}
            </Typography>
            <FlagChips flags={r.flags} />
            {r.attention.length > 0 && (
              <Stack direction="row" spacing={0.5} useFlexGap flexWrap="wrap">
                {r.attention.map((a) => (
                  <Tooltip key={a} title={t(`attention.${a}.hint`)}>
                    <Chip size="small" color={a === "reports" || a === "low_score" ? "error" : "warning"} label={t(`attention.${a}.label`)} sx={{ height: 20, fontSize: 11 }} />
                  </Tooltip>
                ))}
              </Stack>
            )}
            {r.author_banned && <Chip size="small" color="error" variant="outlined" label={t("queue.authorBanned")} sx={{ height: 20, fontSize: 11, alignSelf: "flex-start" }} />}
          </Box>
        ),
      },
      { id: "techniques", header: t("sets.columns.techniques"), minWidth: 180, hideBelow: "md", cell: (r) => <TechniqueChips terms={r.techniques} /> },
      { id: "targets", header: t("sets.columns.targets"), hideBelow: "lg", cell: (r) => <Targets row={r} /> },
    ];
    if (group !== "hidden" && group !== "rejected") {
      cols.push(
        { id: "score", header: t("sets.columns.score"), serverSort: true, align: "right", cell: (r) => <ScoreCell published={r.published} live={r.live} /> },
        { id: "devices", header: t("sets.columns.evidence"), serverSort: true, align: "right", hideBelow: "sm", cell: (r) => <EvidenceCell evidence={r.evidence} /> },
      );
    }
    cols.push({
      id: "reports",
      header: t("sets.columns.reports"),
      serverSort: true,
      align: "right",
      hideBelow: "sm",
      cell: (r) =>
        r.reports > 0 ? (
          <Tooltip title={t("sets.reportsTip", { open: r.open_reports, total: r.reports, independent: r.independent_reports })}>
            <Chip size="small" color={r.open_reports > 0 ? "warning" : "default"} variant={r.open_reports > 0 ? "filled" : "outlined"} label={`${String(r.open_reports)}/${String(r.reports)}`} />
          </Tooltip>
        ) : (
          ""
        ),
    });
    cols.push({
      id: "author",
      header: t("sets.columns.author"),
      serverSort: true,
      hideBelow: "md",
      cell: (r) => (
        <Box>
          <KeyRef hmac={r.author_hmac} label={r.author} banned={r.author_banned} />
          {r.asn_observed && (
            <Typography variant="caption" sx={{ display: "block", color: colors.text.disabled }}>
              AS{r.asn_observed} {r.country_observed}
            </Typography>
          )}
        </Box>
      ),
    });
    if (group === "superseded") {
      cols.push({
        id: "superseded",
        header: t("sets.columns.supersededBy"),
        nowrap: true,
        cell: (r) => (
          <>
            v{r.superseded_by}
            <Typography variant="caption" sx={{ display: "block", color: colors.text.disabled }}>
              {formatStamp(r.superseded_at)}
            </Typography>
          </>
        ),
      });
    }
    if (group === "hidden" || group === "rejected" || group === "withheld") {
      cols.push({
        id: "reason",
        header: t("sets.columns.reason"),
        cell: (r) => (
          <Box sx={{ maxWidth: 320, overflowWrap: "anywhere" }}>
            {group === "withheld" ? t(`sets.withheldReason.${r.withheld ?? "set_withdrawn"}`) : reasonText(t, r.status_reason, r.independent_reports)}
            {group === "hidden" && r.status_reason === "reports" && r.open_reports > 0 && (
              <Chip size="small" color="warning" variant="outlined" label={t("sets.awaitingReview")} sx={{ ml: 1 }} />
            )}
          </Box>
        ),
      });
    }
    cols.push({
      id: "updated",
      header: t("sets.columns.updated"),
      serverSort: true,
      align: "right",
      nowrap: true,
      cell: (r) => <span title={formatStamp(r.updated_at)}>{formatAgo(t, r.updated_at)}</span>,
    });
    return cols;
  }, [group, t]);

  const actions = bulkActions[group] ?? [];
  const targets = (data?.rows ?? [])
    .filter((r) => selected.has(rowKey(r)))
    .map((r) => ({ set_id: r.set_id, version: r.version, title: r.title, status: r.status }));

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
      >
        {groups.map((g) => (
          <Tab key={g} value={g} label={`${t(`sets.${g}`)}${data ? ` (${String(data.groups[g] ?? 0)})` : ""}`} />
        ))}
      </Tabs>
      <DataTable
        columns={columns}
        rows={data?.rows}
        rowKey={rowKey}
        loading={rows.isLoading}
        fetching={rows.isFetching}
        error={rows.error}
        onRetry={() => void rows.refetch()}
        emptyText={t("sets.empty")}
        noMatchText={t("sets.noMatch")}
        filtered={table.query !== "" || filter !== ""}
        sort={{ by: table.sort, dir: table.dir }}
        onSort={table.setSort}
        paging={{ mode: "server", page: table.page, pageSize: table.pageSize, total: data?.total ?? 0, onPage: table.setPage, onPageSize: table.setPageSize }}
        onRowClick={(r) => overlay.open(setPath(r.set_id))}
        selection={actions.length > 0 ? { selected, onChange: setSelected } : undefined}
        bulkBar={() => (
          <Stack direction="row" spacing={1}>
            {actions.map((a) => (
              <Button key={a} size="small" variant="contained" color={a === "hide" ? "error" : "success"} onClick={() => setBulk(a)}>
                {t(`moderation.${a}.confirm`)}
              </Button>
            ))}
            <Button size="small" color="inherit" onClick={() => setSelected(new Set())}>
              {t("table.clearSelection")}
            </Button>
          </Stack>
        )}
        toolbar={
          <>
            <SearchField value={table.q} onChange={table.setQ} placeholder={t("sets.searchPlaceholder")} />
            {group === "listed" && (
              <ToggleButton
                size="small"
                value="attention"
                selected={filter === "attention"}
                onChange={() => table.setParam("filter", filter === "attention" ? null : "attention")}
                sx={{ textTransform: "none" }}
              >
                {t("sets.needsAttention", { count: data?.attention ?? 0 })}
              </ToggleButton>
            )}
            <ToggleButton
              size="small"
              value="reports"
              selected={filter === "reports"}
              onChange={() => table.setParam("filter", filter === "reports" ? null : "reports")}
              sx={{ textTransform: "none" }}
            >
              {t("sets.withReports")}
            </ToggleButton>
            <ToggleButton
              size="small"
              value="edited"
              selected={filter === "edited"}
              onChange={() => table.setParam("filter", filter === "edited" ? null : "edited")}
              sx={{ textTransform: "none" }}
            >
              {t("sets.edited")}
            </ToggleButton>
          </>
        }
      />
      <BulkDialog action={bulk ?? "hide"} targets={targets} open={bulk !== null} onClose={() => setBulk(null)} onDone={() => setSelected(new Set())} />
    </Box>
  );
}
