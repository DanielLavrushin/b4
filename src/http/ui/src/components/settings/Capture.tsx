import { useState, useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import {
  Grid,
  Stack,
  Typography,
  Button,
  Box,
  Paper,
  CircularProgress,
  Tooltip,
  IconButton,
} from "@mui/material";
import {
  CaptureIcon,
  ClearIcon,
  CoreIcon,
  DownloadIcon,
  RefreshIcon,
  SuccessIcon,
  UploadIcon,
  WarningIcon,
} from "@b4.icons";
import { useSnackbar } from "@context/SnackbarProvider";
import {
  B4Dialog,
  B4TextField,
  B4Section,
  B4Alert,
  B4Select,
} from "@b4.elements";
import {
  useBuiltinPayloads,
  useCaptures,
  Capture,
  PayloadInspection,
  PayloadProtocol,
} from "@b4.capture";
import { ApiError } from "@api/apiClient";
import { colors, radius } from "@design";
import { copyText, describeApiError } from "@utils";
import { useLastTarget } from "@hooks/useLastTarget";
import { bytesToHex } from "./payloads/hex";
import {
  asProtocol,
  builtinLabel,
  builtinSource,
  bytesLabel,
  captureWarnings,
  inspectionWarnings,
  kindLabel,
} from "./payloads/facts";
import { PayloadCard } from "./payloads/PayloadCard";
import { PayloadWarnings } from "./payloads/PayloadWarnings";

interface HexTarget {
  name: string;
  protocol: string;
  size: number;
  read: () => Promise<Uint8Array>;
}

interface UploadForm {
  domain: string;
  nameTyped: boolean;
  protocol: PayloadProtocol;
  file: File | null;
}

const emptyUploadForm: UploadForm = {
  domain: "",
  nameTyped: false,
  protocol: "tls",
  file: null,
};

function existsConflictSets(error: unknown): string[] | null {
  if (
    !(error instanceof ApiError) ||
    error.status !== 409 ||
    error.code !== "exists"
  ) {
    return null;
  }
  const body = error.body as { used_by?: unknown } | undefined;
  return Array.isArray(body?.used_by)
    ? body.used_by.filter((name): name is string => typeof name === "string")
    : [];
}

export const CaptureSettings = () => {
  const { t } = useTranslation();
  const { showError, showSuccess } = useSnackbar();
  const [probeForm, setProbeForm] = useState({ domain: "" });
  const [uploadForm, setUploadForm] = useState<UploadForm>(emptyUploadForm);
  const [inspection, setInspection] = useState<{
    file: File;
    result: PayloadInspection | null;
    error: string | null;
    refused?: boolean;
  } | null>(null);
  const [replaceDialog, setReplaceDialog] = useState<{
    domain: string;
    protocol: PayloadProtocol;
    sets: string[];
  } | null>(null);
  const [regenerateDialog, setRegenerateDialog] = useState<Capture | null>(
    null,
  );
  const [deleteDialog, setDeleteDialog] = useState<{
    capture: Capture;
    sets: string[];
  } | null>(null);
  const [clearDialog, setClearDialog] = useState<{ sets: string[] } | null>(
    null,
  );

  const {
    captures,
    loading,
    listError,
    uploading,
    generating,
    regenerating,
    loadCaptures,
    refreshCaptures,
    generate,
    regenerate,
    deleteCapture,
    clearAll,
    inspect,
    upload,
    readBytes,
    download,
  } = useCaptures();
  const {
    builtins,
    builtinsError,
    loadBuiltins,
    readBuiltin,
    downloadBuiltin,
  } = useBuiltinPayloads();

  useEffect(() => {
    loadCaptures().catch(() => {});
  }, [loadCaptures]);

  useEffect(() => {
    loadBuiltins().catch(() => {});
  }, [loadBuiltins]);

  const handleFileChange = async (file: File | null) => {
    if (!file) return;
    const fileName = file.name.replace(/\.[^.]+$/, "").toLowerCase();
    setUploadForm((prev) => ({
      ...prev,
      file,
      protocol: "tls",
      domain: prev.nameTyped ? prev.domain : fileName,
    }));
    setInspection({ file, result: null, error: null });
    try {
      const result = await inspect(file);
      setInspection((prev) =>
        prev?.file === file ? { file, result, error: null } : prev,
      );
      setUploadForm((prev) =>
        prev.file === file
          ? {
              ...prev,
              protocol: result.protocol || "tls",
              domain: prev.nameTyped
                ? prev.domain
                : (result.name || fileName).toLowerCase(),
            }
          : prev,
      );
    } catch (error) {
      setInspection((prev) =>
        prev?.file === file
          ? {
              file,
              result: null,
              error: describeApiError(error),
              refused: error instanceof ApiError && error.status === 400,
            }
          : prev,
      );
    }
  };

  const reportReload = (result: { reload_error?: string }) => {
    if (result.reload_error) {
      showError(
        t("settings.Capture.reloadFailed", { error: result.reload_error }),
      );
    }
  };

  const generateCapture = async () => {
    if (!probeForm.domain) return;

    const capturedDomain = probeForm.domain.toLowerCase().trim();

    try {
      const result = await generate(capturedDomain, "tls");

      if (result.already_captured) {
        showSuccess(
          t("settings.Capture.alreadyCaptured", { domain: result.domain }),
        );
      } else {
        showSuccess(
          t("settings.Capture.generateSuccess", {
            domain: result.domain,
            size: result.size,
          }),
        );
        setProbeForm({ domain: "" });
        reportReload(result);
      }
    } catch (error) {
      showError(
        t("settings.Capture.generateFailed", {
          error: describeApiError(error),
        }),
      );
    }
  };

  const handleDelete = async (capture: Capture) => {
    let latest: Capture[];
    try {
      latest = await refreshCaptures();
    } catch (error) {
      showError(
        t("settings.Capture.deleteFailed", { error: describeApiError(error) }),
      );
      return;
    }
    const current = latest.find(
      (c) => c.protocol === capture.protocol && c.domain === capture.domain,
    );
    const sets = current?.used_by ?? [];
    if (sets.length > 0) {
      setDeleteDialog({ capture, sets });
      return;
    }
    await deletePayload(capture);
  };

  const deletePayload = async (capture: Capture) => {
    try {
      const result = await deleteCapture(capture.protocol, capture.domain);
      showSuccess(
        t("settings.Capture.deleteSuccess", { domain: capture.domain }),
      );
      reportReload(result);
    } catch (error) {
      showError(
        t("settings.Capture.deleteFailed", { error: describeApiError(error) }),
      );
    }
  };

  const confirmDelete = () => {
    const pending = deleteDialog;
    setDeleteDialog(null);
    if (pending) void deletePayload(pending.capture);
  };

  const confirmRegenerate = async () => {
    const capture = regenerateDialog;
    if (!capture) return;
    try {
      const result = await regenerate(capture.domain);
      setRegenerateDialog(null);
      showSuccess(
        t("settings.Capture.regenerateSuccess", { domain: capture.domain }),
      );
      reportReload(result);
    } catch (error) {
      setRegenerateDialog(null);
      showError(
        t("settings.Capture.regenerateFailed", {
          error: describeApiError(error),
        }),
      );
    }
  };

  const handleClear = async () => {
    let latest: Capture[];
    try {
      latest = await refreshCaptures();
    } catch (error) {
      showError(
        t("settings.Capture.clearFailed", { error: describeApiError(error) }),
      );
      return;
    }
    if (latest.length === 0) return;
    setClearDialog({
      sets: [...new Set(latest.flatMap((c) => c.used_by ?? []))],
    });
  };

  const confirmClear = async () => {
    setClearDialog(null);
    try {
      const result = await clearAll();
      showSuccess(t("settings.Capture.allCleared"));
      reportReload(result);
    } catch (error) {
      showError(
        t("settings.Capture.clearFailed", { error: describeApiError(error) }),
      );
    }
  };

  const [hexDialog, setHexDialog] = useState<{
    target: HexTarget;
    hex: string | null;
    error: string | null;
  } | null>(null);
  const hexRequest = useRef(0);

  const shownDelete = useLastTarget(deleteDialog);
  const shownClear = useLastTarget(clearDialog);
  const shownReplace = useLastTarget(replaceDialog);
  const shownRegenerate = useLastTarget(regenerateDialog);
  const shownHex = useLastTarget(hexDialog);

  const openHex = async (target: HexTarget) => {
    const id = ++hexRequest.current;
    setHexDialog({ target, hex: null, error: null });
    try {
      const bytes = await target.read();
      if (hexRequest.current === id) {
        setHexDialog({ target, hex: bytesToHex(bytes), error: null });
      }
    } catch (error) {
      if (hexRequest.current === id) {
        setHexDialog({ target, hex: null, error: describeApiError(error) });
      }
    }
  };

  const closeHex = () => {
    hexRequest.current += 1;
    setHexDialog(null);
  };

  const sendUpload = async (
    domain: string,
    protocol: PayloadProtocol,
    replace: boolean,
  ) => {
    const file = uploadForm.file;
    if (!file) return;
    try {
      const result = await upload(file, domain, protocol, replace);
      showSuccess(t("settings.Capture.uploadedSuccess", { domain }));
      setUploadForm(emptyUploadForm);
      setInspection(null);
      reportReload(result);
    } catch (error) {
      const sets = replace ? null : existsConflictSets(error);
      if (sets) {
        setReplaceDialog({ domain, protocol, sets });
        return;
      }
      showError(
        t("settings.Capture.uploadFailed", { error: describeApiError(error) }),
      );
    }
  };

  const uploadCapture = async () => {
    const domain = uploadForm.domain.trim().toLowerCase();
    if (!uploadForm.file || !domain) return;
    const existing = captures.find(
      (c) => c.protocol === uploadForm.protocol && c.domain === domain,
    );
    if (existing) {
      setReplaceDialog({
        domain,
        protocol: uploadForm.protocol,
        sets: existing.used_by ?? [],
      });
      return;
    }
    await sendUpload(domain, uploadForm.protocol, false);
  };

  const confirmReplace = () => {
    const pending = replaceDialog;
    setReplaceDialog(null);
    if (pending) void sendUpload(pending.domain, pending.protocol, true);
  };

  const inspected = inspection?.result ?? null;
  const inspecting = !!inspection && !inspection.result && !inspection.error;
  const uploadRefusal = inspection?.refused
    ? t("settings.Capture.cannotUpload", { error: inspection.error })
    : inspected?.size === 0
      ? t("settings.Capture.cannotUploadEmpty")
      : null;
  const uploadWarnings =
    inspected && !uploadRefusal
      ? inspectionWarnings(inspected, uploadForm.protocol)
      : [];
  const foundParts = inspected
    ? [
        inspected.facts ? kindLabel(t, inspected.facts) : null,
        inspected.facts?.sni
          ? t("settings.Capture.sni", { sni: inspected.facts.sni })
          : null,
        bytesLabel(t, inspected.size),
      ].filter((part): part is string => !!part)
    : [];

  const handleDownload = async (save: () => Promise<void>) => {
    try {
      await save();
    } catch (error) {
      showError(
        t("settings.Capture.downloadFailed", { error: describeApiError(error) }),
      );
    }
  };

  const copyHex = async (hexData: string) => {
    const ok = await copyText(hexData);
    if (ok) showSuccess(t("settings.Capture.hexCopied"));
    else showError(t("settings.Capture.hexCopyFailed"));
  };

  return (
    <Stack spacing={3}>
      {/* Info */}
      <B4Alert icon={<CaptureIcon />}>
        <Typography variant="subtitle2" gutterBottom>
          {t("settings.Capture.alert")}
        </Typography>
        <Typography variant="caption" color="text.secondary">
          {t("settings.Capture.alertSub")}
        </Typography>
      </B4Alert>

      {listError && (
        <B4Alert severity="error">
          {t("settings.Capture.listFailed", { error: listError })}
        </B4Alert>
      )}

      <Grid container spacing={3}>
        <Grid size={{ xs: 12, md: 6 }}>
          <B4Section
            title={t("settings.Capture.uploadTitle")}
            description={t("settings.Capture.uploadDescription")}
            icon={<UploadIcon />}
          >
            <Stack spacing={2}>
              <Box sx={{ minWidth: 0 }}>
                <Button
                  component="label"
                  color="secondary"
                  variant="outlined"
                  disabled={uploading}
                  sx={{ maxWidth: "100%", minWidth: 0 }}
                >
                  <Box
                    component="span"
                    sx={{
                      overflow: "hidden",
                      textOverflow: "ellipsis",
                      whiteSpace: "nowrap",
                    }}
                  >
                    {uploadForm.file
                      ? uploadForm.file.name
                      : t("settings.Capture.chooseFile")}
                  </Box>
                  <input
                    type="file"
                    hidden
                    accept=".bin,application/octet-stream"
                    onChange={(e) => {
                      const file = e.target.files?.[0] || null;
                      e.target.value = "";
                      void handleFileChange(file);
                    }}
                  />
                </Button>
                {inspecting && (
                  <Typography
                    variant="caption"
                    color="text.secondary"
                    sx={{ display: "block", mt: 1 }}
                  >
                    {t("settings.Capture.inspecting")}
                  </Typography>
                )}
                {foundParts.length > 0 && (
                  <Typography
                    variant="body2"
                    color="text.secondary"
                    sx={{ mt: 1, overflowWrap: "anywhere" }}
                  >
                    {t("settings.Capture.found", {
                      facts: foundParts.join(" · "),
                    })}
                  </Typography>
                )}
              </Box>
              {uploadRefusal ? (
                <B4Alert severity="error" noWrapper>
                  {uploadRefusal}
                </B4Alert>
              ) : (
                inspection?.error && (
                  <B4Alert severity="warning" noWrapper>
                    {t("settings.Capture.inspectFailed", {
                      error: inspection.error,
                    })}
                  </B4Alert>
                )
              )}
              <B4TextField
                label={t("settings.Capture.nameDomain")}
                value={uploadForm.domain}
                onChange={(e: { target: { value: string } }) =>
                  setUploadForm({
                    ...uploadForm,
                    domain: e.target.value.toLowerCase(),
                    nameTyped: e.target.value !== "",
                  })
                }
                placeholder="youtube.com"
                helperText={
                  inspected?.name &&
                  uploadForm.domain === inspected.name.toLowerCase()
                    ? t("settings.Capture.nameFromSni")
                    : t("settings.Capture.nameDomainHelp")
                }
                disabled={uploading}
              />
              <B4Select
                label={t("settings.Capture.protocol")}
                value={uploadForm.protocol}
                options={[
                  { value: "tls", label: t("settings.Capture.protocolTls") },
                  { value: "quic", label: t("settings.Capture.protocolQuic") },
                ]}
                onChange={(e) =>
                  setUploadForm({
                    ...uploadForm,
                    protocol: asProtocol(String(e.target.value)),
                  })
                }
                helperText={
                  inspected?.protocol &&
                  inspected.protocol === uploadForm.protocol
                    ? t("settings.Capture.protocolDetected")
                    : t("settings.Capture.protocolHelp")
                }
                disabled={uploading}
              />
              <PayloadWarnings
                warnings={uploadWarnings}
                protocol={uploadForm.protocol}
                facts={inspected?.facts}
              />
              {!uploadRefusal && (
                <Box sx={{ display: "flex", justifyContent: "flex-end" }}>
                  <Button
                    variant="contained"
                    startIcon={
                      uploading ? (
                        <CircularProgress size={16} />
                      ) : (
                        <UploadIcon />
                      )
                    }
                    onClick={() => {
                      uploadCapture().catch(() => {});
                    }}
                    disabled={
                      uploading ||
                      inspecting ||
                      !uploadForm.file ||
                      !uploadForm.domain.trim()
                    }
                  >
                    {uploading ? t("core.uploading") : t("core.upload")}
                  </Button>
                </Box>
              )}
            </Stack>
          </B4Section>
        </Grid>

        <Grid size={{ xs: 12, md: 6 }}>
          <B4Section
            title={t("settings.Capture.generateTitle")}
            description={t("settings.Capture.generateDescription")}
            icon={<CaptureIcon />}
          >
            <Stack spacing={2}>
              <B4TextField
                label={t("settings.Capture.domain")}
                value={probeForm.domain}
                onChange={(e: { target: { value: string } }) =>
                  setProbeForm({ domain: e.target.value.toLowerCase() })
                }
                onKeyDown={(e: { key: string }) => {
                  if (e.key === "Enter" && !generating && probeForm.domain) {
                    void generateCapture();
                  }
                }}
                placeholder="max.ru"
                helperText={t("settings.Capture.domainHelp")}
                disabled={generating}
              />
              <Stack direction="row" spacing={1}>
                <Button
                  fullWidth
                  variant="contained"
                  startIcon={
                    generating ? (
                      <CircularProgress size={16} />
                    ) : (
                      <CaptureIcon />
                    )
                  }
                  onClick={() => void generateCapture()}
                  disabled={generating || !probeForm.domain}
                >
                  {generating
                    ? t("settings.Capture.generating")
                    : t("core.generate")}
                </Button>
                <Tooltip title={t("settings.Capture.refreshList")}>
                  <IconButton
                    onClick={() => {
                      loadCaptures().catch(() => {});
                    }}
                    disabled={loading}
                  >
                    <RefreshIcon />
                  </IconButton>
                </Tooltip>
                {captures.length > 0 && (
                  <Tooltip title={t("settings.Capture.clearAll")}>
                    <IconButton
                      onClick={() => void handleClear()}
                      color="error"
                      disabled={loading}
                    >
                      <ClearIcon />
                    </IconButton>
                  </Tooltip>
                )}
              </Stack>
            </Stack>
          </B4Section>
        </Grid>
      </Grid>

      {/* Generated Payloads - Flat grid like SetCards */}
      {captures.length > 0 && (
        <B4Section
          title={t("settings.Capture.generatedTitle")}
          description={t("settings.Capture.generatedDesc", {
            count: captures.length,
          })}
          icon={<DownloadIcon />}
        >
          <Grid container spacing={3}>
            {captures.map((capture) => (
              <Grid
                key={`${capture.protocol}:${capture.domain}`}
                size={{ xs: 12, sm: 6, lg: 4, xl: 3 }}
              >
                <PayloadCard
                  title={capture.domain}
                  protocol={asProtocol(capture.protocol)}
                  size={capture.size}
                  facts={capture.facts}
                  origin={capture.origin}
                  timestamp={capture.timestamp}
                  warnings={captureWarnings(capture)}
                  usedBy={capture.used_by ?? []}
                  onViewHex={
                    capture.missing
                      ? undefined
                      : () =>
                          void openHex({
                            name: capture.domain,
                            protocol: capture.protocol,
                            size: capture.size,
                            read: () => readBytes(capture),
                          })
                  }
                  onDownload={
                    capture.missing
                      ? undefined
                      : () => void handleDownload(() => download(capture))
                  }
                  onRegenerate={
                    capture.origin === "generated" && capture.protocol === "tls"
                      ? () => setRegenerateDialog(capture)
                      : undefined
                  }
                  onDelete={() => void handleDelete(capture)}
                />
              </Grid>
            ))}
          </Grid>
        </B4Section>
      )}

      {/* Empty State */}
      {captures.length === 0 && !loading && !listError && (
        <Paper
          elevation={0}
          sx={{
            p: 4,
            textAlign: "center",
            border: `1px dashed ${colors.border.default}`,
            borderRadius: radius.md,
          }}
        >
          <CaptureIcon
            sx={{ fontSize: 48, color: colors.text.secondary, mb: 2 }}
          />
          <Typography variant="h6" color="text.secondary">
            {t("settings.Capture.emptyTitle")}
          </Typography>
          <Typography variant="body2" color="text.secondary">
            {t("settings.Capture.emptyDesc")}
          </Typography>
        </Paper>
      )}

      <B4Section
        title={t("settings.Capture.builtinTitle")}
        description={t("settings.Capture.builtinDesc")}
        icon={<CoreIcon />}
      >
        {builtinsError ? (
          <B4Alert severity="error" noWrapper>
            {t("settings.Capture.builtinFailed", { error: builtinsError })}
          </B4Alert>
        ) : (
          <Grid container spacing={3}>
            {builtins.map((builtin) => (
              <Grid key={builtin.id} size={{ xs: 12, sm: 6, lg: 4, xl: 3 }}>
                <PayloadCard
                  title={builtinLabel(t, builtin)}
                  protocol={asProtocol(builtin.protocol)}
                  size={builtin.size}
                  facts={builtin.facts}
                  source={builtinSource(t, builtin)}
                  onViewHex={() =>
                    void openHex({
                      name: builtinLabel(t, builtin),
                      protocol: builtin.protocol,
                      size: builtin.size,
                      read: () => readBuiltin(builtin),
                    })
                  }
                  onDownload={() =>
                    void handleDownload(() => downloadBuiltin(builtin))
                  }
                />
              </Grid>
            ))}
          </Grid>
        )}
      </B4Section>

      <B4Dialog
        title={t("settings.Capture.deleteTitle", {
          domain: shownDelete?.capture.domain ?? "",
        })}
        icon={<WarningIcon />}
        open={deleteDialog !== null}
        onClose={() => setDeleteDialog(null)}
        maxWidth="sm"
        fullWidth
        actions={
          <>
            <Button onClick={() => setDeleteDialog(null)}>
              {t("core.cancel")}
            </Button>
            <Box sx={{ flex: 1 }} />
            <Button variant="contained" color="error" onClick={confirmDelete}>
              {t("core.delete")}
            </Button>
          </>
        }
      >
        {shownDelete && (
          <Typography variant="body2">
            {t("settings.Capture.deleteInUseConfirm", {
              sets: shownDelete.sets.join(", "),
            })}
          </Typography>
        )}
      </B4Dialog>

      <B4Dialog
        title={t("settings.Capture.clearTitle")}
        icon={<WarningIcon />}
        open={clearDialog !== null}
        onClose={() => setClearDialog(null)}
        maxWidth="sm"
        fullWidth
        actions={
          <>
            <Button onClick={() => setClearDialog(null)}>
              {t("core.cancel")}
            </Button>
            <Box sx={{ flex: 1 }} />
            <Button
              variant="contained"
              color="error"
              onClick={() => void confirmClear()}
            >
              {t("settings.Capture.clearAll")}
            </Button>
          </>
        }
      >
        {shownClear && (
          <Typography variant="body2">
            {shownClear.sets.length > 0
              ? t("settings.Capture.clearInUseConfirm", {
                  sets: shownClear.sets.join(", "),
                })
              : t("settings.Capture.clearConfirm")}
          </Typography>
        )}
      </B4Dialog>

      <B4Dialog
        title={t("settings.Capture.replaceTitle", {
          domain: shownReplace?.domain ?? "",
        })}
        icon={<UploadIcon />}
        open={replaceDialog !== null}
        onClose={() => setReplaceDialog(null)}
        maxWidth="sm"
        fullWidth
        actions={
          <>
            <Button onClick={() => setReplaceDialog(null)}>
              {t("core.cancel")}
            </Button>
            <Box sx={{ flex: 1 }} />
            <Button
              variant="contained"
              disabled={uploading}
              onClick={confirmReplace}
            >
              {t("settings.Capture.replace")}
            </Button>
          </>
        }
      >
        {shownReplace && (
          <Typography variant="body2">
            {shownReplace.sets.length > 0
              ? t("settings.Capture.replaceInUse", {
                  protocol: shownReplace.protocol.toUpperCase(),
                  domain: shownReplace.domain,
                  sets: shownReplace.sets.join(", "),
                })
              : t("settings.Capture.replaceBody", {
                  protocol: shownReplace.protocol.toUpperCase(),
                  domain: shownReplace.domain,
                })}
          </Typography>
        )}
      </B4Dialog>

      <B4Dialog
        title={t("settings.Capture.regenerateTitle", {
          domain: shownRegenerate?.domain ?? "",
        })}
        icon={<RefreshIcon />}
        open={regenerateDialog !== null}
        onClose={() => {
          if (!regenerating) setRegenerateDialog(null);
        }}
        maxWidth="sm"
        fullWidth
        actions={
          <>
            <Button
              onClick={() => setRegenerateDialog(null)}
              disabled={regenerating}
            >
              {t("core.cancel")}
            </Button>
            <Box sx={{ flex: 1 }} />
            <Button
              variant="contained"
              disabled={regenerating}
              startIcon={
                regenerating ? <CircularProgress size={16} /> : <RefreshIcon />
              }
              onClick={() => void confirmRegenerate()}
            >
              {t("settings.Capture.regenerate")}
            </Button>
          </>
        }
      >
        {shownRegenerate && (
          <Stack spacing={1.5}>
            <Typography variant="body2">
              {t("settings.Capture.regenerateBody", {
                domain: shownRegenerate.domain,
              })}
              {(shownRegenerate.warnings ?? []).includes("malformed") &&
                ` ${t("settings.Capture.regenerateFixes")}`}
            </Typography>
            {(shownRegenerate.used_by ?? []).length > 0 && (
              <Typography variant="body2">
                {t("settings.Capture.regenerateInUse", {
                  sets: (shownRegenerate.used_by ?? []).join(", "),
                })}
              </Typography>
            )}
          </Stack>
        )}
      </B4Dialog>

      <B4Dialog
        title={t("settings.Capture.hexTitle")}
        subtitle={t("settings.Capture.hexSubtitle")}
        icon={<CaptureIcon />}
        open={hexDialog !== null}
        onClose={closeHex}
        maxWidth="md"
        fullWidth
        actions={
          <>
            <Button onClick={closeHex}>{t("core.close")}</Button>
            <Box sx={{ flex: 1 }} />
            <Button
              variant="contained"
              disabled={!hexDialog?.hex}
              onClick={() => {
                if (hexDialog?.hex) void copyHex(hexDialog.hex);
                closeHex();
              }}
            >
              {t("settings.Capture.copyClose")}
            </Button>
          </>
        }
      >
        {shownHex && (
          <Stack spacing={2}>
            <B4Alert icon={<SuccessIcon />}>
              {t("settings.Capture.hexCaption", {
                protocol: shownHex.target.protocol.toUpperCase(),
                name: shownHex.target.name,
                size: bytesLabel(t, shownHex.target.size),
              })}
            </B4Alert>
            {shownHex.error && (
              <B4Alert severity="error">
                {t("settings.Capture.hexFailed", { error: shownHex.error })}
              </B4Alert>
            )}
            {!shownHex.error && shownHex.hex === null && (
              <Box sx={{ display: "flex", justifyContent: "center", p: 2 }}>
                <CircularProgress size={24} />
              </Box>
            )}
            {shownHex.hex !== null && (
              <Box
                sx={{
                  p: 2,
                  bgcolor: colors.background.dark,
                  borderRadius: radius.sm,
                  fontFamily: "monospace",
                  fontSize: "0.8rem",
                  wordBreak: "break-all",
                  maxHeight: 400,
                  overflow: "auto",
                  userSelect: "all",
                }}
              >
                {shownHex.hex}
              </Box>
            )}
          </Stack>
        )}
      </B4Dialog>
    </Stack>
  );
};
