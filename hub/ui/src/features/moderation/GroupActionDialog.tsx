import { useEffect, useState } from "react";
import {
  Alert,
  Box,
  Button,
  Checkbox,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  FormControlLabel,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  TextField,
  Typography,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors } from "@design";
import type { GroupActionRequest, GroupEffectView, GroupMemberEffectView, GroupVersionView, SetAction } from "@/models/api";
import { useSnackbar } from "@/app/SnackbarProvider";
import { ErrorState } from "@/shared/components/States";
import { setRef } from "@/shared/utils/format";
import { useGroupAction, useGroupPreview } from "./api";
import { EffectCell } from "./BulkDialog";
import { catalogueEffect } from "./EffectText";
import { ReasonPicker } from "./ReasonPicker";

interface GroupActionDialogProps {
  action: SetAction;
  group: GroupVersionView | null;
  onClose: () => void;
}

const reasonMode: Record<SetAction, "required" | "optional" | "none"> = {
  approve: "none",
  reject: "required",
  hide: "optional",
  restore: "none",
};

const forceable = new Set(["author_banned", "set_withdrawn"]);

export const memberTitle = (group: GroupVersionView, position: number): string => {
  const member = group.members.find((m) => m.position === position);
  return member?.entry?.title ?? member?.row?.title ?? "";
};

function GroupEffect({ effect }: Readonly<{ effect: GroupEffectView }>) {
  const { t } = useTranslation();
  if (!effect.ok) {
    return <Alert severity="error">{t(`errors.${effect.code ?? "invalid"}`, { ...(effect.params ?? {}), defaultValue: effect.code })}</Alert>;
  }
  const text = catalogueEffect(effect, "moderation.groupEffect");
  const leaves = effect.listed > 0 && effect.listed_after === 0;
  const unlisted = effect.listed_after === 0 && effect.to === "active";
  return (
    <Alert severity={leaves ? "warning" : "info"}>
      <Stack spacing={0.5}>
        <Typography variant="body2" sx={{ fontWeight: 600 }}>
          {t(text.key, text.params)}
        </Typography>
        {effect.withheld && effect.listed_after === 0 && (
          <Typography variant="body2" sx={{ color: colors.text.secondary }}>
            {t(`moderation.groupEffect.withheld.${effect.withheld}`)}
          </Typography>
        )}
        {unlisted && !effect.withheld && (
          <Typography variant="body2" sx={{ color: colors.text.secondary }}>
            {t("moderation.groupEffect.notListed")}
          </Typography>
        )}
      </Stack>
    </Alert>
  );
}

function MemberChange({ member }: Readonly<{ member: GroupMemberEffectView }>) {
  const { t } = useTranslation();
  if (member.missing) return <Box sx={{ color: colors.text.disabled }}>{t("ref.deleted")}</Box>;
  if (!member.ok) return <EffectCell item={member} />;
  if (member.code) return <Box sx={{ color: colors.text.secondary }}>{t(`moderation.group.${member.code}`, { ...(member.params ?? {}), defaultValue: member.code })}</Box>;
  if (!member.to) return <Box sx={{ color: colors.text.secondary }}>{t(member.brought ? "moderation.group.untouched" : "moderation.group.linked")}</Box>;
  return (
    <>
      <Box sx={{ fontWeight: 600 }}>{t("moderation.group.becomes", { status: t(`status.set.${member.to}`) })}</Box>
      <EffectCell item={member} />
    </>
  );
}

export function GroupActionDialog({ action, group, onClose }: Readonly<GroupActionDialogProps>) {
  const { t } = useTranslation();
  const { notifyResult, notifyError } = useSnackbar();
  const mutation = useGroupAction();
  const [reason, setReason] = useState("");
  const [force, setForce] = useState(false);
  const [keepReports, setKeepReports] = useState(false);

  useEffect(() => {
    setReason("");
    setForce(false);
    setKeepReports(false);
  }, [group, action]);

  const target = group ? { group_id: group.group_id, version: group.version, action } : null;
  const previewBody: GroupActionRequest = { reason: action === "reject" ? "preview" : "", force, keep_reports: keepReports };
  const preview = useGroupPreview(target, previewBody);
  const effect = preview.data?.group;
  const members = preview.data?.members ?? [];
  const mode = reasonMode[action];
  const needsReason = mode === "required" && reason.trim() === "";
  const refusals = [...(effect && !effect.ok ? [effect.code ?? ""] : []), ...members.filter((m) => !m.ok).map((m) => m.code ?? "")];
  const canForce = action === "approve" && refusals.length > 0 && refusals.every((code) => forceable.has(code));
  const withReports = action === "reject" && members.some((m) => m.ok && m.to && m.reports > 0);
  const blocked = mutation.isPending || needsReason || effect === undefined || refusals.length > 0;

  if (!group || !target) return null;

  const confirm = async () => {
    try {
      const result = await mutation.mutateAsync({
        target,
        body: {
          reason,
          expect_status: group.status,
          members: members
            .filter((m) => m.from)
            .map((m) => ({ set_id: m.set_id, version: m.version, expect_status: m.from, expect_kept: m.code === "kept_newer_group_version" })),
          force,
          keep_reports: keepReports,
        },
      });
      notifyResult(result);
      onClose();
    } catch (err) {
      notifyError(err);
    }
  };

  return (
    <Dialog open onClose={mutation.isPending ? undefined : onClose} fullWidth maxWidth="md">
      <DialogTitle>{t(`moderation.group.${action}.title`, { title: group.title })}</DialogTitle>
      <DialogContent sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
        <DialogContentText>{t(`moderation.group.${action}.text`)}</DialogContentText>
        {preview.isLoading && (
          <Box sx={{ display: "flex", justifyContent: "center" }}>
            <CircularProgress size={20} />
          </Box>
        )}
        {preview.error && <ErrorState error={preview.error} onRetry={() => void preview.refetch()} />}
        {effect && <GroupEffect effect={effect} />}
        {members.length > 0 && (
          <Box sx={{ maxHeight: 320, overflow: "auto", border: `1px solid ${colors.border.light}`, borderRadius: 1 }}>
            <Table size="small" stickyHeader>
              <TableHead>
                <TableRow>
                  <TableCell>{t("moderation.group.columns.set")}</TableCell>
                  <TableCell>{t("moderation.group.columns.change")}</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {members.map((m) => (
                  <TableRow key={m.position}>
                    <TableCell>
                      {t("groups.position", { position: m.position + 1 })} {memberTitle(group, m.position) || m.title}
                      <Box component="span" sx={{ color: colors.text.secondary, fontSize: 11, ml: 1 }}>
                        {setRef(m.set_id, m.version)}
                      </Box>
                    </TableCell>
                    <TableCell>
                      <MemberChange member={m} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </Box>
        )}
        {refusals.length > 0 && !canForce && <Alert severity="warning">{t("moderation.group.refused")}</Alert>}
        {canForce && (
          <FormControlLabel control={<Checkbox checked={force} onChange={(e) => setForce(e.target.checked)} />} label={t(`moderation.force.${refusals[0] ?? ""}`)} />
        )}
        {force && action === "approve" && (
          <FormControlLabel control={<Checkbox checked onChange={() => setForce(false)} />} label={t("moderation.force.active")} />
        )}
        {(withReports || keepReports) && (
          <FormControlLabel control={<Checkbox checked={keepReports} onChange={(e) => setKeepReports(e.target.checked)} />} label={t("moderation.keepReports")} />
        )}
        {(action === "reject" || action === "hide") && <ReasonPicker scope={action} value={reason} onPick={setReason} />}
        {mode !== "none" && (
          <TextField
            autoFocus
            fullWidth
            size="small"
            label={t("app.reason")}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            required={mode === "required"}
            helperText={needsReason ? t("app.reasonRequired") : t("moderation.reasonHint")}
          />
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={mutation.isPending} color="inherit">
          {t("app.cancel")}
        </Button>
        <Button
          autoFocus={mode === "none"}
          onClick={() => void confirm()}
          disabled={blocked}
          variant="contained"
          color={action === "approve" || action === "restore" ? "primary" : "error"}
        >
          {t(`moderation.group.${action}.confirm`)}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
