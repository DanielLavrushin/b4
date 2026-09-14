import { useEffect, useState } from "react";
import {
  Box,
  Button,
  CircularProgress,
  Stack,
  Tooltip,
  Typography,
} from "@mui/material";
import { Link } from "react-router";
import { B4Alert, B4Section, B4TextField } from "@b4.elements";
import {
  CopyIcon,
  DownloadIcon,
  PublishIcon,
  ShareIcon,
  WarningIcon,
} from "@b4.icons";
import { useSnackbar } from "@context/SnackbarProvider";
import { useTranslation } from "react-i18next";
import { B4SetConfig } from "@models/config";
import {
  HubEnvelopeResponse,
  HubShareResponse,
  formatWarningParam,
} from "@models/hub";
import { hubApi } from "@api/hub";
import { ApiError } from "@api/apiClient";
import { useHubShare, useHubStatus } from "@hooks/useHub";
import { copyText, describeHubError } from "@utils";

interface ShareEnvelopeProps {
  config: B4SetConfig;
  isNew?: boolean;
  dirty?: boolean;
}

interface DuplicateAnswer {
  hub_id: string;
  version: number;
}

function fileNameFor(name: string): string {
  const safe = name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return `${safe || "set"}.b4set.json`;
}

const hubLink = (id: string) => `/hub?set=${encodeURIComponent(id)}`;

export const ShareEnvelope = ({
  config,
  isNew = false,
  dirty = false,
}: ShareEnvelopeProps) => {
  const { t } = useTranslation();
  const { showSuccess, showError } = useSnackbar();
  const [result, setResult] = useState<HubEnvelopeResponse | null>(null);
  const [busy, setBusy] = useState(false);
  const [published, setPublished] = useState<HubShareResponse | null>(null);
  const [duplicate, setDuplicate] = useState<DuplicateAnswer | null>(null);
  const hubStatus = useHubStatus();
  const share = useHubShare();

  useEffect(() => {
    setResult(null);
  }, [config]);

  useEffect(() => {
    setPublished(null);
    setDuplicate(null);
  }, [config.id]);

  const json = result ? JSON.stringify(result.envelope) : "";
  const hubReady = Boolean(
    hubStatus.data?.enabled && hubStatus.data?.configured,
  );

  const prepare = async () => {
    setBusy(true);
    try {
      setResult(await hubApi.buildEnvelope(config));
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
    const blob = new Blob([json], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = fileNameFor(config.name);
    a.click();
    URL.revokeObjectURL(url);
  };

  const publish = async () => {
    setPublished(null);
    setDuplicate(null);
    try {
      const res = await share.mutateAsync({ setId: config.id });
      setPublished(res);
      showSuccess(t("sets.share.published", { id: res.hub_id, version: res.version }));
    } catch (e) {
      if (e instanceof ApiError && e.code === "duplicate_strategy") {
        const body = (e.body ?? {}) as Partial<DuplicateAnswer>;
        setDuplicate({
          hub_id: body.hub_id ?? "",
          version: body.version ?? 0,
        });
        return;
      }
      if (e instanceof ApiError && e.code === "hub_unreachable") {
        showError(t("sets.share.publishUnreachable"));
        return;
      }
      showError(t("sets.share.publishFailed", { error: describeHubError(e, t) }));
    }
  };

  const stripped = result?.report.stripped ?? [];
  const warnings = result?.report.warnings ?? [];
  const payloads = result?.envelope.payloads ?? [];

  let publishBlocked = "";
  if (isNew) publishBlocked = t("sets.share.publishSaveFirst");
  else if (dirty) publishBlocked = t("sets.share.publishUnsaved");

  const linkedId = config.hub?.id ?? "";
  const linkedUnmodified = Boolean(linkedId) && config.hub_state === "unmodified";
  const linkedModified = Boolean(linkedId) && config.hub_state === "modified";

  return (
    <B4Section title={t("sets.share.sectionTitle")} icon={<ShareIcon />}>
      <B4Alert severity="info" sx={{ mb: 2 }}>
        {t("sets.share.info")}
      </B4Alert>
      <Stack spacing={2}>
        {hubReady && linkedUnmodified && !published && (
          <B4Alert severity="info">
            {t("sets.share.linkedUnmodified", {
              id: linkedId,
              version: config.hub?.version ?? 0,
            })}{" "}
            <Link to={hubLink(linkedId)}>{t("sets.share.openHub")}</Link>
          </B4Alert>
        )}
        {hubReady && (!linkedUnmodified || published) && (
          <Box>
            {!published && (
              <Stack direction="row" spacing={2} alignItems="center" flexWrap="wrap" useFlexGap>
                <Tooltip title={publishBlocked}>
                  <span>
                    <Button
                      variant="contained"
                      startIcon={
                        share.isPending ? (
                          <CircularProgress size={16} color="inherit" />
                        ) : (
                          <PublishIcon />
                        )
                      }
                      onClick={() => void publish()}
                      disabled={share.isPending || Boolean(publishBlocked)}
                    >
                      {linkedModified
                        ? t("sets.share.publishNewVersion")
                        : t("sets.share.publish")}
                    </Button>
                  </span>
                </Tooltip>
                <Typography variant="caption" sx={{ color: "text.secondary" }}>
                  {linkedModified
                    ? t("sets.share.publishNewVersionHint", { id: linkedId })
                    : t("sets.share.publishHint")}
                </Typography>
              </Stack>
            )}
            {published && (
              <B4Alert severity="success" sx={{ mt: 2 }}>
                {t("sets.share.publishedDetail", {
                  id: published.hub_id,
                  version: published.version,
                  status: t(`hub.status.set.${published.status}`, {
                    defaultValue: published.status,
                  }),
                })}{" "}
                <Link to={hubLink(published.hub_id)}>
                  {t("sets.share.openHub")}
                </Link>
              </B4Alert>
            )}
            {duplicate && (
              <B4Alert severity="warning" sx={{ mt: 2 }}>
                {t("sets.share.publishDuplicate", {
                  id: duplicate.hub_id,
                  version: duplicate.version,
                })}{" "}
                {duplicate.hub_id && (
                  <Link to={hubLink(duplicate.hub_id)}>
                    {t("sets.share.openHub")}
                  </Link>
                )}
              </B4Alert>
            )}
          </Box>
        )}
        {!result && (
          <Box>
            <Button
              variant={hubReady ? "outlined" : "contained"}
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
              rows={8}
              slotProps={{ input: { readOnly: true } }}
            />
            <Stack direction="row" spacing={2} alignItems="center">
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
              <Button variant="text" onClick={() => void prepare()} disabled={busy}>
                {t("sets.share.refresh")}
              </Button>
            </Stack>
          </>
        )}
      </Stack>
    </B4Section>
  );
};
