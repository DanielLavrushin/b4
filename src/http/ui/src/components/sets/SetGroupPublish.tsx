import { useState } from "react";
import { Box, Button, CircularProgress, Stack, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { B4Alert, B4Dialog, B4TextField } from "@b4.elements";
import { PublishIcon } from "@b4.icons";
import { colors } from "@design";
import { ApiError } from "@api/apiClient";
import { useHubGroups, useHubGroupShare, useHubStatus } from "@hooks/useHub";
import { B4SetConfig } from "@models/config";
import { HubGroupShareResponse } from "@models/hub";
import { hubGroupDescriptionOf, hubGroupMemberOf } from "@models/hubGroups";
import { describeHubError, describeHubGroupError } from "@utils";

const MAX_DESCRIPTION_CHARS = 2000;

interface PublishFailure {
  severity: "error" | "warning";
  message: string;
  setId: string;
}

const publishFailure = (t: TFunction, e: unknown): PublishFailure => {
  const body = e instanceof ApiError ? e.body : undefined;
  if (e instanceof ApiError && e.code === "duplicate_group") {
    const dup = (body ?? {}) as { group_id?: string; version?: number };
    return {
      severity: "warning",
      setId: "",
      message: t("sets.groups.publish.duplicate", {
        id: dup.group_id ?? "",
        version: dup.version ?? 0,
      }),
    };
  }
  const ref = hubGroupMemberOf(body);
  if (ref) {
    return {
      severity: "error",
      setId: ref.setId,
      message: t("sets.groups.publish.memberFailed", {
        name: ref.name,
        error: describeHubError(e, t),
      }),
    };
  }
  return {
    severity: "error",
    setId: "",
    message: t("sets.groups.publish.failed", {
      error: describeHubGroupError(e, t),
    }),
  };
};

interface SetGroupPublishDialogProps {
  group: string;
  sets: B4SetConfig[];
  onPublished: () => void;
  onClose: () => void;
}

export const SetGroupPublishDialog = ({
  group,
  sets,
  onPublished,
  onClose,
}: SetGroupPublishDialogProps) => {
  const { t } = useTranslation();
  const share = useHubGroupShare();
  const hubStatus = useHubStatus();
  const hubGroups = useHubGroups(
    Boolean(hubStatus.data?.enabled && hubStatus.data?.configured),
  );
  const [typed, setTyped] = useState<string | null>(null);
  const description =
    typed ?? hubGroupDescriptionOf(hubGroups.data?.groups, group);
  const [published, setPublished] = useState<HubGroupShareResponse | null>(
    null,
  );
  const [failure, setFailure] = useState<PublishFailure | null>(null);

  const publish = async () => {
    setFailure(null);
    try {
      const res = await share.mutateAsync({
        group,
        description: description.trim() || undefined,
      });
      setPublished(res);
      onPublished();
    } catch (e) {
      setFailure(publishFailure(t, e));
    }
  };

  const resultOf = (setId: string): string => {
    const member = published?.members.find((m) => m.set_id === setId);
    if (!member) return "";
    return t(`sets.groups.publish.result.${member.result}`, {
      id: member.hub_id,
      version: member.version,
    });
  };

  return (
    <B4Dialog
      open
      title={t("sets.groups.publish.title")}
      subtitle={group}
      icon={<PublishIcon />}
      onClose={onClose}
      maxWidth="sm"
      fullWidth
      actions={
        <>
          <Button onClick={onClose}>
            {published ? t("core.close") : t("core.cancel")}
          </Button>
          <Box sx={{ flex: 1 }} />
          {!published && (
            <Button
              variant="contained"
              startIcon={
                share.isPending ? (
                  <CircularProgress size={16} color="inherit" />
                ) : (
                  <PublishIcon />
                )
              }
              disabled={share.isPending}
              onClick={() => void publish()}
            >
              {t("sets.groups.publish.confirm")}
            </Button>
          )}
        </>
      }
    >
      <Stack spacing={2} sx={{ mt: 2 }}>
        <Typography variant="body2" sx={{ color: colors.text.secondary }}>
          {t("sets.groups.publish.intro", { count: sets.length, group })}
        </Typography>
        <B4TextField
          label={t("sets.groups.publish.description")}
          helperText={t("sets.groups.publish.descriptionHelp")}
          value={description}
          onChange={(e) => setTyped(e.target.value)}
          disabled={share.isPending || Boolean(published)}
          multiline
          rows={3}
          slotProps={{ htmlInput: { maxLength: MAX_DESCRIPTION_CHARS } }}
        />
        <Stack spacing={0.75}>
          {sets.map((set, position) => {
            const refused = failure?.setId === set.id;
            const result = resultOf(set.id);
            return (
              <Box
                key={set.id}
                sx={{
                  p: 1.25,
                  border: `1px solid ${refused ? colors.state.error : colors.border.light}`,
                  borderRadius: 1.5,
                  bgcolor: colors.background.dark,
                }}
              >
                <Typography
                  variant="subtitle2"
                  sx={{ color: colors.text.primary, overflowWrap: "anywhere" }}
                >
                  {`${position + 1}. ${set.name}`}
                </Typography>
                {result && (
                  <Typography
                    variant="body2"
                    sx={{ color: colors.text.secondary, overflowWrap: "anywhere" }}
                  >
                    {result}
                  </Typography>
                )}
              </Box>
            );
          })}
        </Stack>
        {failure && <B4Alert severity={failure.severity}>{failure.message}</B4Alert>}
        {published && (
          <B4Alert severity="success">
            {t("sets.groups.publish.done", {
              id: published.group_id,
              version: published.version,
              status: t(`hub.status.set.${published.status}`, {
                defaultValue: published.status,
              }),
            })}
          </B4Alert>
        )}
      </Stack>
    </B4Dialog>
  );
};
