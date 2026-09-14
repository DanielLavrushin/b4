import { Box, Button, Link, Stack, Table, TableBody, TableCell, TableHead, TableRow, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import { useMirrors } from "@/api/hub";
import type { MirrorView } from "@/models/api";
import { EmptyState, ErrorState, Loading } from "@/components/common/States";
import { StatusChip } from "@/components/common/StatusChip";
import { Mono } from "@/components/common/Mono";
import { useModeration } from "@/components/sets/useModeration";
import { formatAgo, formatStamp } from "@/utils/format";

function Health({ m }: { m: MirrorView }) {
  const { t } = useTranslation();
  if (!m.last_check) return <StatusChip status="pending" label={t("mirrors.health.unchecked")} />;
  if (m.healthy) return <StatusChip status="healthy" label={t("mirrors.health.ok", { when: formatAgo(t, m.last_check) })} />;
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 0.5 }}>
      <StatusChip status="unhealthy" label={t("mirrors.health.failed", { when: formatAgo(t, m.last_check) })} />
      {m.reason && m.status !== "rejected" && (
        <Typography variant="caption" sx={{ color: colors.text.secondary }}>
          {m.reason}
        </Typography>
      )}
      {m.last_ok && (
        <Typography variant="caption" sx={{ color: colors.text.disabled }}>
          {t("mirrors.health.lastOk", { when: formatAgo(t, m.last_ok) })}
        </Typography>
      )}
    </Box>
  );
}

export function MirrorsPage() {
  const { t } = useTranslation();
  const mirrors = useMirrors();
  const moderation = useModeration();

  if (mirrors.isLoading) return <Loading />;
  if (mirrors.error) return <ErrorState error={mirrors.error} />;
  const data = mirrors.data ?? [];

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
      <Box>
        <Typography variant="sectionHeader">{t("mirrors.title", { count: data.length })}</Typography>
        <Typography variant="caption" sx={{ color: colors.text.secondary }}>
          {t("mirrors.listedHint")}
        </Typography>
      </Box>
      {data.length === 0 ? (
        <EmptyState text={t("mirrors.empty")} />
      ) : (
        <Box sx={{ overflowX: "auto", border: `1px solid ${colors.border.default}`, borderRadius: 1, bgcolor: colors.background.paper }}>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>{t("mirrors.columns.url")}</TableCell>
                <TableCell>{t("mirrors.columns.key")}</TableCell>
                <TableCell>{t("mirrors.columns.status")}</TableCell>
                <TableCell>{t("mirrors.columns.firstSeen")}</TableCell>
                <TableCell>{t("mirrors.columns.lastSeen")}</TableCell>
                <TableCell>{t("mirrors.columns.health")}</TableCell>
                <TableCell />
              </TableRow>
            </TableHead>
            <TableBody>
              {data.map((m) => (
                <TableRow key={m.id} hover>
                  <TableCell sx={{ overflowWrap: "anywhere" }}>
                    <Link href={m.url} rel="noreferrer" underline="hover">
                      {m.url}
                    </Link>
                  </TableCell>
                  <TableCell><Mono title={m.key_hmac}>{m.key}</Mono></TableCell>
                  <TableCell>
                    <StatusChip status={m.status} />
                    {m.status === "rejected" && m.reason && (
                      <Typography variant="caption" sx={{ display: "block", color: colors.text.secondary }}>
                        {m.reason}
                      </Typography>
                    )}
                  </TableCell>
                  <TableCell sx={{ whiteSpace: "nowrap" }} title={formatStamp(m.first_seen)}>{formatAgo(t, m.first_seen)}</TableCell>
                  <TableCell sx={{ whiteSpace: "nowrap" }} title={formatStamp(m.last_seen)}>{formatAgo(t, m.last_seen)}</TableCell>
                  <TableCell><Health m={m} /></TableCell>
                  <TableCell align="right">
                    <Stack direction="row" spacing={1} justifyContent="flex-end" useFlexGap flexWrap="wrap">
                      {m.status !== "approved" && (
                        <Button size="small" variant="contained" color="success" disabled={moderation.busy} onClick={() => void moderation.approveMirror(m)}>
                          {t("mirrors.approve")}
                        </Button>
                      )}
                      {m.status !== "rejected" && (
                        <Button size="small" variant="outlined" color="error" disabled={moderation.busy} onClick={() => moderation.rejectMirror(m)}>
                          {t("mirrors.reject")}
                        </Button>
                      )}
                      <Button size="small" variant="text" color="inherit" disabled={moderation.busy} onClick={() => moderation.removeMirror(m)}>
                        {t("mirrors.remove")}
                      </Button>
                    </Stack>
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
