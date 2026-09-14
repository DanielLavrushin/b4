import { useMemo, useState } from "react";
import { Box, Chip, Tab, Table, TableBody, TableCell, TableHead, TableRow, Tabs, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import { useSets } from "@/api/hub";
import type { EntryView, SetGroup } from "@/models/api";
import { EmptyState, ErrorState, Loading } from "@/components/common/States";
import { SearchField } from "@/components/common/SearchField";
import { Mono } from "@/components/common/Mono";
import { SetDetailDrawer } from "@/components/sets/SetDetailDrawer";
import { TargetsSummary } from "@/components/sets/EntryFacts";
import { useModeration } from "@/components/sets/useModeration";
import { formatAgo, formatStamp } from "@/utils/format";

const groups: Exclude<SetGroup, "pending">[] = ["listed", "superseded", "hidden", "rejected"];
const recentLimit = 50;

const matches = (entry: EntryView, needle: string) => {
  if (!needle) return true;
  const hay = [
    entry.title,
    entry.set_id,
    entry.author,
    entry.family ?? "",
    entry.status_reason ?? "",
    ...entry.targets.domains,
    ...entry.targets.geosite,
    ...entry.strategy,
  ]
    .join("\n")
    .toLowerCase();
  return hay.includes(needle);
};

export function SetsPage() {
  const { t } = useTranslation();
  const sets = useSets();
  const moderation = useModeration();
  const [group, setGroup] = useState<Exclude<SetGroup, "pending">>("listed");
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<string | null>(null);

  const rows = useMemo(() => {
    const all = sets.data?.[group] ?? [];
    const needle = query.trim().toLowerCase();
    return all.filter((e) => matches(e, needle));
  }, [sets.data, group, query]);

  if (sets.isLoading) return <Loading />;
  if (sets.error) return <ErrorState error={sets.error} />;
  const data = sets.data;
  if (!data) return null;
  const decisions = group === "hidden" || group === "rejected";

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
      <Box sx={{ display: "flex", gap: 2, alignItems: "center", flexWrap: "wrap" }}>
        <Tabs value={group} onChange={(_e, v: Exclude<SetGroup, "pending">) => setGroup(v)} sx={{ flex: 1, minWidth: 0 }}>
          {groups.map((g) => (
            <Tab key={g} value={g} label={`${t(`sets.${g}`)} (${String(data[g].length)})`} />
          ))}
        </Tabs>
        <SearchField value={query} onChange={setQuery} placeholder={t("sets.searchPlaceholder")} />
      </Box>

      {data[group].length === 0 ? (
        <EmptyState text={t("sets.empty")} />
      ) : rows.length === 0 ? (
        <EmptyState text={t("sets.noMatch")} />
      ) : (
        <Box sx={{ overflowX: "auto", border: `1px solid ${colors.border.default}`, borderRadius: 1, bgcolor: colors.background.paper }}>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>{t("sets.columns.title")}</TableCell>
                <TableCell>{t("sets.columns.targets")}</TableCell>
                {!decisions && <TableCell>{t("sets.columns.strategy")}</TableCell>}
                {!decisions && <TableCell align="right">{t("sets.columns.votes")}</TableCell>}
                <TableCell>{t("sets.columns.author")}</TableCell>
                {group === "superseded" && <TableCell>{t("sets.columns.supersededBy")}</TableCell>}
                {decisions && <TableCell>{t("sets.columns.reason")}</TableCell>}
                <TableCell align="right">{t("sets.columns.updated")}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {rows.map((e) => (
                <TableRow
                  key={`${e.set_id}/${String(e.version)}`}
                  hover
                  onClick={() => setSelected(e.set_id)}
                  sx={{ cursor: "pointer" }}
                >
                  <TableCell sx={{ minWidth: 220 }}>
                    <Typography variant="body2" sx={{ fontWeight: 600, overflowWrap: "anywhere" }}>
                      {e.title}
                    </Typography>
                    <Typography variant="monoSmall" sx={{ color: colors.text.secondary, display: "block" }}>
                      {e.set_id}/{e.version}
                      {e.family ? ` · ${e.family}` : ""}
                      {e.versions && e.versions.length > 1 ? ` · ${t("sets.versions", { list: e.versions.join(", ") })}` : ""}
                    </Typography>
                    {e.flags.length > 0 && (
                      <Box sx={{ display: "flex", gap: 0.5, flexWrap: "wrap", mt: 0.5 }}>
                        {e.flags.map((f) => (
                          <Chip key={f} size="small" variant="outlined" color="warning" label={f} />
                        ))}
                      </Box>
                    )}
                  </TableCell>
                  <TableCell sx={{ maxWidth: 260 }}>
                    <TargetsSummary entry={e} />
                  </TableCell>
                  {!decisions && (
                    <TableCell sx={{ maxWidth: 320 }}>
                      <Typography variant="caption" sx={{ color: colors.text.secondary, display: "block", overflowWrap: "anywhere" }}>
                        {e.strategy.join("; ")}
                      </Typography>
                    </TableCell>
                  )}
                  {!decisions && (
                    <TableCell align="right" sx={{ whiteSpace: "nowrap" }}>
                      <Typography component="span" variant="body2" sx={{ color: colors.state.success }}>
                        +{e.votes.works}
                      </Typography>{" "}
                      <Typography component="span" variant="body2" sx={{ color: colors.state.error }}>
                        -{e.votes.broken}
                      </Typography>
                      {e.reports.length > 0 && (
                        <Chip size="small" color="warning" label={e.reports.length} sx={{ ml: 1 }} />
                      )}
                    </TableCell>
                  )}
                  <TableCell>
                    <Mono title={e.uploader_hmac}>{e.author}</Mono>
                    {e.asn_observed && (
                      <Typography variant="caption" sx={{ display: "block", color: colors.text.disabled }}>
                        AS{e.asn_observed} {e.country_observed}
                      </Typography>
                    )}
                  </TableCell>
                  {group === "superseded" && (
                    <TableCell sx={{ whiteSpace: "nowrap" }}>
                      v{e.superseded_by}
                      <Typography variant="caption" sx={{ display: "block", color: colors.text.disabled }}>
                        {formatStamp(e.superseded_at)}
                      </Typography>
                    </TableCell>
                  )}
                  {decisions && <TableCell sx={{ maxWidth: 320, overflowWrap: "anywhere" }}>{e.status_reason}</TableCell>}
                  <TableCell align="right" sx={{ whiteSpace: "nowrap" }} title={formatStamp(e.updated_at)}>
                    {formatAgo(t, e.updated_at)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Box>
      )}
      {decisions && data[group].length >= recentLimit && (
        <Typography variant="caption" sx={{ color: colors.text.secondary }}>
          {t("sets.recentOnly", { count: recentLimit })}
        </Typography>
      )}
      <SetDetailDrawer id={selected} onClose={() => setSelected(null)} moderation={moderation} />
      {moderation.dialog}
    </Box>
  );
}
