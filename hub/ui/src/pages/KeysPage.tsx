import { useMemo, useState } from "react";
import { Box, Button, FormControlLabel, Switch, Table, TableBody, TableCell, TableHead, TableRow, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import { useKeys } from "@/api/hub";
import { EmptyState, ErrorState, Loading } from "@/components/common/States";
import { SearchField } from "@/components/common/SearchField";
import { StatusChip } from "@/components/common/StatusChip";
import { Mono } from "@/components/common/Mono";
import { useModeration } from "@/components/sets/useModeration";
import { formatAgo, formatStamp } from "@/utils/format";

export function KeysPage() {
  const { t } = useTranslation();
  const keys = useKeys();
  const moderation = useModeration();
  const [query, setQuery] = useState("");
  const [bannedOnly, setBannedOnly] = useState(false);

  const rows = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return (keys.data ?? []).filter((k) => (!bannedOnly || k.banned) && (!needle || k.key_hmac.includes(needle)));
  }, [keys.data, query, bannedOnly]);

  if (keys.isLoading) return <Loading />;
  if (keys.error) return <ErrorState error={keys.error} />;
  const data = keys.data ?? [];

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
      <Box sx={{ display: "flex", gap: 2, alignItems: "center", flexWrap: "wrap" }}>
        <Typography variant="sectionHeader" sx={{ flex: 1 }}>
          {t("keys.title", { count: data.length })}
        </Typography>
        <FormControlLabel control={<Switch size="small" checked={bannedOnly} onChange={(e) => setBannedOnly(e.target.checked)} />} label={t("keys.showBanned")} />
        <SearchField value={query} onChange={setQuery} placeholder={t("keys.searchPlaceholder")} />
      </Box>
      {data.length === 0 ? (
        <EmptyState text={t("keys.empty")} />
      ) : (
        <Box sx={{ overflowX: "auto", border: `1px solid ${colors.border.default}`, borderRadius: 1, bgcolor: colors.background.paper }}>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>{t("keys.columns.key")}</TableCell>
                <TableCell>{t("keys.columns.firstSeen")}</TableCell>
                <TableCell align="right">{t("keys.columns.sets")}</TableCell>
                <TableCell align="right">{t("keys.columns.votes")}</TableCell>
                <TableCell align="right">{t("keys.columns.reports")}</TableCell>
                <TableCell>{t("keys.columns.status")}</TableCell>
                <TableCell />
              </TableRow>
            </TableHead>
            <TableBody>
              {rows.map((k) => (
                <TableRow key={k.key_hmac} hover>
                  <TableCell><Mono>{k.key_hmac}</Mono></TableCell>
                  <TableCell sx={{ whiteSpace: "nowrap" }} title={formatStamp(k.first_seen)}>{formatAgo(t, k.first_seen)}</TableCell>
                  <TableCell align="right">{k.sets}</TableCell>
                  <TableCell align="right">{k.votes}</TableCell>
                  <TableCell align="right">{k.reports}</TableCell>
                  <TableCell>
                    {k.banned ? (
                      <Box sx={{ display: "flex", flexDirection: "column", gap: 0.5 }}>
                        <StatusChip status="banned" label={t("keys.banned", { when: formatAgo(t, k.banned_at) })} />
                        {k.ban_reason && (
                          <Typography variant="caption" sx={{ color: colors.text.secondary }}>
                            {k.ban_reason}
                          </Typography>
                        )}
                      </Box>
                    ) : (
                      <StatusChip status="ok" label={t("keys.ok")} />
                    )}
                  </TableCell>
                  <TableCell align="right" sx={{ whiteSpace: "nowrap" }}>
                    {k.banned ? (
                      <Button size="small" variant="outlined" color="success" disabled={moderation.busy} onClick={() => void moderation.unban(k.key_hmac)}>
                        {t("keys.unban")}
                      </Button>
                    ) : (
                      <Button size="small" variant="outlined" color="error" disabled={moderation.busy} onClick={() => moderation.ban(k.key_hmac, k.label)}>
                        {t("keys.ban")}
                      </Button>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Box>
      )}
      {moderation.dialog}
    </Box>
  );
}
