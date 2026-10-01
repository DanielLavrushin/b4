import { Box, Button, Chip, CircularProgress, IconButton, Link, Stack, Table, TableBody, TableCell, TableHead, TableRow, Tooltip, Typography } from "@mui/material";
import RefreshIcon from "@mui/icons-material/Refresh";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import { useSnackbar } from "@/app/SnackbarProvider";
import type { MirrorView, MirrorsView } from "@/models/api";
import { EmptyState } from "@/shared/components/States";
import { QueryView } from "@/shared/components/QueryView";
import { StatusChip } from "@/shared/components/StatusChip";
import { Mono } from "@/shared/components/Mono";
import type { Moderation } from "@/features/moderation/useModeration";
import { useModerationContext } from "@/features/moderation/ModerationProvider";
import { formatAgo, formatStamp } from "@/shared/utils/format";
import { useMirrorCheck, useMirrors, useMirrorsCheck } from "./api";

function Announced({ m }: { m: MirrorView }) {
  const { t } = useTranslation();
  if (m.status !== "approved") return <Typography variant="caption" sx={{ color: colors.text.disabled }}>{t("mirrors.announced.no")}</Typography>;
  if (m.kept) return <Chip size="small" color="warning" variant="outlined" label={t("mirrors.announced.kept")} />;
  if (m.announced && m.announce_next) return <Chip size="small" color="success" variant="outlined" label={t("mirrors.announced.listed")} />;
  if (m.announced) return <Chip size="small" color="warning" variant="outlined" label={t("mirrors.announced.leaving")} />;
  if (m.announce_next) return <Chip size="small" color="info" variant="outlined" label={t("mirrors.announced.joining")} />;
  return <Chip size="small" variant="outlined" label={t("mirrors.announced.notListed")} />;
}

function Health({ m }: { m: MirrorView }) {
  const { t } = useTranslation();
  if (!m.last_check) return <StatusChip status="pending" label={t("mirrors.health.unchecked")} />;
  if (m.healthy) {
    return (
      <Tooltip title={formatStamp(m.last_check)}>
        <span>
          <StatusChip status="healthy" label={t("mirrors.health.ok", { when: formatAgo(t, m.last_check) })} />
          {m.check_ms ? (
            <Typography component="span" variant="caption" sx={{ color: colors.text.disabled, ml: 0.5 }}>
              {t("mirrors.health.ms", { ms: m.check_ms })}
            </Typography>
          ) : null}
        </span>
      </Tooltip>
    );
  }
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 0.5 }}>
      <Tooltip title={m.check_error ?? ""}>
        <span>
          <StatusChip status="unhealthy" label={t("mirrors.health.failed", { when: formatAgo(t, m.last_check) })} />
        </span>
      </Tooltip>
      {m.check_code && (
        <Typography variant="caption" sx={{ color: colors.text.secondary }}>
          {t(`mirrors.checkCode.${m.check_code}`, { defaultValue: m.check_code })}
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

function Serving({ m }: { m: MirrorView }) {
  const { t } = useTranslation();
  if (!m.served_epoch) return <Typography variant="caption" sx={{ color: colors.text.disabled }}>{t("mirrors.lag.unknown")}</Typography>;
  const tone = m.lag === "current" ? "success" : m.lag === "behind" ? "info" : m.lag === "stale" ? "error" : "default";
  return (
    <Box>
      <Mono>
        {m.served_epoch}/{m.served_seq}
      </Mono>{" "}
      <Chip size="small" variant="outlined" color={tone} label={t(`mirrors.lag.${m.lag}`, { count: m.behind_by ?? 0 })} />
    </Box>
  );
}

function MirrorRow({ m, busy, moderation }: { m: MirrorView; busy: boolean; moderation: Moderation }) {
  const { t } = useTranslation();
  const check = useMirrorCheck();
  const { notifyResult, notifyError } = useSnackbar();
  const runCheck = async () => {
    try {
      notifyResult(await check.mutateAsync(m.id));
    } catch (err) {
      notifyError(err);
    }
  };
  return (
    <TableRow hover>
      <TableCell sx={{ overflowWrap: "anywhere" }}>
        <Link href={m.url} rel="noreferrer" underline="hover">
          {m.url}
        </Link>
        <Typography variant="caption" sx={{ display: "block", color: colors.text.disabled }}>
          {m.version ? m.version : t("mirrors.versionUnknown")} · <Mono title={m.key_hmac}>{m.key}</Mono>
        </Typography>
      </TableCell>
      <TableCell>
        <StatusChip status={m.status} label={t(`status.mirror.${m.status}`)} />
        {m.status === "rejected" && m.reason && (
          <Typography variant="caption" sx={{ display: "block", color: colors.text.secondary }}>
            {m.reason}
          </Typography>
        )}
      </TableCell>
      <TableCell>
        <Announced m={m} />
        {m.drops_at && m.status === "approved" && (
          <Typography variant="caption" sx={{ display: "block", color: colors.text.disabled }} title={formatStamp(m.drops_at)}>
            {t("mirrors.dropsAt", { when: formatStamp(m.drops_at) })}
          </Typography>
        )}
      </TableCell>
      <TableCell>
        <Health m={m} />
      </TableCell>
      <TableCell>
        <Serving m={m} />
      </TableCell>
      <TableCell sx={{ whiteSpace: "nowrap" }} title={formatStamp(m.last_seen)}>
        {formatAgo(t, m.last_seen)}
      </TableCell>
      <TableCell align="right">
        <Stack direction="row" spacing={1} justifyContent="flex-end" alignItems="center" useFlexGap flexWrap="wrap">
          <Tooltip title={t("mirrors.checkNow")}>
            <span>
              <IconButton size="small" disabled={check.isPending} onClick={() => void runCheck()}>
                {check.isPending ? <CircularProgress size={16} /> : <RefreshIcon fontSize="small" />}
              </IconButton>
            </span>
          </Tooltip>
          {m.status !== "approved" && (
            <Button size="small" variant="contained" color="success" disabled={busy || moderation.busy} onClick={() => moderation.approveMirror(m)}>
              {t("mirrors.approve")}
            </Button>
          )}
          {m.status !== "rejected" && (
            <Button size="small" variant="outlined" color="error" disabled={busy || moderation.busy} onClick={() => moderation.rejectMirror(m)}>
              {t("mirrors.reject")}
            </Button>
          )}
          <Button size="small" variant="text" color="inherit" disabled={busy || moderation.busy} onClick={() => moderation.removeMirror(m)}>
            {t("mirrors.remove")}
          </Button>
        </Stack>
      </TableCell>
    </TableRow>
  );
}

function Mirrors({ data }: { data: MirrorsView }) {
  const { t } = useTranslation();
  const checkAll = useMirrorsCheck();
  const moderation = useModerationContext();
  const { notifyResult, notifyError } = useSnackbar();
  const [busy, setBusy] = useState(false);
  const runAll = async () => {
    setBusy(true);
    try {
      notifyResult(await checkAll.mutateAsync());
    } catch (err) {
      notifyError(err);
    } finally {
      setBusy(false);
    }
  };
  const announced = data.manifest.listed.filter((u) => u !== data.manifest.hub_url).length;
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
      <Box sx={{ display: "flex", alignItems: "flex-start", gap: 2, flexWrap: "wrap" }}>
        <Box sx={{ flex: 1, minWidth: 240 }}>
          <Typography variant="sectionHeader" sx={{ display: "block" }}>
            {t("mirrors.title", { count: data.mirrors.length })}
          </Typography>
          <Typography variant="caption" sx={{ color: colors.text.secondary, display: "block", mt: 0.5 }}>
            {data.manifest.published
              ? t("mirrors.summary", {
                  epoch: data.manifest.epoch,
                  seq: data.manifest.seq,
                  count: announced,
                  hours: Math.round(data.window_s / 3600),
                  minutes: Math.round(data.check_interval_s / 60),
                })
              : t("overview.notPublished")}
          </Typography>
        </Box>
        <Button variant="outlined" size="small" startIcon={busy ? <CircularProgress size={14} /> : <RefreshIcon />} disabled={busy} onClick={() => void runAll()}>
          {t("mirrors.checkAll")}
        </Button>
      </Box>
      {data.orphans.length > 0 && (
        <Typography variant="body2" sx={{ color: colors.state.warning }}>
          {t("mirrors.orphans", { list: data.orphans.join(", ") })}
        </Typography>
      )}
      {data.mirrors.length === 0 ? (
        <EmptyState text={t("mirrors.empty")} />
      ) : (
        <Box sx={{ overflowX: "auto", border: `1px solid ${colors.border.default}`, borderRadius: 1, bgcolor: colors.background.paper }}>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>{t("mirrors.columns.url")}</TableCell>
                <TableCell>{t("mirrors.columns.status")}</TableCell>
                <TableCell>{t("mirrors.columns.announced")}</TableCell>
                <TableCell>{t("mirrors.columns.health")}</TableCell>
                <TableCell>{t("mirrors.columns.serving")}</TableCell>
                <TableCell>{t("mirrors.columns.lastAnnounced")}</TableCell>
                <TableCell />
              </TableRow>
            </TableHead>
            <TableBody>
              {data.mirrors.map((m) => (
                <MirrorRow key={m.id} m={m} busy={busy} moderation={moderation} />
              ))}
            </TableBody>
          </Table>
        </Box>
      )}
    </Box>
  );
}

export function MirrorsPage() {
  const mirrors = useMirrors();
  return <QueryView query={mirrors}>{(data) => <Mirrors data={data} />}</QueryView>;
}
