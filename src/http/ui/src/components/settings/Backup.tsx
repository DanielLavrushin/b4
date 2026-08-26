import { useState, useRef } from "react";
import { useTranslation } from "react-i18next";
import { Button, Grid, Stack, Typography, Box } from "@mui/material";
import { BackupIcon, DownloadIcon, UploadIcon } from "@b4.icons";
import { B4Section, B4Alert } from "@b4.elements";
import { useSnackbar } from "@context/SnackbarProvider";
import { RestartDialog } from "./RestartDialog";
import { colors } from "@design";
import { getAuthToken } from "@context/AuthProvider";

interface ApiError {
  error?: string;
}

export const BackupSettings = () => {
  const { t } = useTranslation();
  const { showError, showSuccess } = useSnackbar();
  const [downloading, setDownloading] = useState(false);
  const [restoring, setRestoring] = useState(false);
  const [showRestartDialog, setShowRestartDialog] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const handleDownload = async () => {
    try {
      setDownloading(true);

      const headers: Record<string, string> = {};
      const token = getAuthToken();
      if (token) {
        headers["Authorization"] = `Bearer ${token}`;
      }

      const response = await fetch("/api/backup", { headers });
      if (!response.ok) {
        const data = (await response.json().catch(() => ({}))) as ApiError;
        throw new Error(
          data.error ?? `Download failed: ${response.statusText}`,
        );
      }

      const blob = await response.blob();
      const disposition = response.headers.get("Content-Disposition");
      const filenameMatch = disposition?.match(/filename="(.+)"/);
      const filename = filenameMatch?.[1] ?? "b4-backup.tar.gz";

      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = filename;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);

      showSuccess(t("settings.Backup.downloadSuccess"));
    } catch (error) {
      showError(
        error instanceof Error
          ? error.message
          : t("settings.Backup.downloadFailed"),
      );
    } finally {
      setDownloading(false);
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
        const data = (await response.json().catch(() => ({}))) as ApiError;
        throw new Error(data.error ?? `Restore failed: ${response.statusText}`);
      }

      showSuccess(t("settings.Backup.restoreSuccess"));
      setShowRestartDialog(true);
    } catch (error) {
      showError(
        error instanceof Error
          ? error.message
          : t("settings.Backup.restoreFailed"),
      );
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
      </Grid>

      <RestartDialog
        open={showRestartDialog}
        onClose={() => setShowRestartDialog(false)}
      />
    </Stack>
  );
};
