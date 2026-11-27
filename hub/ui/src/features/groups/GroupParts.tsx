import { Button, Paper, Stack, Typography } from "@mui/material";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import type { GroupMemberView, GroupVersionView } from "@/models/api";
import type { Moderation } from "@/features/moderation/useModeration";
import { memberTitle } from "@/features/moderation/GroupActionDialog";
import { ScoreSummary } from "@/features/sets/components/ScoreSummary";
import { SetRef } from "@/shared/components/SetRef";
import { formatStamp } from "@/shared/utils/format";

export const groupOrigin = (t: TFunction, group: GroupVersionView): string[] => {
  const lines = [t("queue.received", { when: formatStamp(group.created_at) })];
  if (group.asn_observed) lines.push(t("entry.seenFrom", { asn: group.asn_observed, country: group.country_observed ?? "" }).trim());
  if (group.asn_hint) lines.push(t("entry.claims", { asn: group.asn_hint, country: group.country_hint ?? "" }).trim());
  return lines;
};

export function GroupActions({ group, moderation }: Readonly<{ group: GroupVersionView; moderation: Moderation }>) {
  const { t } = useTranslation();
  const busy = moderation.busy;
  const run = (action: "approve" | "reject" | "hide" | "restore") => () => moderation.moderateGroup(action, group);
  return (
    <Stack direction="row" spacing={1} useFlexGap flexWrap="wrap" alignItems="center">
      {group.status === "pending" && (
        <>
          <Button size="small" variant="contained" disabled={busy} onClick={run("approve")}>
            {t("groups.approve")}
          </Button>
          <Button size="small" variant="outlined" disabled={busy} onClick={run("reject")}>
            {t("groups.reject")}
          </Button>
          <Button size="small" variant="outlined" disabled={busy} onClick={run("hide")}>
            {t("groups.hide")}
          </Button>
        </>
      )}
      {group.status === "hidden" && (
        <Button size="small" variant="contained" disabled={busy} onClick={run("restore")}>
          {t("groups.restore")}
        </Button>
      )}
      {group.status === "rejected" && (
        <Button size="small" variant="contained" disabled={busy} onClick={run("approve")}>
          {t("sets.approveAfterAll")}
        </Button>
      )}
      {group.status === "active" && (
        <Button size="small" variant="outlined" disabled={busy} onClick={run("hide")}>
          {t("groups.hide")}
        </Button>
      )}
    </Stack>
  );
}

const memberKind = (t: TFunction, member: GroupMemberView): string => {
  if (member.brought) return t("groups.member.brought");
  if (member.brought_in) return t("groups.member.broughtIn", { version: member.brought_in });
  return t("groups.member.linked");
};

export function MemberCaption({ group, member }: Readonly<{ group: GroupVersionView; member: GroupMemberView }>) {
  const { t } = useTranslation();
  const escalate = member.escalate;
  const kind = member.missing ? t("ref.deleted") : memberKind(t, member);
  const listed = member.listed_version ? t("groups.member.listed", { version: member.listed_version }) : t("groups.member.notListed");
  return (
    <Typography variant="caption" sx={{ display: "block", color: colors.text.secondary, mb: 0.5, overflowWrap: "anywhere" }}>
      <strong>{t("groups.position", { position: member.position + 1 })}</strong> {kind}
      {!member.missing && (
        <>
          {" · "}
          <span style={!member.entry && !member.listed_version ? { color: colors.state.warning } : undefined}>{listed}</span>
        </>
      )}
      {escalate && ` · ${t("groups.escalates", { position: escalate.to + 1, title: memberTitle(group, escalate.to) })}`}
    </Typography>
  );
}

export function MemberRow({ group, member }: Readonly<{ group: GroupVersionView; member: GroupMemberView }>) {
  const { t } = useTranslation();
  const row = member.row;
  const title = member.entry?.title ?? row?.title;
  const status = member.entry?.status ?? row?.status;
  return (
    <Paper variant="outlined" sx={{ p: 1.5, minWidth: 0, bgcolor: colors.background.paper }}>
      <MemberCaption group={group} member={member} />
      {!member.missing && (
        <Stack direction="row" spacing={1} useFlexGap flexWrap="wrap" alignItems="center" justifyContent="space-between" sx={{ minWidth: 0 }}>
          <SetRef id={member.set_id} version={member.version} title={title} status={status} />
          {row && <ScoreSummary published={row.published} live={row.live} evidence={row.evidence} />}
        </Stack>
      )}
      {row && (
        <Typography variant="caption" sx={{ display: "block", color: colors.text.secondary, mt: 0.5 }}>
          {t("card.by", { author: row.author })}
        </Typography>
      )}
    </Paper>
  );
}
