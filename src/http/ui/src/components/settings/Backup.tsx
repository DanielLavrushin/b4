import { useState, useRef } from "react";
import { useTranslation } from "react-i18next";
import {
  Button,
  DialogContent,
  DialogContentText,
  Grid,
  Stack,
  Typography,
  Box,
} from "@mui/material";
import {
  BackupIcon,
  DescriptionIcon,
  DownloadIcon,
  RestoreIcon,
  UploadIcon,
} from "@b4.icons";
import { B4Section, B4Alert, B4Dialog } from "@b4.elements";
import { configApi } from "@api/settings";
import { useSnackbar } from "@context/SnackbarProvider";
import { RestartDialog } from "./RestartDialog";
import { colors } from "@design";
import { getAuthToken } from "@context/AuthProvider";
import { ApiError } from "@api/apiClient";

interface ErrorBody {
  error?: string;
}

type ConfigDownload = "as-is" | "safe";

const saveAttachment = async (path: string, fallbackName: string) => {
  const headers: Record<string, string> = {};
  const token = getAuthToken();
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  const response = await fetch(path, { headers });
  if (!response.ok) {
    const data = (await response.json().catch(() => ({}))) as ErrorBody;
    throw new Error(data.error ?? response.statusText);
  }

  const blob = await response.blob();
  const disposition = response.headers.get("Content-Disposition");
  const filenameMatch = disposition?.match(/filename="(.+)"/);
  const filename = filenameMatch?.[1] ?? fallbackName;

  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
};

const failureDetail = (error: unknown): string => {
  if (error instanceof ApiError && error.body && typeof error.body === "object") {
    const detail = (error.body as Record<string, unknown>).error;
    if (typeof detail === "string" && detail) return detail;
  }
  return error instanceof Error ? error.message : "";
};

export const BackupSettings = () => {
  const { t } = useTranslation();
  const { showError, showSuccess } = useSnackbar();
  const [downloading, setDownloading] = useState(false);
  const [downloadingConfig, setDownloadingConfig] =
    useState<ConfigDownload | null>(null);
  const [restoring, setRestoring] = useState(false);
  const [showRestartDialog, setShowRestartDialog] = useState(false);
  const [showResetDialog, setShowResetDialog] = useState(false);
  const [resetting, setResetting] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const showFailure = (key: string, error: unknown) => {
    const detail = failureDetail(error);
    showError(detail ? `${t(key)}: ${detail}` : t(key));
  };

  const handleResetConfirm = async () => {
    try {
      setResetting(true);
      await configApi.reset();
      showSuccess(t("settings.Control.resetSuccess"));
      setTimeout(() => globalThis.window.location.reload(), 800);
    } catch (error) {
      showFailure("settings.Control.resetError", error);
      setResetting(false);
      setShowResetDialog(false);
    }
  };

  const handleDownload = async () => {
    try {
      setDownloading(true);
      await saveAttachment("/api/backup", "b4-backup.tar.gz");
      showSuccess(t("settings.Backup.downloadSuccess"));
    } catch (error) {
      showFailure("settings.Backup.downloadFailed", error);
    } finally {
      setDownloading(false);
    }
  };

  const handleConfigDownload = async (kind: ConfigDownload) => {
    const safe = kind === "safe";
    try {
      setDownloadingConfig(kind);
      await saveAttachment(
        safe ? "/api/config/download?safe=true" : "/api/config/download",
        safe ? "b4-config-safe.json" : "b4-config.json",
      );
      showSuccess(t("settings.Backup.configDownloadSuccess"));
    } catch (error) {
      showFailure("settings.Backup.configDownloadFailed", error);
    } finally {
      setDownloadingConfig(null);
    }
  };

  const handleRestore = async (file: File) => {
    try {
      setRestoring(true);

      const formData = new FormData();
      formData.append("file", file);

      const headers: Record<string, string> = {};
      const token = getAuthToken();
      if (token) {
        headers["Authorization"] = `Bearer ${token}`;
      }

      const response = await fetch("/api/backup/restore", {
        method: "POST",
        headers,
        body: formData,
      });

      if (!response.ok) {
        const data = (await response.json().catch(() => ({}))) as ErrorBody;
        throw new Error(data.error ?? response.statusText);
      }

      showSuccess(t("settings.Backup.restoreSuccess"));
      setShowRestartDialog(true);
    } catch (error) {
      showFailure("settings.Backup.restoreFailed", error);
    } finally {
      setRestoring(false);
      if (fileInputRef.current) {
        fileInputRef.current.value = "";
      }
    }
  };

  const handleFileSelect = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (file) {
      void handleRestore(file);
    }
  };

  return (
    <Stack spacing={3}>
      <B4Alert icon={<BackupIcon />}>{t("settings.Backup.alert")}</B4Alert>

      <Grid container spacing={2}>
        <Grid size={{ xs: 12, md: 6 }}>
          <B4Section
            title={t("settings.Backup.downloadTitle")}
            description={t("settings.Backup.downloadDescription")}
            icon={<DownloadIcon />}
          >
            <Stack spacing={2}>
              <Typography variant="body2" sx={{ color: colors.text.secondary }}>
                {t("settings.Backup.downloadInfo")}
              </Typography>
              <Box>
                <Button
                  variant="contained"
                  startIcon={<DownloadIcon />}
                  onClick={() => {
                    void handleDownload();
                  }}
                  disabled={downloading}
                >
                  {downloading
                    ? t("settings.Backup.generating")
                    : t("settings.Backup.downloadButton")}
                </Button>
              </Box>
            </Stack>
          </B4Section>
        </Grid>

        <Grid size={{ xs: 12, md: 6 }}>
          <B4Section
            title={t("settings.Backup.restoreTitle")}
            description={t("settings.Backup.restoreDescription")}
            icon={<UploadIcon />}
          >
            <Stack spacing={2}>
              <Typography variant="body2" sx={{ color: colors.text.secondary }}>
                {t("settings.Backup.restoreInfo")}
              </Typography>
              <Box>
                <input
                  ref={fileInputRef}
                  type="file"
                  accept=".gz,.tgz,application/gzip,application/x-gzip"
                  style={{ display: "none" }}
                  onChange={handleFileSelect}
                />
                <Button
                  variant="outlined"
                  startIcon={<UploadIcon />}
                  onClick={() => fileInputRef.current?.click()}
                  disabled={restoring}
                >
                  {restoring
                    ? t("settings.Backup.restoring")
                    : t("settings.Backup.restoreButton")}
                </Button>
              </Box>
            </Stack>
          </B4Section>
        </Grid>

        <Grid size={{ xs: 12, md: 6 }}>
          <B4Section
            title={t("settings.Backup.configTitle")}
            description={t("settings.Backup.configDescription")}
            icon={<DescriptionIcon />}
          >
            <Stack spacing={2}>
              <Typography variant="body2" sx={{ color: colors.text.secondary }}>
                {t("settings.Backup.configInfo")}
              </Typography>
              <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1 }}>
                <Button
                  variant="contained"
                  startIcon={<DownloadIcon />}
                  onClick={() => {
                    void handleConfigDownload("safe");
                  }}
                  disabled={downloadingConfig !== null}
                >
                  {downloadingConfig === "safe"
                    ? t("settings.Backup.generating")
                    : t("settings.Backup.configDownloadSafe")}
                </Button>
                <Button
                  variant="outlined"
                  startIcon={<DownloadIcon />}
                  onClick={() => {
                    void handleConfigDownload("as-is");
                  }}
                  disabled={downloadingConfig !== null}
                >
                  {downloadingConfig === "as-is"
                    ? t("settings.Backup.generating")
                    : t("settings.Backup.configDownload")}
                </Button>
              </Box>
            </Stack>
          </B4Section>
        </Grid>

        <Grid size={{ xs: 12, md: 6 }}>
          <B4Section
            title={t("settings.Control.resetConfig")}
            icon={<RestoreIcon />}
          >
            <Stack spacing={2}>
              <Typography variant="body2" sx={{ color: colors.text.secondary }}>
                {t("settings.Control.resetConfirm")}
              </Typography>
              <Box>
                <Button
                  variant="outlined"
                  color="warning"
                  startIcon={<RestoreIcon />}
                  onClick={() => setShowResetDialog(true)}
                >
                  {t("settings.Control.resetButton")}
                </Button>
              </Box>
            </Stack>
          </B4Section>
        </Grid>
      </Grid>

      <RestartDialog
        open={showRestartDialog}
        onClose={() => setShowRestartDialog(false)}
      />
      <B4Dialog
        title={t("settings.Control.resetConfig")}
        open={showResetDialog}
        onClose={() => !resetting && setShowResetDialog(false)}
        actions={
          <>
            <Button
              onClick={() => setShowResetDialog(false)}
              disabled={resetting}
            >
              {t("core.cancel")}
            </Button>
            <Button
              onClick={() => {
                void handleResetConfirm();
              }}
              variant="contained"
              color="warning"
              disabled={resetting}
            >
              {resetting
                ? t("core.saving")
                : t("settings.Control.resetButton")}
            </Button>
          </>
        }
      >
        <DialogContent>
          <DialogContentText>
            {t("settings.Control.resetConfirm")}
          </DialogContentText>
        </DialogContent>
      </B4Dialog>
    </Stack>
  );
};
