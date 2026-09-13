import { useState } from "react";
import { Box, Chip, Link, MenuItem, Select, Tab, Table, TableBody, TableCell, TableHead, TableRow, Tabs, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import { useFeedback } from "@/api/hub";
import { EmptyState, ErrorState, Loading } from "@/components/common/States";
import { Mono } from "@/components/common/Mono";
import { SetDetailDrawer } from "@/components/sets/SetDetailDrawer";
import { useModeration } from "@/components/sets/useModeration";
import { formatStamp, setRef } from "@/utils/format";

const limits = [50, 200, 500, 1000];

export function FeedbackPage() {
  const { t } = useTranslation();
  const [tab, setTab] = useState<"votes" | "reports">("votes");
  const [limit, setLimit] = useState(200);
  const [selected, setSelected] = useState<string | null>(null);
  const feedback = useFeedback(limit);
  const moderation = useModeration();

  if (feedback.isLoading) return <Loading />;
  if (feedback.error) return <ErrorState error={feedback.error} />;
  const data = feedback.data;
  if (!data) return null;

  const setLink = (id: string, version: number) => (
    <Link component="button" type="button" underline="hover" onClick={() => setSelected(id)}>
      <Mono>{setRef(id, version)}</Mono>
    </Link>
  );

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
      <Box sx={{ display: "flex", gap: 2, alignItems: "center", flexWrap: "wrap" }}>
        <Tabs value={tab} onChange={(_e, v: "votes" | "reports") => setTab(v)} sx={{ flex: 1 }}>
          <Tab value="votes" label={`${t("feedback.votes")} (${String(data.votes.length)})`} />
          <Tab value="reports" label={`${t("feedback.reports")} (${String(data.reports.length)})`} />
        </Tabs>
        <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
          <Typography variant="caption" sx={{ color: colors.text.secondary }}>
            {t("feedback.limit")}
          </Typography>
          <Select size="small" value={limit} onChange={(e) => setLimit(Number(e.target.value))}>
            {limits.map((n) => (
              <MenuItem key={n} value={n}>
                {n}
              </MenuItem>
            ))}
          </Select>
        </Box>
      </Box>

      {tab === "votes" &&
        (data.votes.length === 0 ? (
          <EmptyState text={t("feedback.emptyVotes")} />
        ) : (
          <Box sx={{ overflowX: "auto", border: `1px solid ${colors.border.default}`, borderRadius: 1, bgcolor: colors.background.paper }}>
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell>{t("feedback.columns.when")}</TableCell>
                  <TableCell>{t("feedback.columns.set")}</TableCell>
                  <TableCell>{t("feedback.columns.kind")}</TableCell>
                  <TableCell align="right">{t("feedback.columns.weight")}</TableCell>
                  <TableCell>{t("feedback.columns.origin")}</TableCell>
                  <TableCell>{t("feedback.columns.key")}</TableCell>
                  <TableCell>{t("feedback.columns.domain")}</TableCell>
                  <TableCell>{t("feedback.columns.b4")}</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {data.votes.map((v) => (
                  <TableRow key={v.id} hover>
                    <TableCell sx={{ whiteSpace: "nowrap" }}>{formatStamp(v.received_at)}</TableCell>
                    <TableCell>{setLink(v.set_id, v.version)}</TableCell>
                    <TableCell>
                      <Chip size="small" variant="outlined" color={v.weight >= 0 ? "success" : "error"} label={t(`feedback.kinds.${v.kind}`, { defaultValue: v.kind })} />
                    </TableCell>
                    <TableCell align="right">{v.weight.toFixed(1)}</TableCell>
                    <TableCell sx={{ whiteSpace: "nowrap" }}>
                      {v.asn_observed ? `AS${v.asn_observed} ${v.country_observed ?? ""}` : ""}
                      {v.asn_hint && (
                        <Typography component="span" variant="caption" sx={{ color: colors.text.disabled, ml: 0.5 }}>
                          ({v.asn_hint} {v.country_hint ?? ""})
                        </Typography>
                      )}
                    </TableCell>
                    <TableCell><Mono title={v.key_hmac}>{v.key}</Mono></TableCell>
                    <TableCell>{v.domain ?? ""}</TableCell>
                    <TableCell sx={{ whiteSpace: "nowrap" }}>
                      {v.b4_version ?? ""} {v.engine ?? ""}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </Box>
        ))}

      {tab === "reports" &&
        (data.reports.length === 0 ? (
          <EmptyState text={t("feedback.emptyReports")} />
        ) : (
          <Box sx={{ overflowX: "auto", border: `1px solid ${colors.border.default}`, borderRadius: 1, bgcolor: colors.background.paper }}>
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell>{t("feedback.columns.when")}</TableCell>
                  <TableCell>{t("feedback.columns.set")}</TableCell>
                  <TableCell>{t("feedback.columns.origin")}</TableCell>
                  <TableCell>{t("feedback.columns.key")}</TableCell>
                  <TableCell>{t("feedback.columns.reason")}</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {data.reports.map((r) => (
                  <TableRow key={r.id} hover>
                    <TableCell sx={{ whiteSpace: "nowrap" }}>{formatStamp(r.received_at)}</TableCell>
                    <TableCell>{setLink(r.set_id, r.version)}</TableCell>
                    <TableCell>{r.asn_observed ? `AS${r.asn_observed}` : ""}</TableCell>
                    <TableCell><Mono title={r.key_hmac}>{r.key}</Mono></TableCell>
                    <TableCell sx={{ overflowWrap: "anywhere" }}>{r.reason}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </Box>
        ))}

      <SetDetailDrawer id={selected} onClose={() => setSelected(null)} moderation={moderation} />
      {moderation.dialog}
    </Box>
  );
}
