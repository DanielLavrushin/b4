import { useMemo } from "react";
import { Box, Button, Chip, Stack, Tab, Tabs, ToggleButton, ToggleButtonGroup, Typography } from "@mui/material";
import { useSearchParams } from "react-router";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import type { VoteRowView } from "@/models/api";
import { DataTable, type Column } from "@/shared/table/DataTable";
import { SetRef } from "@/shared/components/SetRef";
import { KeyRef } from "@/shared/components/KeyRef";
import { useOverlay } from "@/shared/hooks/useOverlay";
import { formatAgo, formatStamp } from "@/shared/utils/format";
import { setPath } from "@/features/sets/SetDrawerHost";
import { AppliedWeight } from "./AppliedWeight";
import { ReportsInbox } from "./ReportsInbox";
import { OriginFilters } from "./OriginFilters";
import { normalizeAsn, normalizeCountry } from "./origins";
import { useVotes, type VoteFilter } from "./api";

function VotesTab() {
  const { t } = useTranslation();
  const [params, setParams] = useSearchParams();
  const get = (name: string) => params.get(name) ?? "";
  const update = (changes: Record<string, string | null>) =>
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        for (const [name, value] of Object.entries(changes)) {
          if (value === null || value === "") next.delete(name);
          else next.set(name, value);
        }
        return next;
      },
      { replace: true },
    );
  const set = (name: string, value: string | null) => update({ [name]: value });
  const filter: VoteFilter = {
    set: get("set"),
    key: get("key"),
    sign: get("sign"),
    author: get("author"),
    verified: get("verified"),
    asn: normalizeAsn(get("asn")),
    cc: normalizeCountry(get("cc")),
  };
  const votes = useVotes(filter);
  const rows = useMemo(() => votes.data?.pages.flatMap((p) => p.items) ?? [], [votes.data]);
  const total = votes.data?.pages[0]?.total ?? 0;

  const columns = useMemo<Column<VoteRowView>[]>(
    () => [
      { id: "when", header: t("feedback.columns.when"), nowrap: true, cell: (v) => <span title={formatStamp(v.received_at)}>{formatAgo(t, v.received_at)}</span> },
      { id: "set", header: t("feedback.columns.set"), minWidth: 200, cell: (v) => <SetRef id={v.set_id} version={v.version} title={v.title} status={v.set_status} /> },
      {
        id: "kind",
        header: t("feedback.columns.kind"),
        cell: (v) => <Chip size="small" variant="outlined" color={v.weight >= 0 ? "success" : "error"} label={t(`feedback.kinds.${v.kind}`, { defaultValue: v.kind })} />,
      },
      { id: "weight", header: t("feedback.columns.applied"), align: "right", cell: (v) => <AppliedWeight vote={v} /> },
      {
        id: "origin",
        header: t("feedback.columns.origin"),
        nowrap: true,
        hideBelow: "sm",
        cell: (v) => (
          <>
            {v.asn_observed ? `AS${v.asn_observed} ${v.country_observed ?? ""}` : t("feedback.unverified")}
            {v.asn_hint && (
              <Typography component="span" variant="caption" sx={{ color: colors.text.disabled, ml: 0.5 }}>
                ({t("feedback.claims", { asn: v.asn_hint, cc: v.country_hint ?? "" })})
              </Typography>
            )}
          </>
        ),
      },
      { id: "key", header: t("feedback.columns.key"), hideBelow: "md", cell: (v) => <KeyRef hmac={v.key_hmac} label={v.key} /> },
      { id: "domain", header: t("feedback.columns.domain"), hideBelow: "lg", cell: (v) => v.domain ?? "" },
      { id: "b4", header: t("feedback.columns.b4"), nowrap: true, hideBelow: "lg", cell: (v) => `${v.b4_version ?? ""} ${v.engine ?? ""}` },
    ],
    [t],
  );

  const toggle = (name: string, values: [string, string][]) => (
    <ToggleButtonGroup size="small" exclusive value={get(name)} onChange={(_e, v: string | null) => set(name, v)} sx={{ "& .MuiToggleButton-root": { textTransform: "none", py: 0.25 } }}>
      {values.map(([value, label]) => (
        <ToggleButton key={value || "all"} value={value}>
          {label}
        </ToggleButton>
      ))}
    </ToggleButtonGroup>
  );

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
      <DataTable
        columns={columns}
        rows={votes.data ? rows : undefined}
        rowKey={(v) => String(v.id)}
        loading={votes.isLoading}
        fetching={votes.isFetching}
        error={votes.error}
        onRetry={() => void votes.refetch()}
        emptyText={t("feedback.emptyVotes")}
        noMatchText={t("feedback.noMatch")}
        filtered={Object.values(filter).some(Boolean)}
        toolbar={
          <Stack direction="row" spacing={1.5} useFlexGap flexWrap="wrap" alignItems="center">
            {toggle("sign", [["", t("feedback.filters.all")], ["works", t("feedback.filters.works")], ["broken", t("feedback.filters.broken")]])}
            {toggle("author", [["", t("feedback.filters.anyone")], ["exclude", t("feedback.filters.notAuthor")], ["only", t("feedback.filters.onlyAuthor")]])}
            {toggle("verified", [["", t("feedback.filters.anyOrigin")], ["1", t("feedback.filters.verified")], ["0", t("feedback.filters.unverified")]])}
            <OriginFilters asn={filter.asn} cc={filter.cc} onChange={update} />
            {filter.set && <Chip label={t("feedback.filters.setChip", { id: filter.set.slice(0, 8) })} onDelete={() => set("set", null)} />}
            {filter.key && <Chip label={t("feedback.filters.keyChip", { key: filter.key.slice(0, 8) })} onDelete={() => set("key", null)} />}
          </Stack>
        }
      />
      <Box sx={{ display: "flex", alignItems: "center", gap: 2 }}>
        <Typography variant="caption" sx={{ color: colors.text.secondary }}>
          {t("table.shown", { shown: rows.length, total })}
        </Typography>
        {votes.hasNextPage && (
          <Button size="small" disabled={votes.isFetchingNextPage} onClick={() => void votes.fetchNextPage()}>
            {t("app.loadMore")}
          </Button>
        )}
      </Box>
    </Box>
  );
}

export function FeedbackPage() {
  const { t } = useTranslation();
  const overlay = useOverlay();
  const [params, setParams] = useSearchParams();
  const tab = params.get("tab") === "reports" ? "reports" : "votes";
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
      <Tabs
        value={tab}
        onChange={(_e, v: "votes" | "reports") =>
          setParams(
            (prev) => {
              const next = new URLSearchParams(prev);
              next.set("tab", v);
              return next;
            },
            { replace: true },
          )
        }
      >
        <Tab value="votes" label={t("feedback.votes")} />
        <Tab value="reports" label={t("feedback.reports")} />
      </Tabs>
      <Typography variant="body2" sx={{ color: colors.text.secondary }}>
        {t(tab === "votes" ? "feedback.votesHint" : "reports.inboxHint")}
      </Typography>
      {tab === "votes" ? <VotesTab /> : <ReportsInbox onOpenSet={(id) => overlay.open(setPath(id))} />}
    </Box>
  );
}
