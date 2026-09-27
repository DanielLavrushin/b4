import { useMemo, useState } from "react";
import {
  Box,
  Button,
  Checkbox,
  Chip,
  Link,
  Paper,
  Stack,
  Tab,
  Tabs,
  Tooltip,
  Typography,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors, radiusPx } from "@design";
import { useSnackbar } from "@/app/SnackbarProvider";
import type { ReportAction, ReportState, ReportView } from "@/models/api";
import { EmptyState, ErrorState, Loading } from "@/shared/components/States";
import { StatusChip } from "@/shared/components/StatusChip";
import { Mono } from "@/shared/components/Mono";
import { formatAgo, formatStamp, setRef } from "@/shared/utils/format";
import { useReports, useReportsAction } from "./api";

type StateFilter = ReportState | "all";

const states: StateFilter[] = ["open", "dismissed", "resolved", "all"];

interface Group {
  key: string;
  setId: string;
  version: number;
  title: string;
  status?: string;
  reports: ReportView[];
}

const groupReports = (reports: ReportView[]): Group[] => {
  const map = new Map<string, Group>();
  reports.forEach((r) => {
    const key = setRef(r.set_id, r.version);
    let g = map.get(key);
    if (!g) {
      g = { key, setId: r.set_id, version: r.version, title: r.title ?? r.set_id, status: r.set_status, reports: [] };
      map.set(key, g);
    }
    g.reports.push(r);
  });
  return [...map.values()];
};

function stateLabel(t: (k: string, o?: Record<string, unknown>) => string, r: ReportView): string {
  if (r.state === "open") return t("reports.state.open");
  const resolution = r.resolution ? t(`reports.resolution.${r.resolution}`, { defaultValue: r.resolution }) : "";
  return resolution ? `${t(`reports.state.${r.state}`)}: ${resolution}` : t(`reports.state.${r.state}`);
}

export function ReportsInbox({ onOpenSet }: { onOpenSet: (id: string) => void }) {
  const { t } = useTranslation();
  const [state, setState] = useState<StateFilter>("open");
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const reports = useReports({ state });
  const action = useReportsAction();
  const { notifyResult, notifyError } = useSnackbar();
  const pages = reports.data?.pages;
  const items = useMemo(() => pages?.flatMap((p) => p.items) ?? [], [pages]);
  const groups = useMemo(() => groupReports(items), [items]);
  const counts = pages?.[0]?.counts ?? {};
  const total = pages?.[0]?.total ?? 0;

  const run = async (ids: number[], act: ReportAction) => {
    if (ids.length === 0) return;
    try {
      notifyResult(await action.mutateAsync({ ids, action: act }));
      setSelected(new Set());
    } catch (err) {
      notifyError(err);
    }
  };

  const toggle = (id: number) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const allCount = (counts.open ?? 0) + (counts.dismissed ?? 0) + (counts.resolved ?? 0);

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
      <Tabs value={state} onChange={(_e, v: StateFilter) => { setState(v); setSelected(new Set()); }}>
        {states.map((s) => (
          <Tab key={s} value={s} label={`${t(`reports.filter.${s}`)} (${String(s === "all" ? allCount : (counts[s] ?? 0))})`} />
        ))}
      </Tabs>
      {selected.size > 0 && (
        <Paper variant="outlined" sx={{ p: 1.5, display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap", position: "sticky", top: 0, zIndex: 2, bgcolor: colors.background.paper }}>
          <Typography variant="body2" sx={{ flex: 1 }}>
            {t("table.selected", { count: selected.size })}
          </Typography>
          <Button size="small" variant="outlined" disabled={action.isPending} onClick={() => void run([...selected], "dismiss")}>
            {t("reports.actions.dismiss")}
          </Button>
          <Button size="small" variant="outlined" disabled={action.isPending} onClick={() => void run([...selected], "resolve")}>
            {t("reports.actions.resolve")}
          </Button>
          <Button size="small" variant="text" disabled={action.isPending} onClick={() => void run([...selected], "reopen")}>
            {t("reports.actions.reopen")}
          </Button>
          <Button size="small" color="inherit" onClick={() => setSelected(new Set())}>
            {t("table.clearSelection")}
          </Button>
        </Paper>
      )}
      {reports.isLoading && <Loading />}
      {reports.error && <ErrorState error={reports.error} onRetry={() => void reports.refetch()} />}
      {reports.data && groups.length === 0 && <EmptyState text={t(state === "open" ? "reports.emptyOpen" : "feedback.emptyReports")} />}
      {groups.map((g) => {
        const open = g.reports.filter((r) => r.state === "open");
        const counting = g.reports.filter((r) => r.counts).length;
        return (
          <Paper key={g.key} variant="outlined" sx={{ p: 2, borderRadius: `${radiusPx.md}px`, bgcolor: colors.background.paper, display: "flex", flexDirection: "column", gap: 1 }}>
            <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
              <Link component="button" type="button" underline="hover" onClick={() => onOpenSet(g.setId)} sx={{ fontWeight: 600, fontSize: 16, textAlign: "left" }}>
                {g.title}
              </Link>
              <Mono>{g.key}</Mono>
              {g.status && <StatusChip status={g.status} label={t(`status.set.${g.status}`)} />}
              {open.length > 0 && (
                <Tooltip title={t("reports.countingHint")}>
                  <Chip size="small" color={counting >= 3 ? "error" : "warning"} variant="outlined" label={t("reports.counting", { count: counting })} />
                </Tooltip>
              )}
              <Box sx={{ flex: 1 }} />
              {open.length > 0 && (
                <Stack direction="row" spacing={1}>
                  <Button size="small" variant="outlined" disabled={action.isPending} onClick={() => void run(open.map((r) => r.id), "dismiss")}>
                    {t("reports.actions.dismissAll", { count: open.length })}
                  </Button>
                  <Button size="small" variant="text" disabled={action.isPending} onClick={() => void run(open.map((r) => r.id), "resolve")}>
                    {t("reports.actions.resolveAll", { count: open.length })}
                  </Button>
                </Stack>
              )}
            </Box>
            {g.reports.map((r) => (
              <Box key={r.id} sx={{ display: "flex", alignItems: "flex-start", flexWrap: { xs: "wrap", sm: "nowrap" }, gap: 1, borderTop: `1px solid ${colors.border.light}`, pt: 1 }}>
                <Checkbox size="small" checked={selected.has(r.id)} onChange={() => toggle(r.id)} sx={{ p: 0.5 }} />
                <Box sx={{ flex: 1, minWidth: { xs: "calc(100% - 48px)", sm: 0 } }}>
                  <Typography variant="body2" sx={{ overflowWrap: "anywhere" }}>
                    {r.reason || t("reports.noReason")}
                  </Typography>
                  <Typography variant="caption" sx={{ color: colors.text.secondary }}>
                    <span title={formatStamp(r.received_at)}>{formatAgo(t, r.received_at)}</span>
                    {" · "}
                    <Mono title={r.key_hmac}>{r.key}</Mono>
                    {r.key_banned ? ` · ${t("reports.keyBanned")}` : ""}
                    {r.key_test ? ` · ${t("reports.keyTest")}` : ""}
                    {r.asn_observed ? ` · AS${r.asn_observed}` : ` · ${t("reports.noAsn")}`}
                    {r.note ? ` · ${t("reports.noteShown", { note: r.note })}` : ""}
                  </Typography>
                </Box>
                <Chip size="small" variant="outlined" color={r.state === "open" ? "warning" : "default"} label={stateLabel(t, r)} />
                {r.state === "open" ? (
                  <Stack direction="row" spacing={0.5}>
                    <Button size="small" disabled={action.isPending} onClick={() => void run([r.id], "dismiss")}>
                      {t("reports.actions.dismiss")}
                    </Button>
                    <Button size="small" disabled={action.isPending} onClick={() => void run([r.id], "resolve")}>
                      {t("reports.actions.resolve")}
                    </Button>
                  </Stack>
                ) : (
                  <Button size="small" disabled={action.isPending} onClick={() => void run([r.id], "reopen")}>
                    {t("reports.actions.reopen")}
                  </Button>
                )}
              </Box>
            ))}
          </Paper>
        );
      })}
      {reports.hasNextPage && (
        <Box>
          <Button size="small" disabled={reports.isFetchingNextPage} onClick={() => void reports.fetchNextPage()}>
            {t("app.loadMore")}
          </Button>
        </Box>
      )}
      {reports.data && items.length > 0 && (
        <Typography variant="caption" sx={{ color: colors.text.secondary }}>
          {t("table.shown", { shown: items.length, total })}
        </Typography>
      )}
    </Box>
  );
}
