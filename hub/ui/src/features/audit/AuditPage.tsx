import { useMemo } from "react";
import { Box, Button, Chip, MenuItem, TextField, Tooltip, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import type { AuditEntryView } from "@/models/api";
import { DataTable, type Column } from "@/shared/table/DataTable";
import { useTableState } from "@/shared/table/useTableState";
import { SearchField } from "@/shared/components/SearchField";
import { SetRef } from "@/shared/components/SetRef";
import { KeyRef } from "@/shared/components/KeyRef";
import { Mono } from "@/shared/components/Mono";
import { formatAgo, formatStamp } from "@/shared/utils/format";
import { actorText, changeSummary } from "./HistoryList";
import { useAuditLog } from "./api";

const kinds = ["set", "key", "mirror", "report", "catalogue", "settings", "notify"];
const actors = ["console", "basic", "cli", "system"];

function Target({ entry }: Readonly<{ entry: AuditEntryView }>) {
  const { t } = useTranslation();
  if (entry.target_kind === "set" && entry.target_id) {
    return <SetRef id={entry.target_id} version={entry.version || undefined} title={entry.target_label || entry.target_id.slice(0, 10)} />;
  }
  if (entry.target_kind === "key" && entry.target_label !== "" && entry.target_id && /^[0-9a-f]{64}$/.test(entry.target_id)) {
    return <KeyRef hmac={entry.target_id} />;
  }
  return (
    <Box>
      <Typography variant="caption" sx={{ color: colors.text.secondary }}>
        {t(`audit.kind.${entry.target_kind}`, { defaultValue: entry.target_kind })}
      </Typography>
      {entry.target_id && (
        <Box sx={{ overflowWrap: "anywhere" }}>
          <Mono>{entry.target_label || entry.target_id}</Mono>
        </Box>
      )}
    </Box>
  );
}

export function AuditPage() {
  const { t } = useTranslation();
  const table = useTableState({ sort: "at", dir: "desc", pageSize: 50 });
  const filter = {
    target_kind: kinds.includes(table.param("kind")) ? table.param("kind") : "",
    target_id: table.exact,
    actor: actors.includes(table.param("actor")) ? table.param("actor") : "",
    batch: table.param("batch"),
  };
  const log = useAuditLog(filter);
  const rows = useMemo(() => log.data?.pages.flatMap((p) => p.items) ?? [], [log.data]);

  const columns = useMemo<Column<AuditEntryView>[]>(
    () => [
      {
        id: "at",
        header: t("audit.columns.at"),
        nowrap: true,
        cell: (e) => <span title={formatStamp(e.at)}>{formatAgo(t, e.at)}</span>,
      },
      {
        id: "action",
        header: t("audit.columns.action"),
        minWidth: 160,
        cell: (e) => (
          <Box>
            <Typography variant="body2" sx={{ fontWeight: 600 }}>
              {t(`audit.action.${e.action}`, { defaultValue: e.action })}
            </Typography>
            {e.batch_id && (
              <Tooltip title={t("audit.batchHint")}>
                <Chip
                  size="small"
                  variant="outlined"
                  label={t("audit.batch")}
                  onClick={() => table.setParam("batch", e.batch_id ?? null)}
                  sx={{ height: 18, fontSize: 10, mt: 0.25 }}
                />
              </Tooltip>
            )}
          </Box>
        ),
      },
      { id: "target", header: t("audit.columns.target"), minWidth: 200, cell: (e) => <Target entry={e} /> },
      {
        id: "actor",
        header: t("audit.columns.actor"),
        hideBelow: "sm",
        cell: (e) => (
          <Box>
            <Typography variant="body2">{actorText(t, e)}</Typography>
            {e.actor_ip && (
              <Typography variant="caption" sx={{ color: colors.text.disabled }}>
                {e.actor_ip}
              </Typography>
            )}
          </Box>
        ),
      },
      {
        id: "details",
        header: t("audit.columns.details"),
        hideBelow: "md",
        cell: (e) => (
          <Box sx={{ maxWidth: 420, overflowWrap: "anywhere" }}>
            {e.reason && <Typography variant="body2">{e.reason}</Typography>}
            <Typography variant="caption" sx={{ color: colors.text.secondary }}>
              {changeSummary(e)}
            </Typography>
          </Box>
        ),
      },
    ],
    [t, table],
  );

  const filtered = Object.values(filter).some((v) => v !== "");

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
      <DataTable
        columns={columns}
        rows={log.data ? rows : undefined}
        rowKey={(e) => String(e.id)}
        loading={log.isLoading}
        fetching={log.isFetching}
        error={log.error}
        onRetry={() => void log.refetch()}
        emptyText={t("audit.empty")}
        noMatchText={t("audit.noMatch")}
        filtered={filtered}
        toolbar={
          <>
            <SearchField value={table.q} onChange={table.setQ} placeholder={t("audit.searchPlaceholder")} />
            <TextField
              select
              size="small"
              label={t("audit.columns.target")}
              value={filter.target_kind}
              slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true } }}
              onChange={(ev) => table.setParam("kind", ev.target.value || null)}
              sx={{ minWidth: 150 }}
            >
              <MenuItem value="">{t("audit.all")}</MenuItem>
              {kinds.map((k) => (
                <MenuItem key={k} value={k}>
                  {t(`audit.kind.${k}`)}
                </MenuItem>
              ))}
            </TextField>
            <TextField
              select
              size="small"
              label={t("audit.columns.actor")}
              value={filter.actor}
              slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true } }}
              onChange={(ev) => table.setParam("actor", ev.target.value || null)}
              sx={{ minWidth: 150 }}
            >
              <MenuItem value="">{t("audit.all")}</MenuItem>
              {actors.map((a) => (
                <MenuItem key={a} value={a}>
                  {t(`audit.actor.${a}`)}
                </MenuItem>
              ))}
            </TextField>
            {filter.batch && <Chip label={t("audit.batchFilter", { id: filter.batch.slice(0, 8) })} onDelete={() => table.setParam("batch", null)} size="small" />}
          </>
        }
      />
      {log.hasNextPage && (
        <Box sx={{ display: "flex", justifyContent: "center" }}>
          <Button variant="outlined" size="small" disabled={log.isFetchingNextPage} onClick={() => void log.fetchNextPage()}>
            {t("audit.loadMore")}
          </Button>
        </Box>
      )}
    </Box>
  );
}
