import {
  Box,
  Button,
  Chip,
  Divider,
  Drawer,
  IconButton,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Typography,
} from "@mui/material";
import CloseIcon from "@mui/icons-material/Close";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import { useSetDetail } from "@/api/hub";
import type { EntryView, VoteView } from "@/models/api";
import { ErrorState, Loading, EmptyState } from "@/components/common/States";
import { StatusChip } from "@/components/common/StatusChip";
import { Mono } from "@/components/common/Mono";
import { formatStamp, setRef } from "@/utils/format";
import { EntryFacts, Origin } from "./EntryFacts";
import type { Moderation } from "./useModeration";

interface SetDetailDrawerProps {
  id: string | null;
  onClose: () => void;
  moderation: Moderation;
}

function VersionActions({ entry, moderation }: { entry: EntryView; moderation: Moderation }) {
  const { t } = useTranslation();
  return (
    <Stack direction="row" spacing={1} useFlexGap flexWrap="wrap">
      {entry.status === "pending" && (
        <>
          <Button size="small" variant="contained" color="success" disabled={moderation.busy} onClick={() => moderation.approve(entry)}>
            {t("queue.approve")}
          </Button>
          <Button size="small" variant="outlined" color="error" disabled={moderation.busy} onClick={() => moderation.reject(entry)}>
            {t("queue.reject")}
          </Button>
        </>
      )}
      {entry.status === "active" && (
        <Button size="small" variant="outlined" color="inherit" disabled={moderation.busy} onClick={() => moderation.hide(entry)}>
          {t("queue.hide")}
        </Button>
      )}
      {entry.status === "hidden" && (
        <Button size="small" variant="outlined" color="success" disabled={moderation.busy} onClick={() => moderation.approve(entry)}>
          {t("sets.restore")}
        </Button>
      )}
      {entry.status === "rejected" && (
        <Button size="small" variant="outlined" color="success" disabled={moderation.busy} onClick={() => moderation.approve(entry)}>
          {t("sets.approveAfterAll")}
        </Button>
      )}
      <Button size="small" variant="text" color="error" disabled={moderation.busy} onClick={() => moderation.ban(entry.uploader_hmac, entry.author)}>
        {t("queue.banUploader")}
      </Button>
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
              <TableCell><Mono title={v.key_hmac}>{v.key}</Mono></TableCell>
              <TableCell>{v.domain ?? ""}</TableCell>
              <TableCell>{v.b4_version ?? ""}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </Box>
  );
}

export function SetDetailDrawer({ id, onClose, moderation }: SetDetailDrawerProps) {
  const { t } = useTranslation();
  const detail = useSetDetail(id);
  const data = detail.data;

  return (
    <Drawer
      anchor="right"
      open={id !== null}
      onClose={onClose}
      slotProps={{ paper: { sx: { width: { xs: "100%", md: 760 }, maxWidth: "100%", bgcolor: colors.background.default } } }}
    >
      <Box sx={{ p: 3, display: "flex", flexDirection: "column", gap: 2.5, minWidth: 0 }}>
        <Box sx={{ display: "flex", alignItems: "flex-start", gap: 2 }}>
          <Box sx={{ flex: 1, minWidth: 0 }}>
            <Typography variant="metricLabel">{t("detail.title", { id: "" })}</Typography>
            <Typography variant="monoSmall" sx={{ display: "block", fontSize: 14, mt: 0.5, overflowWrap: "anywhere" }}>
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

        {detail.isLoading && <Loading />}
        {detail.error && <ErrorState error={detail.error} />}
        {data && (
          <>
            <Typography variant="sectionHeader">{t("detail.versions")}</Typography>
            {[...data.versions].reverse().map((v) => (
              <Box
                key={v.version}
                sx={{ border: `1px solid ${colors.border.light}`, borderRadius: 1, p: 2, display: "flex", flexDirection: "column", gap: 1.5, bgcolor: colors.background.paper }}
              >
                <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
                  <Typography sx={{ fontWeight: 600 }}>{setRef(v.set_id, v.version)}</Typography>
                  <StatusChip status={v.status} />
                  {v.version === data.current_version && v.status === "active" && (
                    <Chip size="small" color="secondary" label={t("detail.current")} />
                  )}
                  <Typography variant="caption" sx={{ color: colors.text.secondary, ml: "auto" }}>
                    {formatStamp(v.updated_at)}
                  </Typography>
                </Box>
                <Typography sx={{ fontSize: 16, overflowWrap: "anywhere" }}>{v.title}</Typography>
                <Origin entry={v} />
                {v.status_reason && (
                  <Typography variant="body2" sx={{ color: colors.state.warning }}>
                    {v.status_reason}
                  </Typography>
                )}
                <EntryFacts entry={v} />
                <Divider sx={{ borderColor: colors.border.light }} />
                <VersionActions entry={v} moderation={moderation} />
              </Box>
            ))}
            <Typography variant="sectionHeader">{t("detail.votes", { count: data.votes.length })}</Typography>
            <Votes votes={data.votes} />
          </>
        )}
      </Box>
    </Drawer>
  );
}
