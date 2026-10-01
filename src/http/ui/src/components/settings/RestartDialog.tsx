import { useState } from "react";
import {
  Button,
  CircularProgress,
  Stack,
  Typography,
  LinearProgress,
  Box,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import { RestartIcon, CheckIcon, ErrorIcon } from "@b4.icons";
import { useSystemRestart } from "@hooks/useSystemRestart";
import { colors } from "@design";
import { B4Alert, B4Dialog } from "@b4.elements";

interface RestartDialogProps {
  open: boolean;
  onClose: () => void;
}

type RestartState = "confirm" | "restarting" | "waiting" | "success" | "error";

export const RestartDialog = ({ open, onClose }: RestartDialogProps) => {
  const [state, setState] = useState<RestartState>("confirm");
  const [message, setMessage] = useState("");
  const { restart, waitForReconnection, error } = useSystemRestart();
  const { t } = useTranslation();

  const handleRestart = async () => {
    setState("restarting");
    setMessage(t("settings.RestartDialog.initiating"));

    const response = await restart();

    if (response?.success) {
      setState("waiting");
      setMessage(t("settings.RestartDialog.restarting"));

      const reconnected = await waitForReconnection(30);

      if (reconnected) {
        setState("success");
        setMessage(t("settings.RestartDialog.success"));
        setTimeout(() => globalThis.window.location.reload(), 5000);
      } else {
        setState("error");
        setMessage(t("settings.RestartDialog.timeout"));
      }
    } else {
      setState("error");
      setMessage(error || "Failed to restart service");
    }
  };

  const handleClose = () => {
    if (state !== "restarting" && state !== "waiting") {
      setState("confirm");
      setMessage("");
      onClose();
    }
  };

  // Dynamic dialog props based on state
  const defaultDialogProps = {
    title: t("settings.RestartDialog.title"),
    subtitle: t("settings.RestartDialog.subtitle"),
    icon: <RestartIcon />,
  };

  const getDialogProps = () => {
    switch (state) {
      case "confirm":
        return {
          ...defaultDialogProps,
        };
      case "restarting":
      case "waiting":
        return {
          ...defaultDialogProps,
          title: t("settings.RestartDialog.restartingTitle"),
          subtitle: t("settings.RestartDialog.pleaseWait"),
        };
      case "success":
        return {
          ...defaultDialogProps,
          title: t("settings.RestartDialog.successTitle"),
          subtitle: t("settings.RestartDialog.successSubtitle"),
        };
      case "error":
        return {
          ...defaultDialogProps,
          title: t("settings.RestartDialog.failedTitle"),
          subtitle: t("settings.RestartDialog.failedSubtitle"),
        };
      default:
        return {
          ...defaultDialogProps,
        };
    }
  };

  // Content for each state
  const renderContent = () => {
    switch (state) {
      case "confirm":
        return (
          <B4Alert>
            <Typography variant="body2" sx={{ mb: 1 }}>
              {t("settings.RestartDialog.warning")}
            </Typography>
            <Typography variant="caption" sx={{ color: colors.text.secondary }}>
              {t("settings.RestartDialog.downtime")}
            </Typography>
          </B4Alert>
        );

      case "restarting":
      case "waiting":
        return (
          <Stack
            spacing={3}
            sx={{
              alignItems: "center",
              py: 4
            }}>
            <Box
              sx={{
                p: 2,
                borderRadius: 3,
                bgcolor: colors.accent.secondary,
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
              }}
            >
              <CircularProgress size={48} sx={{ color: colors.secondary }} />
            </Box>
            <Box sx={{ textAlign: "center" }}>
              <Typography
                variant="h6"
                sx={{ color: colors.text.primary, mb: 1 }}
              >
                {message}
              </Typography>
              <Typography
                variant="caption"
                sx={{ color: colors.text.secondary }}
              >
                {t("settings.RestartDialog.doNotClose")}
              </Typography>
            </Box>
            <Box sx={{ width: "100%", px: 2 }}>
              <LinearProgress
                sx={{
                  height: 6,
                  borderRadius: 3,
                  bgcolor: colors.background.dark,
                  "& .MuiLinearProgress-bar": {
                    bgcolor: colors.secondary,
                    borderRadius: 3,
                  },
                }}
              />
            </Box>
          </Stack>
        );

      case "success":
        return (
          <Stack
            spacing={3}
            sx={{
              alignItems: "center",
              py: 4
            }}>
            <Box
              sx={{
                p: 2,
                borderRadius: 3,
                bgcolor: colors.accent.secondary,
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
              }}
            >
              <CheckIcon sx={{ fontSize: 64, color: colors.secondary }} />
            </Box>
            <Box sx={{ textAlign: "center" }}>
              <Typography
                variant="h6"
                sx={{ color: colors.text.primary, mb: 1 }}
              >
                {message}
              </Typography>
              <Typography variant="body2" sx={{ color: colors.text.secondary }}>
                {t("settings.RestartDialog.reloading")}
              </Typography>
            </Box>
          </Stack>
        );

      case "error":
        return (
          <Stack
            spacing={3}
            sx={{
              alignItems: "center",
              py: 4
            }}>
            <Box
              sx={{
                p: 2,
                borderRadius: 3,
                bgcolor: `${colors.quaternary}22`,
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
              }}
            >
              <ErrorIcon sx={{ fontSize: 64, color: colors.quaternary }} />
            </Box>
            <Box sx={{ textAlign: "center", width: "100%" }}>
              <Typography
                variant="h6"
                sx={{ color: colors.text.primary, mb: 2 }}
              >
                {t("settings.RestartDialog.failedTitle")}
              </Typography>
              <B4Alert severity="error">{message}</B4Alert>
            </Box>
          </Stack>
        );
    }
  };

  // Actions for each state
  const renderActions = () => {
    switch (state) {
      case "confirm":
        return (
          <>
            <Button onClick={handleClose}>{t("core.cancel")}</Button>
            <Box sx={{ flex: 1 }} />
            <Button
              onClick={() => {
                void handleRestart();
              }}
              variant="contained"
              startIcon={<RestartIcon />}
            >
              {t("settings.RestartDialog.restartButton")}
            </Button>
          </>
        );

      case "error":
        return (
          <Button
            onClick={handleClose}
            variant="contained"
            sx={{
              bgcolor: colors.secondary,
              color: colors.background.default,
              "&:hover": { bgcolor: colors.primary },
            }}
          >
            {t("core.close")}
          </Button>
        );

      default:
        return null;
    }
  };

  return (
    <B4Dialog
      {...getDialogProps()}
      open={open}
      onClose={handleClose}
      maxWidth="sm"
      fullWidth
      actions={renderActions()}
    >
      {renderContent()}
    </B4Dialog>
  );
};
