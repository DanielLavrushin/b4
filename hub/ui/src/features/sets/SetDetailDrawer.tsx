import { Fragment, useEffect, useRef } from "react";
import {
  Alert,
  Box,
  Button,
  Chip,
  Divider,
  Drawer,
  IconButton,
  Link,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Typography,
} from "@mui/material";
import CloseIcon from "@mui/icons-material/Close";
import { useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import { useSetDetail } from "@/features/sets/api";
import type { EntryView, SetDetailView, VoteView } from "@/models/api";
import { EmptyState, ErrorState, Loading } from "@/shared/components/States";
import { HistoryList } from "@/features/audit/HistoryList";
import { useTargetHistory } from "@/features/audit/api";
import { QueryView } from "@/shared/components/QueryView";
import { KeyRef } from "@/shared/components/KeyRef";
import { statusTone } from "@/shared/components/StatusChip";
import { StatusDot, statusToneColor } from "@/shared/components/StatusDot";
import { formatAgo, formatStamp } from "@/shared/utils/format";
import { reasonText } from "@/shared/utils/reason";
import { EditedNotice } from "@/features/sets/EditedNotice";
import { EntryFacts, Origin } from "@/features/sets/EntryFacts";
import { VersionPill } from "@/features/sets/card/SetCard";
import type { Moderation } from "@/features/moderation/useModeration";

interface SetDetailDrawerProps {
  id: string | null;
  open: boolean;
  focusVersion?: number;
  onClose: () => void;
  moderation: Moderation;
}

const quiet = { color: colors.text.secondary, "&:hover": { color: colors.text.primary } } as const;

function VersionActions({ entry, authorBanned, moderation }: { entry: EntryView; authorBanned: boolean; moderation: Moderation }) {
  const { t } = useTranslation();
  const busy = moderation.busy;
  return (
    <Stack direction="row" spacing={1} useFlexGap flexWrap="wrap" alignItems="center">
      {entry.status === "pending" && (
        <>
          <Button size="small" variant="contained" disabled={busy} onClick={() => moderation.approve(entry)}>
            {t("queue.approve")}
          </Button>
          <Button size="small" variant="outlined" disabled={busy} onClick={() => moderation.edit(entry)}>
            {t("queue.edit")}
          </Button>
          <Button size="small" variant="outlined" disabled={busy} onClick={() => moderation.reject(entry)}>
            {t("queue.reject")}
          </Button>
          <Button size="small" variant="outlined" disabled={busy} onClick={() => moderation.hide(entry)}>
            {t("queue.hide")}
          </Button>
        </>
      )}
      {entry.status === "hidden" && (
        <Button size="small" variant="contained" disabled={busy} onClick={() => moderation.restore(entry)}>
          {t("sets.restore")}
        </Button>
      )}
      {entry.status === "rejected" && (
        <Button size="small" variant="contained" disabled={busy} onClick={() => moderation.approve(entry)}>
          {t("sets.approveAfterAll")}
        </Button>
      )}
      {entry.status === "active" && (
        <Button size="small" variant="outlined" disabled={busy} onClick={() => moderation.hide(entry)}>
          {t("queue.hide")}
        </Button>
      )}
      {(entry.status === "active" || entry.status === "hidden") && (
        <Button size="small" variant="outlined" disabled={busy} onClick={() => moderation.editText(entry)}>
          {t("editText.button")}
        </Button>
      )}
      {entry.open_reports > 0 && (
        <>
          <Button size="small" variant="outlined" disabled={busy} onClick={() => moderation.reports(entry, "dismiss")}>
            {t("reports.version.dismiss", { count: entry.open_reports })}
          </Button>
          <Button size="small" variant="outlined" disabled={busy} onClick={() => moderation.reports(entry, "resolve")}>
            {t("reports.version.resolve", { count: entry.open_reports })}
          </Button>
        </>
      )}
      {!authorBanned && (
        <Button size="small" disabled={busy} onClick={() => moderation.ban(entry.uploader_hmac, entry.author)} sx={{ ...quiet, ml: "auto" }}>
          {t("queue.banUploader")}
        </Button>
      )}
    </Stack>
  );
}

const breakable = (domain: string) =>
  domain.split(".").map((part, i, parts) => (
    <Fragment key={`${String(i)}-${part}`}>
      {part}
      {i < parts.length - 1 && (
        <>
          .<wbr />
        </>
      )}
    </Fragment>
  ));

function Votes({ votes }: { votes: VoteView[] }) {
  const { t } = useTranslation();
  if (votes.length === 0) return <EmptyState text={t("detail.noVotes")} />;
  return (
    <Box sx={{ overflowX: "auto" }}>
      <Table size="small" sx={{ "& .MuiTableCell-root": { px: 1, "&:first-of-type": { pl: 0 }, "&:last-of-type": { pr: 0 } } }}>
        <TableHead>
          <TableRow>
            <TableCell>{t("detail.voteColumns.when")}</TableCell>
            <TableCell>{t("detail.voteColumns.kind")}</TableCell>
            <TableCell>{t("detail.voteColumns.version")}</TableCell>
            <TableCell>{t("detail.voteColumns.origin")}</TableCell>
            <TableCell>{t("detail.voteColumns.key")}</TableCell>
            <TableCell>{t("detail.voteColumns.domain")}</TableCell>
            <TableCell>{t("detail.voteColumns.b4")}</TableCell>
          </TableRow>
        </TableHead>
        <TableBody>
          {votes.map((v) => (
            <TableRow key={v.id}>
              <TableCell sx={{ whiteSpace: "nowrap" }}>
                <span title={formatStamp(v.received_at)}>{formatAgo(t, v.received_at)}</span>
              </TableCell>
              <TableCell sx={{ whiteSpace: "nowrap" }}>
                <Box sx={{ display: "flex", alignItems: "baseline", gap: 0.75 }}>
                  <Box component="span" aria-hidden sx={{ width: 8, height: 8, borderRadius: "50%", flexShrink: 0, bgcolor: statusToneColor[v.weight >= 0 ? "success" : "error"] }} />
                  {t(`feedback.kinds.${v.kind}`, { defaultValue: v.kind })}
                </Box>
              </TableCell>
              <TableCell>{v.version}</TableCell>
              <TableCell>
                {v.asn_observed && (
                  <Box component="span" sx={{ whiteSpace: "nowrap" }}>
                    AS{v.asn_observed} {v.country_observed ?? ""}
                  </Box>
                )}{" "}
                <Typography component="span" variant="caption" sx={{ color: colors.text.disabled, whiteSpace: "nowrap" }}>
                  {v.origin_verified ? t("detail.verified") : t("detail.unverified")}
                </Typography>
              </TableCell>
              <TableCell sx={{ whiteSpace: "nowrap" }}>
                <KeyRef hmac={v.key_hmac} label={v.key} dot={false} />
              </TableCell>
              <TableCell sx={{ overflowWrap: "break-word", minWidth: 96 }}>{v.domain ? breakable(v.domain) : ""}</TableCell>
              <TableCell sx={{ whiteSpace: "nowrap" }}>{v.b4_version ?? ""}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </Box>
  );
}

function SetHeaderState({ data, moderation }: { data: SetDetailView; moderation: Moderation }) {
  const { t } = useTranslation();
  const title = data.versions[data.versions.length - 1]?.title ?? data.id;
  const withdrawn = data.withdrawn_at !== undefined;
  const listed = data.listed_version !== undefined;
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
      {withdrawn && (
        <Alert
          severity="warning"
          action={
            <Button size="small" color="inherit" disabled={moderation.busy} onClick={() => moderation.reinstate(data.id, title)}>
              {t("sets.reinstate")}
            </Button>
          }
        >
          {t("sets.withdrawnBanner", { when: formatStamp(data.withdrawn_at) })}
          {data.withdraw_reason ? ` ${t("sets.withdrawnReason", { reason: data.withdraw_reason })}` : ""}
        </Alert>
      )}
      {data.author_banned && <Alert severity="error">{t("sets.authorBannedBanner")}</Alert>}
      {!withdrawn && !data.author_banned && (
        <Box sx={{ display: "flex", alignItems: "center", flexWrap: "wrap", columnGap: 1, rowGap: 0.5, minHeight: 30 }}>
          <Box component="span" aria-hidden sx={{ width: 8, height: 8, borderRadius: "50%", flexShrink: 0, bgcolor: statusToneColor[listed ? "success" : "neutral"] }} />
          <Typography variant="body2" sx={{ flex: 1, minWidth: 0 }}>
            {listed ? t("sets.listedBanner", { version: data.listed_version }) : t("sets.notListedBanner")}
          </Typography>
          {listed && (
            <Button size="small" disabled={moderation.busy} onClick={() => moderation.withdraw(data.id, title)} sx={quiet}>
              {t("sets.withdraw")}
            </Button>
          )}
        </Box>
      )}
    </Box>
  );
}

function History({ id }: Readonly<{ id: string }>) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const history = useTargetHistory("set", id);
  if (!history.data) return history.error ? <ErrorState error={history.error} onRetry={() => void history.refetch()} /> : <Loading />;
  return (
    <>
      <HistoryList entries={history.data.items} />
      {history.data.next ? (
        <Link component="button" type="button" variant="body2" underline="hover" sx={{ alignSelf: "flex-start", color: colors.text.primary }} onClick={() => void navigate(`/audit?kind=set&q=${encodeURIComponent(id)}`)}>
          {t("audit.fullLog")}
        </Link>
      ) : null}
    </>
  );
}

function Detail({ data, focusVersion, onClose, moderation }: { data: SetDetailView; focusVersion?: number; onClose: () => void; moderation: Moderation }) {
  const { t } = useTranslation();
  const focused = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    focused.current?.scrollIntoView({ block: "start" });
  }, [data.id, focusVersion]);
  return (
    <>
      <SetHeaderState data={data} moderation={moderation} />
      <Typography variant="sectionHeader">{t("detail.versions")}</Typography>
      {[...data.versions].reverse().map((v) => (
        <Box
          key={v.version}
          ref={v.version === focusVersion ? focused : undefined}
          sx={{
            border: `1px solid ${v.version === focusVersion ? colors.secondary : colors.border.light}`,
            borderRadius: 1,
            p: 2,
            display: "flex",
            flexDirection: "column",
            gap: 1.5,
            bgcolor: colors.background.paper,
          }}
        >
          <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
            <VersionPill version={{ version: v.version }} />
            <StatusDot tone={statusTone(v.status)} label={t(`status.set.${v.status}`)} />
            {v.version === data.listed_version && <Chip size="small" color="secondary" label={t("detail.inCatalogue")} />}
            {v.hidden_from === "pending" && <Chip size="small" variant="outlined" label={t("detail.hiddenFromPending")} />}
            <Typography variant="caption" sx={{ color: colors.text.secondary, ml: "auto" }}>
              {formatStamp(v.updated_at)}
            </Typography>
          </Box>
          <Typography sx={{ fontSize: 16, fontWeight: 600, overflowWrap: "anywhere" }}>{v.title}</Typography>
          <Origin entry={v} />
          <EditedNotice entry={v} />
          {v.status_reason && (
            <Typography variant="body2" sx={{ color: colors.text.secondary, overflowWrap: "anywhere" }}>
              {t("sets.reasonLine", { reason: reasonText(t, v.status_reason, v.independent_reports) })}
            </Typography>
          )}
          <EntryFacts entry={v} />
          <Divider sx={{ borderColor: colors.border.light }} />
          <VersionActions entry={v} authorBanned={data.author_banned === true} moderation={moderation} />
        </Box>
      ))}
      <Typography variant="sectionHeader">{t("detail.votes", { count: data.votes.length })}</Typography>
      <Votes votes={data.votes} />
      <Typography variant="sectionHeader">{t("audit.history")}</Typography>
      <History id={data.id} />
      <Divider sx={{ borderColor: colors.border.light }} />
      <Box sx={{ display: "flex", alignItems: "center", gap: 2, flexWrap: "wrap" }}>
        <Typography variant="body2" sx={{ color: colors.text.secondary, flex: 1, minWidth: 200 }}>
          {t("detail.deleteHint")}
        </Typography>
        <Button size="small" variant="outlined" color="error" disabled={moderation.busy} onClick={() => moderation.remove(data.id, onClose)}>
          {t("detail.delete")}
        </Button>
      </Box>
    </>
  );
}

export function SetDetailDrawer({ id, open, focusVersion, onClose, moderation }: SetDetailDrawerProps) {
  const { t } = useTranslation();
  const detail = useSetDetail(id);
  const data = detail.data;
  const title = data?.versions[data.versions.length - 1]?.title;

  return (
    <Drawer
      anchor="right"
      open={open && id !== null}
      onClose={onClose}
      slotProps={{ paper: { sx: { width: { xs: "100%", md: 880 }, maxWidth: "100%", bgcolor: colors.background.default } } }}
    >
      <Box sx={{ p: 3, display: "flex", flexDirection: "column", gap: 2.5, minWidth: 0 }}>
        <Box sx={{ display: "flex", alignItems: "flex-start", gap: 2 }}>
          <Box sx={{ flex: 1, minWidth: 0 }}>
            <Typography variant="metricLabel">{t("detail.heading")}</Typography>
            <Typography sx={{ fontSize: 20, fontWeight: 600, mt: 0.5, overflowWrap: "anywhere" }}>{title ?? id}</Typography>
            <Typography variant="monoSmall" sx={{ display: "block", mt: 0.5, color: colors.text.secondary, overflowWrap: "anywhere" }}>
              {id}
            </Typography>
            {data && (
              <Box sx={{ display: "flex", alignItems: "center", flexWrap: "wrap", columnGap: 1, rowGap: 0.25, mt: 0.75 }}>
                <Typography variant="caption" sx={{ color: colors.text.secondary }}>
                  {t("entry.author")}
                </Typography>
                <KeyRef hmac={data.author_hmac} label={data.author} banned={data.author_banned} dot={false} />
                {data.derived_from_id && (
                  <Typography variant="caption" sx={{ color: colors.text.secondary, overflowWrap: "anywhere" }}>
                    · {t("detail.derived", { id: data.derived_from_id, version: data.derived_from_version ?? 0 })}
                  </Typography>
                )}
              </Box>
            )}
          </Box>
          <IconButton onClick={onClose} aria-label={t("app.close")}>
            <CloseIcon />
          </IconButton>
        </Box>
        {id !== null && <QueryView query={detail}>{(d) => <Detail data={d} focusVersion={focusVersion} onClose={onClose} moderation={moderation} />}</QueryView>}
      </Box>
    </Drawer>
  );
}
