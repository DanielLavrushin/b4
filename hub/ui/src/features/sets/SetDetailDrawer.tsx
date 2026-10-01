import { useEffect, useRef } from "react";
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
import { StatusChip } from "@/shared/components/StatusChip";
import { Mono } from "@/shared/components/Mono";
import { formatStamp, setRef } from "@/shared/utils/format";
import { reasonText } from "@/shared/utils/reason";
import { EditedNotice } from "@/features/sets/EditedNotice";
import { EntryFacts, Origin } from "@/features/sets/EntryFacts";
import type { Moderation } from "@/features/moderation/useModeration";

interface SetDetailDrawerProps {
  id: string | null;
  open: boolean;
  focusVersion?: number;
  onClose: () => void;
  moderation: Moderation;
}

function VersionActions({ entry, authorBanned, moderation }: { entry: EntryView; authorBanned: boolean; moderation: Moderation }) {
  const { t } = useTranslation();
  return (
    <Stack direction="row" spacing={1} useFlexGap flexWrap="wrap">
      {entry.status === "pending" && (
        <>
          <Button size="small" variant="contained" color="success" disabled={moderation.busy} onClick={() => moderation.approve(entry)}>
            {t("queue.approve")}
          </Button>
          <Button size="small" variant="outlined" color="primary" disabled={moderation.busy} onClick={() => moderation.edit(entry)}>
            {t("queue.edit")}
          </Button>
          <Button size="small" variant="outlined" color="error" disabled={moderation.busy} onClick={() => moderation.reject(entry)}>
            {t("queue.reject")}
          </Button>
          <Button size="small" variant="outlined" color="inherit" disabled={moderation.busy} onClick={() => moderation.hide(entry)}>
            {t("queue.hide")}
          </Button>
        </>
      )}
      {entry.status === "active" && (
        <Button size="small" variant="outlined" color="inherit" disabled={moderation.busy} onClick={() => moderation.hide(entry)}>
          {t("queue.hide")}
        </Button>
      )}
      {(entry.status === "active" || entry.status === "hidden") && (
        <Button size="small" variant="outlined" color="primary" disabled={moderation.busy} onClick={() => moderation.editText(entry)}>
          {t("editText.button")}
        </Button>
      )}
      {entry.status === "hidden" && (
        <Button size="small" variant="outlined" color="success" disabled={moderation.busy} onClick={() => moderation.restore(entry)}>
          {t("sets.restore")}
        </Button>
      )}
      {entry.status === "rejected" && (
        <Button size="small" variant="outlined" color="success" disabled={moderation.busy} onClick={() => moderation.approve(entry)}>
          {t("sets.approveAfterAll")}
        </Button>
      )}
      {entry.open_reports > 0 && (
        <>
          <Button size="small" variant="outlined" color="warning" disabled={moderation.busy} onClick={() => moderation.reports(entry, "dismiss")}>
            {t("reports.version.dismiss", { count: entry.open_reports })}
          </Button>
          <Button size="small" variant="text" color="warning" disabled={moderation.busy} onClick={() => moderation.reports(entry, "resolve")}>
            {t("reports.version.resolve", { count: entry.open_reports })}
          </Button>
        </>
      )}
      {!authorBanned && (
        <Button size="small" variant="text" color="error" disabled={moderation.busy} onClick={() => moderation.ban(entry.uploader_hmac, entry.author)}>
          {t("queue.banUploader")}
        </Button>
      )}
    </Stack>
  );
}

function Votes({ votes }: { votes: VoteView[] }) {
  const { t } = useTranslation();
  if (votes.length === 0) return <EmptyState text={t("detail.noVotes")} />;
  return (
    <Box sx={{ overflowX: "auto" }}>
      <Table size="small">
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
              <TableCell sx={{ whiteSpace: "nowrap" }}>{formatStamp(v.received_at)}</TableCell>
              <TableCell>
                <Chip size="small" variant="outlined" color={v.weight >= 0 ? "success" : "error"} label={t(`feedback.kinds.${v.kind}`, { defaultValue: v.kind })} />
              </TableCell>
              <TableCell>{v.version}</TableCell>
              <TableCell sx={{ whiteSpace: "nowrap" }}>
                {v.asn_observed ? `AS${v.asn_observed} ${v.country_observed ?? ""}` : ""}
                <Typography component="span" variant="caption" sx={{ color: colors.text.disabled, ml: 0.5 }}>
                  {v.origin_verified ? t("detail.verified") : t("detail.unverified")}
                </Typography>
              </TableCell>
              <TableCell>
                <Mono title={v.key_hmac}>{v.key}</Mono>
              </TableCell>
              <TableCell>{v.domain ?? ""}</TableCell>
              <TableCell>{v.b4_version ?? ""}</TableCell>
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
      {data.author_banned && (
        <Alert severity="error" variant="outlined">
          {t("sets.authorBannedBanner")}
        </Alert>
      )}
      {!withdrawn && !data.author_banned && (
        <Alert
          severity={data.listed_version ? "success" : "info"}
          variant="outlined"
          action={
            data.listed_version ? (
              <Button size="small" color="inherit" disabled={moderation.busy} onClick={() => moderation.withdraw(data.id, title)}>
                {t("sets.withdraw")}
              </Button>
            ) : undefined
          }
        >
          {data.listed_version ? t("sets.listedBanner", { version: data.listed_version }) : t("sets.notListedBanner")}
        </Alert>
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
        <Link component="button" type="button" variant="body2" underline="hover" sx={{ alignSelf: "flex-start" }} onClick={() => void navigate(`/audit?kind=set&q=${encodeURIComponent(id)}`)}>
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
            border: `1px solid ${v.version === focusVersion ? colors.primary : colors.border.light}`,
            borderRadius: 1,
            p: 2,
            display: "flex",
            flexDirection: "column",
            gap: 1.5,
            bgcolor: colors.background.paper,
          }}
        >
          <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
            <Typography sx={{ fontWeight: 600 }}>{setRef(v.set_id, v.version)}</Typography>
            <StatusChip status={v.status} label={t(`status.set.${v.status}`)} />
            {v.version === data.listed_version && <Chip size="small" color="secondary" label={t("detail.inCatalogue")} />}
            {v.hidden_from === "pending" && <Chip size="small" variant="outlined" label={t("detail.hiddenFromPending")} />}
            <Typography variant="caption" sx={{ color: colors.text.secondary, ml: "auto" }}>
              {formatStamp(v.updated_at)}
            </Typography>
          </Box>
          <Typography sx={{ fontSize: 16, overflowWrap: "anywhere" }}>{v.title}</Typography>
          <Origin entry={v} />
          <EditedNotice entry={v} />
          {v.status_reason && (
            <Typography variant="body2" sx={{ color: colors.state.warning }}>
              {reasonText(t, v.status_reason, v.independent_reports)}
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
      slotProps={{ paper: { sx: { width: { xs: "100%", md: 760 }, maxWidth: "100%", bgcolor: colors.background.default } } }}
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
              <Typography variant="caption" sx={{ color: colors.text.secondary, display: "block", mt: 0.5 }}>
                {t("detail.author", { author: data.author })}
                {data.derived_from_id ? ` · ${t("detail.derived", { id: data.derived_from_id, version: data.derived_from_version ?? 0 })}` : ""}
              </Typography>
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
