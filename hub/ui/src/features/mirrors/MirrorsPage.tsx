import {
  Alert,
  Box,
  Button,
  CircularProgress,
  IconButton,
  Link,
  Paper,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Tooltip,
  Typography,
  useMediaQuery,
} from "@mui/material";
import RefreshIcon from "@mui/icons-material/Refresh";
import DeleteIcon from "@mui/icons-material/DeleteOutline";
import InfoIcon from "@mui/icons-material/InfoOutlined";
import { Fragment, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { colors, theme } from "@design";
import { useSnackbar } from "@/app/SnackbarProvider";
import type { MirrorView, MirrorsView } from "@/models/api";
import { EmptyState } from "@/shared/components/States";
import { QueryView } from "@/shared/components/QueryView";
import { KeyRef } from "@/shared/components/KeyRef";
import { StatusDot, type StatusTone } from "@/shared/components/StatusDot";
import type { Moderation } from "@/features/moderation/useModeration";
import { useModerationContext } from "@/features/moderation/ModerationProvider";
import { formatAgo, formatStamp } from "@/shared/utils/format";
import { useMirrorCheck, useMirrors, useMirrorsCheck } from "./api";

type Line = string | false | undefined;

interface Signal {
  tone: StatusTone;
  label: string;
  muted?: boolean;
  lines: Line[];
}

const tip = (lines: Line[]): ReactNode => {
  const shown = lines.filter((line): line is string => typeof line === "string" && line !== "");
  if (shown.length === 0) return undefined;
  return <Box sx={{ whiteSpace: "pre-line" }}>{shown.join("\n")}</Box>;
};

function statusSignal(t: TFunction, m: MirrorView): Signal {
  const leaving = m.announced && t("mirrors.announced.leaving");
  if (m.status === "pending") return { tone: "warning", label: t("status.mirror.pending"), lines: [leaving] };
  if (m.status === "rejected") return { tone: "error", label: t("status.mirror.rejected"), lines: [m.reason, leaving] };
  const approved = t("status.mirror.approved");
  const drops = m.drops_at !== undefined && t("mirrors.dropsAt", { when: formatStamp(m.drops_at) });
  if (m.kept) return { tone: "warning", label: t("mirrors.state.kept"), lines: [approved, t("mirrors.announced.kept")] };
  if (m.announced && m.announce_next) return { tone: "success", label: t("mirrors.announced.listed"), lines: [approved, drops] };
  if (m.announced) return { tone: "warning", label: t("mirrors.state.leaving"), lines: [approved, t("mirrors.announced.leaving"), drops] };
  if (m.announce_next) return { tone: "info", label: t("mirrors.state.joining"), lines: [approved, t("mirrors.announced.joining"), drops] };
  return { tone: "neutral", label: t("mirrors.announced.notListed"), lines: [approved, t("mirrors.announced.waiting")] };
}

function checkSignal(t: TFunction, m: MirrorView): Signal {
  if (!m.last_check) return { tone: "neutral", label: t("mirrors.health.unchecked"), muted: true, lines: [] };
  const when = formatAgo(t, m.last_check);
  const stamp = [formatStamp(m.last_check), m.check_ms ? t("mirrors.health.ms", { ms: m.check_ms }) : ""].filter(Boolean).join(" · ");
  if (m.healthy) return { tone: "success", label: `${t("mirrors.health.ok")} · ${when}`, lines: [stamp] };
  return {
    tone: "error",
    label: `${t("mirrors.health.failed")} · ${when}`,
    lines: [
      m.check_code && t(`mirrors.checkCode.${m.check_code}`, { defaultValue: m.check_code }),
      m.check_error,
      stamp,
      m.last_ok !== undefined && t("mirrors.health.lastOk", { when: formatAgo(t, m.last_ok) }),
    ],
  };
}

function SignalDot({ signal }: Readonly<{ signal: Signal }>) {
  return <StatusDot tone={signal.tone} label={signal.label} muted={signal.muted} tooltip={tip(signal.lines)} />;
}

function MirrorCell({ m, withLastSeen }: Readonly<{ m: MirrorView; withLastSeen: boolean }>) {
  const { t } = useTranslation();
  const lines = [m.url, t("mirrors.firstSeen", { when: formatStamp(m.first_seen) }), withLastSeen && t("mirrors.lastSeen", { when: formatAgo(t, m.last_seen) })];
  return (
    <Box sx={{ minWidth: 0 }}>
      <Tooltip title={tip(lines)} describeChild>
        <Link
          href={m.url}
          target="_blank"
          rel="noopener noreferrer"
          underline="hover"
          variant="body2"
          noWrap
          sx={{ display: "block", maxWidth: { xs: 220, xl: 380 }, color: colors.text.primary, fontWeight: 500 }}
        >
          {m.url}
        </Link>
      </Tooltip>
      <Typography variant="caption" component="div" sx={{ display: "flex", alignItems: "center", gap: 0.75, minWidth: 0, overflow: "hidden", color: colors.text.secondary, whiteSpace: "nowrap" }}>
        <span>{m.version || t("mirrors.versionUnknown")}</span>
        <span aria-hidden>·</span>
        <KeyRef hmac={m.key_hmac} label={m.key} dot={false} />
      </Typography>
    </Box>
  );
}

function Serving({ m }: Readonly<{ m: MirrorView }>) {
  const { t } = useTranslation();
  if (!m.served_epoch) {
    return (
      <Typography variant="body2" sx={{ color: colors.text.disabled }}>
        {t("mirrors.lag.unknown")}
      </Typography>
    );
  }
  const served = [t("mirrors.served", { epoch: m.served_epoch, seq: m.served_seq ?? 0 }), formatStamp(m.served_generated_at)].filter(Boolean).join(" · ");
  const behind = m.lag === "stale" && m.behind_by !== undefined && t("mirrors.lag.behind", { count: m.behind_by });
  return (
    <Tooltip title={tip([served, behind])} describeChild>
      <Typography component="span" variant="body2" tabIndex={0} sx={{ color: m.lag === "stale" ? colors.state.error : colors.text.secondary }}>
        {t(`mirrors.lag.${m.lag}`, { count: m.behind_by ?? 0 })}
      </Typography>
    </Tooltip>
  );
}

function LastAnnounced({ m }: Readonly<{ m: MirrorView }>) {
  const { t } = useTranslation();
  return (
    <Tooltip title={formatStamp(m.last_seen)} describeChild>
      <Typography component="span" variant="body2" tabIndex={0} sx={{ color: colors.text.secondary }}>
        {formatAgo(t, m.last_seen)}
      </Typography>
    </Tooltip>
  );
}

function MirrorActions({ m, busy, moderation }: Readonly<{ m: MirrorView; busy: boolean; moderation: Moderation }>) {
  const { t } = useTranslation();
  const check = useMirrorCheck();
  const { notifyResult, notifyError } = useSnackbar();
  const locked = busy || moderation.busy;
  const runCheck = async () => {
    try {
      notifyResult(await check.mutateAsync(m.id));
    } catch (err) {
      notifyError(err);
    }
  };
  return (
    <Stack direction="row" spacing={0.5} justifyContent="flex-end" alignItems="center">
      {m.status !== "approved" && (
        <Button size="small" variant="contained" disabled={locked} onClick={() => moderation.approveMirror(m)}>
          {t("mirrors.approve")}
        </Button>
      )}
      {m.status !== "rejected" && (
        <Button size="small" variant="outlined" disabled={locked} onClick={() => moderation.rejectMirror(m)}>
          {t("mirrors.reject")}
        </Button>
      )}
      <Tooltip title={t("mirrors.checkNow")}>
        <span>
          <IconButton size="small" aria-label={t("mirrors.checkNow")} disabled={check.isPending} onClick={() => void runCheck()} sx={{ color: colors.text.secondary }}>
            {check.isPending ? <CircularProgress size={16} sx={{ color: colors.secondary }} /> : <RefreshIcon fontSize="small" />}
          </IconButton>
        </span>
      </Tooltip>
      <Tooltip title={t("mirrors.remove")}>
        <span>
          <IconButton size="small" aria-label={t("mirrors.remove")} disabled={locked} onClick={() => moderation.removeMirror(m)} sx={{ color: colors.text.secondary }}>
            <DeleteIcon fontSize="small" />
          </IconButton>
        </span>
      </Tooltip>
    </Stack>
  );
}

const flat = { borderBottom: "none" };

function MirrorTable({ mirrors, busy, moderation }: Readonly<{ mirrors: MirrorView[]; busy: boolean; moderation: Moderation }>) {
  const { t } = useTranslation();
  const compact = useMediaQuery(theme.breakpoints.down(1280));
  const lastColumn = useMediaQuery(theme.breakpoints.up("xl"));
  const headers = ["mirror", "status", "check", "serving"];
  if (!compact && lastColumn) headers.push("lastAnnounced");
  if (!compact) headers.push("");
  return (
    <Paper variant="outlined" sx={{ bgcolor: colors.background.paper, border: `1px solid ${colors.border.default}`, overflow: "hidden", minWidth: 0 }}>
      <TableContainer>
        <Table size="small" sx={{ "& .MuiTableCell-root": { px: 1.5, "&:first-of-type": { pl: 2 }, "&:last-of-type": { pr: 2 } } }}>
          <TableHead>
            <TableRow>
              {headers.map((id) => (
                <TableCell key={id || "actions"} sx={{ whiteSpace: "nowrap" }}>
                  {id && t(`mirrors.columns.${id}`)}
                </TableCell>
              ))}
            </TableRow>
          </TableHead>
          <TableBody>
            {mirrors.map((m) =>
              compact ? (
                <Fragment key={m.id}>
                  <TableRow>
                    <TableCell rowSpan={2} sx={{ verticalAlign: "top", width: "40%", maxWidth: 0 }}>
                      <MirrorCell m={m} withLastSeen />
                    </TableCell>
                    <TableCell sx={{ ...flat, whiteSpace: "nowrap" }}>
                      <SignalDot signal={statusSignal(t, m)} />
                    </TableCell>
                    <TableCell sx={{ ...flat, whiteSpace: "nowrap" }}>
                      <SignalDot signal={checkSignal(t, m)} />
                    </TableCell>
                    <TableCell sx={{ ...flat, whiteSpace: "nowrap" }}>
                      <Serving m={m} />
                    </TableCell>
                  </TableRow>
                  <TableRow>
                    <TableCell colSpan={3} sx={{ pt: 0 }}>
                      <MirrorActions m={m} busy={busy} moderation={moderation} />
                    </TableCell>
                  </TableRow>
                </Fragment>
              ) : (
                <TableRow key={m.id} hover>
                  <TableCell>
                    <MirrorCell m={m} withLastSeen={!lastColumn} />
                  </TableCell>
                  <TableCell sx={{ whiteSpace: "nowrap" }}>
                    <SignalDot signal={statusSignal(t, m)} />
                  </TableCell>
                  <TableCell sx={{ whiteSpace: "nowrap" }}>
                    <SignalDot signal={checkSignal(t, m)} />
                  </TableCell>
                  <TableCell sx={{ whiteSpace: "nowrap" }}>
                    <Serving m={m} />
                  </TableCell>
                  {lastColumn && (
                    <TableCell sx={{ whiteSpace: "nowrap" }}>
                      <LastAnnounced m={m} />
                    </TableCell>
                  )}
                  <TableCell align="right" sx={{ whiteSpace: "nowrap" }}>
                    <MirrorActions m={m} busy={busy} moderation={moderation} />
                  </TableCell>
                </TableRow>
              ),
            )}
          </TableBody>
        </Table>
      </TableContainer>
    </Paper>
  );
}

function Mirrors({ data }: Readonly<{ data: MirrorsView }>) {
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
  const policy = t("mirrors.policy", { hours: Math.round(data.window_s / 3600), minutes: Math.round(data.check_interval_s / 60) });
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
      <Box sx={{ display: "flex", alignItems: "flex-start", gap: 2, flexWrap: "wrap" }}>
        <Box sx={{ flex: 1, minWidth: 240 }}>
          <Typography variant="sectionHeader" sx={{ display: "block" }}>
            {t("mirrors.title", { count: data.mirrors.length })}
          </Typography>
          <Typography variant="caption" component="div" sx={{ display: "flex", alignItems: "center", gap: 0.5, mt: 0.5, color: colors.text.secondary }}>
            <span>
              {data.manifest.published
                ? t("mirrors.summary", { epoch: data.manifest.epoch, seq: data.manifest.seq, count: announced })
                : t("overview.notPublished")}
            </span>
            <Tooltip title={policy}>
              <Box component="span" role="img" tabIndex={0} sx={{ display: "inline-flex", color: colors.text.secondary, cursor: "help" }}>
                <InfoIcon sx={{ fontSize: 15 }} />
              </Box>
            </Tooltip>
          </Typography>
        </Box>
        <Button
          variant="outlined"
          size="small"
          startIcon={busy ? <CircularProgress size={14} sx={{ color: colors.secondary }} /> : <RefreshIcon />}
          disabled={busy}
          onClick={() => void runAll()}
        >
          {t("mirrors.checkAll")}
        </Button>
      </Box>
      {data.orphans.length > 0 && <Alert severity="warning">{t("mirrors.orphans", { list: data.orphans.join(", ") })}</Alert>}
      {data.mirrors.length === 0 ? <EmptyState text={t("mirrors.empty")} /> : <MirrorTable mirrors={data.mirrors} busy={busy} moderation={moderation} />}
    </Box>
  );
}

export function MirrorsPage() {
  const mirrors = useMirrors();
  return <QueryView query={mirrors}>{(data) => <Mirrors data={data} />}</QueryView>;
}
