import { useState } from "react";
import {
  Box,
  Button,
  CircularProgress,
  Divider,
  Stack,
  Typography,
} from "@mui/material";
import { Link } from "react-router";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import {
  B4Alert,
  B4Dialog,
  B4FieldMessage,
  B4Help,
  B4TextField,
} from "@b4.elements";
import {
  CopyIcon,
  DownloadIcon,
  PublishIcon,
  ShareIcon,
  WarningIcon,
} from "@b4.icons";
import { useSnackbar } from "@context/SnackbarProvider";
import { colors } from "@design";
import { ApiError } from "@api/apiClient";
import { hubApi } from "@api/hub";
import {
  useHubGroupShare,
  useHubGroups,
  useHubSet,
  useHubShare,
} from "@hooks/useHub";
import { B4SetConfig } from "@models/config";
import {
  HubEnvelopeResponse,
  HubGroupShareResponse,
  HubShareResponse,
  formatWarningParam,
} from "@models/hub";
import { hubGroupDescriptionOf, hubGroupMemberOf } from "@models/hubGroups";
import { setFileName } from "@models/setGroups";
import { copyText, describeHubError, describeHubGroupError } from "@utils";

const MAX_DESCRIPTION_CHARS = 2000;

export type ShareTarget =
  | { set: B4SetConfig }
  | { group: string; sets: B4SetConfig[] };

interface Outcome {
  severity: "success" | "warning" | "error";
  message: string;
  link?: string;
  setId?: string;
}

const hubLink = (id: string) => `/hub?set=${encodeURIComponent(id)}`;

const groupShareOutcome = (t: TFunction, e: unknown): Outcome => {
  const body = e instanceof ApiError ? e.body : undefined;
  if (e instanceof ApiError && e.code === "duplicate_group") {
    const dup = (body ?? {}) as { group_id?: string; version?: number };
    return {
      severity: "warning",
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
    message: t("sets.groups.publish.failed", {
      error: describeHubGroupError(e, t),
    }),
  };
};

const setShareOutcome = (t: TFunction, e: unknown): Outcome => {
  if (e instanceof ApiError && e.code === "duplicate_strategy") {
    const dup = (e.body ?? {}) as { hub_id?: string; version?: number };
    return {
      severity: "warning",
      message: t("sets.share.publishDuplicate", {
        id: dup.hub_id ?? "",
        version: dup.version ?? 0,
      }),
      link: dup.hub_id ? hubLink(dup.hub_id) : undefined,
    };
  }
  if (e instanceof ApiError && e.code === "hub_unreachable") {
    return { severity: "error", message: t("sets.share.publishUnreachable") };
  }
  return {
    severity: "error",
    message: t("sets.share.publishFailed", { error: describeHubError(e, t) }),
  };
};

const SharedSetFile = ({ set }: { set: B4SetConfig }) => {
  const { t } = useTranslation();
  const { showSuccess, showError } = useSnackbar();
  const [result, setResult] = useState<HubEnvelopeResponse | null>(null);
  const [busy, setBusy] = useState(false);
  const json = result ? JSON.stringify(result.envelope) : "";
  const stripped = result?.report.stripped ?? [];
  const warnings = result?.report.warnings ?? [];
  const payloads = result?.envelope.payloads ?? [];

  const prepare = async () => {
    setBusy(true);
    try {
      setResult(await hubApi.buildEnvelope(set));
    } catch {
      showError(t("sets.share.prepareFailed"));
    } finally {
      setBusy(false);
    }
  };

  const copy = async () => {
    const ok = await copyText(json);
    if (ok) showSuccess(t("sets.importExport.copiedToClipboard"));
    else showError(t("sets.importExport.copyFailed"));
  };

  const download = () => {
    const url = URL.createObjectURL(
      new Blob([json], { type: "application/json" }),
    );
    const a = document.createElement("a");
    a.href = url;
    a.download = setFileName(set.name);
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <Stack spacing={1.5}>
      <Box sx={{ display: "flex", alignItems: "center", gap: 0.5 }}>
        <Typography variant="subtitle2">{t("sets.share.jsonLabel")}</Typography>
        <B4Help
          label={t("sets.share.jsonLabel")}
          info={t("sets.share.info")}
          learnMore="sets/sharing"
        />
      </Box>
      <Typography variant="body2" sx={{ color: colors.text.secondary }}>
        {t("sets.share.intro")}
      </Typography>
      {!result && (
        <Box>
          <Button
            variant="outlined"
            startIcon={<ShareIcon />}
            onClick={() => void prepare()}
            disabled={busy}
          >
            {t("sets.share.prepare")}
          </Button>
        </Box>
      )}
      {result && (
        <>
          {warnings.length > 0 && (
            <B4Alert severity="warning" icon={<WarningIcon />}>
              <Typography variant="subtitle2">
                {t("sets.share.warningsTitle")}
              </Typography>
              <ul style={{ margin: "4px 0 0", paddingLeft: 20 }}>
                {warnings.map((w, i) => (
                  <li key={`${w.code}-${i}`}>
                    {t(`sets.share.warnings.${w.code}`, {
                      defaultValue: w.code,
                      ...Object.fromEntries(
                        Object.entries(w.params ?? {}).map(([k, v]) => [
                          k,
                          formatWarningParam(v),
                        ]),
                      ),
                    })}
                  </li>
                ))}
              </ul>
            </B4Alert>
          )}
          {stripped.length > 0 && (
            <Box>
              <Typography variant="subtitle2">
                {t("sets.share.strippedTitle")}
              </Typography>
              <ul style={{ margin: "4px 0 0", paddingLeft: 20 }}>
                {stripped.map((s) => (
                  <li key={s.path}>
                    <Typography variant="body2" component="span">
                      {t(`sets.share.stripped.${s.reason}`, {
                        defaultValue: s.path,
                        path: s.path,
                      })}
                    </Typography>
                  </li>
                ))}
              </ul>
            </Box>
          )}
          <Typography variant="body2" color="text.secondary">
            {t("sets.share.minVersion", {
              version: result.envelope.min_b4_version,
            })}
            {payloads.length > 0 &&
              ` ${t("sets.share.payloadsAttached", { count: payloads.length })}`}
          </Typography>
          <B4TextField
            label={t("sets.share.jsonLabel")}
            value={json}
            selectOnFocus
            multiline
            rows={6}
            slotProps={{ input: { readOnly: true } }}
          />
          <Stack direction="row" spacing={1} useFlexGap flexWrap="wrap">
            <Button
              variant="outlined"
              startIcon={<CopyIcon />}
              onClick={() => void copy()}
            >
              {t("sets.share.copy")}
            </Button>
            <Button
              variant="outlined"
              startIcon={<DownloadIcon />}
              onClick={download}
            >
              {t("sets.share.download")}
            </Button>
            <Button onClick={() => void prepare()} disabled={busy}>
              {t("sets.share.refresh")}
            </Button>
          </Stack>
        </>
      )}
    </Stack>
  );
};

interface ShareDialogProps {
  target: ShareTarget;
  hubReady: boolean;
  onPublished: () => void;
  onClose: () => void;
}

export const ShareDialog = ({
  target,
  hubReady,
  onPublished,
  onClose,
}: ShareDialogProps) => {
  const { t } = useTranslation();
  const set = "set" in target ? target.set : null;
  const group = "group" in target ? target : null;
  const shareSet = useHubShare();
  const shareGroup = useHubGroupShare();
  const linkedId = set?.hub?.id ?? "";
  const linkedUnmodified = Boolean(linkedId) && set?.hub_state === "unmodified";
  const linkedModified = Boolean(linkedId) && set?.hub_state === "modified";
  const hubSet = useHubSet(hubReady && linkedId ? linkedId : null);
  const hubGroups = useHubGroups(hubReady && group !== null);
  const [typed, setTyped] = useState<string | null>(null);
  const description =
    typed ??
    (group
      ? hubGroupDescriptionOf(hubGroups.data?.groups, group.group)
      : (hubSet.data?.description ?? ""));
  const [setResult, setSetResult] = useState<HubShareResponse | null>(null);
  const [groupResult, setGroupResult] = useState<HubGroupShareResponse | null>(
    null,
  );
  const [outcome, setOutcome] = useState<Outcome | null>(null);
  const pending = shareSet.isPending || shareGroup.isPending;
  const published = Boolean(setResult || groupResult);
  const canPublish = hubReady && !published && !(set && linkedUnmodified);

  const publish = async () => {
    setOutcome(null);
    const text = description.trim() || undefined;
    if (group) {
      try {
        const res = await shareGroup.mutateAsync({
          group: group.group,
          description: text,
        });
        setGroupResult(res);
        setOutcome({
          severity: "success",
          message: t("sets.groups.publish.done", {
            id: res.group_id,
            version: res.version,
            status: t(`hub.status.set.${res.status}`, {
              defaultValue: res.status,
            }),
          }),
        });
        onPublished();
      } catch (e) {
        setOutcome(groupShareOutcome(t, e));
      }
      return;
    }
    if (!set) return;
    try {
      const res = await shareSet.mutateAsync({
        setId: set.id,
        description: text,
      });
      setSetResult(res);
      setOutcome({
        severity: "success",
        message: t("sets.share.publishedDetail", {
          id: res.hub_id,
          version: res.version,
          status: t(`hub.status.set.${res.status}`, {
            defaultValue: res.status,
          }),
        }),
        link: hubLink(res.hub_id),
      });
      onPublished();
    } catch (e) {
      setOutcome(setShareOutcome(t, e));
    }
  };

  const memberResult = (setId: string): string => {
    const member = groupResult?.members.find((m) => m.set_id === setId);
    if (!member) return "";
    return t(`sets.groups.publish.result.${member.result}`, {
      id: member.hub_id,
      version: member.version,
    });
  };

  let intro = "";
  if (group) {
    intro = t("sets.groups.publish.intro", {
      count: group.sets.length,
      group: group.group,
    });
  } else if (linkedModified) {
    intro = t("sets.share.publishNewVersionHint", { id: linkedId });
  } else if (!linkedUnmodified) {
    intro = t("sets.share.publishHint");
  }

  return (
    <B4Dialog
      open
      title={t("sets.share.sectionTitle")}
      subtitle={set ? set.name : (group?.group ?? "")}
      icon={<ShareIcon />}
      onClose={onClose}
      maxWidth="sm"
      fullWidth
      actions={
        <>
          <Button onClick={onClose}>
            {published ? t("core.close") : t("core.cancel")}
          </Button>
          <Box sx={{ flex: 1 }} />
          {canPublish && (
            <Button
              variant="contained"
              startIcon={
                pending ? (
                  <CircularProgress size={16} color="inherit" />
                ) : (
                  <PublishIcon />
                )
              }
              disabled={pending}
              onClick={() => void publish()}
            >
              {linkedModified
                ? t("sets.share.publishNewVersion")
                : t("sets.share.publish")}
            </Button>
          )}
        </>
      }
    >
      <Stack spacing={2} sx={{ mt: 2 }}>
        {hubReady && set && linkedUnmodified && !published && (
          <B4FieldMessage severity="info">
            {t("sets.share.linkedUnmodified", {
              id: linkedId,
              version: set.hub?.version ?? 0,
            })}{" "}
            <Link to={hubLink(linkedId)}>{t("sets.share.openHub")}</Link>
          </B4FieldMessage>
        )}
        {hubReady && intro && (
          <Typography variant="body2" sx={{ color: colors.text.secondary }}>
            {intro}
          </Typography>
        )}
        {canPublish && (
          <B4TextField
            label={t("sets.groups.publish.description")}
            helperText={
              group
                ? t("sets.groups.publish.descriptionHelp")
                : t("sets.share.descriptionHelp")
            }
            value={description}
            onChange={(e) => setTyped(e.target.value)}
            disabled={pending}
            multiline
            rows={3}
            slotProps={{ htmlInput: { maxLength: MAX_DESCRIPTION_CHARS } }}
          />
        )}
        {hubReady && group && (
          <Stack spacing={0.75}>
            {group.sets.map((member, position) => {
              const refused = outcome?.setId === member.id;
              const result = memberResult(member.id);
              return (
                <Box
                  key={member.id}
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
                    {`${position + 1}. ${member.name}`}
                  </Typography>
                  {result && (
                    <Typography
                      variant="body2"
                      sx={{
                        color: colors.text.secondary,
                        overflowWrap: "anywhere",
                      }}
                    >
                      {result}
                    </Typography>
                  )}
                </Box>
              );
            })}
          </Stack>
        )}
        {outcome && (
          <B4Alert severity={outcome.severity}>
            {outcome.message}
            {outcome.link && (
              <>
                {" "}
                <Link to={outcome.link}>{t("sets.share.openHub")}</Link>
              </>
            )}
          </B4Alert>
        )}
        {set && (
          <>
            {hubReady && <Divider />}
            <SharedSetFile set={set} />
          </>
        )}
      </Stack>
    </B4Dialog>
  );
};
