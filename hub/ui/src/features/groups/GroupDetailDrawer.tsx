import { useEffect, useRef } from "react";
import { Alert, Box, Chip, Divider, Drawer, Grid, IconButton, Typography } from "@mui/material";
import CloseIcon from "@mui/icons-material/Close";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import { useGroupDetail } from "@/features/sets/api";
import { useTargetHistory } from "@/features/audit/api";
import { HistoryList } from "@/features/audit/HistoryList";
import type { Moderation } from "@/features/moderation/useModeration";
import { VersionPill } from "@/features/sets/card/SetCard";
import { ScoreSummary } from "@/features/sets/components/ScoreSummary";
import type { GroupDetailView } from "@/models/api";
import { KeyRef } from "@/shared/components/KeyRef";
import { QueryView } from "@/shared/components/QueryView";
import { ErrorState, Loading } from "@/shared/components/States";
import { statusTone } from "@/shared/components/StatusChip";
import { StatusDot } from "@/shared/components/StatusDot";
import { formatStamp } from "@/shared/utils/format";
import { reasonText } from "@/shared/utils/reason";
import { GroupActions, MemberRow, groupOrigin } from "./GroupParts";

interface GroupDetailDrawerProps {
  id: string | null;
  open: boolean;
  focusVersion?: number;
  onClose: () => void;
  moderation: Moderation;
}

function History({ id }: Readonly<{ id: string }>) {
  const history = useTargetHistory("group", id);
  if (!history.data) return history.error ? <ErrorState error={history.error} onRetry={() => void history.refetch()} /> : <Loading />;
  return <HistoryList entries={history.data.items} />;
}

function Detail({ data, focusVersion, moderation }: Readonly<{ data: GroupDetailView; focusVersion?: number; moderation: Moderation }>) {
  const { t } = useTranslation();
  const focused = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    focused.current?.scrollIntoView({ block: "start" });
  }, [data.id, focusVersion]);
  return (
    <>
      {data.author_banned && <Alert severity="error">{t("groups.authorBanned")}</Alert>}
      <Box sx={{ display: "flex", alignItems: "center", gap: 2, flexWrap: "wrap" }}>
        <StatusDot
          tone={data.published_version ? "success" : "neutral"}
          label={data.published_version ? t("groups.listedBanner", { version: data.published_version }) : t("groups.notListedBanner")}
        />
        {data.published_version ? <ScoreSummary published={data.published} live={data.live} evidence={data.evidence} group /> : null}
      </Box>
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
            {v.version === data.published_version && <Chip size="small" color="secondary" label={t("detail.inCatalogue")} />}
            {v.hidden_from === "pending" && <Chip size="small" variant="outlined" label={t("detail.hiddenFromPending")} />}
            <Typography variant="caption" sx={{ color: colors.text.secondary, ml: "auto" }}>
              {formatStamp(v.updated_at)}
            </Typography>
          </Box>
          <Typography sx={{ fontSize: 16, fontWeight: 600, overflowWrap: "anywhere" }}>
            {t("groups.heading", { title: v.title, count: v.members.length })}
          </Typography>
          {v.description && (
            <Typography variant="body2" sx={{ color: colors.text.secondary, overflowWrap: "anywhere" }}>
              {v.description}
            </Typography>
          )}
          <Typography variant="caption" sx={{ color: colors.text.secondary, display: "block" }}>
            {groupOrigin(t, v).join(" · ")}
          </Typography>
          {v.status_reason && (
            <Typography variant="body2" sx={{ color: colors.text.secondary, overflowWrap: "anywhere" }}>
              {t("sets.reasonLine", { reason: reasonText(t, v.status_reason) })}
            </Typography>
          )}
          <Grid container spacing={1}>
            {v.members.map((m) => (
              <Grid key={m.position} size={{ xs: 12, sm: 6 }}>
                <MemberRow group={v} member={m} />
              </Grid>
            ))}
          </Grid>
          <Divider sx={{ borderColor: colors.border.light }} />
          <GroupActions group={v} moderation={moderation} />
        </Box>
      ))}
      <Typography variant="sectionHeader">{t("audit.history")}</Typography>
      <History id={data.id} />
    </>
  );
}

export function GroupDetailDrawer({ id, open, focusVersion, onClose, moderation }: Readonly<GroupDetailDrawerProps>) {
  const { t } = useTranslation();
  const detail = useGroupDetail(id);
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
            <Typography variant="metricLabel">{t("groups.label")}</Typography>
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
                    · {t("groups.derived", { id: data.derived_from_id, version: data.derived_from_version ?? 0 })}
                  </Typography>
                )}
              </Box>
            )}
          </Box>
          <IconButton onClick={onClose} aria-label={t("app.close")}>
            <CloseIcon />
          </IconButton>
        </Box>
        {id !== null && <QueryView query={detail}>{(d) => <Detail data={d} focusVersion={focusVersion} moderation={moderation} />}</QueryView>}
      </Box>
    </Drawer>
  );
}
