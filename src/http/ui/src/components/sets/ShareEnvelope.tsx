import { useEffect, useState } from "react";
import { Box, Button, Stack, Typography } from "@mui/material";
import { B4Alert, B4Section, B4TextField } from "@b4.elements";
import { CopyIcon, DownloadIcon, ShareIcon, WarningIcon } from "@b4.icons";
import { useSnackbar } from "@context/SnackbarProvider";
import { useTranslation } from "react-i18next";
import { B4SetConfig } from "@models/config";
import { HubEnvelopeResponse, formatWarningParam } from "@models/hub";
import { hubApi } from "@api/hub";
import { copyText } from "@utils";

interface ShareEnvelopeProps {
  config: B4SetConfig;
}

function fileNameFor(name: string): string {
  const safe = name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return `${safe || "set"}.b4set.json`;
}

export const ShareEnvelope = ({ config }: ShareEnvelopeProps) => {
  const { t } = useTranslation();
  const { showSuccess, showError } = useSnackbar();
  const [result, setResult] = useState<HubEnvelopeResponse | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    setResult(null);
  }, [config]);

  const json = result ? JSON.stringify(result.envelope) : "";

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

  const stripped = result?.report.stripped ?? [];
  const warnings = result?.report.warnings ?? [];
  const payloads = result?.envelope.payloads ?? [];

  return (
    <B4Section title={t("sets.share.sectionTitle")} icon={<ShareIcon />}>
      <B4Alert severity="info" sx={{ mb: 2 }}>
        {t("sets.share.info")}
      </B4Alert>
      <Stack spacing={2}>
        {!result && (
          <Box>
            <Button
              variant="contained"
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
